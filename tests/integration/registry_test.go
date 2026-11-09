package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestScenario4RegistryAndEnvContract covers quickstart Scenario 4 (US3, SC-007):
// the registry file carries every field + format_version and no secrets, and a running
// wrapper's process env carries the APROFILE_* contract.
func TestScenario4RegistryAndEnvContract(t *testing.T) {
	e := newEnv(t)
	e.run(nil, "add", "claude", "work", "--primary", "--label", "Work",
		"--color", "blue", "--icon", "W", "--model", "X",
		"--auth-kind", "oauth-token", "--auth-key", "env:WORK_TOKEN")
	e.run(nil, "add", "claude", "private", "--label", "Private",
		"--color", "red", "--icon", "P",
		"--auth-kind", "oauth-token", "--auth-key", "env:PRIV_TOKEN")
	e.run(nil, "sync")

	// registry path prints the fixed data-dir location.
	regPath := strings.TrimSpace(e.run(nil, "registry", "path"))
	wantPath := filepath.Join(e.root, ".local", "share", "aprofile", "registry.json")
	if regPath != wantPath {
		t.Errorf("registry path = %q, want %q", regPath, wantPath)
	}

	// The registry file: all fields + format_version, no secrets or references.
	data, err := os.ReadFile(regPath)
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	if strings.Contains(string(data), "WORK_TOKEN") || strings.Contains(string(data), "PRIV_TOKEN") {
		t.Errorf("registry must not contain secret references:\n%s", data)
	}
	var reg struct {
		FormatVersion int              `json:"format_version"`
		Profiles      []map[string]any `json:"profiles"`
	}
	if err := json.Unmarshal(data, &reg); err != nil {
		t.Fatalf("registry not valid JSON: %v", err)
	}
	if reg.FormatVersion != 1 {
		t.Errorf("format_version = %d, want 1", reg.FormatVersion)
	}
	if len(reg.Profiles) != 2 {
		t.Fatalf("want 2 registry profiles, got %d", len(reg.Profiles))
	}
	for _, field := range []string{"harness", "name", "label", "color", "icon", "auth_kind", "primary", "config_dir", "wrapper_path"} {
		if _, ok := reg.Profiles[0][field]; !ok {
			t.Errorf("registry entry missing field %q", field)
		}
	}

	// A running wrapper carries the full APROFILE_* contract in its process env.
	out, err := e.runWrapper("claude-work", []string{"WORK_TOKEN=tok"}, "--version")
	if err != nil {
		t.Fatalf("wrapper failed: %v\n%s", err, out)
	}
	for _, v := range []string{
		"STUB_ENV: APROFILE_HARNESS=claude",
		"STUB_ENV: APROFILE_NAME=work",
		"STUB_ENV: APROFILE_LABEL=Work",
		"STUB_ENV: APROFILE_COLOR=blue",
		"STUB_ENV: APROFILE_ICON=W",
		"STUB_ENV: APROFILE_CONFIG_DIR=" + e.configDir("claude", "work"),
		"STUB_ENV: APROFILE_SET_VARS=ANTHROPIC_MODEL",
	} {
		if !strings.Contains(out, v) {
			t.Errorf("wrapper env missing %q\n%s", v, out)
		}
	}
}
