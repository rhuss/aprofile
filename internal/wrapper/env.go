package wrapper

import (
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/launch"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/secret"
)

// This file is the environment model the wrapper script and Environ share: which
// settings the wrapper carries (launch.DecideWith, the same decision the profile
// folder is written from), which env: sources are captured first, which variables
// the cleanup clears, and which arguments carry the model. RenderWith turns it into
// POSIX sh; Environ evaluates it in Go for `aprofile login` (FR-039), so the two
// cannot drift apart (spec 004 FR-012 to FR-016).

// keyCheckVar is the private shell variable a folder-carried key is resolved into
// for the fail-closed check; it is never exported and is unset right after.
const keyCheckVar = "_aprofile_key"

// srcTemp names the temporary that keeps the caller's value of an env: source.
func srcTemp(name string) string { return "_aprofile_src_" + name }

// model is one wrapper's environment model.
type model struct {
	settings []launch.Setting
	// sources are the variables env: references read, captured before any clearing.
	sources []string
	// clear is the cleanup's unset list: launch_clear, the profile's own launch
	// variables, APROFILE_SET_VARS.
	clear []string
	// keys are the selected auth kind's key settings (either carrier).
	keys []launch.Setting
	// vars are the wrapper-carried launch variables, in setting order.
	vars []launch.Setting
	// setVars names every launch variable the profile supplies; setVarsWrapper is
	// true when the wrapper (not the folder) exports APROFILE_SET_VARS.
	setVars        []string
	setVarsWrapper bool
	// modelArgs are the arguments passed before the caller's for a model the
	// harness takes as a launch argument.
	modelArgs []string
	// keepSources are the variables a folder-carried key command reads at launch:
	// an inherited APROFILE_SET_VARS never clears them.
	keepSources []string
	// protected are the exact names the cleanup never clears when an inherited
	// APROFILE_SET_VARS lists them (PATH, HOME, the config_env redirects, the
	// APROFILE_* contract variables and keepSources); names with the
	// internalPrefix are protected too. RenderWith and Environ both use it.
	protected []string
}

// internalPrefix marks the wrapper's own shell temporaries (capture, key check).
const internalPrefix = "_aprofile_"

// contractVars are the APROFILE_* contract variables the wrapper sets. Only these
// are protected from an inherited APROFILE_SET_VARS: a profile's own variable that
// happens to start with APROFILE_ is cleared like any other launch variable.
var contractVars = []string{
	"APROFILE_HARNESS", "APROFILE_NAME", "APROFILE_LABEL", "APROFILE_COLOR", "APROFILE_ICON",
	"APROFILE_CONFIG_DIR", "APROFILE_SHARE", "APROFILE_HISTORY_GROUP", "APROFILE_SET_VARS",
}

// newModel decides the profile's settings from the definition and the profile
// folder's current files and derives the wrapper's environment model.
func newModel(p *profile.Profile, def *harness.Definition, configDir string, hookEnv []harness.EnvVar) model {
	m := model{settings: launch.DecideWith(p, def, launch.ReadDoc(def, configDir), hookEnv)}
	folderVar := false
	addSource := func(r *secret.Reference) {
		if r != nil && r.Kind == secret.KindEnv && !slices.Contains(m.sources, r.Value) {
			m.sources = append(m.sources, r.Value)
		}
	}
	clear := append([]string(nil), def.LaunchClear...)
	for _, s := range m.settings {
		switch {
		case s.Kind == launch.KindKey:
			m.keys = append(m.keys, s)
			addSource(s.Ref)
			if s.Carrier == launch.CarrierFolder && s.Ref.Kind == secret.KindEnv {
				m.keepSources = append(m.keepSources, s.Ref.Value)
			}
		case s.Kind == launch.KindModel && s.Variable == "":
			m.modelArgs = def.ExpandModelArg(s.Value)
		case s.IsVariable():
			m.setVars = append(m.setVars, s.Variable)
			if !slices.Contains(clear, s.Variable) {
				clear = append(clear, s.Variable)
			}
			if s.Carrier == launch.CarrierFolder {
				folderVar = true
				continue
			}
			m.vars = append(m.vars, s)
			addSource(s.Ref)
		}
	}
	sort.Strings(m.sources)
	m.clear = append(clear, "APROFILE_SET_VARS")
	m.protected = []string{"PATH", "HOME"}
	for _, e := range def.ConfigEnv {
		m.protected = append(m.protected, e.Var)
	}
	m.protected = append(m.protected, m.keepSources...)
	m.protected = append(m.protected, contractVars...)
	m.setVarsWrapper = !folderVar
	return m
}

// readRef returns the reference the wrapper resolves: an env: reference reads its
// captured temporary, keeping the original text for the failure message.
func (m model) readRef(r secret.Reference) secret.Reference {
	if r.Kind == secret.KindEnv && slices.Contains(m.sources, r.Value) {
		return secret.Reference{Kind: secret.KindEnv, Value: srcTemp(r.Value), Raw: r.Raw}
	}
	return r
}

// protectedFromInherited reports whether the cleanup never clears name when an
// inherited APROFILE_SET_VARS lists it (the rule renderCleanup renders).
func protectedFromInherited(m model, name string) bool {
	return !secret.ValidEnvVarName(name) || strings.HasPrefix(name, internalPrefix) || slices.Contains(m.protected, name)
}

// Environ evaluates the wrapper's environment in Go: from the caller's environment it
// returns the environment the profile's wrapper (RenderWith with the same opts) would
// exec the harness with (FR-026). `aprofile login` runs the login with it (FR-039).
// The steps are the wrapper's, in its order: capture the env: sources, clear the
// inherited and own launch variables, the config_env redirects, the APROFILE_*
// contract, every auth and auth_clears variable removed and the selected kind's
// wrapper-carried key set (a folder-carried key is only checked), HERDR_AGENT, then
// the wrapper-carried launch variables and APROFILE_SET_VARS when the wrapper
// carries it. References resolve fail-closed. The result is sorted.
func Environ(p *profile.Profile, def *harness.Definition, configDir string, opts RenderOptions, environ []string) ([]string, error) {
	env := map[string]string{}
	for _, kv := range environ {
		if i := strings.IndexByte(kv, '='); i > 0 {
			env[kv[:i]] = kv[i+1:]
		}
	}
	m := newModel(p, def, configDir, opts.HookEnv)
	captured := map[string]string{}
	for _, src := range m.sources {
		captured[src] = env[src]
	}
	resolve := func(ref secret.Reference) (string, error) {
		if ref.Kind == secret.KindEnv {
			if v := captured[ref.Value]; v != "" {
				return v, nil
			}
			return "", fmt.Errorf("reference %s is unset or empty", ref.Raw)
		}
		return readFirstLine(ref)
	}

	for _, v := range strings.Fields(env["APROFILE_SET_VARS"]) {
		if !protectedFromInherited(m, v) {
			delete(env, v)
		}
	}
	for _, v := range m.clear {
		delete(env, v)
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

	for _, v := range def.UnsetAuthVars() {
		delete(env, v)
	}
	for _, k := range m.keys {
		val, err := resolve(*k.Ref)
		if err != nil {
			return nil, fmt.Errorf("profile %s: %w", p.ID(), err)
		}
		if k.Carrier == launch.CarrierWrapper {
			env[k.Variable] = val
		}
	}

	if p.HerdrAgent != "" {
		env["HERDR_AGENT"] = p.HerdrAgent
	}

	for _, v := range m.vars {
		val := v.Value
		if v.Ref != nil {
			var err error
			if val, err = resolve(*v.Ref); err != nil {
				return nil, fmt.Errorf("profile %s: variable %s: %w", p.ID(), v.Variable, err)
			}
		}
		env[v.Variable] = val
	}
	if m.setVarsWrapper {
		env["APROFILE_SET_VARS"] = strings.Join(m.setVars, " ")
	}

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

// readFirstLine resolves a file: reference the way the wrapper's `IFS= read -r`
// does: the first line, without its newline but with a carriage return and
// surrounding whitespace kept, fail-closed when it is empty.
func readFirstLine(ref secret.Reference) (string, error) {
	data, err := os.ReadFile(ref.Value)
	if err != nil {
		return "", fmt.Errorf("reference %s is missing, unreadable or empty", ref.Raw)
	}
	line, _, _ := strings.Cut(string(data), "\n")
	if line == "" {
		return "", fmt.Errorf("reference %s is missing, unreadable or empty", ref.Raw)
	}
	return line, nil
}
