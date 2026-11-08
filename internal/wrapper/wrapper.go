// Package wrapper renders the POSIX-sh launcher aprofile writes onto PATH for each
// profile. The wrapper is self-contained (POSIX shell + the harness binary only),
// exec-style so process detection sees the real harness, sets the profile's
// config_env and APROFILE_* contract (never overridable), resolves secret
// references at launch fail-closed, shell-quotes every other value so nothing can
// inject shell code, and passes all arguments through (FR-026, FR-027, FR-028a/b).
package wrapper

import (
	"fmt"
	"strings"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/secret"
)

// Marker identifies an aprofile-generated wrapper. sync removes only files that
// carry it (FR-023).
const Marker = "# aprofile:wrapper"

// IsWrapper reports whether file content is an aprofile-generated wrapper.
func IsWrapper(content []byte) bool {
	return strings.Contains(string(content), Marker)
}

// ShellQuote renders s as a single POSIX-sh single-quoted literal, so no value can
// inject shell code (FR-028b).
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// RenderOptions carries values resolved outside the wrapper package and woven into
// the script. HookEnv is the output of the harness exec hook's env operation,
// already prefix-checked (FR-011); it is rendered like a profile's extra variables.
type RenderOptions struct {
	HookEnv []harness.EnvVar
}

// RenderWith produces the wrapper script for one profile, weaving in the exec-hook
// env output (FR-011). configDir is the absolute profile config dir (the profile
// root). With empty opts (a definition that declares no exec hook) the wrapper
// carries nothing from a hook, keeping it self-contained (FR-026).
func RenderWith(p *profile.Profile, def *harness.Definition, configDir string, opts RenderOptions) (string, error) {
	if def == nil {
		return "", fmt.Errorf("render wrapper for %s: nil harness definition", p.ID())
	}
	var b strings.Builder
	w := func(s string) { b.WriteString(s + "\n") }

	w("#!/bin/sh")
	w(fmt.Sprintf("%s harness=%s profile=%s", Marker, p.Harness, p.Name))
	w("# Managed by aprofile. Do not edit; regenerate with: aprofile sync")
	w("")

	vars := overridableVars(p, def, opts.HookEnv)
	captured := clearedSources(p, def, vars)

	renderNestedCleanup(w, p, def)
	renderConfigEnv(w, def, configDir)
	renderContract(w, p, configDir)
	renderAuth(w, p, def, captured)
	renderHerdr(w, p)
	renderOverridable(w, p, vars, captured)
	renderGuardAndExec(w, p, def, configDir)

	return b.String(), nil
}

// renderNestedCleanup clears an inherited profile's overridable variables when the
// wrapper is launched inside a different profile's session (FR-027).
func renderNestedCleanup(w func(string), p *profile.Profile, def *harness.Definition) {
	w("# Capture an inherited aprofile session before we overwrite it.")
	w(`_aprofile_prev_name="${APROFILE_NAME:-}"`)
	w(`_aprofile_prev_harness="${APROFILE_HARNESS:-}"`)
	w(`_aprofile_prev_setvars="${APROFILE_SET_VARS:-}"`)
	w("# If launched inside a different profile's session, drop its overridable vars.")
	cond := fmt.Sprintf(`if [ -n "${_aprofile_prev_name}${_aprofile_prev_harness}" ] && { [ "${_aprofile_prev_name}" != %s ] || [ "${_aprofile_prev_harness}" != %s ]; }; then`,
		ShellQuote(p.Name), ShellQuote(p.Harness))
	w(cond)
	w("  for _v in ${_aprofile_prev_setvars}; do")
	w(`    unset "${_v}"`)
	w("  done")
	clear := []string{}
	if def.ModelVar != "" {
		clear = append(clear, def.ModelVar)
	}
	if def.EndpointVar != "" {
		clear = append(clear, def.EndpointVar)
	}
	clear = append(clear, "APROFILE_SET_VARS")
	w("  unset " + strings.Join(clear, " "))
	w("fi")
	w("")
}

// renderConfigEnv sets each config_env redirect to its absolute path (FR-020).
func renderConfigEnv(w func(string), def *harness.Definition, configDir string) {
	w("# Config dir redirect (never overridable).")
	for _, e := range def.ConfigEnv {
		abs := harness.ConfigEnvPath(configDir, e.Path)
		w(fmt.Sprintf("%s=%s", e.Var, ShellQuote(abs)))
		w("export " + e.Var)
	}
	w("")
}

// renderContract sets the APROFILE_* contract variables (FR-026).
func renderContract(w func(string), p *profile.Profile, configDir string) {
	w("# aprofile contract (never overridable).")
	set := func(name, val string) { w(fmt.Sprintf("%s=%s", name, ShellQuote(val))) }
	set("APROFILE_HARNESS", p.Harness)
	set("APROFILE_NAME", p.Name)
	set("APROFILE_LABEL", p.DisplayLabel())
	set("APROFILE_COLOR", p.Color)
	set("APROFILE_ICON", p.Icon)
	set("APROFILE_CONFIG_DIR", configDir)
	set("APROFILE_SHARE", string(p.ShareLevel()))
	set("APROFILE_HISTORY_GROUP", p.HistoryGroup)
	w("export APROFILE_HARNESS APROFILE_NAME APROFILE_LABEL APROFILE_COLOR APROFILE_ICON APROFILE_CONFIG_DIR APROFILE_SHARE APROFILE_HISTORY_GROUP")
	w("")
}

// renderAuth unsets every auth variable and every auth_clears variable, then sets
// the selected kind's variables from their references, fail-closed (FR-027,
// FR-028a). Auth variables are never caller-overridable, and a provider switch the
// definition clears never leaks in from the caller's shell. captured names the
// cleared variables an overridable reference reads (see clearedSources): their
// caller values are kept in temporaries before the unset.
func renderAuth(w func(string), p *profile.Profile, def *harness.Definition, captured []string) {
	all := def.UnsetAuthVars()
	if len(all) == 0 {
		return
	}
	w("# Authentication (never overridable): capture references, clear all auth vars, then set this profile's.")
	// Collect the selected kind's references first. An env reference that reads from an
	// auth variable (for example env:ANTHROPIC_AUTH_TOKEN) must be captured into a temp
	// BEFORE the unset, or the unset would blank the value before it is resolved (FR-028a).
	refs := selectedAuthRefs(p, def)
	for _, ar := range refs {
		if ar.ref.Kind == secret.KindEnv {
			// Capture the caller's value (empty when unset); resolution checks it below.
			w(fmt.Sprintf(`%s="${%s-}"`, authTemp(ar.target), ar.ref.Value))
		}
	}
	for _, v := range captured {
		w(fmt.Sprintf(`%s="${%s-}"`, clearedTemp(v), v))
	}
	w("unset " + strings.Join(all, " "))
	for _, ar := range refs {
		if ar.ref.Kind == secret.KindEnv {
			// Resolve from the temp, keeping the original Raw in the fail-closed message.
			tmp := authTemp(ar.target)
			tmpRef := secret.Reference{Kind: secret.KindEnv, Value: tmp, Raw: ar.ref.Raw}
			w(tmpRef.ResolveShell(ar.target, p.ID(), ShellQuote, ""))
			w("unset " + tmp)
		} else {
			w(ar.ref.ResolveShell(ar.target, p.ID(), ShellQuote, ""))
		}
	}
	w("")
}

// renderHerdr sets HERDR_AGENT only when the profile declares it (FR-029).
func renderHerdr(w func(string), p *profile.Profile) {
	if p.HerdrAgent == "" {
		return
	}
	w("# HERDR_AGENT hint (only when declared).")
	w("HERDR_AGENT=" + ShellQuote(p.HerdrAgent))
	w("export HERDR_AGENT")
	w("")
}

// renderOverridable sets model, endpoint, extra variables and any exec-hook env
// variables only when the caller has not already set them, and exports
// APROFILE_SET_VARS listing their names (FR-027, FR-011). A reference to a cleared
// variable reads the temporary renderAuth captured before the unset.
func renderOverridable(w func(string), p *profile.Profile, vars []overridableVar, captured []string) {
	var setVars []string
	w("# Model, endpoint and extra variables (caller-overridable).")
	for _, v := range vars {
		w(fmt.Sprintf(`if [ -z "${%s:-}" ]; then`, v.name))
		switch {
		case v.ref == nil:
			w(fmt.Sprintf("  %s=%s", v.name, ShellQuote(v.value)))
		case v.ref.Kind == secret.KindEnv && contains(captured, v.ref.Value):
			tmpRef := secret.Reference{Kind: secret.KindEnv, Value: clearedTemp(v.ref.Value), Raw: v.ref.Raw}
			w(tmpRef.ResolveShell(v.name, p.ID(), ShellQuote, "  "))
		default:
			w(v.ref.ResolveShell(v.name, p.ID(), ShellQuote, "  "))
		}
		w("fi")
		w("export " + v.name)
		setVars = append(setVars, v.name)
	}
	if len(captured) > 0 {
		temps := make([]string, len(captured))
		for i, c := range captured {
			temps[i] = clearedTemp(c)
		}
		w("unset " + strings.Join(temps, " "))
	}

	w("APROFILE_SET_VARS=" + ShellQuote(strings.Join(setVars, " ")))
	w("export APROFILE_SET_VARS")
	w("")
}

// renderGuardAndExec checks the config dir exists and the binary is available,
// then execs the harness (FR-026).
func renderGuardAndExec(w func(string), p *profile.Profile, def *harness.Definition, configDir string) {
	w("# Ensure the profile config dir exists before launching.")
	w(fmt.Sprintf("if [ ! -d %s ]; then", ShellQuote(configDir)))
	w(fmt.Sprintf("  printf '%%s\\n' %s >&2", ShellQuote(fmt.Sprintf("aprofile: profile %s: config dir %s does not exist; run: aprofile sync", p.ID(), configDir))))
	w("  exit 1")
	w("fi")
	w("")
	w("# Ensure the harness binary is available.")
	w(fmt.Sprintf("if ! command -v %s >/dev/null 2>&1; then", ShellQuote(def.Binary)))
	w(fmt.Sprintf("  printf '%%s\\n' %s >&2", ShellQuote(fmt.Sprintf("aprofile: harness binary '%s' not found on PATH", def.Binary))))
	w("  exit 127")
	w("fi")
	w("")
	renderPreLaunch(w, p, def, configDir)
	w(fmt.Sprintf(`exec %s "$@"`, ShellQuote(def.Binary)))
}

// renderPreLaunch emits the pre-launch exec-hook call, run inside the wrapper just
// before exec, only when the definition declares a pre-launch operation (FR-011);
// otherwise the wrapper stays self-contained (FR-026). The hook has no timeout here
// (it runs in the shell). Exit 64 means "unsupported" and is a no-op; any other
// non-zero exit stops the wrapper before the harness starts. A declared hook whose
// executable is not on PATH also stops the wrapper (exit 127, T076): the definition
// says the harness needs the hook before it starts, so launching without it would
// silently skip that step.
func renderPreLaunch(w func(string), p *profile.Profile, def *harness.Definition, configDir string) {
	if !def.HookDeclares("pre-launch") {
		return
	}
	bin := harness.HookExecutable(p.Harness)
	w("# Harness pre-launch hook (exit 64 = unsupported, treated as a no-op; FR-011).")
	w(fmt.Sprintf("if ! command -v %s >/dev/null 2>&1; then", ShellQuote(bin)))
	w(fmt.Sprintf("  printf '%%s\\n' %s >&2", ShellQuote(fmt.Sprintf("aprofile: profile %s: exec hook %s not found on PATH; the harness definition declares its pre-launch operation", p.ID(), bin))))
	w("  exit 127")
	w("fi")
	w(fmt.Sprintf("printf '%%s\\n' %s %s %s | %s pre-launch",
		ShellQuote("harness="+p.Harness),
		ShellQuote("profile="+p.Name),
		ShellQuote("config_dir="+configDir),
		ShellQuote(bin)))
	w("_aprofile_hook_status=$?")
	w(`if [ "${_aprofile_hook_status}" -ne 0 ] && [ "${_aprofile_hook_status}" -ne 64 ]; then`)
	w(fmt.Sprintf("  printf '%%s\\n' %s >&2", ShellQuote(fmt.Sprintf("aprofile: pre-launch hook for %s failed", p.ID()))))
	w(`  exit "${_aprofile_hook_status}"`)
	w("fi")
	w("")
}
