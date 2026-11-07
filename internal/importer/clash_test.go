package importer_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/importer"
	"github.com/cc-deck/aprofile/internal/profile"
)

// T069, FR-002: import skips a cc-deck profile whose wrapper name another aprofile
// profile already has, names that profile, and reports it as a failed skip.
func TestImportRejectsWrapperNameClash(t *testing.T) {
	f := newFixture(t, `profiles:
  claude:
    x-aa:
      auth:
        login: true
    ok:
      auth:
        login: true
`)
	writeFile(t, filepath.Join(f.paths.HarnessesDir(), "cx.yaml"),
		"name: cx\nbinary: cx\nwrapper_prefix: claude-x\nconfig_env:\n  CX_DIR: \".\"\n", 0o644)
	set, err := harness.Load(f.paths.HarnessesDir())
	if err != nil {
		t.Fatal(err)
	}
	f.set = set
	if err := f.store.Save(&profile.Profile{Harness: "cx", Name: "aa"}); err != nil {
		t.Fatal(err)
	}

	for _, dry := range []bool{true, false} {
		res := f.run(t, importer.Options{DryRun: dry})
		act := action(t, res, "claude/x-aa")
		if act.Status != "skip" || !act.SkipClash || !strings.Contains(act.Reason, "cx/aa") || !strings.Contains(act.Reason, "claude-x-aa") {
			t.Errorf("dry=%v: clash action = %+v, want a skip naming cx/aa and claude-x-aa", dry, act)
		}
		if got := len(res.ClashSkips()); got != 1 {
			t.Errorf("dry=%v: ClashSkips = %d, want 1", dry, got)
		}
		if a := action(t, res, "claude/ok"); a.Status != "import" {
			t.Errorf("dry=%v: the other profile should import, got %+v", dry, a)
		}
	}
	if f.store.Exists("claude", "x-aa") {
		t.Errorf("a clashing profile must not be stored")
	}
}

// Two cc-deck profiles that would claim one wrapper name: the first imports, the
// second is skipped naming it, in a dry run as in a real run.
func TestImportRejectsClashWithinOneRun(t *testing.T) {
	f := newFixture(t, `profiles:
  claude:
    x-aa:
      auth:
        login: true
  cx:
    aa:
      auth:
        login: true
`)
	writeFile(t, filepath.Join(f.paths.HarnessesDir(), "cx.yaml"),
		"name: cx\nbinary: cx\nwrapper_prefix: claude-x\nconfig_env:\n  CX_DIR: \".\"\nauth_kinds:\n  - name: login\n    vars: {}\ndefault_auth_kind: login\n", 0o644)
	set, err := harness.Load(f.paths.HarnessesDir())
	if err != nil {
		t.Fatal(err)
	}
	f.set = set
	for _, dry := range []bool{true, false} {
		res := f.run(t, importer.Options{DryRun: dry})
		if len(res.ClashSkips()) != 1 {
			t.Errorf("dry=%v: want exactly one clash skip, got %+v", dry, res.Actions)
		}
	}
}
