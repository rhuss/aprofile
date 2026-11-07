package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/testutil"
)

// FR-044: sync reports a wrapper that a different executable earlier on PATH shadows,
// one note per shadowed wrapper, using the same check doctor runs.
func TestSyncReportsShadowedWrapper(t *testing.T) {
	h := testutil.HermeticHome(t)
	shadowDir := filepath.Join(h.Root, "shadow")
	testutil.WriteFile(t, filepath.Join(shadowDir, "claude-sh"), "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(filepath.Join(shadowDir, "claude-sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(h.DataHome, "aprofile", "bin")
	t.Setenv("PATH", shadowDir+string(os.PathListSeparator)+binDir+string(os.PathListSeparator)+"/usr/bin:/bin")

	for _, n := range []string{"sh", "free"} {
		if out, err := runCLI(t, "add", "claude", n, "--auth-kind", "oauth-login"); err != nil {
			t.Fatalf("add %s: %v\n%s", n, err, out)
		}
	}
	out, err := runCLI(t, "sync")
	if err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}
	want := `wrapper "claude-sh" is shadowed by ` + filepath.Join(shadowDir, "claude-sh")
	if strings.Count(out, want) != 1 {
		t.Errorf("sync should note the shadowed wrapper once:\n%s", out)
	}
	if strings.Contains(out, `"claude-free" is shadowed`) {
		t.Errorf("an unshadowed wrapper must not be reported:\n%s", out)
	}

	out, err = runCLI(t, "sync", "--json")
	if err != nil {
		t.Fatalf("sync --json: %v\n%s", err, out)
	}
	if !strings.Contains(out, `wrapper \"claude-sh\" is shadowed by`) {
		t.Errorf("sync --json should carry the shadowing note:\n%s", out)
	}
}
