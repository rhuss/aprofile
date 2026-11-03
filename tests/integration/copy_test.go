package integration

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// canary is a secret-looking literal planted in a source owned MCP value; it must be
// dropped by a cross-harness conversion and never appear in any file the copy writes
// (SC-006, FR-035).
const canary = "sk-ant-CANARYLEAK0000000000000000AAAA"

// sourceClaudeJSON is a seeded .claude.json for the source profile with four MCP
// servers: stdio with an env reference, a remote server with a header reference, a
// disabled-style stdio server, and one carrying a literal secret. It also carries a
// credentials key (oauthAccount) and a state key (projects) that a same-harness copy
// must NOT carry.
const sourceClaudeJSON = `{
  "oauthAccount": {"emailAddress": "work@example.com"},
  "projects": {"/some/project": {"trusted": true}},
  "mcpServers": {
    "fs": {"command": "mcp-fs", "args": ["--root", "/tmp"], "env": {"TOKEN": "${TOKEN}", "MODE": "fast"}},
    "remote": {"type": "http", "url": "https://mcp.example.com", "headers": {"X-Token": "${XTOKEN}"}},
    "plain": {"command": "mcp-plain"},
    "leaky": {"command": "mcp-x", "env": {"API_KEY": "` + canary + `"}}
  }
}`

type copyReport struct {
	Mode string `json:"mode"`
	MCP  struct {
		Converted []string `json:"converted"`
		Drops     []struct {
			Server string `json:"server"`
			Field  string `json:"field"`
			Reason string `json:"reason"`
		} `json:"drops"`
	} `json:"mcp"`
	DroppedSettings []string `json:"dropped_settings"`
	PluginsSkipped  []string `json:"plugins_skipped"`
	SecretWarnings  []struct {
		Server string `json:"server"`
		Field  string `json:"field"`
	} `json:"secret_warnings"`
}

// TestScenario5SameHarnessCopy covers quickstart Scenario 5 (same-harness): the owned
// layer is carried verbatim, credentials and state stay empty, and owned values that
// look like secrets are reported as warnings (FR-033a).
func TestScenario5SameHarnessCopy(t *testing.T) {
	e := newEnv(t)
	e.run(nil, "add", "claude", "work", "--auth-kind", "auth-token", "--auth-key", "env:TOK", "--model", "m1", "--endpoint", "https://ep")
	e.run(nil, "sync")

	work := e.configDir("claude", "work")
	writeFile(t, filepath.Join(work, ".claude.json"), sourceClaudeJSON)

	stdout, _, err := e.tryRunSplit(nil, "copy", "claude/work", "claude/client-x", "--json")
	if err != nil {
		t.Fatalf("copy failed: %v\n%s", err, stdout)
	}
	var rep copyReport
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		t.Fatalf("copy --json not valid JSON: %v\n%s", err, stdout)
	}
	if rep.Mode != "same-harness" {
		t.Errorf("mode = %q, want same-harness", rep.Mode)
	}

	target := e.configDir("claude", "client-x")
	targetJSON := readFile(t, filepath.Join(target, ".claude.json"))

	// Owned mcpServers are carried verbatim, env reference preserved.
	for _, want := range []string{`"fs"`, `"remote"`, `"plain"`, `"leaky"`, "${TOKEN}", "${XTOKEN}"} {
		if !strings.Contains(targetJSON, want) {
			t.Errorf("target .claude.json missing %q:\n%s", want, targetJSON)
		}
	}
	// Credentials (oauthAccount) and state (projects) keys are NOT carried (FR-033a).
	if strings.Contains(targetJSON, "oauthAccount") {
		t.Error("credentials key oauthAccount must not be carried to the target")
	}
	if strings.Contains(targetJSON, "/some/project") {
		t.Error("state key projects must not be carried to the target")
	}
	// Target credentials file is empty (absent).
	if _, err := os.Stat(filepath.Join(target, ".credentials.json")); !os.IsNotExist(err) {
		t.Error("target must have an empty credentials layer (no .credentials.json)")
	}
	// Target state entry is a real empty dir, not a link.
	pj, err := os.Lstat(filepath.Join(target, "projects"))
	if err != nil {
		t.Fatalf("target projects/ not created: %v", err)
	}
	if pj.Mode()&os.ModeSymlink != 0 {
		t.Error("target projects/ must be a real dir, not a link")
	}

	// The secret-looking owned value is reported (FR-033a) but, being the user's own
	// owned file, is carried verbatim.
	if !hasWarning(rep.SecretWarnings, "leaky", "env[API_KEY]") {
		t.Errorf("expected a secret warning for leaky/env[API_KEY], got %+v", rep.SecretWarnings)
	}
	if !strings.Contains(targetJSON, canary) {
		t.Error("same-harness copy should carry the owned literal value verbatim")
	}
}

// TestScenario5CrossHarnessCopy covers quickstart Scenario 5 (cross-harness): MCP is
// converted to the target's native format, references use the target syntax, settings
// are dropped and reported, and plugins are not carried (FR-033b, FR-035, FR-036).
func TestScenario5CrossHarnessCopy(t *testing.T) {
	e := newEnv(t)
	e.run(nil, "add", "claude", "work", "--auth-kind", "auth-token", "--auth-key", "env:TOK", "--model", "m1", "--endpoint", "https://ep")
	e.run(nil, "sync")
	writeFile(t, filepath.Join(e.configDir("claude", "work"), ".claude.json"), sourceClaudeJSON)

	stdout, _, err := e.tryRunSplit(nil, "copy", "claude/work", "codex/work", "--json")
	if err != nil {
		t.Fatalf("cross-harness copy failed: %v\n%s", err, stdout)
	}
	var rep copyReport
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		t.Fatalf("copy --json not valid JSON: %v\n%s", err, stdout)
	}
	if rep.Mode != "cross-harness" {
		t.Errorf("mode = %q, want cross-harness", rep.Mode)
	}

	tomlPath := filepath.Join(e.configDir("codex", "work"), "config.toml")
	toml := readFile(t, tomlPath)

	// Servers are converted into Codex's native TOML, env references become env_vars.
	for _, want := range []string{"mcp_servers", "fs", "env_vars", "TOKEN"} {
		if !strings.Contains(toml, want) {
			t.Errorf("codex config.toml missing %q:\n%s", want, toml)
		}
	}
	// Claude's ${...} inline syntax must NOT appear in the Codex output (references are
	// expressed structurally there).
	if strings.Contains(toml, "${TOKEN}") {
		t.Errorf("Claude reference syntax leaked into codex config.toml:\n%s", toml)
	}
	// The literal secret is dropped and reported, never written (Constitution IV).
	if strings.Contains(toml, canary) {
		t.Errorf("literal secret leaked into codex config.toml:\n%s", toml)
	}
	if !hasDrop(rep.MCP.Drops, "leaky", "env[API_KEY]", "literal-secret") {
		t.Errorf("expected literal-secret drop for leaky/env[API_KEY], got %+v", rep.MCP.Drops)
	}

	// Settings dropped (FR-033b).
	for _, want := range []string{"model", "endpoint", "auth"} {
		if !contains(rep.DroppedSettings, want) {
			t.Errorf("dropped_settings missing %q: %v", want, rep.DroppedSettings)
		}
	}
	// Plugins not carried (FR-036).
	if len(rep.PluginsSkipped) == 0 {
		t.Error("expected plugins to be reported as skipped across harnesses")
	}
	// Cross-harness target takes the Codex default auth kind, not Claude's.
	showOut, _, err := e.tryRunSplit(nil, "show", "codex/work", "--json")
	if err != nil {
		t.Fatalf("show codex/work: %v\n%s", err, showOut)
	}
	if !strings.Contains(showOut, "chatgpt-login") {
		t.Errorf("cross-harness target should use the codex default auth kind:\n%s", showOut)
	}
}

// TestScenario6SecretCanarySweep covers quickstart Scenario 6 / SC-006: a literal
// secret is rejected on input, and a cross-harness conversion plants no secret in any
// file it writes.
func TestScenario6SecretCanarySweep(t *testing.T) {
	e := newEnv(t)

	// A literal secret on the command line is rejected (FR-028).
	if out, err := e.tryRun(nil, "add", "claude", "bad", "--auth-kind", "auth-token", "--auth-key", "sk-REALSECRETVALUE"); err == nil {
		t.Fatalf("add with a literal secret should fail:\n%s", out)
	}

	e.run(nil, "add", "claude", "work", "--auth-kind", "auth-token", "--auth-key", "env:TOK")
	e.run(nil, "sync")
	writeFile(t, filepath.Join(e.configDir("claude", "work"), ".claude.json"), sourceClaudeJSON)

	e.run(nil, "copy", "claude/work", "codex/work")
	e.run(nil, "copy", "claude/work", "opencode/work")
	e.run(nil, "copy", "claude/work", "pi/work")

	// Sweep every file a cross-harness copy wrote plus the profile store and registry:
	// the canary must appear nowhere. The source profile's own .claude.json is an owned
	// harness file that legitimately holds the planted value (Constitution IV exempts
	// owned files), so it is not part of the sweep.
	roots := []string{
		e.configDir("codex", "work"),
		e.configDir("opencode", "work"),
		e.configDir("pi", "work"),
		filepath.Join(e.root, ".config", "aprofile"),                // profile store
		filepath.Join(e.root, ".local", "share", "aprofile", "bin"), // wrappers
		filepath.Join(e.root, ".local", "share", "aprofile", "registry.json"),
	}
	for _, root := range roots {
		assertNoCanary(t, root)
	}
}

func assertNoCanary(t *testing.T, root string) {
	t.Helper()
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		if strings.Contains(string(data), canary) {
			t.Errorf("canary secret found in %s", path)
		}
		return nil
	})
}

func hasWarning(ws []struct {
	Server string `json:"server"`
	Field  string `json:"field"`
}, server, field string) bool {
	for _, w := range ws {
		if w.Server == server && w.Field == field {
			return true
		}
	}
	return false
}

func hasDrop(drops []struct {
	Server string `json:"server"`
	Field  string `json:"field"`
	Reason string `json:"reason"`
}, server, field, reason string) bool {
	for _, d := range drops {
		if d.Server == server && d.Field == field && d.Reason == reason {
			return true
		}
	}
	return false
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
