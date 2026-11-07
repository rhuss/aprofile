package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/testutil"
)

// FR-006b: after remove and rename the old wrapper stays until the next sync; the
// commands say so, list prints one line about leftover wrappers, and doctor reports
// each one. Unmarked files in the bin dir are never reported or touched.
func TestLeftoverWrappersAreReported(t *testing.T) {
	testutil.HermeticHome(t)
	t.Setenv("PATH", "/usr/bin:/bin")
	for _, n := range []string{"zap", "old", "keep"} {
		if out, err := runCLI(t, "add", "claude", n, "--auth-kind", "oauth-login"); err != nil {
			t.Fatalf("add %s: %v\n%s", n, err, out)
		}
	}
	if out, err := runCLI(t, "sync"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}
	paths, _ := config.ResolvePaths()
	mine := filepath.Join(paths.BinDir, "claude-mine")
	testutil.WriteFile(t, mine, "#!/bin/sh\necho mine\n")

	out, err := runCLI(t, "remove", "claude", "zap")
	if err != nil {
		t.Fatalf("remove: %v\n%s", err, out)
	}
	if !strings.Contains(out, "wrapper claude-zap is removed by the next aprofile sync") {
		t.Errorf("remove should say when the old wrapper goes:\n%s", out)
	}

	out, err = runCLI(t, "list")
	if err != nil {
		t.Fatalf("list: %v\n%s", err, out)
	}
	if !strings.Contains(out, "1 leftover wrapper from a removed or renamed profile: claude-zap (run: aprofile sync)") {
		t.Errorf("list should name the leftover wrapper:\n%s", out)
	}

	out, err = runCLI(t, "rename", "claude/old", "new", "--force")
	if err != nil {
		t.Fatalf("rename: %v\n%s", err, out)
	}
	if !strings.Contains(out, "wrapper claude-old is removed by the next aprofile sync") {
		t.Errorf("rename should say when the old wrapper goes:\n%s", out)
	}

	out, _ = runCLI(t, "list")
	if !strings.Contains(out, "2 leftover wrappers from removed or renamed profiles: claude-old, claude-zap (run: aprofile sync)") {
		t.Errorf("list should name both leftover wrappers:\n%s", out)
	}
	if strings.Contains(out, "claude-mine") {
		t.Errorf("an unmarked file must not be reported:\n%s", out)
	}
	out, _ = runCLI(t, "list", "--json")
	if !strings.Contains(out, `"leftover_wrappers": [`) || !strings.Contains(out, `"claude-zap"`) {
		t.Errorf("list --json should carry the leftover wrappers:\n%s", out)
	}

	out, err = runCLI(t, "doctor")
	if err == nil {
		t.Errorf("doctor must report leftover wrappers as problems")
	}
	for _, w := range []string{"claude-zap", "claude-old"} {
		if !strings.Contains(out, "leftover wrapper "+w+" in "+paths.BinDir+" belongs to no profile") {
			t.Errorf("doctor should report %s:\n%s", w, out)
		}
	}
	if !strings.Contains(out, "fix: aprofile sync") || strings.Contains(out, "claude-mine") {
		t.Errorf("doctor fix or unmarked file:\n%s", out)
	}

	if out, err := runCLI(t, "sync"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}
	out, _ = runCLI(t, "list")
	if strings.Contains(out, "leftover") {
		t.Errorf("after sync no leftover wrapper remains:\n%s", out)
	}
	if b, err := os.ReadFile(mine); err != nil || string(b) != "#!/bin/sh\necho mine\n" {
		t.Errorf("the unmarked file must be untouched: %q %v", b, err)
	}
}
