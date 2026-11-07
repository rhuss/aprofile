package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/testutil"
)

// mcpTestProfiles creates two synced claude profiles and returns their config dirs.
func mcpTestProfiles(t *testing.T, names ...string) map[string]string {
	t.Helper()
	testutil.HermeticHome(t)
	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	dirs := map[string]string{}
	for _, n := range names {
		if out, err := runCLI(t, "add", "claude", n, "--auth-kind", "oauth-login"); err != nil {
			t.Fatalf("add %s: %v\n%s", n, err, out)
		}
		dirs[n] = p.ProfileConfigDir("claude", n)
	}
	if out, err := runCLI(t, "sync"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}
	return dirs
}

// Sec4: mcp list masks url secrets (userinfo, query, fragment, path) in its text and
// --json output.
func TestMCPListRedactsURLSecrets(t *testing.T) {
	const tok = "ghp_CANARYMCPLIST000000000000000000000000"
	dirs := mcpTestProfiles(t, "work")
	doc := `{"mcpServers":{
		"q":{"type":"http","url":"https://bot:` + tok + `@h.example.com/mcp?key=` + tok + `&mode=fast"},
		"p":{"type":"http","url":"https://h.example.com/` + tok + `/mcp#tok=` + tok + `"}}}`
	if err := os.WriteFile(filepath.Join(dirs["work"], ".claude.json"), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"mcp", "list", "claude/work"}, {"mcp", "list", "claude/work", "--json"}} {
		out, err := runCLI(t, args...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		if strings.Contains(out, tok) {
			t.Errorf("%v leaks the url secret:\n%s", args, out)
		}
		if !strings.Contains(out, "mode=fast") || !strings.Contains(out, "***") {
			t.Errorf("%v: url not shown masked:\n%s", args, out)
		}
	}
}

// Corr6: mcp copy and copy's MCP conversion fail on an unreadable target MCP file instead of treating it as
// empty and overwriting it.
func TestMCPCopyUnreadableTargetFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a mode-000 file")
	}
	dirs := mcpTestProfiles(t, "src", "dst")
	if err := os.WriteFile(filepath.Join(dirs["src"], ".claude.json"), []byte(`{"mcpServers":{"gh":{"command":"mcp-gh"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dirs["dst"], ".claude.json")
	const keep = `{"mcpServers":{"mine":{"command":"keep-me"}},"oauthAccount":{"x":1}}`
	if err := os.WriteFile(dst, []byte(keep), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dst, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dst, 0o600) })
	if out, err := runCLI(t, "mcp", "copy", "claude/src", "claude/dst"); err == nil {
		t.Errorf("mcp copy onto an unreadable target succeeded:\n%s", out)
	}
	// The copy conversion path (convertMCP, cross-harness) fails the same way.
	claude := copyTestDef(t, "claude")
	if _, err := convertMCP(claude, claude, dirs["src"], dirs["dst"], true); err == nil {
		t.Error("convertMCP onto an unreadable target MCP file succeeded")
	}
	_ = os.Chmod(dst, 0o600)
	if got, _ := os.ReadFile(dst); string(got) != keep {
		t.Errorf("target MCP file changed:\n%s", got)
	}
}
