package integration

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/testutil"
)

// Deep review regressions of feature 003 (history groups). Every test runs against
// the hermetic env of newEnv; nothing here reads the real home.

// rawMembershipLines returns the plan lines of a membership change exactly as
// printed (no timestamp normalization), trimmed.
func rawMembershipLines(out string) []string {
	var lines []string
	in := false
	for _, l := range strings.Split(out, "\n") {
		switch {
		case strings.Contains(l, "history group change for "):
			in = true
		case in && strings.HasPrefix(l, "  "):
			lines = append(lines, strings.TrimSpace(l))
		case in:
			in = false
		}
	}
	return lines
}

// throughLink reports whether path, below configDir, is itself a link or is
// reached through a link component.
func throughLink(configDir, path string) bool {
	rel, err := filepath.Rel(configDir, path)
	if err != nil {
		return false
	}
	cur := configDir
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		cur = filepath.Join(cur, part)
		if fi, err := os.Lstat(cur); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return true
		}
	}
	return false
}

var planLine = regexp.MustCompile(`^(\S+) (\S+)(?: -> (\S+))?`)

// assertPlanApplied checks that every listed step of a membership change took
// effect on disk (SC-005: the real run performs exactly the listed changes), and
// that nothing changed in the watched trees outside the listed paths.
func assertPlanApplied(t *testing.T, configDir string, lines []string, watched map[string]map[string]string) {
	t.Helper()
	abs := func(p string) string {
		if filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(configDir, filepath.FromSlash(p))
	}
	var touched []string
	for _, l := range lines {
		m := planLine.FindStringSubmatch(l)
		if m == nil {
			t.Errorf("unparsable plan line %q", l)
			continue
		}
		kind, entry, target := m[1], abs(m[2]), m[3]
		touched = append(touched, entry)
		if target != "" {
			touched = append(touched, target)
		}
		fi, err := os.Lstat(entry)
		switch kind {
		case "move-to-store", "set-aside":
			// Gone from the profile; what is there now is reached through the
			// entry's link to the store (a later step).
			if err == nil && !throughLink(configDir, entry) {
				t.Errorf("%s: %s still exists in the profile", kind, entry)
			}
			if _, err := os.Lstat(target); err != nil {
				t.Errorf("%s: destination %s missing: %v", kind, target, err)
			}
		case "link-history", "relink-history":
			if err != nil || fi.Mode()&os.ModeSymlink == 0 {
				t.Errorf("%s: %s is not a link", kind, entry)
			} else if got, _ := os.Readlink(entry); filepath.Clean(got) != filepath.Clean(target) {
				t.Errorf("%s: %s -> %s, want %s", kind, entry, got, target)
			}
		case "unlink":
			if err == nil && fi.Mode()&os.ModeSymlink != 0 {
				t.Errorf("unlink: %s is still a link", entry)
			}
		case "mkdir":
			if err != nil || !fi.IsDir() {
				t.Errorf("mkdir: %s is not a directory", entry)
			}
		case "create":
			if err != nil || !fi.Mode().IsRegular() {
				t.Errorf("create: %s is not a file", entry)
			}
		}
	}
	for root, before := range watched {
		for p := range diffSnapshots(before, entrySnapshot(t, root)) {
			full := filepath.Join(root, filepath.FromSlash(p))
			listed := false
			for _, tp := range touched {
				if full == tp || strings.HasPrefix(full, tp+string(filepath.Separator)) || strings.HasPrefix(tp, full+string(filepath.Separator)) {
					listed = true
					break
				}
			}
			// The set-aside manifest and the profile record are written by the
			// change itself.
			if !listed && !strings.HasSuffix(p, "manifest.json") {
				t.Errorf("%s changed but is not in the listed plan", full)
			}
		}
	}
}

// Review fix: --share with --group plans the join against the same entries in the
// dry run and the real run (the share plan leaves history entries to the join).
func TestHistoryGroupReviewShareAndGroupDryRunMatches(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	e.run(nil, "group", "create", "claude/work")
	e.run(nil, addArgs("claude", "aa", "--share", "none")...)
	// No sync: the profile's history entries do not exist yet.
	dry := e.run(nil, "edit", "claude/aa", "--share", "content", "--group", "work", "--dry-run")
	out := e.run(nil, "edit", "claude/aa", "--share", "content", "--group", "work", "--yes")
	a, b := membershipLines(dry), membershipLines(out)
	if strings.Join(a, "\n") != strings.Join(b, "\n") || len(a) == 0 {
		t.Errorf("--share with --group: dry run and real run differ:\ndry:\n%s\nreal:\n%s", strings.Join(a, "\n"), strings.Join(b, "\n"))
	}
	for _, l := range b {
		if strings.HasPrefix(l, "  remove-empty-dir") {
			t.Errorf("the join must not see directories the share change just created: %s", l)
		}
	}
	store := readFile(t, e.profileStorePath("claude", "aa"))
	if !strings.Contains(store, "history_group: work") || !strings.Contains(store, "share: content") {
		t.Errorf("profile record after --share with --group:\n%s", store)
	}
}

// Review fixes (SC-003, SC-005): a join into a non-home store that already holds
// history changes exactly the listed paths, never removes or changes a store file,
// and a later move out of it leaves it unchanged.
func TestHistoryGroupReviewJoinSeededStoreAndMoveOut(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	claudeHome := e.defaultLocation("claude")
	e.run(nil, "group", "create", "claude/work")
	store := e.groupStore("claude", "work")
	writeFile(t, filepath.Join(store, "projects", "-w", "shared.jsonl"), "{\"store\":1}\n")
	writeFile(t, filepath.Join(store, "history.jsonl"), "store prompt\n")

	e.run(nil, addArgs("claude", "jj")...)
	e.run(nil, "sync")
	jj := e.configDir("claude", "jj")
	writeFile(t, filepath.Join(jj, "projects", "-w", "shared.jsonl"), "{\"mine\":1}\n")
	writeFile(t, filepath.Join(jj, "projects", "-w", "new.jsonl"), "{\"new\":1}\n")
	writeFile(t, filepath.Join(jj, "history.jsonl"), "my prompt\n")

	storeBefore := entrySnapshot(t, store)
	homeBefore := entrySnapshot(t, claudeHome)
	watched := map[string]map[string]string{store: storeBefore, jj: entrySnapshot(t, jj), claudeHome: homeBefore}
	out := e.run(nil, "edit", "claude/jj", "--group", "work", "--yes")
	assertPlanApplied(t, jj, rawMembershipLines(out), watched)
	storeAfter := entrySnapshot(t, store)
	for p, v := range storeBefore {
		if storeAfter[p] != v {
			t.Errorf("join changed or removed store entry %s (SC-003)", p)
		}
	}
	if readFile(t, filepath.Join(store, "projects", "-w", "new.jsonl")) != "{\"new\":1}\n" {
		t.Error("new.jsonl not merged into the store")
	}
	assertNothingCreated(t, "join work (default setup)", homeBefore, entrySnapshot(t, claudeHome))

	// Move work -> home: the work store is unchanged.
	storeBefore = entrySnapshot(t, store)
	e.run(nil, "edit", "claude/jj", "--group", "home", "--yes")
	assertNothingCreated(t, "move out of work (work store)", storeBefore, entrySnapshot(t, store))
	for p, v := range homeBefore {
		if got := entrySnapshot(t, claudeHome)[p]; got != v {
			t.Errorf("move into home changed default setup entry %s", p)
		}
	}

	// copy never changes the default setup or a store (SC-003).
	homeBefore = entrySnapshot(t, claudeHome)
	storeBefore = entrySnapshot(t, store)
	e.run(nil, "copy", "claude/jj", "claude/jj2")
	assertNothingCreated(t, "copy (default setup)", homeBefore, entrySnapshot(t, claudeHome))
	assertNothingCreated(t, "copy (work store)", storeBefore, entrySnapshot(t, store))
}

// Review fix (US5 AS4, FR-024): the registry and the installed wrapper carry the
// group; an inherited value is overridden.
func TestHistoryGroupReviewRegistryAndWrapper(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	e.run(nil, addArgs("claude", "lab", "--group", "home")...)
	e.run(nil, addArgs("claude", "solo")...)
	e.run(nil, "sync")
	reg := readRegistry(t, e.registryPath())
	if g, ok := reg["claude/lab"]["history_group"]; !ok || g != "home" {
		t.Errorf("registry claude/lab history_group = %v, want home", g)
	}
	if g, ok := reg["claude/solo"]["history_group"]; !ok || g != "" {
		t.Errorf("registry claude/solo history_group = %v, want empty", g)
	}
	inherited := []string{"APROFILE_HISTORY_GROUP=inherited"}
	for name, want := range map[string]string{"claude-lab": "home", "claude-solo": ""} {
		out, err := e.runWrapper(name, inherited, "--version")
		if err != nil {
			t.Fatalf("%s: %v\n%s", name, err, out)
		}
		if !regexp.MustCompile(`(?m)^STUB_ENV: APROFILE_HISTORY_GROUP=` + want + `$`).MatchString(out) {
			t.Errorf("%s: want APROFILE_HISTORY_GROUP=%s in the environment:\n%s", name, want, out)
		}
	}
}

// Review fixes: a history entry reclassified to another category is no longer
// shared after sync (FR-022a), and leaving a group removes such a link too.
func TestHistoryGroupReviewReclassifiedEntryDetaches(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	e.run(nil, "group", "create", "claude/work")
	e.run(nil, addArgs("claude", "rc", "--group", "work")...)
	e.run(nil, addArgs("claude", "lv", "--group", "work")...)
	rc, lv := e.configDir("claude", "rc"), e.configDir("claude", "lv")
	assertLinkTo(t, filepath.Join(rc, "plans"), filepath.Join(e.groupStore("claude", "work"), "plans"))

	store := e.groupStore("claude", "work")
	writeFile(t, filepath.Join(store, "plans", "kept.md"), "kept\n")
	storeBefore := entrySnapshot(t, store)
	e.run(nil, "harness", "classify", "claude", "plans/", "--as", "content")
	e.run(nil, "harness", "classify", "claude", "todos/", "--as", "credentials")
	// Leaving before any sync: the links of the reclassified entries go too.
	e.run(nil, "edit", "claude/lv", "--no-group", "--yes")
	for _, entry := range []string{"plans", "todos"} {
		if target, err := os.Readlink(filepath.Join(lv, entry)); err == nil && strings.HasPrefix(target, e.groupStore("claude", "work")) {
			t.Errorf("leave kept %s linked into the group store", entry)
		}
	}
	e.run(nil, "sync")
	for _, entry := range []string{"plans", "todos"} {
		if target, err := os.Readlink(filepath.Join(rc, entry)); err == nil && strings.HasPrefix(target, e.groupStore("claude", "work")) {
			t.Errorf("sync kept reclassified %s linked into the group store", entry)
		}
	}
	assertRealEntry(t, filepath.Join(rc, "todos"))
	assertNothingCreated(t, "detach (store)", storeBefore, entrySnapshot(t, store))
}

// Review fixes: two joins of one profile within a second get separate set-aside
// directories; --no-group=false never leaves; sync --relink recreates a missing
// store directory before replacing a real entry.
func TestHistoryGroupReviewSetAsideRunsAndRelink(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	e.run(nil, "group", "create", "claude/work")
	e.run(nil, addArgs("claude", "rr")...)
	e.run(nil, "sync")
	rr := e.configDir("claude", "rr")
	writeFile(t, filepath.Join(rr, "history.jsonl"), "one\n")
	e.run(nil, "edit", "claude/rr", "--group", "work", "--yes")
	e.run(nil, "edit", "claude/rr", "--no-group", "--yes")
	writeFile(t, filepath.Join(rr, "history.jsonl"), "two\n")
	e.run(nil, "edit", "claude/rr", "--group", "work", "--yes")
	runs, _ := os.ReadDir(e.setAsideRoot("claude", "rr"))
	if len(runs) != 2 {
		t.Fatalf("want two set-aside runs, got %d", len(runs))
	}
	for _, r := range runs {
		m := readFile(t, filepath.Join(e.setAsideRoot("claude", "rr"), r.Name(), "manifest.json"))
		if !strings.Contains(m, `"path": "history.jsonl"`) {
			t.Errorf("run %s: manifest lacks its item:\n%s", r.Name(), m)
		}
	}

	e.run(nil, "edit", "claude/rr", "--no-group=false")
	if !strings.Contains(readFile(t, e.profileStorePath("claude", "rr")), "history_group: work") {
		t.Error("--no-group=false left the group")
	}

	store := e.groupStore("claude", "work")
	if err := os.RemoveAll(filepath.Join(store, "plans")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(rr, "plans")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(rr, "plans", "p.md"), "plan\n")
	e.run(nil, "sync", "--relink", "--yes")
	if fi, err := os.Stat(filepath.Join(rr, "plans")); err != nil || !fi.IsDir() {
		t.Errorf("after sync --relink, plans must resolve to the recreated store directory: %v", err)
	}
	assertLinkTo(t, filepath.Join(rr, "plans"), filepath.Join(store, "plans"))
}

// Review fix: a join that fails part way records the membership, so sync and
// doctor report the rest as member drift with the command that completes it.
func TestHistoryGroupReviewFailedJoinIsMemberDrift(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permissions do not stop root")
	}
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	e.run(nil, "group", "create", "claude/work")
	e.run(nil, addArgs("claude", "ff")...)
	e.run(nil, "sync")
	ff := e.configDir("claude", "ff")
	locked := filepath.Join(ff, "plans", "x")
	writeFile(t, filepath.Join(locked, "p.md"), "plan\n")
	if err := os.Chmod(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	out, err := e.tryRun(nil, "edit", "claude/ff", "--group", "work", "--yes")
	if err == nil {
		t.Fatalf("join with an unmovable file must fail:\n%s", out)
	}
	if !strings.Contains(out, "applied before the failure") || !strings.Contains(out, "aprofile edit claude/ff --group work") {
		t.Errorf("failed join must list what was applied and the re-run command:\n%s", out)
	}
	if !strings.Contains(readFile(t, e.profileStorePath("claude", "ff")), "history_group: work") {
		t.Error("a failed join must record the membership")
	}
	if dout, _ := e.tryRun(nil, "doctor"); strings.Contains(dout, "leaks across profiles") {
		t.Errorf("doctor reports the partly joined member's links as leaks:\n%s", dout)
	}
	if err := os.Chmod(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	e.run(nil, "edit", "claude/ff", "--group", "work", "--yes")
	assertLinkTo(t, filepath.Join(ff, "plans"), filepath.Join(e.groupStore("claude", "work"), "plans"))
}

// Deep review round 2: moving a member to another group (alone or with --share)
// detaches links into the old store of entries that are no longer history.
func TestHistoryGroupReviewMoveDetachesNoLongerHistory(t *testing.T) {
	for _, withShare := range []bool{false, true} {
		e := newEnv(t)
		testutil.SeedDefaultsAt(t, e.root, "claude")
		e.run(nil, "group", "create", "claude/work")
		e.run(nil, addArgs("claude", "mv", "--group", "work")...)
		mv := e.configDir("claude", "mv")
		assertLinkTo(t, filepath.Join(mv, "todos"), filepath.Join(e.groupStore("claude", "work"), "todos"))
		// todos/ stops being history but stays state.
		e.writeHarness("claude.yaml", "name: claude\nclassification:\n  - { entry: \"todos/\", layer: state }\n  - { entry: \"plans/\", layer: content }\n")
		args := []string{"edit", "claude/mv", "--group", "home", "--yes"}
		if withShare {
			args = append(args, "--share", "none")
		}
		dry := e.run(nil, append(args, "--dry-run")...)
		out := e.run(nil, args...)
		if a, b := membershipLines(dry), membershipLines(out); strings.Join(a, "\n") != strings.Join(b, "\n") {
			t.Errorf("share=%v: dry run and real run differ:\n%v\n%v", withShare, a, b)
		}
		if target, err := os.Readlink(filepath.Join(mv, "todos")); err == nil {
			t.Errorf("share=%v: todos still linked to %s after the move", withShare, target)
		}
		assertRealEntry(t, filepath.Join(mv, "todos"))
		if target, err := os.Readlink(filepath.Join(mv, "plans")); err == nil && strings.HasPrefix(target, e.groupStore("claude", "work")) {
			t.Errorf("share=%v: plans still linked into the old store", withShare)
		}
	}
}

// Deep review round 2: a default setup whose history entry links into the joining
// profile (a hand-made share) refuses the join with exit 2; nothing is deleted.
func TestHistoryGroupReviewStoreLinkedIntoProfile(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	claudeHome := e.defaultLocation("claude")
	e.run(nil, addArgs("claude", "wk")...)
	e.run(nil, "sync")
	wk := e.configDir("claude", "wk")
	writeFile(t, filepath.Join(wk, "projects", "p1", "s.jsonl"), "{\"only\":1}\n")
	if err := os.RemoveAll(filepath.Join(claudeHome, "projects")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(wk, "projects"), filepath.Join(claudeHome, "projects")); err != nil {
		t.Fatal(err)
	}
	before := entrySnapshot(t, e.root)
	out, err := e.tryRun(nil, "edit", "claude/wk", "--group", "home", "--yes")
	if exitCodeOf(err) != 2 || !strings.Contains(out, "lies inside") {
		t.Fatalf("join with a store linked into the profile: exit %d, want 2\n%s", exitCodeOf(err), out)
	}
	assertNothingCreated(t, "refused join", before, entrySnapshot(t, e.root))
	if readFile(t, filepath.Join(wk, "projects", "p1", "s.jsonl")) != "{\"only\":1}\n" {
		t.Error("the profile's only copy was lost")
	}
}

// Deep review round 2: a home member's credentials link into the default setup is
// a leak that plain sync only reports (spec 002 --fix-links), as for an isolated
// profile; it is never replaced as a former history entry.
func TestHistoryGroupReviewHomeMemberCredentialLinkOnlyReported(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	claudeHome := e.defaultLocation("claude")
	writeFile(t, filepath.Join(claudeHome, ".credentials.json"), "{\"token\":1}\n")
	e.run(nil, addArgs("claude", "pa", "--group", "home")...)
	e.run(nil, "sync")
	creds := filepath.Join(e.configDir("claude", "pa"), ".credentials.json")
	_ = os.Remove(creds)
	if err := os.Symlink(filepath.Join(claudeHome, ".credentials.json"), creds); err != nil {
		t.Fatal(err)
	}
	out := e.run(nil, "sync")
	assertLinkTo(t, creds, filepath.Join(claudeHome, ".credentials.json"))
	if !strings.Contains(out, "fix-links") {
		t.Errorf("sync must report the leaking credentials link with --fix-links:\n%s", out)
	}
	e.run(nil, "edit", "claude/pa", "--no-group", "--yes")
	assertLinkTo(t, creds, filepath.Join(claudeHome, ".credentials.json"))
}

// Deep review round 3: the self-link guard compares file identity, so a store
// entry that reaches the profile through a differently spelled path (letter case
// on a case-insensitive disk) is refused too; and with --share and --yes the
// refusal comes before the share change writes anything.
func TestHistoryGroupReviewStoreLinkedIntoProfileSpelling(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	claudeHome := e.defaultLocation("claude")
	e.run(nil, addArgs("claude", "wk", "--share", "none")...)
	e.run(nil, "sync")
	wk := e.configDir("claude", "wk")
	writeFile(t, filepath.Join(wk, "projects", "p1", "s.jsonl"), "{\"only\":1}\n")
	variant := strings.Replace(filepath.Join(wk, "projects"), "/aprofile/profiles/", "/APROFILE/profiles/", 1)
	if _, err := os.Stat(variant); err != nil {
		t.Skip("case-sensitive file system: no differently spelled path to the same directory")
	}
	if err := os.RemoveAll(filepath.Join(claudeHome, "projects")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(variant, filepath.Join(claudeHome, "projects")); err != nil {
		t.Fatal(err)
	}
	before := entrySnapshot(t, e.root)
	for _, args := range [][]string{
		{"edit", "claude/wk", "--group", "home", "--yes"},
		{"edit", "claude/wk", "--group", "home", "--share", "content", "--yes"},
	} {
		out, err := e.tryRun(nil, args...)
		if exitCodeOf(err) != 2 {
			t.Fatalf("%v: exit %d, want 2\n%s", args, exitCodeOf(err), out)
		}
		assertNothingCreated(t, strings.Join(args, " "), before, entrySnapshot(t, e.root))
	}
	if readFile(t, filepath.Join(wk, "projects", "p1", "s.jsonl")) != "{\"only\":1}\n" {
		t.Error("the profile's only copy was lost")
	}
}
