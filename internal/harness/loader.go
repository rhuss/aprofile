package harness

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/cc-deck/aprofile/internal/secret"
)

//go:embed builtin/*.yaml
var builtinFS embed.FS

// namePattern is shared by profile names, harness names and wrapper prefixes
// (FR-002).
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}[a-z0-9]$`)

// NameValid reports whether a harness/profile name or wrapper prefix is valid.
func NameValid(s string) bool { return namePattern.MatchString(s) }

// LoadError reports an invalid definition by file and field (FR-012). It never
// stops other definitions from loading.
type LoadError struct {
	File  string
	Field string
	Msg   string
}

func (e LoadError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s: field %q: %s", e.File, e.Field, e.Msg)
	}
	return fmt.Sprintf("%s: %s", e.File, e.Msg)
}

// fieldError is a lightweight error carrying a field name, surfaced by custom
// UnmarshalYAML implementations and wrapped into a LoadError by the loader.
type fieldError struct {
	Field string
	Msg   string
}

func (e fieldError) Error() string { return fmt.Sprintf("field %q: %s", e.Field, e.Msg) }

func newFieldError(field, msg string) error { return fieldError{Field: field, Msg: msg} }

// Set is the collection of effective harness definitions plus any load errors.
type Set struct {
	defs   map[string]*Definition
	errors []LoadError
}

// Get returns a definition by name.
func (s *Set) Get(name string) (*Definition, bool) {
	d, ok := s.defs[name]
	return d, ok
}

// All returns every valid definition, sorted by name.
func (s *Set) All() []*Definition {
	out := make([]*Definition, 0, len(s.defs))
	for _, d := range s.defs {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Errors returns the load errors collected while building the set. A bad
// definition is isolated here; the others remain usable (FR-012).
func (s *Set) Errors() []LoadError { return s.errors }

// readBuiltins parses every embedded built-in into both its Definition and its raw
// top-level mapping node (the node is needed by the harnesses.d override-merge). A
// definition that fails to parse or validate is reported in errs but does not stop
// the others (FR-012).
func readBuiltins() (defs map[string]*Definition, nodes map[string]*yaml.Node, errs []LoadError) {
	defs = map[string]*Definition{}
	nodes = map[string]*yaml.Node{}
	entries, err := fs.Glob(builtinFS, "builtin/*.yaml")
	if err != nil {
		errs = append(errs, LoadError{File: "builtin", Msg: err.Error()})
		return defs, nodes, errs
	}
	sort.Strings(entries)
	for _, name := range entries {
		data, err := builtinFS.ReadFile(name)
		if err != nil {
			errs = append(errs, LoadError{File: name, Msg: err.Error()})
			continue
		}
		def, lerr := parseDefinitionOpts(data, name, true)
		if lerr != nil {
			errs = append(errs, *lerr)
			continue
		}
		node, terr := topMapping(data)
		if terr != nil {
			errs = append(errs, LoadError{File: name, Msg: terr.Error()})
			continue
		}
		defs[def.Name] = def
		nodes[def.Name] = node
	}
	return defs, nodes, errs
}

// LoadBuiltins parses every embedded built-in definition. A definition that fails
// to parse or validate is reported in the returned errors slice but does not stop
// the others (FR-012).
func LoadBuiltins() (*Set, error) {
	defs, _, errs := readBuiltins()
	set := &Set{defs: map[string]*Definition{}, errors: errs}
	for _, d := range defs {
		markSource(d, "builtin")
		set.defs[d.Name] = d
	}
	checkPrefixClashes(set)
	return set, nil
}

// Load builds the effective set from the embedded built-ins and overlays every
// definition file in harnessesDir onto it (FR-009): a file whose name equals a
// built-in's overrides only the fields it sets; a file with a new name adds a
// harness. An invalid file or a duplicate name is isolated in Errors and does not
// stop the others (FR-012). A missing harnessesDir yields built-ins only.
func Load(harnessesDir string) (*Set, error) {
	defs, nodes, errs := readBuiltins()
	set := &Set{defs: map[string]*Definition{}, errors: errs}
	for _, d := range defs {
		markSource(d, "builtin")
		set.defs[d.Name] = d
	}
	overlayHarnessesDir(set, nodes, harnessesDir)
	// Wrapper prefixes must be unique across the effective definitions (FR-002).
	checkPrefixClashes(set)
	return set, nil
}

// LoadFromBytes builds a Set from in-memory definition files (filename -> content).
// An invalid file is isolated in Errors and does not stop the others (FR-012).
// It is used by tests and, later, by the harnesses.d loader (T044).
func LoadFromBytes(files map[string][]byte) *Set {
	set := &Set{defs: map[string]*Definition{}}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		def, lerr := parseDefinition(files[name], name)
		if lerr != nil {
			set.errors = append(set.errors, *lerr)
			continue
		}
		markSource(def, name)
		set.defs[def.Name] = def
	}
	checkPrefixClashes(set)
	return set
}

// parseDefinition decodes and validates a single user definition (harnesses.d or
// an override merged onto a built-in). It returns a *LoadError (never a bare
// error) so the caller can collect and continue.
func parseDefinition(data []byte, file string) (*Definition, *LoadError) {
	return parseDefinitionOpts(data, file, false)
}

// parseDefinitionOpts decodes, validates and normalizes one definition. Built-ins
// get the stricter rules of contracts/harness-definition.md: no `shared` layer and
// no glob rule with a borrowable layer (Constitution III). For user files the
// `shared` alias is mapped to content and flagged as deprecated. Database rules are
// expanded with their companion files (FR-004).
func parseDefinitionOpts(data []byte, file string, builtin bool) (*Definition, *LoadError) {
	var def Definition
	if err := yaml.Unmarshal(data, &def); err != nil {
		if fe, ok := asFieldError(err); ok {
			return nil, &LoadError{File: file, Field: fe.Field, Msg: fe.Msg}
		}
		return nil, &LoadError{File: file, Msg: err.Error()}
	}
	if field, msg := validate(&def); field != "" || msg != "" {
		return nil, &LoadError{File: file, Field: field, Msg: msg}
	}
	if builtin {
		for _, r := range def.Classification {
			if r.Layer == LayerShared {
				return nil, &LoadError{File: file, Field: "classification", Msg: fmt.Sprintf("entry %q: built-in definitions must not use the deprecated shared layer", r.Entry)}
			}
			if r.IsGlob() && r.Layer.Borrowable() {
				return nil, &LoadError{File: file, Field: "classification", Msg: fmt.Sprintf("entry %q: built-in definitions must not contain a glob rule with a borrowable layer (catch-all)", r.Entry)}
			}
		}
	}
	for i := range def.Classification {
		if def.Classification[i].Layer == LayerShared {
			def.Classification[i].Layer = LayerContent
			def.Classification[i].Deprecated = true
		}
	}
	def.Classification = expandCompanions(def.Classification)
	return &def, nil
}

// companionSuffixes are the files SQLite (and similar stores) open next to a
// database by name (research R2).
var companionSuffixes = []string{"-wal", "-shm", "-journal", ".lock"}

// isDatabaseRule reports whether a rule names a database whose companions must
// share its layer.
func isDatabaseRule(r EntryRule) bool {
	if r.Database {
		return true
	}
	e := strings.ToLower(r.Path())
	return strings.HasSuffix(e, ".sqlite") || strings.HasSuffix(e, ".db")
}

// expandCompanions appends, after each database rule, one rule per companion file
// with the same layer, unless a rule for that companion already exists.
func expandCompanions(rules []EntryRule) []EntryRule {
	have := map[string]bool{}
	for _, r := range rules {
		have[r.Path()] = true
	}
	out := make([]EntryRule, 0, len(rules))
	for _, r := range rules {
		out = append(out, r)
		if r.CompanionOf != "" || !isDatabaseRule(r) || r.IsDir() || r.Layer == LayerMixed {
			continue
		}
		for _, suf := range companionSuffixes {
			e := r.Path() + suf
			if have[e] {
				continue
			}
			have[e] = true
			out = append(out, EntryRule{Entry: e, Layer: r.Layer, CompanionOf: r.Entry, Origin: r.Origin, History: r.History, Merge: r.Merge})
		}
	}
	return out
}

// validate checks a definition's required fields and internal consistency. It
// returns the first offending field and a message, or two empty strings when the
// definition is valid.
func validate(d *Definition) (field, msg string) {
	if d.Name == "" {
		return "name", "is required"
	}
	if !NameValid(d.Name) {
		return "name", "must match ^[a-z][a-z0-9-]{0,62}[a-z0-9]$"
	}
	if d.Binary == "" {
		return "binary", "is required"
	}
	if d.WrapperPrefix == "" {
		return "wrapper_prefix", "is required"
	}
	if !NameValid(d.WrapperPrefix) {
		return "wrapper_prefix", "must match ^[a-z][a-z0-9-]{0,62}[a-z0-9]$"
	}
	if len(d.ConfigEnv) == 0 {
		return "config_env", "must redirect at least one variable into the profile"
	}
	// Every variable name the wrapper interpolates (config_env, model/endpoint, auth
	// kinds) must be a safe shell identifier so a definition cannot inject code into
	// the generated wrapper (FR-028b).
	for _, e := range d.ConfigEnv {
		if !secret.ValidEnvVarName(e.Var) {
			return "config_env", fmt.Sprintf("variable name %q must match [A-Za-z_][A-Za-z0-9_]*", e.Var)
		}
	}
	if d.ModelVar != "" && !secret.ValidEnvVarName(d.ModelVar) {
		return "model_var", fmt.Sprintf("%q must match [A-Za-z_][A-Za-z0-9_]*", d.ModelVar)
	}
	if d.EndpointVar != "" && !secret.ValidEnvVarName(d.EndpointVar) {
		return "endpoint_var", fmt.Sprintf("%q must match [A-Za-z_][A-Za-z0-9_]*", d.EndpointVar)
	}
	for _, ak := range d.AuthKinds {
		for _, v := range ak.VarNames() {
			if !secret.ValidEnvVarName(v) {
				return "auth_kinds", fmt.Sprintf("auth kind %q variable %q must match [A-Za-z_][A-Za-z0-9_]*", ak.Name, v)
			}
		}
	}
	// auth_clears is unset after the config_env redirect and before the overridable
	// model/endpoint values, so it must not name a variable the wrapper owns
	// otherwise: clearing a redirect would break isolation, clearing the model or
	// endpoint variable would make it silently non-overridable.
	for _, v := range d.AuthClears {
		if !secret.ValidEnvVarName(v) {
			return "auth_clears", fmt.Sprintf("variable %q must match [A-Za-z_][A-Za-z0-9_]*", v)
		}
		if strings.HasPrefix(v, "APROFILE_") {
			return "auth_clears", fmt.Sprintf("variable %q is an aprofile contract variable", v)
		}
		if v == d.ModelVar || v == d.EndpointVar {
			return "auth_clears", fmt.Sprintf("variable %q is the model or endpoint variable", v)
		}
		for _, e := range d.ConfigEnv {
			if e.Var == v {
				return "auth_clears", fmt.Sprintf("variable %q is a config_env redirect", v)
			}
		}
	}
	if d.LoginCommand != nil {
		for _, a := range d.LoginCommand.Args {
			if a == "" || strings.ContainsAny(a, "\x00\n\r") {
				return "login_command", fmt.Sprintf("argument %q must be non-empty and single-line", a)
			}
		}
	}
	for _, r := range d.Classification {
		if r.Entry == "" {
			return "classification", "an entry rule is missing its entry"
		}
		// A classification entry is joined onto the profile config dir by sync, copy and
		// import; one import path even RemoveAll's the target before copying. Reject
		// absolute paths and any ".." segment so a definition can never escape the
		// profile dir or delete an arbitrary directory (defense in depth, Principle II).
		if !entryPathSafe(r.Entry) {
			return "classification", fmt.Sprintf("entry %q must be a relative path within the profile dir (no %q segment, not absolute)", r.Entry, "..")
		}
		if !r.Layer.Valid() {
			return "classification", fmt.Sprintf("entry %q has invalid layer %q", r.Entry, r.Layer)
		}
		if r.Layer == LayerMixed {
			if len(r.Keys) == 0 {
				return "classification", fmt.Sprintf("mixed entry %q must declare keys", r.Entry)
			}
			for k, kl := range r.Keys {
				if kl == LayerMixed || !kl.Valid() {
					return "classification", fmt.Sprintf("mixed entry %q key %q has invalid layer %q", r.Entry, k, kl)
				}
			}
		}
		if msg := validateHistoryRule(r); msg != "" {
			return "classification", msg
		}
	}
	if msg := validateHistoryNesting(d.Classification); msg != "" {
		return "classification", msg
	}
	for i, cp := range d.ConfigPatches {
		if field, msg := validateConfigPatch(cp); msg != "" {
			return field, fmt.Sprintf("patch %d: %s", i+1, msg)
		}
	}
	if d.DefaultAuthKind != "" {
		if _, ok := d.AuthKind(d.DefaultAuthKind); !ok {
			return "default_auth_kind", fmt.Sprintf("%q is not a declared auth kind", d.DefaultAuthKind)
		}
	}
	for cat, row := range d.SharePolicy {
		if !cat.Borrowable() || cat == LayerShared {
			return "share_policy", fmt.Sprintf("category %q is not one of content, settings, plugin-store, hook-location", cat)
		}
		for _, lv := range Levels {
			if t := row.Get(lv); t != "" && !validTreatment(t) {
				return "share_policy", fmt.Sprintf("%s at %s: treatment %q must be one of link, own, copy-once, minimal, managed", cat, lv, t)
			}
		}
	}
	if d.Hooks != nil {
		switch d.Hooks.Form {
		case HookFormEventGroups:
			if d.Hooks.File() == "" || d.Hooks.Key() == "" {
				return "hooks", fmt.Sprintf("location %q must be file#key for form event-groups", d.Hooks.Location)
			}
		case HookFormFiles:
			if d.Hooks.File() == "" || d.Hooks.Key() != "" {
				return "hooks", fmt.Sprintf("location %q must be a directory for form files", d.Hooks.Location)
			}
		default:
			return "hooks", fmt.Sprintf("form %q must be event-groups or files", d.Hooks.Form)
		}
		if !entryPathSafe(d.Hooks.File()) {
			return "hooks", fmt.Sprintf("location %q must be a relative path within the profile dir", d.Hooks.Location)
		}
	}
	if msg := validateMCPLocation(d); msg != "" {
		return "mcp", msg
	}
	for _, he := range d.HomeEscapes {
		if he.Path == "" {
			return "home_escapes", "every escape needs a path"
		}
	}
	for prefix := range d.DefaultLocations {
		if !entryPathSafe(prefix) || prefix == "" {
			return "default_locations", fmt.Sprintf("prefix %q must be a relative path within the profile dir", prefix)
		}
	}
	if msg := validateBackends(d); msg != "" {
		return "backends", msg
	}
	return "", ""
}

// validateHistoryRule checks the history fields of one rule (spec 003,
// contracts/harness-definition.md): history only on a plain state entry (no glob, no
// mixed entry); merge only with history, only per-file or none, and per-file only on
// a directory entry. It returns "" when valid.
func validateHistoryRule(r EntryRule) string {
	if r.History {
		if r.Layer != LayerState {
			return fmt.Sprintf("entry %q: history is only valid on layer state, not %q", r.Entry, r.Layer)
		}
		if r.IsGlob() {
			return fmt.Sprintf("entry %q: history is not valid on a glob rule", r.Entry)
		}
	}
	if r.Merge == "" {
		return ""
	}
	if !r.History {
		return fmt.Sprintf("entry %q: merge is only valid together with history: true", r.Entry)
	}
	switch r.Merge {
	case MergeNone:
	case MergePerFile:
		if !r.IsDir() {
			return fmt.Sprintf("entry %q: merge per-file is only valid on a directory entry (trailing /)", r.Entry)
		}
	default:
		return fmt.Sprintf("entry %q: merge %q must be per-file or none", r.Entry, r.Merge)
	}
	return ""
}

// validateHistoryNesting rejects a history entry that holds another classified
// entry (spec 003, FR-003, FR-004): a member's history entry is a link into the
// group store, so every entry below it would be shared with the group too,
// credentials included. It also rejects two rules for the same history entry
// where one is not history. It returns "" when valid.
func validateHistoryNesting(rules []EntryRule) string {
	for _, h := range rules {
		if !h.History {
			continue
		}
		hp := strings.TrimSuffix(h.Path(), "/")
		for _, r := range rules {
			if r.Entry == h.Entry && r.History {
				continue
			}
			rp := strings.TrimSuffix(filepath.ToSlash(r.Entry), "/")
			switch {
			case rp == hp && !r.History:
				return fmt.Sprintf("entry %q is declared as history and also as %s; a history entry has exactly one rule", h.Entry, r.Layer)
			case strings.HasPrefix(rp, hp+"/"):
				return fmt.Sprintf("history entry %q contains entry %q (%s): a history entry is shared with its history group as a whole, so it must not hold other classified entries", h.Entry, r.Entry, r.Layer)
			}
		}
	}
	return ""
}

// validateMCPLocation checks the mcp location ("file#key"): sync, mcp copy and
// import join the file onto the profile config dir and write it, so it must be a
// relative path inside that dir (entryPathSafe). The file must not be a
// credentials or state entry, and a key inside a mixed file must be owned there or
// undeclared: writing servers into a credentials, state or settings key (Claude's
// oauthAccount) would replace that data. It returns "" when valid or absent.
func validateMCPLocation(d *Definition) string {
	if d.MCP == nil {
		return ""
	}
	file, key := d.MCP.File(), d.MCP.Key()
	if file == "" || !entryPathSafe(file) {
		return fmt.Sprintf("location %q: the file must be a relative path within the profile dir (no %q segment, not absolute)", d.MCP.Location, "..")
	}
	for _, r := range d.Classification {
		if r.IsGlob() || r.Path() != file {
			continue
		}
		switch r.Layer {
		case LayerCredentials, LayerState:
			return fmt.Sprintf("location %q: %s is a %s entry; MCP servers must live in an owned entry", d.MCP.Location, file, r.Layer)
		case LayerMixed:
			if kl, ok := r.Keys[key]; ok && key != "" && kl != LayerOwned {
				return fmt.Sprintf("location %q: key %q of mixed file %s is a %s key; the MCP key must be owned or undeclared", d.MCP.Location, key, file, kl)
			}
		}
	}
	return ""
}

// placeholderRE finds the {name} placeholders of a backend env value.
var placeholderRE = regexp.MustCompile(`\{[^{}]*\}`)

// validateBackends checks the backends block (FR-045): backend names are plain
// names; every variable is a valid identifier the wrapper does not own otherwise
// (config_env, model/endpoint, auth kind and APROFILE_ variables); values are single
// lines whose placeholders are all in BackendPlaceholders. It returns "" when valid.
func validateBackends(d *Definition) string {
	owned := map[string]string{}
	for _, e := range d.ConfigEnv {
		owned[e.Var] = "a config_env redirect"
	}
	for _, v := range d.AllAuthVars() {
		owned[v] = "an auth kind variable"
	}
	for _, v := range []string{d.ModelVar, d.EndpointVar} {
		if v != "" {
			owned[v] = "the model or endpoint variable"
		}
	}
	checkVar := func(backend, v string) string {
		switch {
		case !secret.ValidEnvVarName(v):
			return fmt.Sprintf("backend %q: variable %q must match [A-Za-z_][A-Za-z0-9_]*", backend, v)
		case strings.HasPrefix(v, "APROFILE_"):
			return fmt.Sprintf("backend %q: variable %q is an aprofile contract variable", backend, v)
		case owned[v] != "":
			return fmt.Sprintf("backend %q: variable %q is %s", backend, v, owned[v])
		}
		return ""
	}
	allowed := map[string]bool{}
	for _, ph := range BackendPlaceholders {
		allowed["{"+ph+"}"] = true
	}
	names := make([]string, 0, len(d.Backends))
	for n := range d.Backends {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		b := d.Backends[name]
		if !NameValid(name) {
			return fmt.Sprintf("backend name %q must match ^[a-z][a-z0-9-]{0,62}[a-z0-9]$", name)
		}
		seen := map[string]bool{}
		for _, e := range b.Env {
			if msg := checkVar(name, e.Name); msg != "" {
				return msg
			}
			if seen[e.Name] {
				return fmt.Sprintf("backend %q: variable %q is listed twice", name, e.Name)
			}
			seen[e.Name] = true
			if strings.ContainsAny(e.Value, "\x00\n\r") {
				return fmt.Sprintf("backend %q: value of %q must be a single line", name, e.Name)
			}
			for _, ph := range placeholderRE.FindAllString(e.Value, -1) {
				if !allowed[ph] {
					return fmt.Sprintf("backend %q: value of %q uses placeholder %s; allowed: {%s}", name, e.Name, ph, strings.Join(BackendPlaceholders, "}, {"))
				}
			}
		}
		if b.CredentialsVar != "" {
			if msg := checkVar(name, b.CredentialsVar); msg != "" {
				return msg
			}
			if seen[b.CredentialsVar] {
				return fmt.Sprintf("backend %q: credentials_var %q is also set by env", name, b.CredentialsVar)
			}
		}
	}
	return ""
}

// validateConfigPatch checks one config_patches entry (FR-019): the file is a
// relative path inside the profile dir with a .json or .toml extension, and the
// merge is a non-empty map without null values (a patch only sets keys; it never
// removes one, so null has no meaning).
func validateConfigPatch(cp ConfigPatch) (field, msg string) {
	const f = "config_patches"
	if cp.File == "" {
		return f, "file is required"
	}
	if !entryPathSafe(cp.File) {
		return f, fmt.Sprintf("file %q must be a relative path within the profile dir (no %q segment, not absolute)", cp.File, "..")
	}
	if !ConfigPatchFormatSupported(cp.File) {
		return f, fmt.Sprintf("file %q must be a .json or .toml file", cp.File)
	}
	if len(cp.Merge) == 0 {
		return f, fmt.Sprintf("file %q: merge must set at least one key", cp.File)
	}
	if path := nullPatchValue(cp.Merge, ""); path != "" {
		return f, fmt.Sprintf("file %q: key %q is null; a patch only sets keys", cp.File, path)
	}
	return "", ""
}

// ConfigPatchFormatSupported reports whether a patch target is a JSON or TOML file.
func ConfigPatchFormatSupported(file string) bool {
	switch strings.ToLower(filepath.Ext(file)) {
	case ".json", ".toml":
		return true
	}
	return false
}

// nullPatchValue returns the dotted path of the first null value (or empty key) in
// a patch, or "".
func nullPatchValue(v any, prefix string) string {
	switch t := v.(type) {
	case nil:
		if prefix == "" {
			return "."
		}
		return prefix
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			if k == "" {
				return p + "<empty key>"
			}
			if bad := nullPatchValue(t[k], p); bad != "" {
				return bad
			}
		}
	case []any:
		for i, e := range t {
			if bad := nullPatchValue(e, fmt.Sprintf("%s[%d]", prefix, i)); bad != "" {
				return bad
			}
		}
	}
	return ""
}

func validTreatment(t string) bool {
	switch t {
	case TreatLink, TreatOwn, TreatCopyOnce, TreatMinimal, TreatManaged:
		return true
	}
	return false
}

// markSource records the origin of every effective field and classification rule.
func markSource(d *Definition, origin string) {
	if d.Source == nil {
		d.Source = map[string]string{}
	}
	for _, f := range allFieldNames {
		d.Source[f] = origin
	}
	for i := range d.Classification {
		d.Classification[i].Origin = origin
	}
}

// allFieldNames lists the definition fields whose origin harness show reports.
var allFieldNames = []string{
	"name", "binary", "wrapper_prefix", "config_env", "default_config_location",
	"model_var", "endpoint_var", "auth_kinds", "auth_clears", "default_auth_kind", "login_command", "classification",
	"config_patches", "mcp", "skills_location", "plugin_mechanism",
	"capabilities", "exec_hook", "default_locations", "share_policy", "hooks", "home_escapes",
	"backends", "history_note",
}

func asFieldError(err error) (fieldError, bool) {
	// yaml.v3 wraps UnmarshalYAML errors in a TypeError; our fieldError may be
	// returned directly for top-level custom unmarshalers.
	if fe, ok := err.(fieldError); ok {
		return fe, true
	}
	if strings.Contains(err.Error(), "share_policy") {
		return fieldError{Field: "share_policy", Msg: err.Error()}, true
	}
	return fieldError{}, false
}

// sortStrings sorts a slice in place (small helper to avoid importing sort in
// definition.go).
func sortStrings(s []string) { sort.Strings(s) }

// entryPathSafe reports whether a classification entry is a relative path contained
// within the profile config dir: not absolute and with no ".." segment. Glob
// metacharacters and nested slashes are allowed (OpenCode uses "data/opencode/...").
func entryPathSafe(entry string) bool {
	if filepath.IsAbs(entry) {
		return false
	}
	// "." (the profile root itself), empty segments and ".." are rejected: a rule
	// for the root would let a level change replace the whole profile dir.
	for _, seg := range strings.Split(strings.TrimSuffix(filepath.ToSlash(entry), "/"), "/") {
		if seg == ".." || seg == "." || seg == "" {
			return false
		}
	}
	return true
}

// ConfigEnvPath resolves a config_env path (relative to the profile root, or ".")
// against the absolute profile config dir.
func ConfigEnvPath(configDir, rel string) string {
	if rel == "." || rel == "" {
		return configDir
	}
	return filepath.Join(configDir, filepath.FromSlash(rel))
}
