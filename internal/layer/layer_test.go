package layer_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/layer"
	"github.com/cc-deck/aprofile/internal/testutil"
)

func claudeDef(t *testing.T) *harness.Definition {
	t.Helper()
	set, err := harness.LoadBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	d, ok := set.Get("claude")
	if !ok {
		t.Fatal("claude not loaded")
	}
	return d
}

func TestClassifyTable(t *testing.T) {
	d := claudeDef(t)
	cases := []struct {
		entry        string
		wantLayer    harness.Layer
		unclassified bool
	}{
		{".credentials.json", harness.LayerCredentials, false},
		{"projects/", harness.LayerState, false},
		{"projects", harness.LayerState, false}, // trailing slash ignored
		{"statsig/", harness.LayerState, false},
		{"skills/", harness.LayerOwned, false},
		{".claude.json", harness.LayerMixed, false},
		{"settings.json", harness.LayerShared, false}, // falls through to *
		{"CLAUDE.md", harness.LayerShared, false},
	}
	for _, c := range cases {
		res := layer.Classify(d, c.entry)
		if res.Unclassified != c.unclassified {
			t.Errorf("Classify(%q).Unclassified = %v, want %v", c.entry, res.Unclassified, c.unclassified)
		}
		if !c.unclassified && res.Layer != c.wantLayer {
			t.Errorf("Classify(%q).Layer = %q, want %q", c.entry, res.Layer, c.wantLayer)
		}
	}
}

func TestClassifyKey(t *testing.T) {
	d := claudeDef(t)
	res := layer.Classify(d, ".claude.json")
	if res.Rule == nil {
		t.Fatal("expected a rule for .claude.json")
	}
	if l, ok := layer.ClassifyKey(res.Rule, "mcpServers"); !ok || l != harness.LayerOwned {
		t.Errorf("mcpServers key = %q (%v), want owned", l, ok)
	}
	if l, ok := layer.ClassifyKey(res.Rule, "oauthAccount"); !ok || l != harness.LayerCredentials {
		t.Errorf("oauthAccount key = %q (%v), want credentials", l, ok)
	}
	if _, ok := layer.ClassifyKey(res.Rule, "unknownKey"); ok {
		t.Errorf("unknownKey should be unclassified")
	}
}

func TestPrepareConfigDirCreatesStateAndMixed(t *testing.T) {
	h := testutil.HermeticHome(t)
	d := claudeDef(t)
	configDir := filepath.Join(h.DataHome, "aprofile", "profiles", "claude", "work")

	changes, err := layer.PrepareConfigDir(d, configDir, false)
	if err != nil {
		t.Fatalf("PrepareConfigDir: %v", err)
	}
	if len(changes) == 0 {
		t.Fatal("expected changes on first prepare")
	}
	// State dir is real, not a link.
	fi, err := os.Lstat(filepath.Join(configDir, "projects"))
	if err != nil {
		t.Fatalf("projects/ not created: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Error("projects/ must be a real dir, not a symlink")
	}
	// Mixed file seeded.
	if _, err := os.Stat(filepath.Join(configDir, ".claude.json")); err != nil {
		t.Errorf(".claude.json should be seeded: %v", err)
	}

	// Idempotent: a second prepare makes no changes.
	changes2, err := layer.PrepareConfigDir(d, configDir, false)
	if err != nil {
		t.Fatalf("second PrepareConfigDir: %v", err)
	}
	if len(changes2) != 0 {
		t.Errorf("second prepare should be a no-op, got %v", changes2)
	}
}

func TestSeedMixedFileExcludesCredentialsAndState(t *testing.T) {
	h := testutil.HermeticHome(t)
	d := claudeDef(t)
	// A default .claude.json holding a credential key, two state keys and an owned key.
	defaultSetup := filepath.Join(h.Root, ".claude")
	testutil.WriteFile(t, filepath.Join(defaultSetup, ".claude.json"),
		`{"oauthAccount":{"email":"me@example.com"},"projects":{"/x":{"trusted":true}},"numStartups":7,"mcpServers":{"s":{"command":"x"}}}`)
	configDir := filepath.Join(h.DataHome, "aprofile", "profiles", "claude", "work")
	if _, err := layer.PrepareConfigDir(d, configDir, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(configDir, ".claude.json"))
	if err != nil {
		t.Fatalf("seeded file: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("seeded file is not valid JSON: %v", err)
	}
	if _, ok := m["oauthAccount"]; ok {
		t.Error("credential key oauthAccount must not be seeded into a new profile (FR-014)")
	}
	if _, ok := m["projects"]; ok {
		t.Error("state key projects must not be seeded (FR-015)")
	}
	if _, ok := m["numStartups"]; ok {
		t.Error("state key numStartups must not be seeded (FR-015)")
	}
	if _, ok := m["mcpServers"]; !ok {
		t.Error("owned key mcpServers should be seeded from the default setup")
	}
}

func TestPrepareSharedSymlinkLazyAndNonDestructive(t *testing.T) {
	h := testutil.HermeticHome(t)
	d := claudeDef(t)
	configDir := filepath.Join(h.DataHome, "aprofile", "profiles", "claude", "work")

	// No default setup yet: shared links are deferred (lazy).
	if _, err := layer.PrepareConfigDir(d, configDir, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(configDir, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Error("shared link should not be created before the default setup exists")
	}

	// Create a default setup with a shared file and re-sync: link appears.
	defaultSetup := filepath.Join(h.Root, ".claude")
	testutil.WriteFile(t, filepath.Join(defaultSetup, "CLAUDE.md"), "# shared\n")
	if _, err := layer.PrepareConfigDir(d, configDir, false); err != nil {
		t.Fatal(err)
	}
	li, err := os.Lstat(filepath.Join(configDir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("shared link not created after default setup appeared: %v", err)
	}
	if li.Mode()&os.ModeSymlink == 0 {
		t.Error("CLAUDE.md should be a symlink to the default setup")
	}
}
