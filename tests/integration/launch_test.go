package integration

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestScenario1Launch covers quickstart Scenario 1 (US1): add -> sync -> the
// wrapper execs the stub with CLAUDE_CONFIG_DIR and APROFILE_* set; list --json
// shows the profile; the config dir exists.
func TestScenario1Launch(t *testing.T) {
	e := newEnv(t)

	e.run(nil, "add", "claude", "work",
		"--model", "X", "--endpoint", "Y", "--color", "blue", "--icon", "W",
		"--label", "Work", "--auth-kind", "oauth-token", "--auth-key", "env:WORK_TOKEN")

	e.run(nil, "sync")

	// The config dir exists.
	if fi, err := os.Stat(e.configDir("claude", "work")); err != nil || !fi.IsDir() {
		t.Fatalf("profile config dir not created: %v", err)
	}

	// Run the wrapper; it must exec the stub with the right environment.
	out, err := e.runWrapper("claude-work", []string{"WORK_TOKEN=secret-value"}, "--version")
	if err != nil {
		t.Fatalf("claude-work failed: %v\n%s", err, out)
	}
	wantConfigDir := "STUB_ENV: CLAUDE_CONFIG_DIR=" + e.configDir("claude", "work")
	checks := []string{
		wantConfigDir,
		"STUB_ENV: APROFILE_HARNESS=claude",
		"STUB_ENV: APROFILE_NAME=work",
		"STUB_ENV: APROFILE_LABEL=Work",
		"STUB_ENV: APROFILE_COLOR=blue",
		"STUB_ENV: APROFILE_ICON=W",
		"STUB_ENV: APROFILE_CONFIG_DIR=" + e.configDir("claude", "work"),
		"STUB_ENV: ANTHROPIC_MODEL=X",
		"STUB_ENV: ANTHROPIC_BASE_URL=Y",
		"STUB_ENV: CLAUDE_CODE_OAUTH_TOKEN=secret-value",
		"STUB_ARGS: [--version]",
	}
	for _, c := range checks {
		if !strings.Contains(out, c) {
			t.Errorf("wrapper output missing %q\n---\n%s", c, out)
		}
	}
	// The resolved secret value must reach the harness, but only via the
	// environment at launch (it is never written to the wrapper file).
	wrapperContent, _ := os.ReadFile(e.root + "/.local/share/aprofile/bin/claude-work")
	if strings.Contains(string(wrapperContent), "secret-value") {
		t.Errorf("wrapper file must not contain the secret value")
	}

	// list --json shows the profile.
	js := e.run(nil, "list", "--json")
	var parsed struct {
		FormatVersion int `json:"format_version"`
		Profiles      []struct {
			Harness       string `json:"harness"`
			Name          string `json:"name"`
			Label         string `json:"label"`
			Color         string `json:"color"`
			WrapperStatus string `json:"wrapper_status"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal([]byte(js), &parsed); err != nil {
		t.Fatalf("list --json not valid JSON: %v\n%s", err, js)
	}
	if len(parsed.Profiles) != 1 || parsed.Profiles[0].Name != "work" || parsed.Profiles[0].Color != "blue" {
		t.Errorf("list --json unexpected: %+v", parsed)
	}
	if parsed.Profiles[0].WrapperStatus != "synced" {
		t.Errorf("wrapper status = %q, want synced", parsed.Profiles[0].WrapperStatus)
	}
}

// TestWrapperFailClosedMissingReference covers FR-028a: a missing reference at
// launch stops the wrapper naming the profile/reference before the harness starts.
func TestWrapperFailClosedMissingReference(t *testing.T) {
	e := newEnv(t)
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-token", "--auth-key", "env:WORK_TOKEN")
	e.run(nil, "sync")

	// Run the wrapper WITHOUT WORK_TOKEN set.
	out, err := e.runWrapper("claude-work", []string{"WORK_TOKEN="}, "--version")
	if err == nil {
		t.Fatalf("wrapper should fail closed when the reference is unset\n%s", out)
	}
	if !strings.Contains(out, "claude/work") || !strings.Contains(out, "WORK_TOKEN") {
		t.Errorf("fail-closed message should name the profile and reference:\n%s", out)
	}
	// The harness must NOT have started.
	if strings.Contains(out, "STUB_ARGS") {
		t.Errorf("harness started despite an unresolved reference:\n%s", out)
	}
}

// TestLiteralSecretRejected covers quickstart Scenario 6 / FR-028: a literal
// secret on the command line is rejected and names a reference instead.
func TestLiteralSecretRejected(t *testing.T) {
	e := newEnv(t)
	out, err := e.tryRun(nil, "add", "claude", "bad", "--auth-kind", "oauth-token", "--auth-key", "sk-REALSECRET")
	if err == nil {
		t.Fatalf("literal secret should be rejected\n%s", out)
	}
	if !strings.Contains(out, "env:") {
		t.Errorf("rejection should suggest a reference:\n%s", out)
	}
}
