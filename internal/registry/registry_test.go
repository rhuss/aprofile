package registry_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/registry"
	"github.com/cc-deck/aprofile/internal/testutil"
)

func sampleEntries() []registry.Entry {
	return []registry.Entry{
		{Harness: "claude", Name: "work", Label: "Work", Color: "blue", Icon: "W", AuthKind: "oauth-token", Primary: true, ConfigDir: "/d/work", WrapperPath: "/b/claude-work"},
		{Harness: "claude", Name: "private", Label: "Private", Color: "red", Icon: "P", AuthKind: "oauth-login", ConfigDir: "/d/private", WrapperPath: "/b/claude-private"},
	}
}

func TestMarshalDeterministicAndSorted(t *testing.T) {
	a, err := registry.Marshal(sampleEntries())
	if err != nil {
		t.Fatal(err)
	}
	// Reversed input yields identical bytes (sorted by harness,name).
	rev := sampleEntries()
	rev[0], rev[1] = rev[1], rev[0]
	b, err := registry.Marshal(rev)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Errorf("marshal not order-independent:\n%s\n---\n%s", a, b)
	}
	// private sorts before work.
	if strings.Index(string(a), `"name": "private"`) > strings.Index(string(a), `"name": "work"`) {
		t.Error("entries not sorted by name")
	}
	if !strings.Contains(string(a), `"format_version": 1`) {
		t.Error("missing format_version")
	}
}

func TestWriteIdempotent(t *testing.T) {
	h := testutil.HermeticHome(t)
	path := filepath.Join(h.DataHome, "aprofile", "registry.json")

	changed, err := registry.Write(path, sampleEntries())
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("first write should report changed")
	}
	info1, _ := os.Stat(path)

	changed2, err := registry.Write(path, sampleEntries())
	if err != nil {
		t.Fatal(err)
	}
	if changed2 {
		t.Error("second identical write should report no change (SC-003)")
	}
	info2, _ := os.Stat(path)
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Error("unchanged write must not rewrite the file")
	}
}

func TestNoSecretsInRegistry(t *testing.T) {
	data, err := registry.Marshal(sampleEntries())
	if err != nil {
		t.Fatal(err)
	}
	// Ensure the record only has the documented keys.
	var raw struct {
		Profiles []map[string]any `json:"profiles"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"harness": true, "name": true, "label": true, "color": true, "icon": true,
		"auth_kind": true, "primary": true, "config_dir": true, "wrapper_path": true,
	}
	for _, p := range raw.Profiles {
		for k := range p {
			if !allowed[k] {
				t.Errorf("unexpected registry field %q (possible secret leak)", k)
			}
		}
	}
}

// TestRegistrySchemaMatchesContract pins the public registry schema documented in
// contracts/registry-and-env.md (FR-030): the top level is exactly
// {format_version, profiles}, and each profile carries exactly the nine documented
// fields. A change here is a breaking change that must bump FormatVersion.
func TestRegistrySchemaMatchesContract(t *testing.T) {
	data, err := registry.Marshal(sampleEntries())
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatal(err)
	}
	if len(top) != 2 || top["format_version"] == nil || top["profiles"] == nil {
		t.Errorf("top-level keys must be exactly {format_version, profiles}, got %d keys", len(top))
	}

	var reg struct {
		FormatVersion int                          `json:"format_version"`
		Profiles      []map[string]json.RawMessage `json:"profiles"`
	}
	if err := json.Unmarshal(data, &reg); err != nil {
		t.Fatal(err)
	}
	if reg.FormatVersion != registry.FormatVersion {
		t.Errorf("format_version = %d, want %d", reg.FormatVersion, registry.FormatVersion)
	}
	wantFields := []string{"harness", "name", "label", "color", "icon", "auth_kind", "primary", "config_dir", "wrapper_path"}
	for _, p := range reg.Profiles {
		if len(p) != len(wantFields) {
			t.Errorf("profile has %d fields, want exactly %d (%v): %v", len(p), len(wantFields), wantFields, keysOf(p))
		}
		for _, f := range wantFields {
			if _, ok := p[f]; !ok {
				t.Errorf("registry entry missing documented field %q", f)
			}
		}
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestRoundTrip(t *testing.T) {
	h := testutil.HermeticHome(t)
	path := filepath.Join(h.DataHome, "aprofile", "registry.json")
	if _, err := registry.Write(path, sampleEntries()); err != nil {
		t.Fatal(err)
	}
	reg, err := registry.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if reg.FormatVersion != registry.FormatVersion || len(reg.Profiles) != 2 {
		t.Errorf("round-trip mismatch: %+v", reg)
	}
}

func TestReadMissingFile(t *testing.T) {
	h := testutil.HermeticHome(t)
	reg, err := registry.Read(filepath.Join(h.DataHome, "aprofile", "nope.json"))
	if err != nil {
		t.Fatalf("missing registry should not error: %v", err)
	}
	if len(reg.Profiles) != 0 {
		t.Error("missing registry should be empty")
	}
}
