package wrapper

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/secret"
)

// This file is the environment model the wrapper script and Environ share: which
// auth references the selected kind sets, which caller-overridable variables a
// profile has and in which order, and which cleared variables those read. RenderWith
// turns it into POSIX sh; Environ evaluates it in Go for `aprofile login` (FR-027,
// FR-039), so the two cannot drift apart.

// authRef is one variable of the selected auth kind that has a reference.
type authRef struct {
	target string
	ref    secret.Reference
}

// selectedAuthRefs returns the selected kind's variables that have a reference, in
// the kind's variable order. Variables without one stay unset (credentials come from
// files or a login).
func selectedAuthRefs(p *profile.Profile, def *harness.Definition) []authRef {
	var out []authRef
	if k, ok := def.AuthKind(p.AuthKind); ok {
		for _, v := range k.VarNames() {
			if ref, has := p.AuthRefs[v]; has {
				out = append(out, authRef{target: v, ref: ref})
			}
		}
	}
	return out
}

// overridableVar is one caller-overridable variable (FR-027, FR-011). ref is set
// when the value is a reference resolved at launch; otherwise value is a literal.
type overridableVar struct {
	name  string
	value string
	ref   *secret.Reference
}

// overridableVars lists the model and endpoint variables, the profile's extra
// variables and the exec-hook env output, in the order the wrapper sets them. A value
// that is not a well-formed reference is a literal.
func overridableVars(p *profile.Profile, def *harness.Definition, hookEnv []harness.EnvVar) []overridableVar {
	var out []overridableVar
	add := func(name, value string, mayRef bool) {
		if mayRef && secret.IsReference(value) {
			if ref, err := secret.ParseReference(value); err == nil {
				out = append(out, overridableVar{name: name, value: value, ref: &ref})
				return
			}
		}
		out = append(out, overridableVar{name: name, value: value})
	}
	if def.ModelVar != "" && p.Model != "" {
		add(def.ModelVar, p.Model, false)
	}
	if def.EndpointVar != "" && p.Endpoint != "" {
		add(def.EndpointVar, p.Endpoint, false)
	}
	for _, e := range p.ExtraEnv {
		add(e.Name, e.Value, !e.Literal)
	}
	for _, e := range hookEnv {
		add(e.Name, e.Value, true)
	}
	return out
}

// clearedSources returns, sorted, the variables the auth step clears and does not set
// again that an overridable env reference reads, such as CLOUD_ML_REGION for
// `--env CLOUD_ML_REGION=env:CLOUD_ML_REGION`. Their caller values are captured before
// the clear, as the auth references are, so passing a cleared variable through is
// possible (it is still never inherited without the profile asking for it).
func clearedSources(p *profile.Profile, def *harness.Definition, vars []overridableVar) []string {
	cleared := map[string]bool{}
	for _, v := range def.UnsetAuthVars() {
		cleared[v] = true
	}
	for _, ar := range selectedAuthRefs(p, def) {
		delete(cleared, ar.target) // set from the profile: a reference reads the profile's value
	}
	var out []string
	for _, v := range vars {
		if v.ref != nil && v.ref.Kind == secret.KindEnv && cleared[v.ref.Value] && !slices.Contains(out, v.ref.Value) {
			out = append(out, v.ref.Value)
		}
	}
	sort.Strings(out)
	return out
}

func authTemp(target string) string  { return "_aprofile_authsrc_" + target }
func clearedTemp(name string) string { return "_aprofile_clrsrc_" + name }

func contains(list []string, s string) bool { return slices.Contains(list, s) }

// Environ evaluates the wrapper's environment in Go: from the caller's environment it
// returns the environment the profile's wrapper (RenderWith with the same opts) would
// exec the harness with (FR-026, FR-027). `aprofile login` runs the login with it
// (FR-039). The steps are the wrapper's, in its order: the nested-session cleanup of a
// different profile's APROFILE_SET_VARS, the config_env redirects, the APROFILE_*
// contract, every auth and auth_clears variable removed and the selected kind's set,
// HERDR_AGENT, then the caller-overridable model, endpoint, extra and exec-hook env
// variables, listed in APROFILE_SET_VARS. References resolve fail-closed against the
// environment as it stands at that step, as in the wrapper's shell. The result is
// sorted.
func Environ(p *profile.Profile, def *harness.Definition, configDir string, opts RenderOptions, environ []string) ([]string, error) {
	env := map[string]string{}
	for _, kv := range environ {
		if i := strings.IndexByte(kv, '='); i > 0 {
			env[kv[:i]] = kv[i+1:]
		}
	}
	envValue := func(name, raw string, from map[string]string) (string, error) {
		if v := from[name]; v != "" {
			return v, nil
		}
		return "", fmt.Errorf("reference %s is unset or empty", raw)
	}
	resolve := func(ref secret.Reference) (string, error) {
		if ref.Kind == secret.KindEnv {
			return envValue(ref.Value, ref.Raw, env)
		}
		return secret.Resolve(ref)
	}

	// Nested session: launched inside a different profile's session, drop that
	// profile's overridable variables first.
	prevName, prevHarness := env["APROFILE_NAME"], env["APROFILE_HARNESS"]
	if prevName+prevHarness != "" && (prevName != p.Name || prevHarness != p.Harness) {
		for _, v := range strings.Fields(env["APROFILE_SET_VARS"]) {
			delete(env, v)
		}
		if def.ModelVar != "" {
			delete(env, def.ModelVar)
		}
		if def.EndpointVar != "" {
			delete(env, def.EndpointVar)
		}
		delete(env, "APROFILE_SET_VARS")
	}

	for _, e := range def.ConfigEnv {
		env[e.Var] = harness.ConfigEnvPath(configDir, e.Path)
	}
	env["APROFILE_HARNESS"] = p.Harness
	env["APROFILE_NAME"] = p.Name
	env["APROFILE_LABEL"] = p.DisplayLabel()
	env["APROFILE_COLOR"] = p.Color
	env["APROFILE_ICON"] = p.Icon
	env["APROFILE_CONFIG_DIR"] = configDir
	env["APROFILE_SHARE"] = string(p.ShareLevel())
	env["APROFILE_HISTORY_GROUP"] = p.HistoryGroup

	// Auth: resolve the selected kind's references and capture the cleared variables an
	// overridable reference reads, both before the clear; clear every auth and
	// auth_clears variable; then set the selected kind's.
	vars := overridableVars(p, def, opts.HookEnv)
	captured := map[string]string{}
	for _, v := range clearedSources(p, def, vars) {
		captured[v] = env[v]
	}
	auth := map[string]string{}
	for _, ar := range selectedAuthRefs(p, def) {
		val, err := resolve(ar.ref)
		if err != nil {
			return nil, fmt.Errorf("profile %s: %w", p.ID(), err)
		}
		auth[ar.target] = val
	}
	for _, v := range def.UnsetAuthVars() {
		delete(env, v)
	}
	for k, v := range auth {
		env[k] = v
	}

	if p.HerdrAgent != "" {
		env["HERDR_AGENT"] = p.HerdrAgent
	}

	// Caller-overridable variables: set only when the caller has not set them (an empty
	// value counts as unset), and listed in APROFILE_SET_VARS.
	setVars := make([]string, 0, len(vars))
	for _, v := range vars {
		setVars = append(setVars, v.name)
		if env[v.name] != "" {
			continue
		}
		val := v.value
		if v.ref != nil {
			var err error
			if _, ok := captured[v.ref.Value]; ok && v.ref.Kind == secret.KindEnv {
				val, err = envValue(v.ref.Value, v.ref.Raw, captured)
			} else {
				val, err = resolve(*v.ref)
			}
			if err != nil {
				return nil, fmt.Errorf("profile %s: variable %s: %w", p.ID(), v.name, err)
			}
		}
		env[v.name] = val
	}
	env["APROFILE_SET_VARS"] = strings.Join(setVars, " ")

	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out, nil
}

// HookContext is the exec-hook input of a profile whose config dir is configDir
// (FR-011): every operation gets the same harness, profile, config dir, auth kind,
// model and endpoint.
func HookContext(p *profile.Profile, configDir string) harness.HookContext {
	return harness.HookContext{
		Harness:   p.Harness,
		Profile:   p.Name,
		ConfigDir: configDir,
		AuthKind:  p.AuthKind,
		Model:     p.Model,
		Endpoint:  p.Endpoint,
	}
}

// Expected returns the wrapper sync writes for the profile: it calls the exec hook's
// env operation when the definition declares it and renders the script with its
// output (FR-011, FR-026). sync writes it, and list, show and doctor compare it with
// the wrapper on disk (FR-006b, FR-044). An env-hook failure is returned unchanged
// (it names the hook and the operation); a render failure is wrapped.
func Expected(p *profile.Profile, def *harness.Definition, configDir string) (string, error) {
	hookEnv, err := harness.RunEnvHook(def, HookContext(p, configDir))
	if err != nil {
		return "", err
	}
	content, err := RenderWith(p, def, configDir, RenderOptions{HookEnv: hookEnv})
	if err != nil {
		return "", fmt.Errorf("render wrapper: %w", err)
	}
	return content, nil
}
