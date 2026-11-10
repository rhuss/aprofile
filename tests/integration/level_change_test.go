package integration

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cc-deck/aprofile/internal/profile"
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
	// The aprofile under test carries the test seam (testutil.BuildBinary, T092):
	// ask it for the real process scan where this machine can read the helper's
	// environment, and otherwise (a sandbox) force the helper's pid as the running
	// answer, so the refusal path is exercised either way.
	seam := "APROFILE_TEST_RUNNING=real"
	if _, running, known := profile.RunningProcess(e.configDir("claude", "busy")); !known || !running {
		t.Logf("process scan unavailable here (known=%v running=%v): forcing the running answer", known, running)
		seam = "APROFILE_TEST_RUNNING=" + strconv.Itoa(cmd.Process.Pid)
	}
	out, err := e.tryRun([]string{seam}, "edit", "claude/busy", "--share", "none", "--yes")
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

// Feature 004 FR-008a: a level change to all drops the launch record (settings.json
// becomes a link) and makes the launch settings wrapper-only; a change from all lets
// the next sync place them. Both the dry run and the applied change say so.
func TestLevelChangeLaunchSettings(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	key := e.keyFile("w", "sk-level\n")
	e.run(nil, "add", "claude", "work", "--auth-kind", "api-key", "--auth-key", "file:"+key, "--share", "content", "--model", "gw")
	e.run(nil, "sync")
	rec := filepath.Join(e.root, ".local", "share", "aprofile", "managed-launch", "claude", "work.json")
	if _, err := os.Stat(rec); err != nil {
		t.Fatalf("no launch record after sync: %v", err)
	}
	const toAll = "launch settings become wrapper-only (settings.json is linked)"
	const fromAll = "launch settings will be placed into settings.json by the next sync"
	dry := e.run(nil, "edit", "claude/work", "--share", "all", "--dry-run")
	if !strings.Contains(dry, toAll) {
		t.Errorf("dry run lacks %q:\n%s", toAll, dry)
	}
	if _, err := os.Stat(rec); err != nil {
		t.Error("the dry run dropped the record")
	}
	out := e.run(nil, "edit", "claude/work", "--share", "all", "--yes")
	if !strings.Contains(out, toAll) {
		t.Errorf("applied change lacks %q:\n%s", toAll, out)
	}
	if _, err := os.Stat(rec); !os.IsNotExist(err) {
		t.Error("the record was not dropped")
	}
	e.run(nil, "sync")
	if w, err := e.runWrapper("claude-work", nil); err != nil || !strings.Contains(w, "STUB_ENV: ANTHROPIC_MODEL=gw") {
		t.Errorf("at all the wrapper carries the model: %v\n%s", err, w)
	}
	// FR-008: the settings file is a link and the default setup's file holds no
	// launch entries, so it stays linkable for every other profile.
	if fi, err := os.Lstat(filepath.Join(e.configDir("claude", "work"), "settings.json")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("settings.json is not a link at all: %v", err)
	}
	def := readSettings(t, filepath.Join(e.root, ".claude", "settings.json"))
	if _, has := def["apiKeyHelper"]; has {
		t.Errorf("the default settings.json got an apiKeyHelper: %v", def)
	}
	if env, _ := def["env"].(map[string]any); env != nil {
		for _, k := range []string{"ANTHROPIC_MODEL", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "APROFILE_SET_VARS"} {
			if _, has := env[k]; has {
				t.Errorf("the default settings.json got env %s: %v", k, env)
			}
		}
	}

	dry = e.run(nil, "edit", "claude/work", "--share", "content", "--dry-run")
	if !strings.Contains(dry, fromAll) {
		t.Errorf("dry run lacks %q:\n%s", fromAll, dry)
	}
	out = e.run(nil, "edit", "claude/work", "--share", "content", "--yes")
	if !strings.Contains(out, fromAll) {
		t.Errorf("applied change lacks %q:\n%s", fromAll, out)
	}
	e.run(nil, "sync")
	m := readSettings(t, filepath.Join(e.configDir("claude", "work"), "settings.json"))
	if env, _ := m["env"].(map[string]any); env["ANTHROPIC_MODEL"] != "gw" {
		t.Errorf("sync did not place the launch settings after the change from all: %v", m)
	}
}

// Spec 005 edge case: a login profile without launch settings still carries
// neutralizing entries, so a level change to all drops its launch record and says
// so, and a change back lets the next sync place the entries again.
func TestLevelChangeLoginNeutralizing(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	e.run(nil, "add", "claude", "login", "--auth-kind", "oauth-login", "--share", "content")
	e.run(nil, "sync")
	rec := filepath.Join(e.root, ".local", "share", "aprofile", "managed-launch", "claude", "login.json")
	if _, err := os.Stat(rec); err != nil {
		t.Fatalf("no launch record after sync: %v", err)
	}
	const toAll = "launch settings become wrapper-only (settings.json is linked)"
	const fromAll = "launch settings will be placed into settings.json by the next sync"
	if out := e.run(nil, "edit", "claude/login", "--share", "all", "--yes"); !strings.Contains(out, toAll) {
		t.Errorf("applied change lacks %q:\n%s", toAll, out)
	}
	if _, err := os.Stat(rec); !os.IsNotExist(err) {
		t.Error("the record was not dropped")
	}
	if out := e.run(nil, "edit", "claude/login", "--share", "content", "--yes"); !strings.Contains(out, fromAll) {
		t.Errorf("applied change lacks %q:\n%s", fromAll, out)
	}
	e.run(nil, "sync")
	env, _ := readSettings(t, filepath.Join(e.configDir("claude", "login"), "settings.json"))["env"].(map[string]any)
	if got, ok := env["CLAUDE_CODE_USE_VERTEX"]; !ok || got != "" {
		t.Errorf("env.CLAUDE_CODE_USE_VERTEX = %v (present %v), want empty after the change from all", got, ok)
	}
}

// Feature 004 (research R6): rename moves the launch record, remove --purge deletes it.
func TestLaunchRecordFollowsRenameAndPurge(t *testing.T) {
	e := newEnv(t)
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login", "--share", "content", "--model", "gw")
	e.run(nil, "sync")
	recDir := filepath.Join(e.root, ".local", "share", "aprofile", "managed-launch", "claude")
	if _, err := os.Stat(filepath.Join(recDir, "work.json")); err != nil {
		t.Fatalf("no record: %v", err)
	}
	e.run(nil, "rename", "claude/work", "job", "--force")
	if _, err := os.Stat(filepath.Join(recDir, "work.json")); !os.IsNotExist(err) {
		t.Error("rename left the old record")
	}
	if _, err := os.Stat(filepath.Join(recDir, "job.json")); err != nil {
		t.Errorf("rename did not move the record: %v", err)
	}
	if out := e.run(nil, "sync"); strings.Contains(out, "replaced user entry") {
		t.Errorf("after rename the entries must still count as placed:\n%s", out)
	}
	e.run(nil, "remove", "claude", "job", "--purge", "--yes")
	if _, err := os.Stat(filepath.Join(recDir, "job.json")); !os.IsNotExist(err) {
		t.Error("purge left the record")
	}
}

// A launch record that cannot be read does not undo a share level change that is
// already applied: the change is saved and the record problem is a warning.
func TestLevelChangeCorruptLaunchRecordKeepsLevel(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login", "--share", "content", "--model", "gw")
	e.run(nil, "sync")
	rec := filepath.Join(e.root, ".local", "share", "aprofile", "managed-launch", "claude", "work.json")
	if err := os.WriteFile(rec, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := e.tryRunSplit(nil, "edit", "claude/work", "--share", "all", "--yes")
	if err != nil {
		t.Fatalf("edit failed on a corrupt launch record: %v\n%s\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stderr, "launch record not updated") || !strings.Contains(stderr, "remove "+rec) {
		t.Errorf("no warning naming the launch record:\n%s", stderr)
	}
	// The named remedy works: after removing the record, sync succeeds.
	if err := os.Remove(rec); err != nil {
		t.Fatal(err)
	}
	e.run(nil, "sync")
	if data, _ := os.ReadFile(e.profileFile("claude", "work")); !strings.Contains(string(data), "share: all") {
		t.Errorf("the share level was not saved:\n%s", data)
	}
}
