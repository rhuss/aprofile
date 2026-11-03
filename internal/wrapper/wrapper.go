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

// Render produces the wrapper script for one profile with no exec-hook env output.
// configDir is the absolute profile config dir (the profile root).
func Render(p *profile.Profile, def *harness.Definition, configDir string) (string, error) {
	return RenderWith(p, def, configDir, RenderOptions{})
}

// RenderWith produces the wrapper script for one profile, weaving in the exec-hook
// env output (FR-011). A profile whose definition declares no exec hook renders
// byte-for-byte as Render, keeping wrappers self-contained (FR-026).
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

	renderNestedCleanup(w, p, def)
	renderConfigEnv(w, def, configDir)
	renderContract(w, p, configDir)
	renderAuth(w, p, def)
	renderHerdr(w, p)
	renderOverridable(w, p, def, opts.HookEnv)
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
	w("export APROFILE_HARNESS APROFILE_NAME APROFILE_LABEL APROFILE_COLOR APROFILE_ICON APROFILE_CONFIG_DIR")
	w("")
}

// renderAuth unsets every auth variable, then sets the selected kind's variables
// from their references, fail-closed (FR-027, FR-028a). Auth variables are never
// caller-overridable.
func renderAuth(w func(string), p *profile.Profile, def *harness.Definition) {
	all := def.AllAuthVars()
	if len(all) == 0 {
		return
	}
	w("# Authentication (never overridable): capture references, clear all auth vars, then set this profile's.")
	// Collect the selected kind's references first. An env reference that reads from an
	// auth variable (for example env:ANTHROPIC_AUTH_TOKEN) must be captured into a temp
	// BEFORE the unset, or the unset would blank the value before it is resolved (FR-028a).
	type authRef struct {
		target string
		tmp    string // non-empty for an env reference captured before the unset
		ref    secret.Reference
	}
	var refs []authRef
	if k, ok := def.AuthKind(p.AuthKind); ok {
		for _, v := range k.VarNames() {
			ref, has := p.AuthRefs[v]
			if !has {
				continue // left unset; credentials come from files/login
			}
			ar := authRef{target: v, ref: ref}
			if ref.Kind == secret.KindEnv {
				ar.tmp = "_aprofile_authsrc_" + v
				// Capture the caller's value (empty when unset); resolution checks it below.
				w(fmt.Sprintf(`%s="${%s-}"`, ar.tmp, ref.Value))
			}
			refs = append(refs, ar)
		}
	}
	w("unset " + strings.Join(all, " "))
	for _, ar := range refs {
		if ar.tmp != "" {
			// Resolve from the temp, keeping the original Raw in the fail-closed message.
			tmpRef := secret.Reference{Kind: secret.KindEnv, Value: ar.tmp, Raw: ar.ref.Raw}
			w(tmpRef.ResolveShell(ar.target, p.ID(), ShellQuote, ""))
			w("unset " + ar.tmp)
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
// APROFILE_SET_VARS listing their names (FR-027, FR-011).
func renderOverridable(w func(string), p *profile.Profile, def *harness.Definition, hookEnv []harness.EnvVar) {
	var setVars []string
	w("# Model, endpoint and extra variables (caller-overridable).")

	overridableLiteral := func(name, val string) {
		w(fmt.Sprintf(`if [ -z "${%s:-}" ]; then`, name))
		w(fmt.Sprintf("  %s=%s", name, ShellQuote(val)))
		w("fi")
		w("export " + name)
		setVars = append(setVars, name)
	}
	// overridableRef renders a reference resolved fail-closed at launch; it returns
	// false when the value is not a well-formed reference so the caller falls back to
	// a shell-quoted literal.
	overridableRef := func(name, rawRef string) bool {
		ref, err := secret.ParseReference(rawRef)
		if err != nil {
			return false
		}
		w(fmt.Sprintf(`if [ -z "${%s:-}" ]; then`, name))
		w(ref.ResolveShell(name, p.ID(), ShellQuote, "  "))
		w("fi")
		w("export " + name)
		setVars = append(setVars, name)
		return true
	}

	if def.ModelVar != "" && p.Model != "" {
		overridableLiteral(def.ModelVar, p.Model)
	}
	if def.EndpointVar != "" && p.Endpoint != "" {
		overridableLiteral(def.EndpointVar, p.Endpoint)
	}
	for _, e := range p.ExtraEnv {
		if !e.Literal && secret.IsReference(e.Value) && overridableRef(e.Name, e.Value) {
			continue
		}
		overridableLiteral(e.Name, e.Value)
	}
	// Exec-hook env output (FR-011): references resolve at launch, literals are
	// shell-quoted; both are caller-overridable like a profile's extra variables.
	for _, e := range hookEnv {
		if secret.IsReference(e.Value) && overridableRef(e.Name, e.Value) {
			continue
		}
		overridableLiteral(e.Name, e.Value)
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
// non-zero exit stops the wrapper before the harness starts.
func renderPreLaunch(w func(string), p *profile.Profile, def *harness.Definition, configDir string) {
	if !def.HookDeclares("pre-launch") {
		return
	}
	bin := "aprofile-harness-" + p.Harness
	w("# Harness pre-launch hook (exit 64 = unsupported, treated as a no-op; FR-011).")
	w(fmt.Sprintf("if command -v %s >/dev/null 2>&1; then", ShellQuote(bin)))
	w(fmt.Sprintf("  printf '%%s\\n' %s %s %s | %s pre-launch",
		ShellQuote("harness="+p.Harness),
		ShellQuote("profile="+p.Name),
		ShellQuote("config_dir="+configDir),
		ShellQuote(bin)))
	w("  _aprofile_hook_status=$?")
	w(`  if [ "${_aprofile_hook_status}" -ne 0 ] && [ "${_aprofile_hook_status}" -ne 64 ]; then`)
	w(fmt.Sprintf("    printf '%%s\\n' %s >&2", ShellQuote(fmt.Sprintf("aprofile: pre-launch hook for %s failed", p.ID()))))
	w(`    exit "${_aprofile_hook_status}"`)
	w("  fi")
	w("fi")
	w("")
}
