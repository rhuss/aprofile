// Feature-derived desired entries (spec 006, T007): resolving a profile's
// feature states against its harness's capability mapping into the concrete
// entries sync places, honoring when/when-not gates, per-harness defaults and
// param references. Placement itself goes through the same planJSON/planTOML
// record maintenance every launch entry uses (FR-010, FR-018).

package launch

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/profile"
)

// DesiredEntry is one feature-derived desired entry with its resolved value:
// a string for JSON env entries, any TOML value (string, nested table) for
// nested TOML keys (FR-012).
type DesiredEntry struct {
	File     string
	Location string
	Value    any
	Feature  string
}

// FeatureEnvNames lists, sorted, every environment variable a feature places on
// the harness (record-independent), so the mutation-time collision checks
// (FR-015) and drift reporting share one source (research R6).
func FeatureEnvNames(def *harness.Definition, feature string) []string {
	return def.FeatureEnvNames(feature)
}

// FeatureDesired resolves a profile's Features against the harness capability
// mapping into concrete desired entries (file, location, value). A feature the
// harness does not support, or whose stored state fails the mapping's
// validation, places nothing (data-model "Feature state"); the load warnings
// and the features status carry the reason. Later rules for the same location
// win, so a mapping covers both branches of a gated signal.
func FeatureDesired(def *harness.Definition, p *profile.Profile) []DesiredEntry {
	if len(p.Features) == 0 || len(def.Features) == 0 {
		return nil
	}
	var out []DesiredEntry
	perFeature := map[string][]DesiredEntry{}
	for _, fs := range p.Features {
		fm, ok := def.FeatureMapping(fs.Feature)
		if !ok {
			continue
		}
		params, err := def.ResolveFeatureParams(fs.Feature, fs.Params)
		if err != nil {
			continue // invalid stored state: place nothing
		}
		var entries []DesiredEntry
		for _, r := range fm.Place {
			if !ruleGated(r, params) {
				continue
			}
			v, ok, err := resolveFeatureValue(r.Value, params)
			if err != nil || !ok {
				// A switch case the param does not select, or an unresolvable
				// reference: the feature cannot be placed as stored.
				entries = nil
				break
			}
			entries = append(entries, DesiredEntry{File: r.File, Location: r.Location, Value: v, Feature: fs.Feature})
		}
		if len(entries) > 0 {
			perFeature[fs.Feature] = entries
		}
	}
	ids := make([]string, 0, len(perFeature))
	for id := range perFeature {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		out = append(out, perFeature[id]...)
	}
	return out
}

// ruleGated reports whether a placement rule applies under the resolved params:
// when requires the bool param on, when-not requires it off.
func ruleGated(r harness.FeaturePlace, params map[string]string) bool {
	if r.When != "" && params[r.When] != "on" {
		return false
	}
	if r.WhenNot != "" && params[r.WhenNot] != "off" {
		return false
	}
	return true
}

// featParamRefRE matches a whole-string {param: <name>} reference in a value.
var featParamRefRE = regexp.MustCompile(`^\{param:([A-Za-z0-9_-]+)\}$`)

// resolveFeatureValue resolves a placement value against the params: a literal
// string, a {param:...} reference, a switch selecting per-value structures, or a
// nested structure whose string leaves may be references. ok=false when a
// switch's param value has no case (the feature places nothing).
func resolveFeatureValue(v harness.FeatureValue, params map[string]string) (any, bool, error) {
	switch {
	case v.Switch != "" || v.Cases != nil:
		cv, ok := v.Cases[params[v.Switch]]
		if !ok {
			return nil, false, nil
		}
		return resolveFeatureValue(cv, params)
	case v.IsStruct:
		m, err := resolveStructValue(v.Struct, params)
		return m, true, err
	default:
		if m := featParamRefRE.FindStringSubmatch(v.Str); m != nil {
			val, ok := params[m[1]]
			if !ok || val == "" {
				return nil, false, fmt.Errorf("param %q has no value", m[1])
			}
			return val, true, nil
		}
		return v.Str, true, nil
	}
}

// resolveStructValue walks a nested structure replacing whole-string param
// references at the leaves.
func resolveStructValue(in map[string]any, params map[string]string) (map[string]any, error) {
	out := make(map[string]any, len(in))
	for k, val := range in {
		switch t := val.(type) {
		case string:
			if m := featParamRefRE.FindStringSubmatch(t); m != nil {
				pv, ok := params[m[1]]
				if !ok || pv == "" {
					return nil, fmt.Errorf("param %q has no value", m[1])
				}
				out[k] = pv
				continue
			}
			out[k] = t
		case map[string]any:
			m, err := resolveStructValue(t, params)
			if err != nil {
				return nil, err
			}
			out[k] = m
		default:
			out[k] = val
		}
	}
	return out, nil
}

// featureFiles lists the profile-relative files any feature placement rule
// names, de-duplicated, sorted.
func featureFiles(def *harness.Definition) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range def.FeatureLocations() {
		if !seen[l.File] {
			seen[l.File] = true
			out = append(out, l.File)
		}
	}
	return out
}

// envFeatureEntries splits the feature entries whose location is an entry of
// the folder env block (env.<NAME> in the env file) from the rest: the former
// ride the existing env placement path, the latter the generic nested paths.
func envFeatureEntries(fl *harness.FolderLaunch, def *harness.Definition, entries []DesiredEntry) (env []envEntry, rest []DesiredEntry) {
	ek := ""
	if fl != nil && fl.Env != "" {
		ek = fl.EnvKey()
	}
	for _, e := range entries {
		if ek != "" && e.File == fl.EnvFile() {
			if name, ok := strings.CutPrefix(e.Location, ek+"."); ok {
				s, isStr := e.Value.(string)
				if !isStr {
					continue // an env entry must be a string; treat as generic
				}
				env = append(env, envEntry{name, s})
				continue
			}
		}
		rest = append(rest, e)
	}
	return env, rest
}

// featureLocationsOf returns the set of "file#location" keys a harness's
// features may place (record-independent), for classifying record entries.
func featureLocationSet(def *harness.Definition) map[string]bool {
	out := map[string]bool{}
	for _, l := range def.FeatureLocations() {
		out[l.File+"#"+l.Location] = true
	}
	return out
}

// featureEntriesIn filters the desired entries of one file.
func featureEntriesIn(entries []DesiredEntry, file string) []DesiredEntry {
	var out []DesiredEntry
	for _, e := range entries {
		if e.File == file {
			out = append(out, e)
		}
	}
	return out
}
