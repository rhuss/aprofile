package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cc-deck/aprofile/internal/testutil"
)

// T097 (Notable-3, FR-022): a sync with problems and no changes does not call
// itself "already up to date"; its summary counts the problems.
func TestSyncSummaryCountsProblems(t *testing.T) {
	h := testutil.HermeticHome(t)
	if out, err := runCLI(t, "add", "claude", "ok", "--auth-kind", "oauth-login"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	if out, err := runCLI(t, "sync"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}
	out, err := runCLI(t, "sync")
	if err != nil || !strings.Contains(out, "already up to date (0 changes)") {
		t.Fatalf("a clean unchanged sync is up to date: %v\n%s", err, out)
	}

	store := filepath.Join(h.ConfigHome, "aprofile", "profiles", "claude")
	testutil.WriteFile(t, filepath.Join(store, "broken.yaml"), "harness: [not a profile\n")
	testutil.WriteFile(t, filepath.Join(store, "broken2.yaml"), "name: {\n")

	out, err = runCLI(t, "sync")
	if err == nil {
		t.Fatalf("a sync with problems fails:\n%s", out)
	}
	if strings.Contains(out, "already up to date") {
		t.Errorf("a sync with problems is not up to date:\n%s", out)
	}
	if !strings.Contains(out, "0 changes, 2 problems (see above)") {
		t.Errorf("the summary should count the problems:\n%s", out)
	}

	out, _ = runCLI(t, "sync", "--dry-run")
	if !strings.Contains(out, "(dry-run) no changes, 2 problems (see above)") {
		t.Errorf("the dry-run summary should count the problems:\n%s", out)
	}
}

// Review follow-up (T091): removing a stale temp entry is a change, so the
// summary never says "0 changes" or "up to date" after deleting files, and a dry
// run lists the removal as a planned change.
func TestSyncSummaryCountsTempRemovals(t *testing.T) {
	h := testutil.HermeticHome(t)
	if out, err := runCLI(t, "add", "claude", "ok", "--auth-kind", "oauth-login"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	if out, err := runCLI(t, "sync"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}
	stale := filepath.Join(h.DataHome, "aprofile", "profiles", "claude", "ok", ".aprofile-tmp-31")
	testutil.WriteFile(t, stale, "partial")
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	out, err := runCLI(t, "sync", "--dry-run")
	if err != nil || !strings.Contains(out, "remove-temp .aprofile-tmp-31") || !strings.Contains(out, "(dry-run) changes listed above") {
		t.Errorf("a dry run lists the removal as a planned change: %v\n%s", err, out)
	}
	out, err = runCLI(t, "sync")
	if err != nil || !strings.Contains(out, "remove-temp .aprofile-tmp-31") {
		t.Fatalf("sync lists the removal as a change: %v\n%s", err, out)
	}
	if strings.Contains(out, "0 changes") || strings.Contains(out, "up to date") {
		t.Errorf("a sync that removed a file is not up to date:\n%s", out)
	}
	if _, err := os.Lstat(stale); !os.IsNotExist(err) {
		t.Error("the stale temp entry should be gone")
	}
}
