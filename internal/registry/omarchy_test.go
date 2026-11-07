package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeJSONFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func decodeFile(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("%s: %v\n%s", path, err, data)
	}
	return m
}

func accountsByID(t *testing.T, m map[string]any) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	list, _ := m["accounts"].([]any)
	for _, a := range list {
		obj := a.(map[string]any)
		out[obj["id"].(string)] = obj
	}
	return out
}

// T074/FR-031: a provider whose last profile was removed still has its stale
// aprofile entries dropped; the file is kept (with its foreign entries, or an empty
// accounts list), never deleted.
func TestOmarchyDropsStaleEntriesFromEveryProviderFile(t *testing.T) {
	dir := t.TempDir()
	openai := filepath.Join(dir, "openai.json")
	writeJSONFile(t, openai, `{"accounts":[{"id":"aprofile:codex/gone","label":"Gone","home":"/g","primary":false}]}`)
	pi := filepath.Join(dir, "pi.json")
	writeJSONFile(t, pi, `{"accounts":[{"id":"manual:x","label":"X","home":"/x","primary":true},{"id":"aprofile:pi/old","label":"Old","home":"/o","primary":false}]}`)

	written, err := ExportOmarchy(dir, []OmarchyProfile{
		{Harness: "claude", Name: "work", Provider: "anthropic", Label: "Work", Home: "/w", Primary: true},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "anthropic.json"), openai, pi}
	if !reflect.DeepEqual(written, want) {
		t.Errorf("written = %v, want %v", written, want)
	}
	if got := accountsByID(t, decodeFile(t, openai)); len(got) != 0 {
		t.Errorf("openai.json keeps stale entries: %v", got)
	}
	if m := decodeFile(t, openai); m["accounts"] == nil {
		t.Error("openai.json must keep an (empty) accounts list")
	}
	got := accountsByID(t, decodeFile(t, pi))
	if _, ok := got["manual:x"]; !ok || len(got) != 1 {
		t.Errorf("pi.json: %v", got)
	}

	// Idempotent: a second export writes nothing.
	written, err = ExportOmarchy(dir, []OmarchyProfile{
		{Harness: "claude", Name: "work", Provider: "anthropic", Label: "Work", Home: "/w", Primary: true},
	}, nil)
	if err != nil || len(written) != 0 {
		t.Errorf("second export wrote %v (err %v)", written, err)
	}
}

// T074/FR-031: foreign entries, unknown top-level keys and unknown fields survive
// verbatim; aprofile entries keep fields another tool added.
func TestOmarchyPreservesUnknownJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "anthropic.json")
	writeJSONFile(t, path, `{
  "version": 3,
  "accounts": [
    {"id": "manual:other", "label": "Other", "home": "/x", "primary": false, "color": "#ff0000", "extra": {"nested": [1, 2]}},
    {"id": "aprofile:claude/work", "label": "Old", "home": "/old", "primary": false, "last_used": "2026-11-01"},
    "not-an-object"
  ],
  "settings": {"sort": "label"}
}`)
	_, err := ExportOmarchy(dir, []OmarchyProfile{
		{Harness: "claude", Name: "work", Provider: "anthropic", Label: "Work", Home: "/w", Primary: true},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := decodeFile(t, path)
	if m["version"] != float64(3) {
		t.Errorf("top-level version lost: %v", m)
	}
	if s, _ := m["settings"].(map[string]any); s["sort"] != "label" {
		t.Errorf("top-level settings lost: %v", m)
	}
	list := m["accounts"].([]any)
	if len(list) != 3 || list[2] != "not-an-object" {
		t.Errorf("accounts: %v", list)
	}
	got := accountsByID(t, map[string]any{"accounts": list[:2]})
	other := got["manual:other"]
	if other["color"] != "#ff0000" || other["extra"] == nil {
		t.Errorf("foreign entry fields lost: %v", other)
	}
	work := got["aprofile:claude/work"]
	if work["label"] != "Work" || work["home"] != "/w" || work["primary"] != true {
		t.Errorf("aprofile entry not updated: %v", work)
	}
	if work["last_used"] != "2026-11-01" {
		t.Errorf("field added by another tool lost: %v", work)
	}
}

// Entries of profiles that exist but could not be exported (failed to load) are
// kept unchanged, not treated as removed profiles.
func TestOmarchyKeepsEntriesOfFailedProfiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "anthropic.json")
	writeJSONFile(t, path, `{"accounts":[{"id":"aprofile:claude/broken","label":"B","home":"/b","primary":false},{"id":"aprofile:claude/gone","label":"G","home":"/g","primary":false}]}`)
	if _, err := ExportOmarchy(dir, nil, []string{"claude/broken"}); err != nil {
		t.Fatal(err)
	}
	got := accountsByID(t, decodeFile(t, path))
	if _, ok := got["aprofile:claude/broken"]; !ok {
		t.Error("entry of a failed profile was dropped")
	}
	if _, ok := got["aprofile:claude/gone"]; ok {
		t.Error("stale entry kept")
	}
}

// An unparseable accounts file aborts the export before any file is written.
func TestOmarchyUnparseableFileWritesNothing(t *testing.T) {
	dir := t.TempDir()
	writeJSONFile(t, filepath.Join(dir, "zzz.json"), `{"accounts": [`)
	_, err := ExportOmarchy(dir, []OmarchyProfile{
		{Harness: "claude", Name: "work", Provider: "anthropic", Label: "Work", Home: "/w"},
	}, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if _, err := os.Stat(filepath.Join(dir, "anthropic.json")); !os.IsNotExist(err) {
		t.Error("a file was written although the export failed")
	}
}

// A provider file with no aprofile entries and nothing to change is not rewritten,
// even when its formatting differs from aprofile's.
func TestOmarchyLeavesUntouchedFilesAlone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pi.json")
	content := `{"accounts":[{"id":"manual:x","home":"/x"}]}`
	writeJSONFile(t, path, content)
	written, err := ExportOmarchy(dir, nil, nil)
	if err != nil || len(written) != 0 {
		t.Fatalf("written %v err %v", written, err)
	}
	if data, _ := os.ReadFile(path); string(data) != content {
		t.Errorf("file rewritten: %s", data)
	}
}

// An accounts file with bytes after its top-level object is rejected: the export
// fails and no file (neither that one nor another provider's) is written or
// changed, so trailing content is never silently dropped by a rewrite.
func TestOmarchyRejectsTrailingData(t *testing.T) {
	cases := map[string]string{
		"garbage":       `{"accounts":[]} garbage`,
		"closing brace": `{"accounts":[]} }`,
		"second object": `{"accounts":[]} {"accounts":[]}`,
		"number":        `{"accounts":[]} 1`,
		"bracket":       `{"accounts":[]} ]`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "anthropic.json")
			writeJSONFile(t, path, content)
			other := filepath.Join(dir, "openai.json")
			otherContent := `{"accounts":[{"id":"aprofile:codex/gone","label":"G","home":"/g","primary":false}]}`
			writeJSONFile(t, other, otherContent)
			written, err := ExportOmarchy(dir, []OmarchyProfile{
				{Harness: "claude", Name: "work", Provider: "anthropic", Label: "Work", Home: "/w"},
			}, nil)
			if err == nil {
				t.Fatalf("expected an error, wrote %v", written)
			}
			if data, _ := os.ReadFile(path); string(data) != content {
				t.Errorf("anthropic.json changed: %q", data)
			}
			if data, _ := os.ReadFile(other); string(data) != otherContent {
				t.Errorf("openai.json changed: %q", data)
			}
		})
	}
}

// Trailing whitespace after the object is not trailing data.
func TestOmarchyAcceptsTrailingWhitespace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "anthropic.json")
	writeJSONFile(t, path, "{\"accounts\":[]}\n\n \t\n")
	if _, err := ExportOmarchy(dir, []OmarchyProfile{
		{Harness: "claude", Name: "work", Provider: "anthropic", Label: "Work", Home: "/w"},
	}, nil); err != nil {
		t.Fatal(err)
	}
	if got := accountsByID(t, decodeFile(t, path)); len(got) != 1 {
		t.Errorf("accounts: %v", got)
	}
}
