package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// This file implements the harnesses.d override-merge (FR-009). A user file whose
// `name` equals a built-in's overrides only the fields it sets; every other field
// comes from the built-in. The merge follows FR-009: maps merge by key, scalars and
// lists replace, and an explicit null removes an inherited entry. A file with a new
// name adds a harness. aprofile harness show marks each effective field's origin
// (FR-010) via Definition.Source. There is no harness-name branching here
// (Principle I).

// overlayHarnessesDir reads *.yaml from dir and overlays each onto the matching
// built-in (by name) or adds it as a new harness. Invalid files and duplicate names
// are isolated in set.errors and do not stop the others (FR-012). A missing dir is a
// no-op (built-ins only).
func overlayHarnessesDir(set *Set, builtinNodes map[string]*yaml.Node, dir string) {
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		set.errors = append(set.errors, LoadError{File: dir, Msg: err.Error()})
		return
	}
	sort.Strings(files)

	type parsedFile struct {
		file string
		node *yaml.Node
		name string
	}
	var parsed []parsedFile
	nameFiles := map[string][]string{}

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			set.errors = append(set.errors, LoadError{File: f, Msg: err.Error()})
			continue
		}
		node, terr := topMapping(data)
		if terr != nil {
			set.errors = append(set.errors, LoadError{File: f, Msg: terr.Error()})
			continue
		}
		name, ferr := nameFromMapping(node)
		if ferr != nil {
			set.errors = append(set.errors, LoadError{File: f, Field: ferr.Field, Msg: ferr.Msg})
			continue
		}
		parsed = append(parsed, parsedFile{file: f, node: node, name: name})
		nameFiles[name] = append(nameFiles[name], f)
	}

	// Two user files with the same name are an error naming both files (FR-009);
	// neither is applied so the built-in (if any) keeps working.
	dup := map[string]bool{}
	for name, fs := range nameFiles {
		if len(fs) > 1 {
			dup[name] = true
			set.errors = append(set.errors, LoadError{
				File:  fs[0],
				Field: "name",
				Msg:   fmt.Sprintf("duplicate harness name %q also defined in %s", name, strings.Join(fs[1:], ", ")),
			})
		}
	}

	for _, pf := range parsed {
		if dup[pf.name] {
			continue
		}
		if bn, ok := builtinNodes[pf.name]; ok {
			overridden := mergeMapping(bn, pf.node)
			data, err := yaml.Marshal(bn)
			if err != nil {
				set.errors = append(set.errors, LoadError{File: pf.file, Msg: err.Error()})
				continue
			}
			def, lerr := parseDefinition(data, pf.file)
			if lerr != nil {
				set.errors = append(set.errors, *lerr)
				continue
			}
			setMergedSource(def, pf.file, overridden)
			set.defs[def.Name] = def
			continue
		}
		// A brand-new harness defined entirely by the file.
		data, err := yaml.Marshal(pf.node)
		if err != nil {
			set.errors = append(set.errors, LoadError{File: pf.file, Msg: err.Error()})
			continue
		}
		def, lerr := parseDefinition(data, pf.file)
		if lerr != nil {
			set.errors = append(set.errors, *lerr)
			continue
		}
		markSource(def, pf.file)
		set.defs[def.Name] = def
	}
}

// setMergedSource marks each effective field's origin: the overriding file for a
// field the user file set, "builtin" otherwise (FR-010).
func setMergedSource(def *Definition, filePath string, overridden []string) {
	ov := map[string]bool{}
	for _, k := range overridden {
		ov[k] = true
	}
	def.Source = map[string]string{}
	for _, f := range allFieldNames {
		if ov[f] {
			def.Source[f] = filePath
		} else {
			def.Source[f] = "builtin"
		}
	}
}

// mergeMapping merges the override mapping node into base in place and returns the
// top-level keys the override set (FR-009): a mapping value merges by key, an
// explicit null removes the inherited key, and any other value replaces.
func mergeMapping(base, override *yaml.Node) []string {
	var overridden []string
	for i := 0; i+1 < len(override.Content); i += 2 {
		k := override.Content[i]
		v := override.Content[i+1]
		overridden = append(overridden, k.Value)
		if v.Tag == "!!null" {
			removeMapKey(base, k.Value)
			continue
		}
		bv := findMapValue(base, k.Value)
		if bv != nil && bv.Kind == yaml.MappingNode && v.Kind == yaml.MappingNode {
			mergeMapping(bv, v)
		} else {
			setMapKey(base, k.Value, v)
		}
	}
	return overridden
}

// topMapping decodes YAML bytes into the top-level mapping node.
func topMapping(data []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return nil, fmt.Errorf("definition is empty")
	}
	m := doc.Content[0]
	if m.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("definition must be a YAML mapping")
	}
	return m, nil
}

// nameFromMapping extracts the required `name` scalar from a mapping node.
func nameFromMapping(node *yaml.Node) (string, *fieldError) {
	v := findMapValue(node, "name")
	if v == nil || v.Value == "" {
		return "", &fieldError{Field: "name", Msg: "is required"}
	}
	return v.Value, nil
}

// findMapValue returns the value node for key in a mapping node, or nil.
func findMapValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// setMapKey sets key to val in a mapping node, replacing or appending.
func setMapKey(m *yaml.Node, key string, val *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = val
			return
		}
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		val,
	)
}

// removeMapKey deletes key from a mapping node.
func removeMapKey(m *yaml.Node, key string) {
	out := m.Content[:0]
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			continue
		}
		out = append(out, m.Content[i], m.Content[i+1])
	}
	m.Content = out
}
