package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Canaries planted in MCP args and url (T061, FR-035).
const (
	argCanary = "ghp_CANARYARGLEAK00000000000000000000000" // betterleaks:allow
	urlCanary = "sk-ant-api03-CANARYURLLEAK000000000000"   // betterleaks:allow
)

// claudeArgsURLJSON holds literal secrets in an args element and a url query
// parameter, plus unmodeled Claude fields (cwd, timeout).
const claudeArgsURLJSON = `{
  "mcpServers": {
    "gh": {"command": "mcp-gh", "args": ["--token", "` + argCanary + `", "--verbose"], "cwd": "/w", "timeout": 30000},
    "web": {"type": "http", "url": "https://mcp.example.com/v1?key=` + urlCanary + `&mode=fast"}
  }
}`

// TestCrossHarnessCopyDropsArgsAndURLSecrets is the audit regression for T061: a
// cross-harness copy wrote args and url secrets verbatim into config.toml. Both must
// be dropped and reported, and unmodeled native fields must be reported too, in the
// JSON report and in the human-readable output.
func TestCrossHarnessCopyDropsArgsAndURLSecrets(t *testing.T) {
	e := newEnv(t)
	e.run(nil, "add", "claude", "work", "--auth-kind", "auth-token", "--auth-key", "env:TOK")
	e.run(nil, "sync")
	writeFile(t, filepath.Join(e.configDir("claude", "work"), ".claude.json"), claudeArgsURLJSON)

	stdout, _, err := e.tryRunSplit(nil, "copy", "claude/work", "codex/c1", "--json")
	if err != nil {
		t.Fatalf("copy failed: %v\n%s", err, stdout)
	}
	var rep copyReport
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		t.Fatalf("copy --json not valid JSON: %v\n%s", err, stdout)
	}
	toml := readFile(t, filepath.Join(e.configDir("codex", "c1"), "config.toml"))
	for _, c := range []string{argCanary, urlCanary} {
		if strings.Contains(toml, c) {
			t.Errorf("literal secret leaked into codex config.toml:\n%s", toml)
		}
	}
	if !strings.Contains(toml, "mode=fast") {
		t.Errorf("non-secret url parts lost:\n%s", toml)
	}
	// Dropping the positional token would turn "--verbose" into the token, so gh is
	// skipped rather than written with shifted args (its drops are still reported).
	if strings.Contains(toml, "mcp-gh") {
		t.Errorf("server with a dropped positional arg written:\n%s", toml)
	}
	skipped := false
	for _, s := range rep.MCP.Skipped {
		skipped = skipped || (s.Name == "gh" && strings.Contains(s.Reason, "args[1]"))
	}
	if !skipped {
		t.Errorf("gh not reported skipped: %+v", rep.MCP.Skipped)
	}
	for _, d := range []struct{ server, field, reason string }{
		{"gh", "args[1]", "literal-secret"},
		{"web", "url.query[key]", "literal-secret"},
		{"gh", "cwd", "unrepresentable"},
		{"gh", "timeout", "unrepresentable"},
	} {
		if !hasDrop(rep.MCP.Drops, d.server, d.field, d.reason) {
			t.Errorf("missing drop %s %s (%s): %+v", d.server, d.field, d.reason, rep.MCP.Drops)
		}
	}
	if strings.Contains(stdout, argCanary) || strings.Contains(stdout, urlCanary) {
		t.Errorf("report contains a secret value:\n%s", stdout)
	}

	// The human-readable report names the same drops.
	out := e.run(nil, "copy", "claude/work", "pi/p1")
	for _, want := range []string{"gh args[1] (literal-secret)", "web url.query[key] (literal-secret)", "gh cwd (unrepresentable)"} {
		if !strings.Contains(out, want) {
			t.Errorf("copy output missing %q:\n%s", want, out)
		}
	}
	e.run(nil, "add", "opencode", "o1")
	mcpOut := e.run(nil, "mcp", "copy", "claude/work", "opencode/o1")
	if !strings.Contains(mcpOut, "gh args[1] (literal-secret)") {
		t.Errorf("mcp copy output missing the args drop:\n%s", mcpOut)
	}
}

// TestSameHarnessCopyWarnsArgsAndURLSecrets proves a same-harness copy warns about
// literal secrets in args and url as well (FR-033a).
func TestSameHarnessCopyWarnsArgsAndURLSecrets(t *testing.T) {
	e := newEnv(t)
	e.run(nil, "add", "claude", "work", "--auth-kind", "auth-token", "--auth-key", "env:TOK")
	e.run(nil, "sync")
	writeFile(t, filepath.Join(e.configDir("claude", "work"), ".claude.json"), claudeArgsURLJSON)

	stdout, _, err := e.tryRunSplit(nil, "copy", "claude/work", "claude/twin", "--json")
	if err != nil {
		t.Fatalf("copy failed: %v\n%s", err, stdout)
	}
	var rep copyReport
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		t.Fatalf("copy --json not valid JSON: %v\n%s", err, stdout)
	}
	for _, w := range [][2]string{{"gh", "args[1]"}, {"web", "url.query[key]"}} {
		if !hasWarning(rep.SecretWarnings, w[0], w[1]) {
			t.Errorf("missing secret warning %s %s: %+v", w[0], w[1], rep.SecretWarnings)
		}
	}
}

// TestMCPFileModePreserved covers T064: copy and mcp copy keep an existing target MCP
// file's mode (a 0600 .claude.json stays 0600) and create a new MCP file as 0600.
func TestMCPFileModePreserved(t *testing.T) {
	e := newEnv(t)
	e.run(nil, "add", "claude", "work", "--auth-kind", "auth-token", "--auth-key", "env:TOK")
	e.run(nil, "add", "codex", "src")
	e.run(nil, "sync")
	writeFile(t, filepath.Join(e.configDir("claude", "work"), ".claude.json"), claudeArgsURLJSON)
	writeFile(t, filepath.Join(e.configDir("codex", "src"), "config.toml"), "[mcp_servers.tool]\ncommand = \"mcp-tool\"\n")

	// A new MCP file is created 0600.
	e.run(nil, "copy", "claude/work", "codex/fresh")
	assertMode(t, filepath.Join(e.configDir("codex", "fresh"), "config.toml"), 0o600)

	// An existing 0600 .claude.json stays 0600 through mcp copy.
	dst := filepath.Join(e.configDir("claude", "work"), ".claude.json")
	if err := os.Chmod(dst, 0o600); err != nil {
		t.Fatal(err)
	}
	e.run(nil, "mcp", "copy", "codex/src", "claude/work")
	assertMode(t, dst, 0o600)

	// An existing 0640 file keeps 0640 (mode preserved, not forced).
	if err := os.Chmod(dst, 0o640); err != nil {
		t.Fatal(err)
	}
	e.run(nil, "mcp", "copy", "codex/src", "claude/work", "--overwrite")
	assertMode(t, dst, 0o640)

	// copy --overwrite into an existing target keeps its 0600 .claude.json.
	if err := os.Chmod(dst, 0o600); err != nil {
		t.Fatal(err)
	}
	e.run(nil, "copy", "codex/src", "claude/work", "--overwrite", "--yes")
	assertMode(t, dst, 0o600)
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := fi.Mode().Perm(); got != want {
		t.Errorf("%s mode = %o, want %o", path, got, want)
	}
}
