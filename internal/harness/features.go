// The capability mapping (spec 006, FR-005): per harness, which catalog features
// exist, which parameters they take (including per-harness defaults and enums),
// and the placement rules each enabled feature materializes into the harness's
// own settings files. Lives in the definition records (the features: key),
// overridable through harnesses.d like every other definition section
// (Principle I); the cross-harness catalog it references is Go code
// (internal/feature).

package harness

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/cc-deck/aprofile/internal/feature"
	"github.com/cc-deck/aprofile/internal/secret"
)

// FeatureMapping is one feature's capability statement on a harness.
type FeatureMapping struct {
	// Params maps each supported catalog param to its per-harness declaration:
	// a default value and, for enum params, the harness's value list.
	Params map[string]FeatureParam `yaml:"params,omitempty" json:"params,omitempty"`
	// Place lists, in order, the entries an enabled feature places. Rules for the
	// same location must be mutually exclusive (when/when-not gates); the last
	// applicable rule wins.
	Place []FeaturePlace `yaml:"place" json:"place"`
}

// FeatureParam declares a param's per-harness default and enum values.
type FeatureParam struct {
	Default string   `yaml:"default,omitempty" json:"default,omitempty"`
	Values  []string `yaml:"values,omitempty" json:"values,omitempty"`
}

// FeaturePlace is one placement rule: an entry in a classified settings file at
// a launch-record location, with a value (FR-012's nested TOML subtrees included).
type FeaturePlace struct {
	// File is a profile-relative settings path the classification covers.
	File string `yaml:"file" json:"file"`
	// Location is the launch-record location: env.<NAME> for JSON env entries,
	// a dotted TOML path (otel.exporter) for nested TOML keys.
	Location string       `yaml:"location" json:"location"`
	Value    FeatureValue `yaml:"value" json:"value"`
	// When names a bool param; the rule applies only when it is on. WhenNot is
	// the complement. A signal that is off places its exporter entry as none,
	// so a mapping covers both branches of each signal (FR-011).
	When    string `yaml:"when,omitempty" json:"when,omitempty"`
	WhenNot string `yaml:"when-not,omitempty" json:"when_not,omitempty"`
}

// FeatureValue is a placement value: a literal string, a {param: <name>}
// reference, a switch on a param selecting per-value structures (Codex's
// externally-tagged exporter tables), or a nested structure whose string leaves
// may be {param: <name>} references. Exactly one of Str, Switch or Struct is
// set.
type FeatureValue struct {
	Str    string                  `yaml:"-" json:"-"`
	Switch string                  `yaml:"-" json:"-"`
	Cases  map[string]FeatureValue `yaml:"-" json:"-"`
	Struct map[string]any          `yaml:"-" json:"-"`
	// IsStruct marks the Struct form (an empty mapping is a valid structure).
	IsStruct bool `yaml:"-" json:"-"`
}

// UnmarshalYAML decodes a scalar (literal or reference), a switch mapping
// (exactly the keys switch and cases), or an arbitrary nested structure.
func (v *FeatureValue) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		v.Str = value.Value
		return nil
	case yaml.MappingNode:
		keys := map[string]bool{}
		for i := 0; i+1 < len(value.Content); i += 2 {
			keys[value.Content[i].Value] = true
		}
		if len(keys) == 2 && keys["switch"] && keys["cases"] {
			sw := findMapValue(value, "switch")
			if sw == nil || sw.Kind != yaml.ScalarNode {
				return newFieldError("features", "switch must name a param")
			}
			v.Switch = sw.Value
			cases := findMapValue(value, "cases")
			if cases == nil || cases.Kind != yaml.MappingNode {
				return newFieldError("features", "cases must be a mapping of param value to value")
			}
			v.Cases = map[string]FeatureValue{}
			for i := 0; i+1 < len(cases.Content); i += 2 {
				var cv FeatureValue
				if err := cases.Content[i+1].Decode(&cv); err != nil {
					return err
				}
				v.Cases[cases.Content[i].Value] = cv
			}
			return nil
		}
		var m map[string]any
		if err := value.Decode(&m); err != nil {
			return newFieldError("features", "value must be a string, a switch or a mapping")
		}
		v.Struct = m
		v.IsStruct = true
		return nil
	}
	return newFieldError("features", "value must be a string, a switch or a mapping")
}

// MarshalYAML re-encodes a placement value (used when a harnesses.d override is
// merged back onto a built-in node).
func (v FeatureValue) MarshalYAML() (any, error) {
	switch {
	case v.Switch != "" || v.Cases != nil:
		n := &yaml.Node{Kind: yaml.MappingNode}
		n.Content = append(n.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "switch"},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v.Switch},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "cases"},
		)
		cases := &yaml.Node{Kind: yaml.MappingNode}
		keys := make([]string, 0, len(v.Cases))
		for k := range v.Cases {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			cv, err := v.Cases[k].MarshalYAML()
			if err != nil {
				return nil, err
			}
			cases.Content = append(cases.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k})
			cn := &yaml.Node{}
			if err := cn.Encode(cv); err != nil {
				return nil, err
			}
			cases.Content = append(cases.Content, cn)
		}
		n.Content = append(n.Content, cases)
		return n, nil
	case v.IsStruct:
		return v.Struct, nil
	default:
		return v.Str, nil
	}
}

// paramRefRE matches a whole-string {param: <name>} reference.
var paramRefRE = regexp.MustCompile(`^\{param:([A-Za-z0-9_-]+)\}$`)

// validateFeatures checks the features: block of a definition (T004): feature
// ids must exist in the catalog, param names must match the catalog, enum
// defaults must be inside the values, placement files must be classified
// settings paths, and gates and references must name declared params. It returns
// the offending field and a message, or two empty strings when valid.
func validateFeatures(d *Definition) (field, msg string) {
	const f = "features"
	ids := make([]string, 0, len(d.Features))
	for id := range d.Features {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		fm := d.Features[id]
		cat, ok := feature.ByID(id)
		if !ok {
			return f, fmt.Sprintf("feature %q is not in the catalog", id)
		}
		if len(fm.Place) == 0 {
			return f, fmt.Sprintf("feature %q declares no placement rules", id)
		}
		for name, fp := range fm.Params {
			cp, ok := cat.Param(name)
			if !ok {
				return f, fmt.Sprintf("feature %q has no param %q", id, name)
			}
			if cp.Type != feature.TypeEnum && len(fp.Values) > 0 {
				return f, fmt.Sprintf("feature %q param %q: only enum params declare values", id, name)
			}
			if cp.Type == feature.TypeEnum && len(fp.Values) == 0 {
				return f, fmt.Sprintf("feature %q param %q: an enum param must declare its values", id, name)
			}
			if fp.Default == "" {
				continue
			}
			switch {
			case cp.Type == feature.TypeBool && fp.Default != "on" && fp.Default != "off":
				return f, fmt.Sprintf("feature %q param %q: default %q must be on or off", id, name, fp.Default)
			case cp.Type == feature.TypeEnum && !containsString(fp.Values, fp.Default):
				return f, fmt.Sprintf("feature %q param %q: default %q is not one of its values", id, name, fp.Default)
			}
		}
		for i, r := range fm.Place {
			rule := fmt.Sprintf("feature %q place rule %d", id, i+1)
			if r.File == "" || !entryPathSafe(r.File) {
				return f, fmt.Sprintf("%s: file %q must be a relative path within the profile dir", rule, r.File)
			}
			if !classifiedFile(d, r.File) {
				return f, fmt.Sprintf("%s: file %s is not a classified settings path", rule, r.File)
			}
			if m := validateFeatureLocation(r.File, r.Location); m != "" {
				return f, fmt.Sprintf("%s: %s", rule, m)
			}
			if r.When != "" && r.WhenNot != "" {
				return f, fmt.Sprintf("%s: when and when-not cannot both be set", rule)
			}
			for what, gate := range map[string]string{"when": r.When, "when-not": r.WhenNot} {
				if gate == "" {
					continue
				}
				cp, ok := cat.Param(gate)
				if !ok {
					return f, fmt.Sprintf("%s: %s names unknown param %q", rule, what, gate)
				}
				if cp.Type != feature.TypeBool {
					return f, fmt.Sprintf("%s: %s must name a bool param, not %q (%s)", rule, what, gate, cp.Type)
				}
			}
			if m := r.Value.validate(cat, rule); m != "" {
				return f, m
			}
		}
	}
	return "", ""
}

// validate checks one placement value against the feature's catalog params.
func (v FeatureValue) validate(cat feature.Feature, rule string) string {
	checkRef := func(s string) string {
		m := paramRefRE.FindStringSubmatch(s)
		if m == nil {
			return ""
		}
		if _, ok := cat.Param(m[1]); !ok {
			return fmt.Sprintf("%s: value references unknown param %q", rule, m[1])
		}
		return ""
	}
	switch {
	case v.Switch != "" || v.Cases != nil:
		cp, ok := cat.Param(v.Switch)
		if !ok {
			return fmt.Sprintf("%s: switch names unknown param %q", rule, v.Switch)
		}
		if cp.Type != feature.TypeEnum {
			return fmt.Sprintf("%s: switch must name an enum param, not %q (%s)", rule, v.Switch, cp.Type)
		}
		keys := make([]string, 0, len(v.Cases))
		for k := range v.Cases {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if !containsString(cp.Values, k) {
				return fmt.Sprintf("%s: switch case %q is not one of the values of %s", rule, k, v.Switch)
			}
			if m := v.Cases[k].validate(cat, rule); m != "" {
				return m
			}
		}
		return ""
	case v.IsStruct:
		return validateStructValue(v.Struct, cat, rule)
	default:
		return checkRef(v.Str)
	}
}

// validateStructValue walks a nested structure checking every string leaf's
// param references.
func validateStructValue(m map[string]any, cat feature.Feature, rule string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		switch t := m[k].(type) {
		case string:
			if m := checkStructRef(t, cat, rule); m != "" {
				return m
			}
		case map[string]any:
			if m := validateStructValue(t, cat, rule); m != "" {
				return m
			}
		case nil:
			return fmt.Sprintf("%s: key %q is null; a placement value only sets keys", rule, k)
		}
	}
	return ""
}

func checkStructRef(s string, cat feature.Feature, rule string) string {
	m := paramRefRE.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	if _, ok := cat.Param(m[1]); !ok {
		return fmt.Sprintf("%s: value references unknown param %q", rule, m[1])
	}
	return ""
}

// tomlKeyRE matches a TOML bare key segment.
var tomlKeyRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// validateFeatureLocation checks a placement location for its file's format:
// env.<NAME> with a valid variable name for JSON files, a dotted path of TOML
// bare keys for TOML files.
func validateFeatureLocation(file, loc string) string {
	if loc == "" || strings.Contains(loc, "#") {
		return fmt.Sprintf("location %q must be a non-empty path without #", loc)
	}
	segs := strings.Split(loc, ".")
	if strings.EqualFold(fileExt(file), ".json") {
		if len(segs) != 2 || segs[0] != "env" || !secret.ValidEnvVarName(segs[1]) {
			return fmt.Sprintf("location %q must be env.<NAME> with a valid variable name", loc)
		}
		return ""
	}
	for _, s := range segs {
		if !tomlKeyRE.MatchString(s) {
			return fmt.Sprintf("location %q must be a dotted path of TOML keys", loc)
		}
	}
	return ""
}

// classifiedFile reports whether a profile-relative file has its own
// non-glob classification rule.
func classifiedFile(d *Definition, file string) bool {
	for _, r := range d.Classification {
		if !r.IsGlob() && r.Path() == file {
			return true
		}
	}
	return false
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func fileExt(file string) string {
	if i := strings.LastIndexByte(file, '.'); i >= 0 {
		return file[i:]
	}
	return ""
}

// FeatureMapping returns the capability mapping of a feature, or ok=false when
// the harness does not support it.
func (d *Definition) FeatureMapping(id string) (FeatureMapping, bool) {
	fm, ok := d.Features[id]
	return fm, ok
}

// SupportedFeatures lists the catalog features the harness declares, sorted by
// id.
func (d *Definition) SupportedFeatures() []string {
	var out []string
	for id := range d.Features {
		if _, ok := feature.ByID(id); ok {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// SupportsFeature reports whether the harness declares the catalog feature.
func (d *Definition) SupportsFeature(id string) bool {
	_, ok := d.Features[id]
	return ok
}

// ResolveFeatureParams resolves a feature's stored params against the
// capability mapping: values given (non-empty) are validated and kept, missing
// ones take the mapping's default, bool params default to off, and an enum
// param with neither value nor default is an error (FR-004). An unsupported
// feature or an invalid value returns an error naming the harness, feature and
// param; callers treat it as "place nothing".
func (d *Definition) ResolveFeatureParams(id string, params map[string]string) (map[string]string, error) {
	fm, ok := d.Features[id]
	if !ok {
		return nil, fmt.Errorf("harness %s does not support feature %q", d.Name, id)
	}
	cat, ok := feature.ByID(id)
	if !ok {
		return nil, fmt.Errorf("unknown feature %q", id)
	}
	out := make(map[string]string, len(cat.Params))
	for _, cp := range cat.Params {
		v := params[cp.Name]
		if v == "" {
			v = fm.Params[cp.Name].Default
		}
		switch cp.Type {
		case feature.TypeBool:
			if v == "" {
				v = "off"
			}
			if v != "on" && v != "off" {
				return nil, fmt.Errorf("harness %s: feature %q param %q must be on or off, not %q", d.Name, id, cp.Name, v)
			}
		case feature.TypeEnum:
			if v == "" {
				return nil, fmt.Errorf("harness %s: feature %q param %q has no value and no default", d.Name, id, cp.Name)
			}
			if !containsString(fm.Params[cp.Name].Values, v) {
				return nil, fmt.Errorf("harness %s: feature %q param %q does not accept %q (values: %s)", d.Name, id, cp.Name, v, strings.Join(fm.Params[cp.Name].Values, ", "))
			}
		}
		out[cp.Name] = v
	}
	for name := range params {
		if _, ok := cat.Param(name); !ok {
			return nil, fmt.Errorf("feature %q has no param %q", id, name)
		}
	}
	return out, nil
}

// FeatureEnvNames lists, sorted, every environment variable a feature places on
// this harness (the place rules with env. locations), record-independent, so
// mutation-time collision checks (FR-015) and drift reporting share one source.
func (d *Definition) FeatureEnvNames(id string) []string {
	var out []string
	fm, ok := d.Features[id]
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	for _, r := range fm.Place {
		if name, ok := strings.CutPrefix(r.Location, "env."); ok && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// FeatureLocations lists, sorted by file then location, every location any
// feature of this harness may place (record-independent), for drift reporting.
func (d *Definition) FeatureLocations() []EntryLocation {
	var out []EntryLocation
	ids := d.SupportedFeatures()
	for _, id := range ids {
		for _, r := range d.Features[id].Place {
			out = append(out, EntryLocation{Feature: id, File: r.File, Location: r.Location})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Location < out[j].Location
	})
	return out
}

// EntryLocation names one placeable location of a feature.
type EntryLocation struct {
	Feature  string
	File     string
	Location string
}
