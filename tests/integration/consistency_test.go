package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cxDefinition is a user harness whose wrapper prefix makes `cx/aa` and
// `claude/x-aa` share the wrapper name claude-x-aa.
const cxDefinition = "name: cx\nbinary: cx\nwrapper_prefix: claude-x\nconfig_env:\n  CX_DIR: \".\"\n" +
	"auth_kinds:\n  - name: login\n    vars: {}\ndefault_auth_kind: login\n"

func (e *env) writeHarness(file, content string) {
	e.t.Helper()
	writeFile(e.t, filepath.Join(e.root, ".config", "aprofile", "harnesses.d", file), content)
}

// T069, FR-002: add, rename and import refuse a wrapper name another profile has,
// naming that profile; nothing is changed.
func TestWrapperNameClashAcrossCommands(t *testing.T) {
	e := newEnv(t)
	e.writeHarness("cx.yaml", cxDefinition)
	e.run(nil, "add", "claude", "x-aa", "--auth-kind", "oauth-login")

	out, err := e.tryRun(nil, "add", "cx", "aa")
	if err == nil || !strings.Contains(out, "claude/x-aa") || !strings.Contains(out, "claude-x-aa") {
		t.Fatalf("add should refuse naming claude/x-aa and the wrapper: %v\n%s", err, out)
	}
	if _, err := os.Stat(e.profileStorePath("cx", "aa")); !os.IsNotExist(err) {
		t.Fatalf("refused add must not create a profile")
	}

	e.run(nil, "add", "cx", "bb")
	out, err = e.tryRun(nil, "rename", "cx/bb", "aa")
	if err == nil || !strings.Contains(out, "claude/x-aa") {
		t.Fatalf("rename should refuse naming claude/x-aa: %v\n%s", err, out)
	}
	if _, err := os.Stat(e.profileStorePath("cx", "bb")); err != nil {
		t.Fatalf("refused rename must keep cx/bb: %v", err)
	}

	writeFixtureFile(t, filepath.Join(e.root, ".config", "cc-deck", "config.yaml"),
		"profiles:\n  claude:\n    x-bb:\n      auth:\n        login: true\n    fine:\n      auth:\n        login: true\n")
	out, err = e.tryRun(nil, "import", "cc-deck")
	if exitCodeOf(err) == 0 || !strings.Contains(out, "cx/bb") || !strings.Contains(out, "claude-x-bb") {
		t.Fatalf("import should skip claude/x-bb naming cx/bb and exit non-zero: %v\n%s", err, out)
	}
	if _, err := os.Stat(e.profileStorePath("claude", "x-bb")); !os.IsNotExist(err) {
		t.Errorf("a clashing import must not be stored")
	}
	if _, err := os.Stat(e.profileStorePath("claude", "fine")); err != nil {
		t.Errorf("the other cc-deck profile should still import: %v", err)
	}
}

// T069, FR-002: two definitions with one wrapper_prefix are a load error.
func TestDuplicateWrapperPrefixReported(t *testing.T) {
	e := newEnv(t)
	e.writeHarness("dup.yaml", "name: dup\nbinary: dup\nwrapper_prefix: codex\nconfig_env:\n  DUP_DIR: \".\"\n")
	out := e.run(nil, "harness", "list")
	if !strings.Contains(out, "wrapper_prefix") || !strings.Contains(out, "codex") || !strings.Contains(out, "dup") || !strings.Contains(out, "more than one harness") {
		t.Fatalf("harness list should report the duplicate prefix:\n%s", out)
	}
}

// T075, FR-037: a server named twice in mcp copy is converted once and never
// reported as replaced by its own second copy.
func TestMCPCopyDedupesServerNames(t *testing.T) {
	e := newEnv(t)
	e.run(nil, "add", "claude", "src", "--auth-kind", "oauth-login")
	e.run(nil, "add", "claude", "dst", "--auth-kind", "oauth-login")
	e.run(nil, "sync")
	writeFile(t, filepath.Join(e.configDir("claude", "src"), ".claude.json"),
		`{"mcpServers":{"gh":{"command":"mcp-gh"},"web":{"type":"http","url":"https://mcp.example.com"}}}`)
	out := e.run(nil, "mcp", "copy", "claude/src", "claude/dst", "gh", "gh", "--overwrite")
	if !strings.Contains(out, "MCP servers converted: gh\n") || strings.Contains(out, "replaced") {
		t.Fatalf("gh should be converted once and not replaced:\n%s", out)
	}
}
