// Package hooksets implements the hook-set library (FR-023) and the placement of
// managed hook entries into profiles (FR-028, research R3 to R5). A hook set is one
// YAML file in <config>/aprofile/hooks.d with one body per harness; the body's shape
// follows the harness definition's hook form (event-groups or files). Which hook
// entries aprofile placed is recorded per profile, by fingerprint, so entries the
// user or another tool added are never touched. There is no harness-name branching:
// every decision comes from the definition's `hooks` block (Principle I).
package hooksets

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/secret"
)

// Set is one hook set from the library.
type Set struct {
	Name         string
	Description  string
	EveryProfile bool
	Bodies       map[string]Body // harness name -> body
	File         string          // the library file it was loaded from
	FilesDir     string          // hooks.d/files/<name>: where files-form bodies live
	// LoadErr is set for a library file that failed to load. The set never
	// applies, and the entries it placed earlier are left unchanged rather than
	// removed, so a typo in a set does not strip hooks from profiles.
	LoadErr error
}

// Body is a hook set's content for one harness. For form event-groups it holds
// matcher groups per event (the same shape as Claude Code's hooks key); for form
// files it lists files relative to hooks.d/files/<set>/.
type Body struct {
	Events map[string][]json.RawMessage `json:"events,omitempty"`
	Files  []string                     `json:"files,omitempty"`
}

// EventNames returns the body's events, sorted.
func (b Body) EventNames() []string {
	var out []string
	for e := range b.Events {
		out = append(out, e)
	}
	sort.Strings(out)
	return out
}

// Harnesses returns the harness names the set has a body for, sorted.
func (s *Set) Harnesses() []string {
	var out []string
	for h := range s.Bodies {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

type setFile struct {
	Name         string               `yaml:"name"`
	Description  string               `yaml:"description"`
	EveryProfile bool                 `yaml:"every_profile"`
	Bodies       map[string]yaml.Node `yaml:"bodies"`
}

// LoadLibrary reads every <name>.yaml in dir. A file that fails to parse is
// reported in errs and left out; the others still load. A missing dir is an empty
// library.
func LoadLibrary(dir string) (map[string]*Set, []error) {
	lib := map[string]*Set{}
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return lib, []error{err}
	}
	sort.Strings(files)
	var errs []error
	for _, f := range files {
		s, err := loadSet(f)
		if err != nil {
			errs = append(errs, err)
			name := strings.TrimSuffix(filepath.Base(f), ".yaml")
			lib[name] = &Set{Name: name, File: f, Bodies: map[string]Body{}, LoadErr: err}
			continue
		}
		lib[s.Name] = s
	}
	return lib, errs
}

func loadSet(file string) (*Set, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("hook set %s: %w", file, err)
	}
	var sf setFile
	if err := yaml.Unmarshal(data, &sf); err != nil {
		return nil, fmt.Errorf("hook set %s: %w", file, err)
	}
	want := strings.TrimSuffix(filepath.Base(file), ".yaml")
	if sf.Name == "" {
		sf.Name = want
	}
	if sf.Name != want {
		return nil, fmt.Errorf("hook set %s: name %q must match the file name %q", file, sf.Name, want)
	}
	if !profile.NameValid(sf.Name) {
		return nil, fmt.Errorf("hook set %s: name %q must match ^[a-z][a-z0-9-]{0,62}[a-z0-9]$", file, sf.Name)
	}
	s := &Set{Name: sf.Name, Description: sf.Description, EveryProfile: sf.EveryProfile, Bodies: map[string]Body{}, File: file,
		FilesDir: FilesDir(filepath.Dir(file), sf.Name)}
	for h, node := range sf.Bodies {
		b, err := parseBody(node)
		if err != nil {
			return nil, fmt.Errorf("hook set %s: body %q: %w", file, h, err)
		}
		if err := checkSecrets(node); err != nil {
			return nil, fmt.Errorf("hook set %s: body %q: %w", file, h, err)
		}
		s.Bodies[h] = b
	}
	return s, nil
}

// parseBody decodes a body: a mapping with only `files` is the files form,
// anything else maps events to lists of matcher groups.
func parseBody(node yaml.Node) (Body, error) {
	var raw map[string]any
	if err := node.Decode(&raw); err != nil {
		return Body{}, fmt.Errorf("must be a mapping")
	}
	if f, ok := raw["files"]; ok && len(raw) == 1 {
		list, ok := f.([]any)
		if !ok {
			return Body{}, fmt.Errorf("files must be a list of file names")
		}
		var b Body
		for _, item := range list {
			name, ok := item.(string)
			if !ok || name == "" || filepath.IsAbs(name) || strings.Contains(filepath.ToSlash(name), "..") {
				return Body{}, fmt.Errorf("files entry %v must be a relative file name", item)
			}
			b.Files = append(b.Files, name)
		}
		return b, nil
	}
	b := Body{Events: map[string][]json.RawMessage{}}
	for event, groups := range raw {
		list, ok := groups.([]any)
		if !ok {
			return Body{}, fmt.Errorf("event %q must hold a list of matcher groups", event)
		}
		for _, g := range list {
			if _, ok := g.(map[string]any); !ok {
				return Body{}, fmt.Errorf("event %q: each group must be a mapping", event)
			}
			data, err := json.Marshal(g)
			if err != nil {
				return Body{}, fmt.Errorf("event %q: %w", event, err)
			}
			b.Events[event] = append(b.Events[event], data)
		}
	}
	return b, nil
}

// checkSecrets rejects literal values with a known secret prefix (Constitution IV),
// as for profile values.
func checkSecrets(node yaml.Node) error {
	var walkErr error
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		if walkErr != nil {
			return
		}
		if n.Kind == yaml.ScalarNode {
			if err := secret.CheckPrefix(n.Value); err != nil {
				walkErr = err
			}
			return
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(&node)
	return walkErr
}

// ValidateFor checks a set's body for one harness against the definition's hook
// form (FR-024): the harness must declare a hook location and the body must have
// that form's shape.
func ValidateFor(s *Set, def *harness.Definition) error {
	if s.LoadErr != nil {
		return fmt.Errorf("hook set %q failed to load: %v", s.Name, s.LoadErr)
	}
	if def.Hooks == nil {
		return fmt.Errorf("harness %q does not declare a hook location; it does not support hook sets", def.Name)
	}
	b, ok := s.Bodies[def.Name]
	if !ok {
		return fmt.Errorf("hook set %q has no body for harness %q", s.Name, def.Name)
	}
	switch def.Hooks.Form {
	case harness.HookFormEventGroups:
		if len(b.Files) > 0 || b.Events == nil {
			return fmt.Errorf("hook set %q: the %s body must map events to matcher groups (form event-groups)", s.Name, def.Name)
		}
	case harness.HookFormFiles:
		if b.Events != nil {
			return fmt.Errorf("hook set %q: the %s body must list files (form files)", s.Name, def.Name)
		}
	}
	return nil
}

// Applies reports whether set s applies to profile p (data-model.md): the set has a
// body for the harness, the profile is not at all, and it is either named or an
// every-profile set at content that the profile does not exclude.
func Applies(s *Set, p *profile.Profile) bool {
	if s.LoadErr != nil {
		return false
	}
	if _, ok := s.Bodies[p.Harness]; !ok {
		return false
	}
	level := p.ShareLevel()
	if level == profile.ShareAll {
		return false
	}
	if contains(p.HookSets, s.Name) {
		return true
	}
	return s.EveryProfile && level == profile.ShareContent && !contains(p.ExcludeHookSets, s.Name)
}

// Reason explains why a set applies, or why not: named, every-profile, excluded, or
// "" when it does not apply for another reason.
func Reason(s *Set, p *profile.Profile) string {
	switch {
	case !Applies(s, p) && s.EveryProfile && contains(p.ExcludeHookSets, s.Name):
		return "excluded"
	case !Applies(s, p):
		return ""
	case contains(p.HookSets, s.Name):
		return "named"
	default:
		return "every-profile"
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// SaveSet writes a set to the library as YAML. It refuses to overwrite an existing
// set (FR-031).
func SaveSet(dir string, s *Set) (string, error) {
	path := filepath.Join(dir, s.Name+".yaml")
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("hook set %q already exists at %s", s.Name, path)
	}
	doc := map[string]any{"name": s.Name}
	if s.Description != "" {
		doc["description"] = s.Description
	}
	doc["every_profile"] = s.EveryProfile
	bodies := map[string]any{}
	for h, b := range s.Bodies {
		if b.Files != nil {
			bodies[h] = map[string]any{"files": b.Files}
			continue
		}
		events := map[string]any{}
		for e, groups := range b.Events {
			var list []any
			for _, g := range groups {
				var v any
				if err := json.Unmarshal(g, &v); err != nil {
					return "", err
				}
				list = append(list, v)
			}
			events[e] = list
		}
		bodies[h] = events
	}
	doc["bodies"] = bodies
	data, err := yaml.Marshal(doc)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// Written to a temporary file and hard-linked into place: the link fails when
	// the set exists, and a reader never sees a partial file.
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Link(tmp.Name(), path); err != nil {
		return "", fmt.Errorf("create hook set %s: %w", path, err)
	}
	return path, nil
}

// FilesDir is where a files-form set's files live in the library: <lib>/files/<set>.
func FilesDir(libDir, set string) string {
	return filepath.Join(libDir, "files", set)
}
