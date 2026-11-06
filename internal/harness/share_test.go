package harness_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
)

// minimalDef is a valid user definition body that tests extend with one field.
const minimalDef = "name: toy\nbinary: toy\nwrapper_prefix: toy\nconfig_env: { TOY_HOME: \".\" }\n"

func loadOne(t *testing.T, body string) (*harness.Definition, []harness.LoadError) {
	t.Helper()
	set := harness.LoadFromBytes(map[string][]byte{"toy.yaml": []byte(body)})
	d, _ := set.Get("toy")
	return d, set.Errors()
}

// T005: the borrowable layers and the new definition fields load and validate.
func TestBorrowableLayersAndFieldsValidate(t *testing.T) {
	cases := []struct {
		name    string
		extra   string
		wantErr string // substring of the error field or message; empty means valid
	}{
		{"content layer", "classification: [{ entry: \"CLAUDE.md\", layer: content }]", ""},
		{"settings layer", "classification: [{ entry: \"settings.json\", layer: settings }]", ""},
		{"plugin-store layer", "classification: [{ entry: \"plugins/\", layer: plugin-store }]", ""},
		{"unknown layer", "classification: [{ entry: \"x\", layer: bogus }]", "invalid layer"},
		{"share policy cell", "share_policy: { settings: { all: own } }", ""},
		{"bad share policy treatment", "share_policy: { settings: { all: borrow } }", "share_policy"},
		{"bad share policy category", "share_policy: { state: { all: link } }", "share_policy"},
		{"bad share policy level", "share_policy: { content: { most: link } }", "share_policy"},
		{"event-groups hooks", "hooks: { location: \"settings.json#hooks\", form: event-groups }", ""},
		{"event-groups needs key", "hooks: { location: \"settings.json\", form: event-groups }", "hooks"},
		{"files hooks", "hooks: { location: \"plugins\", form: files }", ""},
		{"bad hook form", "hooks: { location: \"plugins\", form: scripts }", "hooks"},
		{"home escapes", "home_escapes: [{ path: \"~/.agents/skills\", what: skills }]", ""},
		{"home escape without path", "home_escapes: [{ what: skills }]", "home_escapes"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, errs := loadOne(t, minimalDef+c.extra+"\n")
			if c.wantErr == "" {
				if len(errs) != 0 || d == nil {
					t.Fatalf("expected valid definition, got errors %v", errs)
				}
				return
			}
			if len(errs) == 0 {
				t.Fatalf("expected an error containing %q, got none", c.wantErr)
			}
			if !strings.Contains(errs[0].Error(), c.wantErr) {
				t.Errorf("error %q does not mention %q", errs[0].Error(), c.wantErr)
			}
		})
	}
}

// T005: `shared` is a deprecated alias for content, accepted only in user files.
func TestSharedAliasOnlyInUserFiles(t *testing.T) {
	d, errs := loadOne(t, minimalDef+"classification: [{ entry: \"CLAUDE.md\", layer: shared }]\n")
	if len(errs) != 0 {
		t.Fatalf("user file with shared should load, got %v", errs)
	}
	r := d.Classification[0]
	if r.Layer != harness.LayerContent {
		t.Errorf("shared alias mapped to %q, want content", r.Layer)
	}
	if !r.Deprecated {
		t.Error("shared alias should carry the deprecation flag")
	}

	// A built-in that uses shared, or a glob with a borrowable layer, is rejected.
	for _, body := range []string{
		minimalDef + "classification: [{ entry: \"CLAUDE.md\", layer: shared }]\n",
		minimalDef + "classification: [{ entry: \"*\", layer: content }]\n",
	} {
		if _, err := harness.ParseBuiltinForTest([]byte(body)); err == nil {
			t.Errorf("built-in should be rejected: %q", body)
		}
	}
	// A glob with a private layer is fine in a built-in.
	if _, err := harness.ParseBuiltinForTest([]byte(minimalDef + "classification: [{ entry: \"*.sqlite\", layer: state }]\n")); err != nil {
		t.Errorf("state glob should be allowed in a built-in: %v", err)
	}
}

// T005: database rules imply their companion files.
func TestDatabaseCompanionsImplied(t *testing.T) {
	d, errs := loadOne(t, minimalDef+`classification:
  - { entry: "*.sqlite", layer: state }
  - { entry: "store.db", layer: state }
  - { entry: "kv", layer: state, database: true }
  - { entry: "notes.txt", layer: state }
`)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	entries := map[string]harness.Layer{}
	for _, r := range d.Classification {
		entries[r.Entry] = r.Layer
	}
	for _, base := range []string{"*.sqlite", "store.db", "kv"} {
		for _, suf := range []string{"-wal", "-shm", "-journal", ".lock"} {
			if entries[base+suf] != harness.LayerState {
				t.Errorf("companion %s%s missing or wrong layer: %q", base, suf, entries[base+suf])
			}
		}
	}
	if _, ok := entries["notes.txt-wal"]; ok {
		t.Error("a non-database rule must not get companions")
	}
}

// T005/FR-001a: classification overrides merge by entry; other lists still replace.
func TestClassificationMergesByEntry(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "claude.yaml")
	body := `name: claude
classification:
  - { entry: "RTK.md", layer: content }
  - { entry: "projects/", layer: owned }
  - { entry: "statsig/", layer: null }
capabilities: [login]
`
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	set, _ := harness.Load(dir)
	if errs := set.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	d, _ := set.Get("claude")
	got := map[string]harness.EntryRule{}
	for _, r := range d.Classification {
		got[r.Entry] = r
	}
	if r, ok := got["RTK.md"]; !ok || r.Layer != harness.LayerContent || r.Origin != file {
		t.Errorf("appended rule RTK.md = %+v", r)
	}
	if r := got["projects/"]; r.Layer != harness.LayerOwned || r.Origin != file {
		t.Errorf("replaced rule projects/ = %+v", r)
	}
	if _, ok := got["statsig/"]; ok {
		t.Error("layer: null should remove the built-in rule")
	}
	if r := got[".credentials.json"]; r.Layer != harness.LayerCredentials || r.Origin != "builtin" {
		t.Errorf("untouched built-in rule lost or mis-sourced: %+v", r)
	}
	if len(d.Capabilities) != 1 {
		t.Errorf("other lists still replace: capabilities = %v", d.Capabilities)
	}
}

// The effective share policy fills the default table and honors overrides.
func TestEffectiveSharePolicy(t *testing.T) {
	d, errs := loadOne(t, minimalDef+"share_policy: { settings: { all: own } }\n")
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	cases := []struct{ cat, level, want string }{
		{"content", "all", "link"}, {"content", "content", "link"}, {"content", "none", "own"},
		{"settings", "all", "own"}, {"settings", "content", "copy-once"}, {"settings", "none", "minimal"},
		{"plugin-store", "content", "link"}, {"plugin-store", "none", "own"},
		{"hook-location", "all", "link"}, {"hook-location", "content", "managed"}, {"hook-location", "none", "managed"},
	}
	for _, c := range cases {
		if got := d.Treatment(harness.Layer(c.cat), c.level); got != c.want {
			t.Errorf("Treatment(%s, %s) = %q, want %q", c.cat, c.level, got, c.want)
		}
	}
}

// DefaultPath maps a profile-relative entry to its default-setup location, using the
// longest default_locations prefix and falling back to default_config_location.
func TestDefaultPath(t *testing.T) {
	d, errs := loadOne(t, minimalDef+`default_config_location: "/home/u/.toy"
default_locations:
  "config/toy": "/home/u/.config/toy"
`)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if got := d.DefaultPath("config/toy/skills"); got != "/home/u/.config/toy/skills" {
		t.Errorf("mapped DefaultPath = %q", got)
	}
	if got := d.DefaultPath("config/toy"); got != "/home/u/.config/toy" {
		t.Errorf("prefix itself = %q", got)
	}
	if got := d.DefaultPath("other/x"); got != "/home/u/.toy/other/x" {
		t.Errorf("fallback DefaultPath = %q", got)
	}
}

// The built-ins carry the R1 classification: no catch-all, explicit categories.
func TestBuiltinsHaveNoCatchAll(t *testing.T) {
	set, _ := harness.LoadBuiltins()
	if errs := set.Errors(); len(errs) != 0 {
		t.Fatalf("built-ins failed to load: %v", errs)
	}
	for _, d := range set.All() {
		for _, r := range d.Classification {
			if strings.ContainsAny(r.Entry, "*?[") && r.Layer.Borrowable() {
				t.Errorf("%s: built-in glob %q has borrowable layer %q", d.Name, r.Entry, r.Layer)
			}
		}
		if d.Hooks == nil {
			t.Errorf("%s: built-in should declare its hook location", d.Name)
		}
		if d.MCP == nil || d.SkillsLocation == nil {
			t.Errorf("%s: built-in lost its mcp or skills_location block", d.Name)
		}
	}
}
