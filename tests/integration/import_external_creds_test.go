package integration

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestImportDryRunListsExternalCredentialFiles covers T096 (FR-045): the dry run
// (text and --json) names each credential file the cc-deck config points at outside
// cc-deck's own folders, path only, so the user sees what would be copied.
func TestImportDryRunListsExternalCredentialFiles(t *testing.T) {
	e := newEnv(t)
	source := filepath.Join(e.root, ".config", "cc-deck")
	writeFixtureFile(t, filepath.Join(source, "config.yaml"),
		"profiles:\n"+
			"  claude:\n"+
			"    outside:\n"+
			"      auth:\n"+
			"        api_key:\n"+
			"          file: ~/keys/fake_api_key\n"+
			"    own:\n"+
			"      auth:\n"+
			"        api_key:\n"+
			"          file: ~/keys/shadowed\n")
	external := filepath.Join(e.root, "keys", "fake_api_key")
	writeFixtureFile(t, external, "FAKE-KEY-CONTENT\n")
	writeFixtureFile(t, filepath.Join(source, "profiles", "claude", "own", "api_key"), "FAKE-OWN-CONTENT\n")

	out := e.run(nil, "import", "cc-deck", "--source", source, "--dry-run")
	want := "would copy credential file from outside cc-deck's folders: " + external
	if !strings.Contains(out, want) {
		t.Errorf("dry run should list %q:\n%s", want, out)
	}
	if strings.Count(out, "outside cc-deck's folders") != 1 {
		t.Errorf("only the external file should be listed:\n%s", out)
	}
	if strings.Contains(out, "FAKE-KEY-CONTENT") || strings.Contains(out, "FAKE-OWN-CONTENT") {
		t.Errorf("credential content leaked into the report:\n%s", out)
	}

	raw := e.run(nil, "import", "cc-deck", "--source", source, "--dry-run", "--json")
	var doc struct {
		Actions []struct {
			ID                      string   `json:"id"`
			ExternalCredentialFiles []string `json:"external_credential_files"`
		} `json:"actions"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("parse --json: %v\n%s", err, raw)
	}
	got := map[string][]string{}
	for _, a := range doc.Actions {
		got[a.ID] = a.ExternalCredentialFiles
	}
	if f := got["claude/outside"]; len(f) != 1 || f[0] != external {
		t.Errorf("claude/outside external_credential_files = %q, want [%q]", f, external)
	}
	if f, ok := got["claude/own"]; !ok || len(f) != 0 {
		t.Errorf("claude/own external_credential_files = %q (present %v), want omitted", f, ok)
	}
	if !strings.Contains(raw, `"external_credential_files"`) {
		t.Errorf("--json should carry external_credential_files:\n%s", raw)
	}
}
