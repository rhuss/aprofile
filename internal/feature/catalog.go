// Package feature holds the cross-harness feature catalog: the feature ids,
// their named and typed parameters, and their display groups (spec 006, FR-005).
// The catalog is harness-independent vocabulary, like the share levels; which
// features a harness supports, their per-harness defaults and enums, and the
// entries an enabled feature places live in the harness definition records
// (internal/harness), so a catalog entry never branches on a harness name.
package feature

import (
	"fmt"
	"sort"
	"strings"
)

// ParamType is the type of a feature parameter.
type ParamType string

const (
	// TypeBool params hold "on" or "off".
	TypeBool ParamType = "bool"
	// TypeString params hold a free string (an OTLP endpoint URL).
	TypeString ParamType = "string"
	// TypeEnum params hold one of a declared set of values. The catalog's Values
	// field is the cross-harness union; the per-harness list and its validation
	// live in the capability mapping.
	TypeEnum ParamType = "enum"
)

// Param is one named, typed parameter of a feature.
type Param struct {
	Name string
	Type ParamType
	// Values lists the enum's cross-harness union (TypeEnum only).
	Values []string
}

// HasValue reports whether v is one of the param's enum values.
func (p Param) HasValue(v string) bool {
	for _, w := range p.Values {
		if w == v {
			return true
		}
	}
	return false
}

// Feature is one catalog entry: a stable id, a display group, its ordered
// parameters, and its enable-time constraints. Parameterless features
// (automode-log) have no params.
type Feature struct {
	ID     string
	Group  string
	Params []Param
	// RequireOneOf lists bool params of which at least one must be on when the
	// feature is enabled (telemetry needs a signal).
	RequireOneOf []string
	// Required lists params that must carry a non-empty value when the feature
	// is enabled (telemetry needs an endpoint).
	Required []string
}

// CheckConstraints validates a feature's resolved params against its
// enable-time constraints: at least one RequireOneOf param on, and every
// Required param non-empty. It returns nil when the params satisfy them, so the
// mutation path stays feature-agnostic (FR-006: the constraints are data, not
// per-feature command code).
func (f Feature) CheckConstraints(params map[string]string) error {
	if len(f.RequireOneOf) > 0 {
		on := false
		for _, name := range f.RequireOneOf {
			if params[name] == "on" {
				on = true
				break
			}
		}
		if !on {
			return fmt.Errorf("%s needs at least one of %s on; pass --%s on and/or --%s on",
				f.ID, strings.Join(f.RequireOneOf, ", "), f.RequireOneOf[0], f.RequireOneOf[len(f.RequireOneOf)-1])
		}
	}
	for _, name := range f.Required {
		if params[name] == "" {
			return fmt.Errorf("%s needs a value for %s; pass --%s <value>", f.ID, name, name)
		}
	}
	return nil
}

// Param returns the named parameter of the feature, or ok=false.
func (f Feature) Param(name string) (Param, bool) {
	for _, p := range f.Params {
		if p.Name == name {
			return p, true
		}
	}
	return Param{}, false
}

// catalog is the built-in v1 feature list (FR-003: exactly telemetry and
// automode-log). Extending it is a data change: no command code branches on
// these ids (FR-006).
var catalog = []Feature{
	{
		ID:           "telemetry",
		Group:        "telemetry",
		RequireOneOf: []string{"traces", "logs"},
		Required:     []string{"endpoint"},
		Params: []Param{
			{Name: "traces", Type: TypeBool},
			{Name: "logs", Type: TypeBool},
			{Name: "endpoint", Type: TypeString},
			{
				Name:   "protocol",
				Type:   TypeEnum,
				Values: []string{"grpc", "http/json", "http/protobuf", "otlp-http json", "otlp-http binary", "otlp-grpc"},
			},
		},
	},
	{
		ID:    "automode-log",
		Group: "logging",
	},
}

// All returns the whole catalog, ordered by feature id.
func All() []Feature {
	out := append([]Feature(nil), catalog...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ByID returns the feature with the given id, or ok=false.
func ByID(id string) (Feature, bool) {
	for _, f := range catalog {
		if f.ID == id {
			return f, true
		}
	}
	return Feature{}, false
}

// ByGroup returns the catalog features of a display group, ordered by id.
func ByGroup(group string) []Feature {
	var out []Feature
	for _, f := range catalog {
		if f.Group == group {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Groups lists the display groups in catalog order (telemetry, logging), the
// order the status output and its --json schema report them
// (contracts/features-cli.md).
func Groups() []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range catalog {
		if !seen[f.Group] {
			seen[f.Group] = true
			out = append(out, f.Group)
		}
	}
	return out
}
