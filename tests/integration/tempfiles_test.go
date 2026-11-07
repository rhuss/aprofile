package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cc-deck/aprofile/internal/testutil"
)

// T091 (Prod6): temp files an interrupted atomic write leaves in a config dir are
// not unclassified entries: doctor does not report them, and sync removes the stale
// ones as changes instead of advising to classify them. A temp directory is stale
// only when everything in it is.
func TestTempFilesNotReportedAndStaleRemoved(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login", "--share", "content")
	e.run(nil, "sync")
	dir := e.configDir("claude", "work")
	stale := filepath.Join(dir, ".aprofile-tmp-4711")
	staleDir := filepath.Join(dir, ".skills.aprofile-new-12-old")
	fresh := filepath.Join(dir, ".settings.json.aprofile-tmp-99")
	writeFile(t, stale, "partial")
	writeFile(t, filepath.Join(staleDir, "x.md"), "partial")
	writeFile(t, fresh, "in progress")
	old := time.Now().Add(-10 * time.Minute)
	for _, p := range []string{stale, filepath.Join(staleDir, "x.md"), staleDir} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}

	dout, _ := e.tryRun(nil, "doctor")
	if strings.Contains(dout, "aprofile-tmp") || strings.Contains(dout, "aprofile-new") {
		t.Errorf("doctor must not report aprofile's own temp files:\n%s", dout)
	}

	out := e.run(nil, "sync")
	if strings.Contains(out, "unclassified") || strings.Contains(out, "classify") {
		t.Errorf("sync must not advise classifying temp files:\n%s", out)
	}
	if !strings.Contains(out, "remove-temp .aprofile-tmp-4711") || !strings.Contains(out, "remove-temp .skills.aprofile-new-12-old") {
		t.Errorf("sync should list the removed stale temp entries as changes:\n%s", out)
	}
	if strings.Contains(out, "0 changes") || strings.Contains(out, "up to date") {
		t.Errorf("a sync that removed temp entries made changes:\n%s", out)
	}
	for _, p := range []string{stale, staleDir} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("%s should be removed", p)
		}
	}
	if _, err := os.Lstat(fresh); err != nil {
		t.Errorf("a fresh temp file must stay: %v", err)
	}
}
