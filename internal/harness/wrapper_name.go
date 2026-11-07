package harness

import (
	"fmt"
	"sort"
	"strings"
)

// ProfileKey is the (harness, name) identity of a profile (FR-002), used where the
// harness package must reason about profiles without importing the profile store.
type ProfileKey struct {
	Harness string
	Name    string
}

// ID returns "<harness>/<name>".
func (k ProfileKey) ID() string { return k.Harness + "/" + k.Name }

// WrapperName returns the wrapper name `<wrapper_prefix>-<name>` of a profile of
// harness harnessName, or false when the set has no definition for that harness.
func (s *Set) WrapperName(harnessName, name string) (string, bool) {
	d, ok := s.Get(harnessName)
	if !ok {
		return "", false
	}
	return d.WrapperName(name), true
}

// WrapperName returns the wrapper name `<wrapper_prefix>-<name>` of the definition's
// profile name (FR-002). It is the single place that spells the wrapper name.
func (d *Definition) WrapperName(name string) string { return d.WrapperPrefix + "-" + name }

// WrapperClash returns the first profile among others (in ID order) whose wrapper
// name equals the wrapper name the profile (harnessName, name) would get, or false
// when there is none (FR-002). The profile itself and the profile except (an ID
// "<harness>/<name>", for example the old identity of a renamed profile; "" for
// none) are never reported. Profiles of a harness without a definition have no
// wrapper and never clash.
func (s *Set) WrapperClash(harnessName, name string, others []ProfileKey, except string) (ProfileKey, bool) {
	want, ok := s.WrapperName(harnessName, name)
	if !ok {
		return ProfileKey{}, false
	}
	sorted := append([]ProfileKey(nil), others...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID() < sorted[j].ID() })
	for _, o := range sorted {
		if (o.Harness == harnessName && o.Name == name) || o.ID() == except {
			continue
		}
		if got, ok := s.WrapperName(o.Harness, o.Name); ok && got == want {
			return o, true
		}
	}
	return ProfileKey{}, false
}

// checkPrefixClashes reports every wrapper_prefix that more than one effective
// definition declares as a load error (FR-002). The definitions stay in the set: a
// clash only matters for profiles whose wrapper names coincide, which add, rename,
// copy and import reject and sync skips, so a user override never makes every
// profile of a harness unusable.
func checkPrefixClashes(set *Set) {
	byPrefix := map[string][]string{}
	for _, d := range set.defs {
		byPrefix[d.WrapperPrefix] = append(byPrefix[d.WrapperPrefix], d.Name)
	}
	prefixes := make([]string, 0, len(byPrefix))
	for p := range byPrefix {
		prefixes = append(prefixes, p)
	}
	sort.Strings(prefixes)
	for _, p := range prefixes {
		names := byPrefix[p]
		if len(names) < 2 {
			continue
		}
		sort.Strings(names)
		var files []string
		var file string
		for _, n := range names {
			src := set.defs[n].Source["wrapper_prefix"]
			if src == "" {
				src = "builtin"
			}
			files = append(files, fmt.Sprintf("%s (%s)", n, src))
			if src != "builtin" || file == "" {
				file = src // name a user file when one is involved
			}
		}
		set.errors = append(set.errors, LoadError{
			File:  file,
			Field: "wrapper_prefix",
			Msg:   fmt.Sprintf("%q is declared by more than one harness: %s; wrapper prefixes must be unique", p, strings.Join(files, ", ")),
		})
	}
}
