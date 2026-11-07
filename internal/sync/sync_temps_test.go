package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/testutil"
)

// T091 (Prod6): sync removes aprofile's own stale temp entries from a profile config
// dir, keeps fresh ones, and never reports either as unclassified. A removal is a
// change of the run (a dry run lists it as a planned change and removes nothing).
func TestSyncRemovesStaleTemps(t *testing.T) {
	paths, set, store := setup(t, map[string]string{"toy": "name: toy\nbinary: sh\nwrapper_prefix: toy\nconfig_env:\n  TOY_DIR: \".\"\n"})
	if err := store.Save(&profile.Profile{Harness: "toy", Name: "work"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(paths, set, store, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	dir := paths.ProfileConfigDir("toy", "work")
	stale := filepath.Join(dir, ".aprofile-tmp-123")
	fresh := filepath.Join(dir, ".settings.json.aprofile-tmp-9")
	testutil.WriteFile(t, stale, "partial")
	testutil.WriteFile(t, fresh, "in progress")
	old := time.Now().Add(-5 * time.Minute)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	res, err := Run(paths, set, store, nil, Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if count(res.Changes, "remove-temp .aprofile-tmp-123") != 1 || !res.Changed() {
		t.Errorf("dry run should plan the removal as a change:\n%s", strings.Join(res.Changes, "\n"))
	}
	if _, err := os.Lstat(stale); err != nil {
		t.Error("a dry run must not remove the stale temp entry")
	}

	res, err = Run(paths, set, store, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if count(res.Changes, "remove-temp .aprofile-tmp-123") != 1 || res.ConfigDirChanges != 1 || !res.Changed() {
		t.Errorf("sync should count the removal as a change (%d):\n%s", res.ConfigDirChanges, strings.Join(res.Changes, "\n"))
	}
	if count(res.Notes, "aprofile-tmp-9") != 0 || count(res.Changes, "aprofile-tmp-9") != 0 || count(res.Notes, "unclassified") != 0 {
		t.Errorf("fresh temp entries are neither removed nor reported:\n%s", strings.Join(res.Notes, "\n"))
	}
	if _, err := os.Lstat(stale); !os.IsNotExist(err) {
		t.Error("the stale temp entry should be gone")
	}
	if _, err := os.Lstat(fresh); err != nil {
		t.Error("a fresh temp entry may belong to a running write: keep it")
	}
	if res.HasProblems() {
		t.Errorf("no problems expected: %v", res.Problems)
	}
}

// Review follow-up: a stale temp entry sync cannot remove is a problem of the run
// (naming the path), not a note; an interrupted swap is restored as a change.
func TestSyncTempFailuresAreProblems(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks do not apply to root")
	}
	paths, set, store := setup(t, map[string]string{"toy": "name: toy\nbinary: sh\nwrapper_prefix: toy\nconfig_env:\n  TOY_DIR: \".\"\n"})
	if err := store.Save(&profile.Profile{Harness: "toy", Name: "work"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(paths, set, store, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	dir := paths.ProfileConfigDir("toy", "work")
	old := time.Now().Add(-5 * time.Minute)
	swapOld := filepath.Join(dir, ".notes.aprofile-new-4-old")
	testutil.WriteFile(t, swapOld, "the only copy")
	if err := os.Chtimes(swapOld, old, old); err != nil {
		t.Fatal(err)
	}
	res, err := Run(paths, set, store, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if count(res.Changes, "restore notes") != 1 || !res.Changed() {
		t.Errorf("the interrupted swap should be restored as a change:\n%s", strings.Join(res.Changes, "\n"))
	}
	if count(res.Notes, ".notes.aprofile-new-4-old") != 1 {
		t.Errorf("the restore should be explained in a note:\n%s", strings.Join(res.Notes, "\n"))
	}
	if data, err := os.ReadFile(filepath.Join(dir, "notes")); err != nil || string(data) != "the only copy" {
		t.Errorf("notes not restored: %q, %v", data, err)
	}

	stale := filepath.Join(dir, ".aprofile-tmp-77")
	testutil.WriteFile(t, stale, "partial")
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	res, err = Run(paths, set, store, nil, Options{})
	_ = os.Chmod(dir, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if count(res.Problems, stale) != 1 || !res.HasProblems() {
		t.Errorf("a failed removal is a problem naming the path:\nproblems: %v\nnotes: %v", res.Problems, res.Notes)
	}
	if count(res.Notes, stale) != 0 || count(res.Changes, ".aprofile-tmp-77") != 0 {
		t.Errorf("a failed removal is neither a note nor a change:\n%v\n%v", res.Notes, res.Changes)
	}
}
