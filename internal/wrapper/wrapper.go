// Package wrapper renders the POSIX-sh launcher aprofile writes onto PATH for each
// profile. The wrapper is self-contained (POSIX shell + the harness binary only),
// exec-style so process detection sees the real harness, sets the profile's
// config_env and APROFILE_* contract, resolves secret references at launch
// fail-closed, shell-quotes every other value so nothing can inject shell code, and
// passes all arguments through (FR-026, FR-028a/b). Since feature 004 it is the
// single source only for settings the profile folder cannot carry: it clears every
// launch variable a profile session may have left, never exports a setting the
// folder carries, and sets wrapper-only settings unconditionally (spec 004 FR-012 to
// FR-016). One-run changes go through the harness's own flags.
package wrapper

import (
	"fmt"
	"strings"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/launch"
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
// inject shell code (FR-028b). It is an alias of secret.ShellQuote.
func ShellQuote(s string) string { return secret.ShellQuote(s) }

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

	m := newModel(p, def, configDir, opts.HookEnv)

	renderCapture(w, m)
	renderCleanup(w, m)
	renderConfigEnv(w, def, configDir)
	renderContract(w, p, configDir)
	renderAuth(w, p, def, m)
	renderHerdr(w, p)
	renderLaunch(w, p, m)
	renderGuardAndExec(w, p, def, configDir, m.modelArgs)

	return b.String(), nil
}

// renderCapture keeps the caller's value of every variable an env: reference reads
// in a private temporary, before any clearing, so a self-referencing pass-through
// (--env CLOUD_ML_REGION=env:CLOUD_ML_REGION) or a key read from an auth variable
// survives the clears below (spec 004 contracts/registry-and-env.md step 0).
func renderCapture(w func(string), m model) {
	if len(m.sources) == 0 {
		return
	}
	w("# Capture the sources of env: references before anything is cleared.")
	for _, src := range m.sources {
		w(fmt.Sprintf(`%s="${%s-}"`, srcTemp(src), src))
	}
	w("")
}

// renderCleanup clears, unconditionally and without comparing profile names, the
// launch variables a profile session may have left: every valid name in an inherited
// APROFILE_SET_VARS (never m.protected or the wrapper's own temporaries), the
// definition's launch_clear names and this profile's own launch variables (spec 004
// FR-014). Environ applies the same rule through protectedFromInherited.
func renderCleanup(w func(string), m model) {
	w("# Clear the launch variables a profile session may have left (no caller overrides).")
	w(`_aprofile_prev_setvars="${APROFILE_SET_VARS:-}"`)
	w("set -f")
	w("for _v in ${_aprofile_prev_setvars}; do")
	protected := append(append([]string(nil), m.protected...), internalPrefix+"*")
	w(`  case "${_v}" in`)
	w("    " + strings.Join(protected, "|") + ") ;;")
	w(`    ''|[0-9]*|*[!A-Za-z0-9_]*) ;;`)
	w(`    *) command unset "${_v}" 2>/dev/null || : ;;`)
	w("  esac")
	w("done")
	w("set +f")
	w("unset _v _aprofile_prev_setvars")
	w("unset " + strings.Join(m.clear, " "))
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
// FR-028a). A key the profile folder carries is only checked: it is resolved into a
// private variable with the same failure message, never exported, then unset
// (spec 004 FR-015). env: references read the temporaries renderCapture kept.
func renderAuth(w func(string), p *profile.Profile, def *harness.Definition, m model) {
	all := def.UnsetAuthVars()
	if len(all) == 0 && len(m.keys) == 0 {
		return
	}
	w("# Authentication: clear all auth vars and provider switches, then set this profile's.")
	if len(all) > 0 {
		w("unset " + strings.Join(all, " "))
	}
	for _, k := range m.keys {
		ref := m.readRef(*k.Ref)
		if k.Carrier == launch.CarrierFolder {
			w("# The profile folder carries this key: check it resolves, never export it.")
			w(ref.CheckShell(keyCheckVar, p.ID(), ShellQuote, ""))
			w("unset " + keyCheckVar)
			continue
		}
		w(ref.ResolveShell(k.Variable, p.ID(), ShellQuote, ""))
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

// renderLaunch sets the launch settings the wrapper carries, unconditionally (a
// caller's value never overrides them, spec 004 FR-013), exports APROFILE_SET_VARS
// when the folder does not carry it (FR-014), and drops the capture temporaries.
func renderLaunch(w func(string), p *profile.Profile, m model) {
	w("# Launch settings the wrapper carries (always the profile's values).")
	for _, v := range m.vars {
		if v.Ref == nil {
			w(fmt.Sprintf("%s=%s", v.Variable, ShellQuote(v.Value)))
			w("export " + v.Variable)
			continue
		}
		w(m.readRef(*v.Ref).ResolveShell(v.Variable, p.ID(), ShellQuote, ""))
	}
	if m.setVarsWrapper {
		w("APROFILE_SET_VARS=" + ShellQuote(strings.Join(m.setVars, " ")))
		w("export APROFILE_SET_VARS")
	}
	if len(m.sources) > 0 {
		temps := make([]string, len(m.sources))
		for i, s := range m.sources {
			temps[i] = srcTemp(s)
		}
		w("unset " + strings.Join(temps, " "))
	}
	w("")
}

// renderGuardAndExec checks the config dir exists and the binary is available,
// then execs the harness (FR-026), passing the definition's model_arg before the
// caller's arguments so a caller's own model flag wins (spec 004 FR-016).
func renderGuardAndExec(w func(string), p *profile.Profile, def *harness.Definition, configDir string, modelArgs []string) {
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
	argv := []string{ShellQuote(def.Binary)}
	for _, a := range modelArgs {
		argv = append(argv, ShellQuote(a))
	}
	w(fmt.Sprintf(`exec %s "$@"`, strings.Join(argv, " ")))
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
