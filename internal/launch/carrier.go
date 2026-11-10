package launch

import (
	"slices"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/secret"
)

// Carrier names what supplies a launch setting at launch (FR-001).
type Carrier string

const (
	CarrierFolder  Carrier = "folder"
	CarrierWrapper Carrier = "wrapper"
)

// Reason says why a setting is wrapper-only (data-model.md "Launch setting").
type Reason string

const (
	// ReasonNoSupport: the definition declares no folder location for this kind.
	ReasonNoSupport Reason = "no-support"
	// ReasonReferenceValue: an extra variable given as a reference (FR-007).
	ReasonReferenceValue Reason = "reference-value"
	// ReasonAuthKind: the auth kind's key cannot be carried by the folder.
	ReasonAuthKind Reason = "auth-kind"
	// ReasonShareAll: the settings file is linked to the default setup (FR-008).
	ReasonShareAll Reason = "share-all"
	// ReasonNoProvider: the provider-auth selector names no provider defined in the
	// file, or the file is absent (FR-009).
	ReasonNoProvider Reason = "no-provider"
	// ReasonHarnessRewrites: passed by the wrapper as a launch argument because the
	// harness rewrites the setting in its own file (FR-002a).
	ReasonHarnessRewrites Reason = "harness-rewrites"
	// ReasonClearedSource: an env: key whose variable aprofile clears.
	ReasonClearedSource Reason = "cleared-source"
	// ReasonHookEnv: a variable from the exec hook's env operation.
	ReasonHookEnv Reason = "hook-env"
)

// Setting kinds. Extra variables are "extra:<NAME>", exec-hook variables
// "hook:<NAME>".
const (
	KindModel    = "model"
	KindEndpoint = "endpoint"
	KindKey      = "key"
	extraPrefix  = "extra:"
	hookPrefix   = "hook:"
)

// Setting is one launch setting of a profile with its carrier.
type Setting struct {
	Kind string
	// Variable is the environment variable that carries it ("" for a model passed as
	// a launch argument).
	Variable string
	// Value is the literal value of a model, endpoint or literal extra variable, the
	// raw reference text of a reference-valued variable, and always "" for the key:
	// a key value is never held here.
	Value        string
	Carrier      Carrier
	Reason       Reason // "" when folder-carried
	EnvDependent bool   // a folder-carried key read from an env: reference
	Ref          *secret.Reference
}

// IsVariable reports whether the setting is an environment variable the profile
// supplies (model, endpoint, extra or hook variable; not the key).
func (s Setting) IsVariable() bool { return s.Kind != KindKey && s.Variable != "" }

// Decide returns the profile's launch settings with their carriers; it is a pure
// function of profile, definition, share level and current file content (FR-001).
func Decide(p *profile.Profile, def *harness.Definition, doc Doc) []Setting {
	return DecideWith(p, def, doc, nil)
}

// DecideWith is Decide with the exec hook's env output, whose variables are always
// wrapper-carried (reason hook-env) and listed in APROFILE_SET_VARS.
func DecideWith(p *profile.Profile, def *harness.Definition, doc Doc, hookEnv []harness.EnvVar) []Setting {
	var out []Setting
	// add appends a variable setting; a later setting of the same variable replaces
	// the earlier one in place (an extra variable named like the model or endpoint
	// variable wins, as the last export did in the wrapper), so a variable is never
	// supplied twice and the folder never alternates between two values (FR-011).
	add := func(s Setting) {
		for i := range out {
			if out[i].Variable == s.Variable {
				out[i] = s
				return
			}
		}
		out = append(out, s)
	}
	envReason := envCarrier(p, def, doc)
	variable := func(kind, name, value string, ref *secret.Reference) {
		s := Setting{Kind: kind, Variable: name, Value: value, Ref: ref, Carrier: CarrierFolder}
		switch {
		case ref != nil:
			s.Carrier, s.Reason = CarrierWrapper, ReasonReferenceValue
		case envReason != "":
			s.Carrier, s.Reason = CarrierWrapper, envReason
		}
		add(s)
	}
	if p.Model != "" {
		switch {
		case def.ModelVar != "":
			variable(KindModel, def.ModelVar, p.Model, nil)
		case len(def.ModelArg) > 0:
			out = append(out, Setting{Kind: KindModel, Value: p.Model, Carrier: CarrierWrapper, Reason: ReasonHarnessRewrites})
		}
	}
	if p.Endpoint != "" && def.EndpointVar != "" {
		variable(KindEndpoint, def.EndpointVar, p.Endpoint, nil)
	}
	for _, e := range p.ExtraEnv {
		variable(extraPrefix+e.Name, e.Name, e.Value, extraRef(e))
	}
	for _, e := range hookEnv {
		s := Setting{Kind: hookPrefix + e.Name, Variable: e.Name, Value: e.Value, Carrier: CarrierWrapper, Reason: ReasonHookEnv}
		if secret.IsReference(e.Value) {
			if r, err := secret.ParseReference(e.Value); err == nil {
				s.Ref = &r
			}
		}
		add(s)
	}
	out = append(out, keySettings(p, def, doc, out)...)
	return out
}

// extraRef returns the parsed reference of a reference-valued extra variable, or nil
// for a literal (the wrapper's rule: a value that is not a well-formed reference, or
// one marked literal, is a literal).
func extraRef(e profile.ExtraEnvVar) *secret.Reference {
	if e.Literal || !secret.IsReference(e.Value) {
		return nil
	}
	r, err := secret.ParseReference(e.Value)
	if err != nil {
		return nil
	}
	return &r
}

// envCarrier returns "" when the folder can carry environment variables, otherwise
// the reason they stay in the wrapper.
func envCarrier(p *profile.Profile, def *harness.Definition, doc Doc) Reason {
	if def.FolderLaunch == nil || def.FolderLaunch.Env == "" {
		return ReasonNoSupport
	}
	if linked(p, def, doc, def.FolderLaunch.EnvFile()) {
		return ReasonShareAll
	}
	return ""
}

// linked reports whether a folder_launch file is (or will be) linked to the default
// setup: the file or its path is a link, or it is absent and the profile's share
// level links its category (sync creates the link before placing launch entries).
func linked(p *profile.Profile, def *harness.Definition, doc Doc, file string) bool {
	fd := doc.Files[file]
	if fd.Linked {
		return true
	}
	if fd.Exists {
		return false
	}
	for _, r := range def.Classification {
		if !r.IsGlob() && r.Path() == file {
			return r.Layer.Borrowable() && def.Treatment(r.Layer, string(p.ShareLevel())) == harness.TreatLink
		}
	}
	return false
}

// keySettings returns the key setting of the selected auth kind: none for a kind
// without references (an interactive login), otherwise one per referenced variable.
func keySettings(p *profile.Profile, def *harness.Definition, doc Doc, vars []Setting) []Setting {
	kind, ok := def.AuthKind(p.AuthKind)
	if !ok {
		return nil
	}
	var refs []Setting
	for _, v := range kind.VarNames() {
		if r, has := p.AuthRefs[v]; has {
			rr := r
			refs = append(refs, Setting{Kind: KindKey, Variable: v, Ref: &rr})
		}
	}
	for i := range refs {
		s := &refs[i]
		s.Carrier = CarrierWrapper
		switch {
		case def.FolderLaunch == nil || def.FolderLaunch.KeyCommand == nil:
			s.Reason = ReasonNoSupport
		case !def.FolderKeyFor(p.AuthKind) || len(refs) != 1:
			s.Reason = ReasonAuthKind
		case s.Ref.Kind == secret.KindEnv && (clearedVar(def, s.Ref.Value) || ownLaunchVar(vars, s.Ref.Value)):
			s.Reason = ReasonClearedSource
		default:
			s.Reason = keyLocationReason(p, def, doc)
		}
		if s.Reason == "" {
			s.Carrier = CarrierFolder
			s.EnvDependent = s.Ref.Kind == secret.KindEnv
		}
	}
	return refs
}

// clearedVar reports whether every wrapper clears the variable (auth-clearing list
// or launch_clear), so a folder key command reading it would read nothing.
func clearedVar(def *harness.Definition, name string) bool {
	return slices.Contains(def.UnsetAuthVars(), name) || slices.Contains(def.LaunchClear, name)
}

// ownLaunchVar reports whether name is one of the profile's own launch variables
// (model, endpoint, extra or exec-hook variable), which every wrapper of the
// profile clears before launching.
func ownLaunchVar(vars []Setting, name string) bool {
	for _, s := range vars {
		if s.IsVariable() && s.Variable == name {
			return true
		}
	}
	return false
}

// keyLocationReason returns "" when the key command location can take the key, or
// the reason it cannot.
func keyLocationReason(p *profile.Profile, def *harness.Definition, doc Doc) Reason {
	kc := def.FolderLaunch.KeyCommand
	file := kc.File()
	if linked(p, def, doc, file) {
		return ReasonShareAll
	}
	if kc.Form != harness.KeyFormProviderAuth {
		return ""
	}
	fd := doc.Files[file]
	if !fd.Exists {
		return ReasonNoProvider
	}
	if fd.Err != nil {
		// Unparseable: the planner reports it; the carrier cannot be decided safely,
		// so the folder keeps it (no wrapper change until the file is fixed).
		return ""
	}
	if _, ok := selectedProvider(fd.TOML, kc); !ok {
		return ReasonNoProvider
	}
	return ""
}

// selectedProvider returns the provider name the selector names when that provider
// is defined under the key command location (whatever its shape).
func selectedProvider(m map[string]any, kc *harness.KeyCommand) (string, bool) {
	name, _ := m[kc.Selector].(string)
	if name == "" {
		return "", false
	}
	providers, ok := m[kc.Key()].(map[string]any)
	if !ok {
		return name, false
	}
	_, ok = providers[name]
	return name, ok
}
