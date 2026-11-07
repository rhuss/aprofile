// Package harness holds the data-driven description of each coding harness.
// Constitution Principle I: all per-harness knowledge lives in these definition
// records (built-in, embedded as data, or loaded from harnesses.d), never in Go
// code that branches on a harness name.
package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Layer names the isolation layer an entry (or a key of a mixed file) belongs to.
type Layer string

const (
	LayerCredentials Layer = "credentials"
	LayerOwned       Layer = "owned"
	LayerState       Layer = "state"
	LayerMixed       Layer = "mixed"
	// The borrowable categories (feature 002): what a profile may use from the
	// default setup, depending on its share level.
	LayerContent     Layer = "content"
	LayerSettings    Layer = "settings"
	LayerPluginStore Layer = "plugin-store"
	// LayerHookLocation is the category of the definition's hook location. It is
	// declared in the `hooks` block, never in a classification rule.
	LayerHookLocation Layer = "hook-location"
	// LayerShared is the slice one layer. It is accepted only in user definitions,
	// as a deprecated alias for content (contracts/harness-definition.md).
	LayerShared Layer = "shared"
)

// Valid reports whether l is a recognized classification layer value.
func (l Layer) Valid() bool {
	switch l {
	case LayerCredentials, LayerOwned, LayerState, LayerMixed,
		LayerContent, LayerSettings, LayerPluginStore, LayerShared:
		return true
	}
	return false
}

// Borrowable reports whether entries of this layer may be taken from the default
// setup at some share level (content, settings, plugin store, hook location).
func (l Layer) Borrowable() bool {
	switch l {
	case LayerContent, LayerSettings, LayerPluginStore, LayerHookLocation, LayerShared:
		return true
	}
	return false
}

// Share levels (feature 002). The harness package keeps them as plain strings so
// it does not depend on the profile package.
const (
	LevelAll     = "all"
	LevelContent = "content"
	LevelNone    = "none"
)

// Levels lists the share levels in display order.
var Levels = []string{LevelAll, LevelContent, LevelNone}

// Treatments a share policy cell may name (contracts/harness-definition.md).
const (
	TreatLink     = "link"
	TreatOwn      = "own"
	TreatCopyOnce = "copy-once"
	TreatMinimal  = "minimal"
	TreatManaged  = "managed"
)

// LevelTreatment is one row of the share policy: the treatment per level.
type LevelTreatment struct {
	All     string `yaml:"all,omitempty" json:"all,omitempty"`
	Content string `yaml:"content,omitempty" json:"content,omitempty"`
	None    string `yaml:"none,omitempty" json:"none,omitempty"`
}

// UnmarshalYAML decodes one policy row and rejects keys that are not a level, so a
// typo never silently falls back to the default treatment.
func (lt *LevelTreatment) UnmarshalYAML(value *yaml.Node) error {
	var m map[string]string
	if err := value.Decode(&m); err != nil {
		return newFieldError("share_policy", "each category must map levels to treatments")
	}
	for k, v := range m {
		switch k {
		case LevelAll:
			lt.All = v
		case LevelContent:
			lt.Content = v
		case LevelNone:
			lt.None = v
		default:
			return newFieldError("share_policy", fmt.Sprintf("level %q must be all, content or none", k))
		}
	}
	return nil
}

// Get returns the treatment for one level.
func (lt LevelTreatment) Get(level string) string {
	switch level {
	case LevelAll:
		return lt.All
	case LevelContent:
		return lt.Content
	case LevelNone:
		return lt.None
	}
	return ""
}

// SharePolicy maps a borrowable category to its treatment per level. A definition
// may override single cells; DefaultSharePolicy fills the rest.
type SharePolicy map[Layer]LevelTreatment

// DefaultSharePolicy is the level table of the spec as data (Design Decision 2).
var DefaultSharePolicy = SharePolicy{
	LayerContent:      {All: TreatLink, Content: TreatLink, None: TreatOwn},
	LayerSettings:     {All: TreatLink, Content: TreatCopyOnce, None: TreatMinimal},
	LayerPluginStore:  {All: TreatLink, Content: TreatLink, None: TreatOwn},
	LayerHookLocation: {All: TreatLink, Content: TreatManaged, None: TreatManaged},
}

// Hook location forms.
const (
	HookFormEventGroups = "event-groups"
	HookFormFiles       = "files"
)

// HookLocation declares where a harness keeps its hooks and in which form (FR-024).
// For event-groups the location is "file#key"; for files it is a directory.
type HookLocation struct {
	Location string `yaml:"location" json:"location"`
	Form     string `yaml:"form" json:"form"`
}

// File returns the profile-relative file or directory of the hook location.
func (h *HookLocation) File() string {
	if i := strings.IndexByte(h.Location, '#'); i >= 0 {
		return h.Location[:i]
	}
	return strings.TrimSuffix(h.Location, "/")
}

// Key returns the key path inside the file for the event-groups form ("" for files).
func (h *HookLocation) Key() string {
	if i := strings.IndexByte(h.Location, '#'); i >= 0 {
		return h.Location[i+1:]
	}
	return ""
}

// HomeEscape is a location the harness reads relative to $HOME rather than its
// config dir, so no share level separates it (FR-020, FR-022).
type HomeEscape struct {
	Path string `yaml:"path" json:"path"`
	What string `yaml:"what" json:"what"`
}

// Definition is the full data record for one harness (FR-007).
type Definition struct {
	Name                  string     `yaml:"name" json:"name"`
	Binary                string     `yaml:"binary" json:"binary"`
	WrapperPrefix         string     `yaml:"wrapper_prefix" json:"wrapper_prefix"`
	ConfigEnv             ConfigEnv  `yaml:"config_env" json:"config_env"`
	DefaultConfigLocation string     `yaml:"default_config_location" json:"default_config_location,omitempty"`
	ModelVar              string     `yaml:"model_var" json:"model_var,omitempty"`
	EndpointVar           string     `yaml:"endpoint_var" json:"endpoint_var,omitempty"`
	AuthKinds             []AuthKind `yaml:"auth_kinds" json:"auth_kinds,omitempty"`
	// AuthClears lists environment variables that every auth kind clears unless the
	// kind sets the variable itself: provider switches and their settings that must
	// never leak from the caller's shell into a profile (FR-007, FR-027). The wrapper
	// and login unset them together with every auth kind's variables, then set the
	// selected kind's own; a hook's env operation may not set them.
	AuthClears      []string        `yaml:"auth_clears" json:"auth_clears,omitempty"`
	DefaultAuthKind string          `yaml:"default_auth_kind" json:"default_auth_kind,omitempty"`
	LoginCommand    *LoginCommand   `yaml:"login_command" json:"login_command,omitempty"`
	Classification  []EntryRule     `yaml:"classification" json:"classification,omitempty"`
	ConfigPatches   []ConfigPatch   `yaml:"config_patches" json:"config_patches,omitempty"`
	MCP             *MCPLocation    `yaml:"mcp" json:"mcp,omitempty"`
	SkillsLocation  *SkillsLocation `yaml:"skills_location" json:"skills_location,omitempty"`
	PluginMechanism string          `yaml:"plugin_mechanism" json:"plugin_mechanism,omitempty"`
	OmarchyProvider string          `yaml:"omarchy_provider" json:"omarchy_provider,omitempty"`
	Capabilities    []string        `yaml:"capabilities" json:"capabilities,omitempty"`
	ExecHook        *ExecHook       `yaml:"exec_hook" json:"exec_hook,omitempty"`
	// DefaultLocations maps a profile-relative prefix to its default-setup location,
	// for harnesses whose config is spread over several directories (OpenCode's XDG
	// set). An entry without a matching prefix lives under DefaultConfigLocation.
	DefaultLocations map[string]string `yaml:"default_locations" json:"default_locations,omitempty"`
	SharePolicy      SharePolicy       `yaml:"share_policy" json:"share_policy,omitempty"`
	Hooks            *HookLocation     `yaml:"hooks" json:"hooks,omitempty"`
	HomeEscapes      []HomeEscape      `yaml:"home_escapes" json:"home_escapes,omitempty"`
	// Backends maps a model backend name (cc-deck's `backend` field, e.g. vertex) to
	// the environment it implies. `aprofile import cc-deck` reads it (FR-045); a
	// backend the definition does not declare cannot be imported.
	Backends map[string]Backend `yaml:"backends" json:"backends,omitempty"`

	// Source records, per field, where the effective value came from:
	// "builtin" or an absolute file path. Populated by Load's override-merge; for a
	// pure built-in every entry is "builtin". Reported by aprofile harness show (FR-010).
	Source map[string]string `yaml:"-" json:"source,omitempty"`
}

// AuthKind declares one authentication mode and the environment variables it
// drives (FR-038).
type AuthKind struct {
	Name string `yaml:"name" json:"name"`
	// Vars maps each environment variable the kind sets to a shape marker. The
	// marker is "ref" when the variable expects a secret reference; a kind with no
	// vars (e.g. an interactive login) carries credentials in files/Keychain.
	Vars map[string]string `yaml:"vars" json:"vars,omitempty"`
}

// LoginCommand declares how `aprofile login` runs the harness's own login flow
// (FR-039): the binary is started with Args under the profile's environment. An
// empty Args list starts the bare binary, for harnesses whose sign-in happens inside
// a session; Note is printed before the launch to tell the user what to do.
type LoginCommand struct {
	Args []string `yaml:"args" json:"args"`
	Note string   `yaml:"note,omitempty" json:"note,omitempty"`
}

// VarNames returns the environment variable names this auth kind sets, sorted for
// deterministic rendering.
func (a AuthKind) VarNames() []string {
	names := make([]string, 0, len(a.Vars))
	for k := range a.Vars {
		names = append(names, k)
	}
	sortStrings(names)
	return names
}

// EntryRule classifies one config-dir entry (or glob) into a layer. A structured
// file may be declared mixed with a per-key layer map (FR-013).
type EntryRule struct {
	Entry string           `yaml:"entry" json:"entry"`
	Layer Layer            `yaml:"layer" json:"layer"`
	Keys  map[string]Layer `yaml:"keys,omitempty" json:"keys,omitempty"`
	// Database marks a rule whose entry is a database without a .sqlite/.db name;
	// its companion files share its layer (FR-004).
	Database bool `yaml:"database,omitempty" json:"database,omitempty"`

	// CompanionOf names the database rule a companion rule was derived from.
	CompanionOf string `yaml:"-" json:"companion_of,omitempty"`
	// Deprecated is set when a user rule used the slice one `shared` layer.
	Deprecated bool `yaml:"-" json:"deprecated,omitempty"`
	// Origin is "builtin" or the harnesses.d file the rule came from.
	Origin string `yaml:"-" json:"origin,omitempty"`
}

// IsGlob reports whether the rule's entry contains glob metacharacters.
func (r EntryRule) IsGlob() bool { return strings.ContainsAny(r.Entry, "*?[") }

// IsDir reports whether the rule names a directory (trailing slash).
func (r EntryRule) IsDir() bool { return strings.HasSuffix(r.Entry, "/") }

// Path returns the entry without its trailing slash, in slash form.
func (r EntryRule) Path() string { return strings.TrimSuffix(r.Entry, "/") }

// ConfigPatch is a merge-only patch applied to an existing config file (FR-019).
type ConfigPatch struct {
	File  string         `yaml:"file" json:"file"`
	Merge map[string]any `yaml:"merge" json:"merge,omitempty"`
}

// MCPLocation names where a harness keeps its MCP servers and the schema flavor
// used to read/write them.
type MCPLocation struct {
	Location string `yaml:"location" json:"location"`
	Flavor   string `yaml:"flavor" json:"flavor"`
}

// SkillsLocation names the skills directory and whether it is per-profile or a
// shared global location aprofile must not write (FR-021).
type SkillsLocation struct {
	Path  string `yaml:"path" json:"path"`
	Scope string `yaml:"scope" json:"scope"` // "per-profile" | "shared"
}

// ExecHook lists the operations the aprofile-harness-<name> executable implements
// (FR-011).
type ExecHook struct {
	Operations []string `yaml:"operations" json:"operations"`
}

// Treatment returns the effective treatment of a borrowable category at a share
// level: the definition's cell when set, otherwise the default policy.
func (d *Definition) Treatment(category Layer, level string) string {
	if category == LayerShared {
		category = LayerContent
	}
	if row, ok := d.SharePolicy[category]; ok {
		if t := row.Get(level); t != "" {
			return t
		}
	}
	return DefaultSharePolicy[category].Get(level)
}

// DefaultPath returns the default-setup location of a profile-relative entry: the
// longest matching default_locations prefix wins, otherwise the entry lives under
// default_config_location. A leading ~ is expanded to the current HOME.
func (d *Definition) DefaultPath(rel string) string {
	home, _ := os.UserHomeDir()
	return d.DefaultPathUnder(home, rel)
}

// DefaultPathUnder is DefaultPath with an explicit home directory for the ~
// expansion (tests seed fake default setups under a temporary home).
func (d *Definition) DefaultPathUnder(home, rel string) string {
	exp := func(p string) string { return expandHomeWith(home, p) }
	rel = strings.TrimSuffix(filepath.ToSlash(rel), "/")
	best := ""
	for prefix := range d.DefaultLocations {
		p := strings.TrimSuffix(prefix, "/")
		if (rel == p || strings.HasPrefix(rel, p+"/")) && len(p) > len(best) {
			best = p
		}
	}
	if best != "" {
		base := exp(d.DefaultLocations[best])
		rest := strings.TrimPrefix(strings.TrimPrefix(rel, best), "/")
		if rest == "" {
			return base
		}
		return filepath.Join(base, filepath.FromSlash(rest))
	}
	base := exp(d.DefaultConfigLocation)
	if rel == "" || rel == "." {
		return base
	}
	return filepath.Join(base, filepath.FromSlash(rel))
}

// DefaultRoots returns the default-setup directories with the profile-relative
// prefix each one corresponds to, sorted by prefix. Used to enumerate entries the
// default setup holds (FR-001a). A default_locations prefix may relocate a single
// entry (Claude Code keeps .claude.json at ~/.claude.json, outside ~/.claude); the
// entries no prefix covers then still live under default_config_location, which is
// a root too, with the empty prefix. A definition whose prefixes cover every
// classification rule (OpenCode's XDG set) gets no such root.
func (d *Definition) DefaultRoots() []DefaultRoot {
	var out []DefaultRoot
	if len(d.DefaultLocations) == 0 {
		return []DefaultRoot{{Prefix: "", Dir: ExpandHome(d.DefaultConfigLocation)}}
	}
	for prefix, loc := range d.DefaultLocations {
		out = append(out, DefaultRoot{Prefix: strings.TrimSuffix(prefix, "/"), Dir: ExpandHome(loc)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Prefix < out[j].Prefix })
	if d.DefaultConfigLocation != "" && d.hasEntryOutsideDefaultLocations() {
		out = append([]DefaultRoot{{Prefix: "", Dir: ExpandHome(d.DefaultConfigLocation)}}, out...)
	}
	return out
}

// hasEntryOutsideDefaultLocations reports whether a classification rule's entry lies
// under none of the default_locations prefixes.
func (d *Definition) hasEntryOutsideDefaultLocations() bool {
	covered := func(entry string) bool {
		e := strings.TrimSuffix(filepath.ToSlash(entry), "/")
		for prefix := range d.DefaultLocations {
			p := strings.TrimSuffix(filepath.ToSlash(prefix), "/")
			if e == p || strings.HasPrefix(e, p+"/") {
				return true
			}
		}
		return false
	}
	for _, r := range d.Classification {
		if !covered(r.Entry) {
			return true
		}
	}
	return false
}

// DefaultRoot pairs a default-setup directory with its profile-relative prefix.
type DefaultRoot struct {
	Prefix string
	Dir    string
}

// ExpandHome expands a leading ~ to the current HOME.
func ExpandHome(p string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return expandHomeWith(home, p)
}

func expandHomeWith(home, p string) string {
	if home != "" && (p == "~" || strings.HasPrefix(p, "~/")) {
		return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
	}
	return p
}

// HookLocationRule returns a synthetic rule for the hook location when it is an
// entry of its own (a separate hooks file or a directory), or nil when the hook
// location is a key of a file the classification already covers (for example
// Claude's settings.json#hooks).
func (d *Definition) HookLocationRule() *EntryRule {
	if d.Hooks == nil {
		return nil
	}
	file := d.Hooks.File()
	for _, r := range d.Classification {
		if !r.IsGlob() && r.Path() == file {
			return nil
		}
	}
	entry := file
	if d.Hooks.Form == HookFormFiles {
		entry += "/"
	}
	return &EntryRule{Entry: entry, Layer: LayerHookLocation, Origin: d.Source["hooks"]}
}

// HookKeyIn returns the hook location key when the hooks are a key of the file
// rel (event-groups form), or "" otherwise. The one-time settings copy, settings
// pull and copy leave this key out (FR-010, FR-018, FR-007a).
func (d *Definition) HookKeyIn(rel string) string {
	if d.Hooks == nil || d.Hooks.Form != HookFormEventGroups || d.Hooks.File() != strings.TrimSuffix(rel, "/") {
		return ""
	}
	return d.Hooks.Key()
}

// HookFileRule returns the classification rule of the file that holds the hook
// location key (event-groups form inside a classified file), or nil.
func (d *Definition) HookFileRule() *EntryRule {
	if d.Hooks == nil || d.Hooks.Form != HookFormEventGroups {
		return nil
	}
	file := d.Hooks.File()
	for i := range d.Classification {
		r := &d.Classification[i]
		if !r.IsGlob() && r.Path() == file {
			return r
		}
	}
	return nil
}

// AuthKind looks up a declared auth kind by name.
func (d *Definition) AuthKind(name string) (AuthKind, bool) {
	for _, k := range d.AuthKinds {
		if k.Name == name {
			return k, true
		}
	}
	return AuthKind{}, false
}

// AllAuthVars returns every environment variable named by any auth kind, sorted
// and de-duplicated. The wrapper unsets the ones that do not belong to the
// profile's selected kind (FR-027).
func (d *Definition) AllAuthVars() []string {
	seen := map[string]bool{}
	var out []string
	for _, k := range d.AuthKinds {
		for v := range k.Vars {
			if !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
	}
	sortStrings(out)
	return out
}

// UnsetAuthVars returns every variable the wrapper and login clear before setting
// the selected auth kind's own: all auth kinds' variables plus auth_clears, sorted
// and de-duplicated. A clear that the selected kind sets is still unset first and
// then set from the profile, so clears apply to every kind except the one that sets
// the variable (FR-027).
func (d *Definition) UnsetAuthVars() []string {
	all := d.AllAuthVars()
	seen := make(map[string]bool, len(all)+len(d.AuthClears))
	for _, v := range all {
		seen[v] = true
	}
	for _, v := range d.AuthClears {
		if !seen[v] {
			seen[v] = true
			all = append(all, v)
		}
	}
	sortStrings(all)
	return all
}

// LoginArgv returns the argv of the declared login command (binary first), or nil
// when the definition declares none (FR-039).
func (d *Definition) LoginArgv() []string {
	if d.LoginCommand == nil {
		return nil
	}
	return append([]string{d.Binary}, d.LoginCommand.Args...)
}

// HasCapability reports whether the definition declares the given capability.
func (d *Definition) HasCapability(cap string) bool {
	for _, c := range d.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
}

// Backend declares the environment one model backend implies (FR-045). Env values
// may name the placeholders in BackendPlaceholders; CredentialsVar, when set, is the
// variable that receives the backend's credentials file (a path or a reference).
type Backend struct {
	Env            BackendEnv `yaml:"env" json:"env,omitempty"`
	CredentialsVar string     `yaml:"credentials_var" json:"credentials_var,omitempty"`
}

// BackendPlaceholders are the placeholders a backend env value may use, written as
// {name}. Import fills them from the cc-deck profile's fields of the same name.
var BackendPlaceholders = []string{"project", "region"}

// Expand returns the backend's variables with every {placeholder} replaced by its
// value in values (a missing one by ""). A variable whose value expands to empty is
// left out, as cc-deck's wrapper does.
func (b Backend) Expand(values map[string]string) BackendEnv {
	pairs := make([]string, 0, 2*len(BackendPlaceholders))
	for _, ph := range BackendPlaceholders {
		pairs = append(pairs, "{"+ph+"}", values[ph])
	}
	r := strings.NewReplacer(pairs...)
	var out BackendEnv
	for _, e := range b.Env {
		if v := r.Replace(e.Value); v != "" {
			out = append(out, BackendEnvVar{Name: e.Name, Value: v})
		}
	}
	return out
}

// BackendEnvVar is one variable a backend sets.
type BackendEnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// BackendEnv is an ordered list of backend variables, written as a YAML mapping
// whose key order is kept.
type BackendEnv []BackendEnvVar

// UnmarshalYAML decodes a YAML mapping into an ordered BackendEnv.
func (b *BackendEnv) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return newFieldError("backends", "env must be a mapping of variable to value")
	}
	var out BackendEnv
	for i := 0; i+1 < len(value.Content); i += 2 {
		k, v := value.Content[i], value.Content[i+1]
		if v.Kind != yaml.ScalarNode {
			return newFieldError("backends", fmt.Sprintf("env %q must be a string value", k.Value))
		}
		out = append(out, BackendEnvVar{Name: k.Value, Value: v.Value})
	}
	*b = out
	return nil
}

// MarshalYAML encodes BackendEnv back to a mapping, preserving order.
func (b BackendEnv) MarshalYAML() (any, error) {
	n := &yaml.Node{Kind: yaml.MappingNode}
	for _, e := range b {
		n.Content = append(n.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: e.Name},
			&yaml.Node{Kind: yaml.ScalarNode, Value: e.Value, Style: yaml.DoubleQuotedStyle},
		)
	}
	return n, nil
}

// ConfigEnvEntry is one variable->path redirect.
type ConfigEnvEntry struct {
	Var  string `json:"var"`
	Path string `json:"path"` // relative to the profile root; "." is the root itself
}

// ConfigEnv is an ordered list of variable->path redirects (FR-020). Order is
// preserved from the definition file so wrapper output is deterministic.
type ConfigEnv []ConfigEnvEntry

// UnmarshalYAML decodes a YAML mapping into an ordered ConfigEnv, preserving the
// key order in the file.
func (c *ConfigEnv) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return newFieldError("config_env", "must be a mapping of variable to path")
	}
	var out ConfigEnv
	for i := 0; i+1 < len(value.Content); i += 2 {
		keyNode := value.Content[i]
		valNode := value.Content[i+1]
		out = append(out, ConfigEnvEntry{Var: keyNode.Value, Path: valNode.Value})
	}
	*c = out
	return nil
}

// MarshalYAML encodes ConfigEnv back to a mapping, preserving order.
func (c ConfigEnv) MarshalYAML() (any, error) {
	n := &yaml.Node{Kind: yaml.MappingNode}
	for _, e := range c {
		n.Content = append(n.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: e.Var},
			&yaml.Node{Kind: yaml.ScalarNode, Value: e.Path},
		)
	}
	return n, nil
}
