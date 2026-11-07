package wrapper

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cc-deck/aprofile/internal/harness"
)

// MarkerIdentity returns the harness and profile named on a wrapper's marker line
// ("# aprofile:wrapper harness=<h> profile=<n>"); ok is false for content without
// the marker.
func MarkerIdentity(content []byte) (harnessName, profileName string, ok bool) {
	for _, line := range strings.Split(string(content), "\n") {
		if !strings.HasPrefix(line, Marker) {
			continue
		}
		for _, f := range strings.Fields(line[len(Marker):]) {
			if v, found := strings.CutPrefix(f, "harness="); found {
				harnessName = v
			} else if v, found := strings.CutPrefix(f, "profile="); found {
				profileName = v
			}
		}
		return harnessName, profileName, true
	}
	return "", "", false
}

// Leftovers lists, sorted, the files in binDir that carry the aprofile wrapper
// marker but belong to no existing profile (FR-006b): a wrapper left behind by
// remove or rename until the next sync removes it. owner reports, for the harness
// and profile a marker names, whether that profile exists and the wrapper name it
// should have ("" when unknown, for example when its definition failed to load);
// a marked file under another name is a leftover too. Files without the marker are
// never listed. A missing bin dir has no leftovers.
func Leftovers(binDir string, owner func(harnessName, profileName string) (wrapperName string, exists bool)) ([]string, error) {
	entries, err := os.ReadDir(binDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(binDir, e.Name()))
		if err != nil {
			continue
		}
		if !IsWrapper(data) {
			continue // not ours: never reported (FR-023)
		}
		h, n, _ := MarkerIdentity(data)
		name, exists := owner(h, n)
		if exists && (name == "" || name == e.Name()) {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out, nil
}

// LeftoversFor is Leftovers for the given profiles (loaded or failing to load),
// with wrapper names from their definitions in set. As in sync, a wrapper of a
// harness whose definition failed to load is kept while any profile names that
// harness, so it is not listed.
func LeftoversFor(binDir string, set *harness.Set, profiles []harness.ProfileKey) ([]string, error) {
	exists := map[harness.ProfileKey]bool{}
	named := map[string]bool{}
	for _, k := range profiles {
		exists[k] = true
		named[k.Harness] = true
	}
	return Leftovers(binDir, func(h, n string) (string, bool) {
		def, hasDef := set.Get(h)
		if !hasDef {
			return "", exists[harness.ProfileKey{Harness: h, Name: n}] || named[h]
		}
		if !exists[harness.ProfileKey{Harness: h, Name: n}] {
			return "", false
		}
		return def.WrapperName(n), true
	})
}
