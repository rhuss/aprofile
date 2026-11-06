package hooksets

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/secret"
)

// ImportFromDefault creates a hook set from the hooks in a harness's default setup
// (FR-031) and writes it to the library at libDir. For the files form the files are
// copied into hooks.d/files/<name>/ and the set refers to the copies. The default
// setup is only read, never modified. It fails without changes when the set (or
// its files directory) already exists, or when the default setup has no hooks.
func ImportFromDefault(def *harness.Definition, libDir, name, description string, everyProfile bool) (*Set, error) {
	if def.Hooks == nil {
		return nil, fmt.Errorf("harness %q does not declare a hook location; it does not support hook sets", def.Name)
	}
	setFile := filepath.Join(libDir, name+".yaml")
	filesDir := FilesDir(libDir, name)
	if _, err := os.Stat(setFile); err == nil {
		return nil, fmt.Errorf("hook set %q already exists at %s; nothing changed", name, setFile)
	}
	if _, err := os.Stat(filesDir); err == nil {
		return nil, fmt.Errorf("%s already exists; nothing changed", filesDir)
	}
	s := &Set{Name: name, Description: description, EveryProfile: everyProfile, Bodies: map[string]Body{}, FilesDir: filesDir}
	src := def.DefaultPath(def.Hooks.File())
	var copies [][2]string
	switch def.Hooks.Form {
	case harness.HookFormEventGroups:
		data, err := os.ReadFile(src)
		if err != nil {
			return nil, fmt.Errorf("the default setup has no %s: %w", src, err)
		}
		var doc map[string]json.RawMessage
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("%s is not valid JSON: %w", src, err)
		}
		raw, ok := doc[def.Hooks.Key()]
		if !ok || len(bytes.TrimSpace(raw)) == 0 || string(raw) == "null" {
			return nil, fmt.Errorf("%s has no %q key: there are no hooks to import", src, def.Hooks.Key())
		}
		var events map[string][]json.RawMessage
		if err := json.Unmarshal(raw, &events); err != nil {
			return nil, fmt.Errorf("%s: %q is not an object of event groups: %w", src, def.Hooks.Key(), err)
		}
		if len(events) == 0 {
			return nil, fmt.Errorf("%s holds no hooks to import", src)
		}
		// Constitution IV: aprofile never authors a literal secret into the
		// library; the loader would reject such a set anyway.
		for _, ev := range sortedKeys(events) {
			for _, g := range events[ev] {
				if err := checkSecretStrings(g); err != nil {
					return nil, fmt.Errorf("%s: a hook in %s %v; nothing was imported", src, ev, err)
				}
			}
		}
		s.Bodies[def.Name] = Body{Events: events}
	case harness.HookFormFiles:
		entries, err := os.ReadDir(src)
		if err != nil {
			return nil, fmt.Errorf("the default setup has no %s: %w", src, err)
		}
		var files []string
		for _, e := range entries {
			if e.Type().IsRegular() {
				files = append(files, e.Name())
				copies = append(copies, [2]string{filepath.Join(src, e.Name()), filepath.Join(filesDir, e.Name())})
			}
		}
		if len(files) == 0 {
			return nil, fmt.Errorf("%s holds no hook files to import", src)
		}
		sort.Strings(files)
		s.Bodies[def.Name] = Body{Files: files}
	}
	for _, c := range copies {
		if err := copyLibraryFile(c[0], c[1]); err != nil {
			_ = os.RemoveAll(filesDir) // fail without changes (FR-031)
			return nil, err
		}
	}
	path, err := SaveSet(libDir, s)
	if err != nil {
		_ = os.RemoveAll(filesDir)
		return nil, err
	}
	s.File = path
	return s, nil
}

func copyLibraryFile(src, dst string) error {
	fi, err := os.Stat(src)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, fi.Mode().Perm())
}

func sortedKeys(m map[string][]json.RawMessage) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// checkSecretStrings applies the secret-prefix check to every string in a JSON
// value; the error names no value.
func checkSecretStrings(raw json.RawMessage) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	var walk func(any) error
	walk = func(v any) error {
		switch t := v.(type) {
		case string:
			if secret.CheckPrefix(t) != nil {
				return fmt.Errorf("holds a value that looks like a secret; use an env:/file: reference in the script instead")
			}
		case []any:
			for _, c := range t {
				if err := walk(c); err != nil {
					return err
				}
			}
		case map[string]any:
			for _, c := range t {
				if err := walk(c); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(v)
}
