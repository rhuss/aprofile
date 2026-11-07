package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/testutil"
)

// copyOverwriteSetup creates claude/src and claude/dst, each with its own MCP
// servers in .claude.json, and returns the paths of dst's store file and
// .claude.json.
func copyOverwriteSetup(t *testing.T) (storeFile, mcpFile string) {
	t.Helper()
	testutil.HermeticHome(t)
	t.Setenv("PATH", "/usr/bin:/bin")
	for _, n := range []string{"src", "dst"} {
		if out, err := runCLI(t, "add", "claude", n, "--auth-kind", "oauth-login", "--model", "model-"+n); err != nil {
			t.Fatalf("add %s: %v\n%s", n, err, out)
		}
	}
	paths, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"src", "dst"} {
		testutil.WriteFile(t, filepath.Join(paths.ProfileConfigDir("claude", n), ".claude.json"),
			`{"theme":"dark","mcpServers":{"`+n+`-server":{"command":"`+n+`"}}}`)
	}
	return paths.ProfileStorePath("claude", "dst"), filepath.Join(paths.ProfileConfigDir("claude", "dst"), ".claude.json")
}

// README promise: copy --overwrite lists what it replaces and asks first; off a
// terminal it needs --yes (exit 3, nothing changed).
func TestCopyOverwriteNeedsYesOffTerminal(t *testing.T) {
	storeFile, mcpFile := copyOverwriteSetup(t)
	storeBefore, _ := os.ReadFile(storeFile)
	mcpBefore, _ := os.ReadFile(mcpFile)

	out, err := runCLI(t, "copy", "claude/src", "claude/dst", "--overwrite")
	if exitCode(err) != ExitNeedsConfirmation {
		t.Fatalf("copy --overwrite without --yes off a terminal: exit %d, want 3: %v\n%s", exitCode(err), err, out)
	}
	if !strings.Contains(out, "copy onto existing profile claude/dst replaces:") || !strings.Contains(out, storeFile) || !strings.Contains(out, mcpFile) {
		t.Errorf("copy should list what it replaces:\n%s", out)
	}
	if !strings.Contains(err.Error(), "re-run with --yes (nothing changed)") {
		t.Errorf("the error should name --yes: %v", err)
	}
	if after, _ := os.ReadFile(storeFile); string(after) != string(storeBefore) {
		t.Errorf("the target's store file changed:\n%s", after)
	}
	if after, _ := os.ReadFile(mcpFile); string(after) != string(mcpBefore) {
		t.Errorf("the target's MCP file changed:\n%s", after)
	}
	paths, _ := config.ResolvePaths()
	if _, err := os.Stat(filepath.Dir(paths.BackupDir("claude", "dst", "x"))); err == nil {
		t.Errorf("a refused copy must not create a backup")
	}
}

// With --yes the target's replaced data is backed up first, then the copy runs.
func TestCopyOverwriteBacksUpWithYes(t *testing.T) {
	storeFile, mcpFile := copyOverwriteSetup(t)
	storeBefore, _ := os.ReadFile(storeFile)
	mcpBefore, _ := os.ReadFile(mcpFile)

	out, err := runCLI(t, "copy", "claude/src", "claude/dst", "--overwrite", "--yes")
	if err != nil {
		t.Fatalf("copy --overwrite --yes: %v\n%s", err, out)
	}
	paths, _ := config.ResolvePaths()
	backups, _ := filepath.Glob(filepath.Join(filepath.Dir(paths.BackupDir("claude", "dst", "x")), "*"))
	if len(backups) != 1 {
		t.Fatalf("want one backup dir, got %v\n%s", backups, out)
	}
	if !strings.Contains(out, "backed up the replaced data of claude/dst to "+backups[0]) {
		t.Errorf("copy should name the backup dir:\n%s", out)
	}
	if b, _ := os.ReadFile(filepath.Join(backups[0], filepath.Base(storeFile))); string(b) != string(storeBefore) {
		t.Errorf("store file not backed up: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(backups[0], ".claude.json")); string(b) != string(mcpBefore) {
		t.Errorf("MCP file not backed up: %q", b)
	}
	if after, _ := os.ReadFile(storeFile); !strings.Contains(string(after), "model-src") {
		t.Errorf("the copy did not run:\n%s", after)
	}

	out, err = runCLI(t, "copy", "claude/src", "claude/dst", "--overwrite", "--yes", "--json")
	if err != nil {
		t.Fatalf("copy --json: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"backup": "`) {
		t.Errorf("copy --json should carry the backup dir:\n%s", out)
	}
}

// On a terminal the user is asked; "n" leaves everything unchanged.
func TestCopyOverwriteAsksOnTerminal(t *testing.T) {
	storeFile, _ := copyOverwriteSetup(t)
	storeBefore, _ := os.ReadFile(storeFile)
	orig := stdinIsTTY
	defer func() { stdinIsTTY = orig }()
	stdinIsTTY = func(*cobra.Command) bool { return true }

	out, err := runCLIInput(t, "n\n", "copy", "claude/src", "claude/dst", "--overwrite")
	if err != nil {
		t.Fatalf("declined copy should exit 0: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Replace this data of claude/dst (a backup is made first)? [y/N]") || !strings.Contains(out, "aborted; nothing changed") {
		t.Errorf("copy should ask and report the abort:\n%s", out)
	}
	if after, _ := os.ReadFile(storeFile); string(after) != string(storeBefore) {
		t.Errorf("a declined copy changed the target")
	}
	out, err = runCLIInput(t, "y\n", "copy", "claude/src", "claude/dst", "--overwrite")
	if err != nil || !strings.Contains(out, "backed up the replaced data of claude/dst") {
		t.Errorf("an accepted copy should back up and run: %v\n%s", err, out)
	}
}
