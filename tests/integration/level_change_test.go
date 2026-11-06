package integration

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cc-deck/aprofile/internal/testutil"
)

// planLines extracts the step lines edit --share prints between its header and
// the "profile data replaced" summary.
func planLines(out string) []string {
	var lines []string
	in := false
	for _, l := range strings.Split(out, "\n") {
		switch {
		case strings.Contains(l, "share level ") && strings.HasSuffix(l, ":") && strings.Contains(l, " -> "):
			in = true
		case strings.HasPrefix(l, "profile data replaced"):
			in = false
		case in && strings.HasPrefix(l, "  "):
			lines = append(lines, l)
		}
	}
	return lines
}

func exitCodeOf(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	if err != nil {
		return -1
	}
	return 0
}

// TestSC006DryRunMatchesApply covers SC-006 and FR-016: for every pair of levels and
// every built-in harness, applying a share level change performs exactly the steps
// its dry run listed, and afterwards sync has nothing left to change.
func TestSC006DryRunMatchesApply(t *testing.T) {
	e := newEnv(t)
	for _, h := range allHarnesses {
		testutil.SeedDefaultsAt(t, e.root, h)
	}
	type pair struct{ from, to string }
	var pairs []pair
	for _, f := range allLevels {
		for _, to := range allLevels {
			if f != to {
				pairs = append(pairs, pair{f, to})
			}
		}
	}
	for _, h := range allHarnesses {
		for _, p := range pairs {
			e.run(nil, "add", h, "p-"+p.from+"-"+p.to, "--auth-kind", interactiveKind[h], "--share", p.from)
		}
	}
	e.run(nil, "sync")
	for _, h := range allHarnesses {
		for _, p := range pairs {
			ref := h + "/p-" + p.from + "-" + p.to
			dry := e.run(nil, "edit", ref, "--share", p.to, "--dry-run")
			if !strings.Contains(dry, "(dry-run)") || !strings.Contains(dry, "profile data replaced:") {
				t.Errorf("%s %s->%s: dry run output:\n%s", ref, p.from, p.to, dry)
			}
			want := planLines(dry)
			if strings.Contains(e.run(nil, "show", ref), "share level:    "+p.to) {
				t.Errorf("%s: a dry run must not change the level", ref)
			}
			dir := e.configDir(h, "p-"+p.from+"-"+p.to)
			before := entrySnapshot(t, dir)
			defaultsBefore := snapshotTree(t, e.root+"/.claude")
			applied := e.run(nil, "edit", ref, "--share", p.to, "--yes")
			got := planLines(applied)
			// The filesystem changes are exactly the entries the dry run named.
			listed := plannedEntries(want)
			for path := range diffSnapshots(before, entrySnapshot(t, dir)) {
				if !coveredBy(path, listed) {
					t.Errorf("%s %s->%s: %s changed but the dry run did not list it\n%s", ref, p.from, p.to, path, strings.Join(want, "\n"))
				}
			}
			assertTreeUnchanged(t, "default setup", defaultsBefore, snapshotTree(t, e.root+"/.claude"))
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Errorf("%s %s->%s: applied steps differ from the dry run\ndry:\n%s\napplied:\n%s", ref, p.from, p.to,
					strings.Join(want, "\n"), strings.Join(got, "\n"))
			}
			if p.to != "all" && strings.Contains(applied, "[replaces profile data]") {
				t.Errorf("%s %s->%s: only a move to all may replace profile data:\n%s", ref, p.from, p.to, applied)
			}
		}
	}
	// Afterwards sync has no config dir change left for these profiles.
	out := e.run(nil, "sync", "--dry-run")
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "config-dir ") {
			t.Errorf("sync still has config dir work after the level changes: %s", l)
		}
	}
}

// TestLevelChangeDryRunContent covers US3 scenario 2 and the all-to-content edge
// case: the dry run lists each change, says nothing is deleted, and lists the
// default setup's hooks with the hook-sets import hint.
func TestLevelChangeDryRunContent(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login", "--share", "all")
	e.run(nil, "sync")
	out := e.run(nil, "edit", "claude/work", "--share", "content", "--dry-run")
	for _, want := range []string{"unlink settings.json", "copy-once settings.json", "PreToolUse (1 group)", "aprofile hook-sets import claude", "nothing is deleted"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry run missing %q:\n%s", want, out)
		}
	}
	if fi, _ := os.Lstat(filepath.Join(e.configDir("claude", "work"), "settings.json")); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("the dry run changed settings.json")
	}
}

// TestLevelChangeToAllNeedsConfirmation covers US3 scenario 3, FR-017 and exit 3:
// moving to all replaces the profile's own settings only with --yes (off a TTY),
// after a backup.
func TestLevelChangeToAllNeedsConfirmation(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login", "--share", "content")
	e.run(nil, "sync")
	settings := filepath.Join(e.configDir("claude", "work"), "settings.json")
	writeFile(t, settings, `{"permissions":{"allow":["Bash(work-only)"]}}`)

	out, err := e.tryRun(nil, "edit", "claude/work", "--share", "all")
	if exitCodeOf(err) != 3 {
		t.Fatalf("without --yes off a TTY: exit %d, want 3\n%s", exitCodeOf(err), out)
	}
	if fi, _ := os.Lstat(settings); fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("nothing may change without confirmation")
	}
	if !strings.Contains(e.run(nil, "show", "claude/work"), "share level:    content") {
		t.Error("the level must stay content when the change is refused")
	}
	out = e.run(nil, "edit", "claude/work", "--share", "all", "--yes")
	if !strings.Contains(out, "replace-with-link settings.json") || !strings.Contains(out, "backed up settings.json to") {
		t.Errorf("the replace and its backup should be reported:\n%s", out)
	}
	if fi, _ := os.Lstat(settings); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("settings.json should now be the shared link")
	}
	backups, _ := filepath.Glob(filepath.Join(e.root, ".local", "share", "aprofile", "backups", "claude", "work", "*", "settings.json"))
	if len(backups) != 1 || !strings.Contains(readFile(t, backups[0]), "work-only") {
		t.Errorf("the profile's own settings must be backed up: %v", backups)
	}
}

// TestLevelChangeNeverSynced covers a level change on a profile that was never
// synced: the config dir is created at the new level.
func TestLevelChangeNeverSynced(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	e.run(nil, "add", "claude", "fresh", "--auth-kind", "oauth-login", "--share", "content")
	out := e.run(nil, "edit", "claude/fresh", "--share", "none")
	if !strings.Contains(out, "mkdir .") {
		t.Errorf("the plan should create the config dir:\n%s", out)
	}
	if l := links(t, e.configDir("claude", "fresh")); len(l) != 0 {
		t.Errorf("a none profile links nothing: %v", l)
	}
}

// TestLevelChangeRefusedWhileRunning covers FR-017 and exit 4.
func TestLevelChangeRefusedWhileRunning(t *testing.T) {
	e := newEnv(t)
	e.run(nil, "add", "claude", "busy", "--auth-kind", "oauth-login", "--share", "content")
	e.run(nil, "sync")
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperRunningHarness$")
	cmd.Env = append(os.Environ(), "APROFILE_CONFIG_DIR="+e.configDir("claude", "busy"), "APROFILE_TEST_HELPER=1")
	if err := cmd.Start(); err != nil {
		t.Skip(err)
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()
	time.Sleep(200 * time.Millisecond)
	out, err := e.tryRun(nil, "edit", "claude/busy", "--share", "none", "--yes")
	if exitCodeOf(err) != 4 {
		t.Fatalf("a running profile: exit %d, want 4\n%s", exitCodeOf(err), out)
	}
	if !strings.Contains(out, "running") {
		t.Errorf("the refusal should say the profile is running:\n%s", out)
	}
}

// TestHelperRunningHarness stands in for a harness started by a profile wrapper.
func TestHelperRunningHarness(t *testing.T) {
	if os.Getenv("APROFILE_TEST_HELPER") != "1" {
		t.Skip("helper process only")
	}
	time.Sleep(20 * time.Second)
}

// entrySnapshot records every entry under dir (not following links): its type,
// link target or content.
func entrySnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.Walk(dir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		rel = filepath.ToSlash(rel)
		switch {
		case fi.Mode()&os.ModeSymlink != 0:
			target, _ := os.Readlink(path)
			out[rel] = "link:" + target
		case fi.IsDir():
			out[rel] = "dir"
		default:
			data, _ := os.ReadFile(path)
			out[rel] = "file:" + string(data)
		}
		return nil
	})
	return out
}

// diffSnapshots returns the paths added, removed or changed.
func diffSnapshots(before, after map[string]string) map[string]bool {
	out := map[string]bool{}
	for k, v := range before {
		if after[k] != v {
			out[k] = true
		}
	}
	for k := range after {
		if _, ok := before[k]; !ok {
			out[k] = true
		}
	}
	return out
}

// plannedEntries extracts the entry (second field) of each plan line; a hook
// location "file#key" counts as its file.
func plannedEntries(lines []string) []string {
	var out []string
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		entry := f[1]
		if i := strings.IndexByte(entry, '#'); i >= 0 {
			entry = entry[:i]
		}
		out = append(out, entry)
	}
	return out
}

// coveredBy reports whether a changed path is a listed entry or lies inside one
// (the profile root "." covers everything a mkdir of the config dir creates).
func coveredBy(path string, listed []string) bool {
	for _, e := range listed {
		if e == "." || path == e || strings.HasPrefix(path, e+"/") {
			return true
		}
	}
	return false
}
