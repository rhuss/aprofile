// Package harness holds the data-driven description of each coding harness.
// Constitution Principle I: all per-harness knowledge lives in these definition
// records (built-in, embedded as data, or loaded from harnesses.d), never in Go
// code that branches on a harness name.
package harness

import (
	"gopkg.in/yaml.v3"
)

// Layer names the isolation layer an entry (or a key of a mixed file) belongs to.
type Layer string

const (
	LayerCredentials Layer = "credentials"
	LayerOwned       Layer = "owned"
	LayerState       Layer = "state"
	LayerShared      Layer = "shared"
	LayerMixed       Layer = "mixed"
)

// Valid reports whether l is a recognized layer value.
func (l Layer) Valid() bool {
	switch l {
	case LayerCredentials, LayerOwned, LayerState, LayerShared, LayerMixed:
		return true
	}
	return false
}

// Definition is the full data record for one harness (FR-007).
type Definition struct {
	Name                  string          `yaml:"name" json:"name"`
	Binary                string          `yaml:"binary" json:"binary"`
	WrapperPrefix         string          `yaml:"wrapper_prefix" json:"wrapper_prefix"`
	ConfigEnv             ConfigEnv       `yaml:"config_env" json:"config_env"`
	DefaultConfigLocation string          `yaml:"default_config_location" json:"default_config_location,omitempty"`
	ModelVar              string          `yaml:"model_var" json:"model_var,omitempty"`
	EndpointVar           string          `yaml:"endpoint_var" json:"endpoint_var,omitempty"`
	AuthKinds             []AuthKind      `yaml:"auth_kinds" json:"auth_kinds,omitempty"`
	DefaultAuthKind       string          `yaml:"default_auth_kind" json:"default_auth_kind,omitempty"`
	Classification        []EntryRule     `yaml:"classification" json:"classification,omitempty"`
	ConfigPatches         []ConfigPatch   `yaml:"config_patches" json:"config_patches,omitempty"`
	MCP                   *MCPLocation    `yaml:"mcp" json:"mcp,omitempty"`
	SkillsLocation        *SkillsLocation `yaml:"skills_location" json:"skills_location,omitempty"`
	PluginMechanism       string          `yaml:"plugin_mechanism" json:"plugin_mechanism,omitempty"`
	OmarchyProvider       string          `yaml:"omarchy_provider" json:"omarchy_provider,omitempty"`
	Capabilities          []string        `yaml:"capabilities" json:"capabilities,omitempty"`
	ExecHook              *ExecHook       `yaml:"exec_hook" json:"exec_hook,omitempty"`

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
}

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

// HasCapability reports whether the definition declares the given capability.
func (d *Definition) HasCapability(cap string) bool {
	for _, c := range d.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
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
