package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/testutil"
)

func testCmd(input string) (*cobra.Command, *bytes.Buffer) {
	cmd := &cobra.Command{}
	out := &bytes.Buffer{}
	cmd.SetIn(strings.NewReader(input))
	cmd.SetOut(out)
	return cmd, out
}

func TestConfirmOrYes(t *testing.T) {
	cmd, _ := testCmd("")
	if err := confirmOrYes(cmd, "replace", true); err != nil {
		t.Errorf("--yes should proceed: %v", err)
	}
	if err := confirmOrYes(cmd, "replace", false); exitCode(err) != ExitNeedsConfirmation {
		t.Errorf("off a TTY without --yes: exit %d, want 3 (%v)", exitCode(err), err)
	}

	orig := stdinIsTTY
	defer func() { stdinIsTTY = orig }()
	stdinIsTTY = func(*cobra.Command) bool { return true }
	yesCmd, out := testCmd("y\n")
	if err := confirmOrYes(yesCmd, "Replace settings?", false); err != nil {
		t.Errorf("answer y should proceed: %v", err)
	}
	if !strings.Contains(out.String(), "Replace settings?") {
		t.Errorf("prompt not shown: %q", out.String())
	}
	noCmd, _ := testCmd("n\n")
	if err := confirmOrYes(noCmd, "Replace?", false); err != errDeclined {
		t.Errorf("answer n should decline, got %v", err)
	}
}

func TestBackupPaths(t *testing.T) {
	testutil.HermeticHome(t)
	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	src := t.TempDir()
	file := filepath.Join(src, "settings.json")
	testutil.WriteFile(t, file, `{"a":1}`)
	testutil.WriteFile(t, filepath.Join(src, "rules", "default.rules"), "allow ls\n")
	target := filepath.Join(src, "target.md")
	testutil.WriteFile(t, target, "target")
	link := filepath.Join(src, "CLAUDE.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	dir, err := backupPaths(p, "claude", "work", []string{file, filepath.Join(src, "rules"), link, filepath.Join(src, "missing")}, now)
	if err != nil {
		t.Fatal(err)
	}
	if want := p.BackupDir("claude", "work", "20261104-100000.000000"); dir != want {
		t.Errorf("backup dir = %q, want %q", dir, want)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "settings.json")); string(b) != `{"a":1}` {
		t.Errorf("file not backed up: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "rules", "default.rules")); string(b) != "allow ls\n" {
		t.Errorf("dir not backed up: %q", b)
	}
	if l, err := os.Readlink(filepath.Join(dir, "CLAUDE.md")); err != nil || l != target {
		t.Errorf("symlink not recorded as link: %q %v", l, err)
	}
	if none, err := backupPaths(p, "claude", "work", []string{filepath.Join(src, "missing")}, now); err != nil || none != "" {
		t.Errorf("nothing to back up should return empty: %q %v", none, err)
	}
}
