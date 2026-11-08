package sync

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/testutil"
)

// histDef is a data-only harness with history entries (spec 003).
const histDef = `name: hist
binary: sh
wrapper_prefix: hist
config_env: { HIST_DIR: "." }
default_config_location: "~/.hist"
classification:
  - { entry: "auth.json", layer: credentials }
  - { entry: "sessions/", layer: state, history: true, merge: per-file }
  - { entry: "history.jsonl", layer: state, history: true }
  - { entry: "logs/", layer: state }
`

// T014: sync links a member's history entries to its store, leaves the default
// setup unwritten, and skips history handling for a profile of an unknown group.
func TestSyncLinksMemberHistory(t *testing.T) {
	paths, set, store := setup(t, map[string]string{"hist": histDef})
	home := paths.Home
	testutil.WriteFile(t, filepath.Join(home, ".hist", "sessions", "s1.jsonl"), "{}")
	for _, p := range []*profile.Profile{
		{Harness: "hist", Name: "lab", Share: profile.ShareAll, HistoryGroup: "home"},
		{Harness: "hist", Name: "ghost", Share: profile.ShareAll, HistoryGroup: "nope"},
	} {
		if err := store.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	ghostDir := paths.ProfileConfigDir("hist", "ghost")
	testutil.WriteFile(t, filepath.Join(ghostDir, "sessions", "own.jsonl"), "{}")
	if err := os.Symlink(filepath.Join(home, ".hist", "history.jsonl"), filepath.Join(ghostDir, "history.jsonl")); err != nil {
		t.Fatal(err)
	}

	res, err := Run(paths, set, store, nil, Options{FixLinks: true})
	if err != nil {
		t.Fatal(err)
	}
	lab := paths.ProfileConfigDir("hist", "lab")
	for _, e := range []string{"sessions", "history.jsonl"} {
		got, err := os.Readlink(filepath.Join(lab, e))
		if err != nil || got != filepath.Join(home, ".hist", e) {
			t.Errorf("lab %s -> %q, %v", e, got, err)
		}
	}
	if fi, err := os.Lstat(filepath.Join(lab, "logs")); err != nil || !fi.IsDir() {
		t.Errorf("lab logs must be a private directory: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".hist", "history.jsonl")); !os.IsNotExist(err) {
		t.Error("sync wrote into the default setup")
	}
	// Unknown group: history entries are left exactly as they are, even with
	// --fix-links, and the condition is reported.
	if target, err := os.Readlink(filepath.Join(ghostDir, "history.jsonl")); err != nil || target != filepath.Join(home, ".hist", "history.jsonl") {
		t.Errorf("ghost history.jsonl changed: %q, %v", target, err)
	}
	if count(res.Notes, "hist/nope does not exist") == 0 {
		t.Errorf("unknown group not reported: %v", res.Notes)
	}
	// Second run: nothing changes.
	again, err := Run(paths, set, store, nil, Options{FixLinks: true})
	if err != nil {
		t.Fatal(err)
	}
	if again.ConfigDirChanges != 0 {
		t.Errorf("second sync changed config dirs: %v", again.Changes)
	}
}

// T029: a member's real history entry is reported in Result.Notes and left.
func TestSyncReportsMemberRealHistoryEntry(t *testing.T) {
	paths, set, store := setup(t, map[string]string{"hist": histDef})
	if err := os.MkdirAll(filepath.Join(paths.Home, ".hist", "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(&profile.Profile{Harness: "hist", Name: "lab", Share: profile.ShareAll, HistoryGroup: "home"}); err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, filepath.Join(paths.ProfileConfigDir("hist", "lab"), "sessions", "own.jsonl"), "{}")
	res, err := Run(paths, set, store, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if count(res.Notes, "aprofile edit hist/lab --group home") != 1 {
		t.Errorf("Result.Notes = %v", res.Notes)
	}
	if fi, err := os.Lstat(filepath.Join(paths.ProfileConfigDir("hist", "lab"), "sessions")); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Error("sync replaced the real history entry")
	}
}
