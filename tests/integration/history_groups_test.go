package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/testutil"
)

// Feature 003: history groups. Every test runs against the hermetic env of
// newEnv; nothing here reads the real home.

// historyEntries lists the history entries of the built-in definitions
// (data-model.md "Built-in values").
var historyEntries = map[string][]string{
	"claude": {"projects", "history.jsonl", "paste-cache", "file-history", "todos", "plans", "tasks"},
	"codex":  {"sessions", "archived_sessions", "session_index.jsonl", "session_index.jsonl.lock", "history.jsonl"},
	"pi":     {"agent/sessions"},
}

// defaultLocation returns the default location of a harness under the env's home.
func (e *env) defaultLocation(harnessName string) string {
	return testutil.DefaultDir(e.t, e.root, harnessName)
}

// groupStore returns the data-dir store of a non-home group.
func (e *env) groupStore(harnessName, group string) string {
	return filepath.Join(e.root, ".local", "share", "aprofile", "groups", harnessName, group)
}

func linkTarget(t *testing.T, path string) string {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is not a link", path)
	}
	target, _ := os.Readlink(path)
	return target
}

func assertLinkTo(t *testing.T, path, want string) {
	t.Helper()
	if got := linkTarget(t, path); filepath.Clean(got) != filepath.Clean(want) {
		t.Errorf("%s -> %s, want %s", path, got, want)
	}
}

func assertRealEntry(t *testing.T, path string) {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Errorf("%s: %v", path, err)
		return
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("%s must be a private real entry, is a link", path)
	}
}

// countNamed counts the regular files named name under the roots, never following
// links, so a file reachable through several links counts once.
func countNamed(roots []string, name string) int {
	n := 0
	for _, r := range roots {
		_ = filepath.Walk(r, func(path string, fi os.FileInfo, err error) error {
			if err == nil && fi.Mode().IsRegular() && fi.Name() == name {
				n++
			}
			return nil
		})
	}
	return n
}

// assertNothingCreated fails when any file, directory or link was added, removed or
// changed between the two entry snapshots (SC-004).
func assertNothingCreated(t *testing.T, label string, before, after map[string]string) {
	t.Helper()
	for p := range diffSnapshots(before, after) {
		t.Errorf("%s: %s changed (nothing may be created)", label, p)
	}
}

type showEntry struct {
	Entry     string `json:"entry"`
	Treatment string `json:"treatment"`
	Reason    string `json:"reason"`
	Status    string `json:"status"`
}

type showHistory struct {
	HistoryGroup string      `json:"history_group"`
	History      string      `json:"history"`
	Entries      []showEntry `json:"entries"`
	SetAside     []struct {
		Dir   string `json:"dir"`
		Items []struct {
			Path   string `json:"path"`
			Reason string `json:"reason"`
		} `json:"items"`
	} `json:"set_aside"`
}

func (e *env) showJSON(ref string) showHistory {
	e.t.Helper()
	out, _, err := e.tryRunSplit(nil, "show", ref, "--json")
	if err != nil {
		e.t.Fatalf("show %s --json: %v\n%s", ref, err, out)
	}
	var sh showHistory
	if err := json.Unmarshal([]byte(out), &sh); err != nil {
		e.t.Fatalf("show --json: %v\n%s", err, out)
	}
	return sh
}

func entryOf(sh showHistory, entry string) *showEntry {
	for i := range sh.Entries {
		if sh.Entries[i].Entry == entry {
			return &sh.Entries[i]
		}
	}
	return nil
}

// addArgs returns the add arguments of a profile without an auth key.
func addArgs(harnessName, name string, extra ...string) []string {
	args := []string{"add", harnessName, name}
	if harnessName == "claude" {
		args = append(args, "--auth-kind", "oauth-login")
	}
	return append(args, extra...)
}

// T015 (US1, SC-001): profiles in home share the default setup's history.
func TestHistoryGroupUS1HomeSharesDefaultHistory(t *testing.T) {
	e := newEnv(t)
	for _, h := range []string{"claude", "codex", "pi"} {
		testutil.SeedDefaultsAt(t, e.root, h)
	}
	claudeHome := e.defaultLocation("claude")
	// A history directory and a history file the default setup does not have yet.
	if err := os.RemoveAll(filepath.Join(claudeHome, "todos")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(claudeHome, "history.jsonl")); err != nil {
		t.Fatal(err)
	}

	out := e.run(nil, addArgs("claude", "personal", "--group", "home")...)
	if !strings.Contains(out, "history group") || !strings.Contains(out, "at the same time") {
		t.Errorf("add --group home should print the group line and the concurrent-use notice:\n%s", out)
	}
	e.run(nil, addArgs("claude", "lab", "--group", "home", "--endpoint", "https://lab.example.com", "--model", "lab-model")...)

	for _, name := range []string{"personal", "lab"} {
		dir := e.configDir("claude", name)
		for _, entry := range historyEntries["claude"] {
			assertLinkTo(t, filepath.Join(dir, entry), filepath.Join(claudeHome, entry))
		}
		// Credentials and non-history state stay private.
		for _, entry := range []string{"sessions", "session-env", "shell-snapshots", "debug"} {
			assertRealEntry(t, filepath.Join(dir, entry))
		}
	}
	// The missing history directory was created empty in the default location; the
	// missing history file is linked and comes into existence when written.
	if fi, err := os.Stat(filepath.Join(claudeHome, "todos")); err != nil || !fi.IsDir() {
		t.Errorf("missing todos/ not created in the default location: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(claudeHome, "history.jsonl")); !os.IsNotExist(err) {
		t.Error("a missing history file must not be created by the join")
	}
	writeFile(t, filepath.Join(e.configDir("claude", "lab"), "history.jsonl"), "lab prompt\n")
	if readFile(t, filepath.Join(claudeHome, "history.jsonl")) != "lab prompt\n" {
		t.Error("a write through the dangling history.jsonl link must land in the default location")
	}

	// A session written through one member is one file, visible through the other
	// and the default setup (SC-001).
	writeFile(t, filepath.Join(e.configDir("claude", "personal"), "projects", "p1", "shared.jsonl"), "{}\n")
	if readFile(t, filepath.Join(e.configDir("claude", "lab"), "projects", "p1", "shared.jsonl")) != "{}\n" {
		t.Error("session not visible through lab")
	}
	if readFile(t, filepath.Join(claudeHome, "projects", "p1", "shared.jsonl")) != "{}\n" {
		t.Error("session not in the default location")
	}
	if n := countNamed([]string{e.root}, "shared.jsonl"); n != 1 {
		t.Errorf("session exists %d times on disk, want 1", n)
	}

	// show lists history entries as linked to the group.
	sh := e.showJSON("claude/lab")
	if sh.HistoryGroup != "home" {
		t.Errorf("show --json history_group = %q", sh.HistoryGroup)
	}
	pe := entryOf(sh, "projects")
	if pe == nil || pe.Treatment != "linked" || pe.Reason != "history group home" || pe.Status != "ok" {
		t.Errorf("show projects = %+v", pe)
	}
	if ce := entryOf(sh, "sessions"); ce == nil || ce.Treatment != "private" {
		t.Errorf("show sessions = %+v", ce)
	}
	text := e.run(nil, "show", "claude/lab")
	if !regexp.MustCompile(`history group:\s+home`).MatchString(text) {
		t.Errorf("show text lacks the group line:\n%s", text)
	}

	// The wrapper still sets the profile's own endpoint and model.
	e.run(nil, "sync")
	wout, err := e.runWrapper("claude-lab", nil, "--version")
	if err != nil {
		t.Fatalf("wrapper: %v\n%s", err, wout)
	}
	for _, want := range []string{"ANTHROPIC_BASE_URL=https://lab.example.com", "ANTHROPIC_MODEL=lab-model",
		"CLAUDE_CONFIG_DIR=" + e.configDir("claude", "lab")} {
		if !strings.Contains(wout, "STUB_ENV: "+want) {
			t.Errorf("wrapper environment lacks %s", want)
		}
	}
	// sync keeps the links and writes nothing into the default setup.
	for _, entry := range historyEntries["claude"] {
		assertLinkTo(t, filepath.Join(e.configDir("claude", "lab"), entry), filepath.Join(claudeHome, entry))
	}

	// Codex and pi (SC-001).
	e.run(nil, addArgs("codex", "cx", "--group", "home")...)
	e.run(nil, addArgs("pi", "pp", "--group", "home")...)
	for h, name := range map[string]string{"codex": "cx", "pi": "pp"} {
		for _, entry := range historyEntries[h] {
			assertLinkTo(t, filepath.Join(e.configDir(h, name), entry), filepath.Join(e.defaultLocation(h), entry))
		}
	}
	writeFile(t, filepath.Join(e.configDir("codex", "cx"), "sessions", "2026", "11", "07", "rollout-cx.jsonl"), "{}\n")
	if readFile(t, filepath.Join(e.defaultLocation("codex"), "sessions", "2026", "11", "07", "rollout-cx.jsonl")) != "{}\n" {
		t.Error("codex session not in the default location")
	}
	writeFile(t, filepath.Join(e.configDir("pi", "pp"), "agent", "sessions", "--tmp--", "pp.jsonl"), "{}\n")
	if readFile(t, filepath.Join(e.defaultLocation("pi"), "agent", "sessions", "--tmp--", "pp.jsonl")) != "{}\n" {
		t.Error("pi session not in the default location")
	}
}

// T015: OpenCode does not support history groups.
func TestHistoryGroupUS1OpenCodeNotSupported(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "opencode")
	before := entrySnapshot(t, e.root)
	out, err := e.tryRun(nil, addArgs("opencode", "oc", "--group", "home")...)
	if exitCodeOf(err) != 2 || !strings.Contains(out, "does not support history groups") {
		t.Fatalf("add opencode --group home: exit %d\n%s", exitCodeOf(err), out)
	}
	assertNothingCreated(t, "opencode --group home", before, entrySnapshot(t, e.root))
}

// T015 (FR-012, FR-020): a none profile may join home; show says it shares history
// with the default setup.
func TestHistoryGroupUS1NoneProfileInHome(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	e.run(nil, addArgs("claude", "nn", "--share", "none", "--group", "home")...)
	const msg = "shares history with your default setup"
	if out := e.run(nil, "show", "claude/nn"); !strings.Contains(out, msg) {
		t.Errorf("show text lacks %q:\n%s", msg, out)
	}
	if sh := e.showJSON("claude/nn"); !strings.Contains(sh.History, msg) {
		t.Errorf("show --json history = %q", sh.History)
	}
	assertLinkTo(t, filepath.Join(e.configDir("claude", "nn"), "projects"), filepath.Join(e.defaultLocation("claude"), "projects"))
}

// T015 (Edge Cases): a home join is refused when the default location is a
// symbolic link or missing, and nothing is created.
func TestHistoryGroupUS1UnsuitableHomeLocation(t *testing.T) {
	cases := map[string]func(e *env){
		"missing": func(e *env) {},
		"symlink": func(e *env) {
			real := filepath.Join(e.root, "real-claude")
			if err := os.MkdirAll(filepath.Join(real, "projects"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(real, e.defaultLocation("claude")); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			setup(e)
			before := entrySnapshot(t, e.root)
			out, err := e.tryRun(nil, addArgs("claude", "xx", "--group", "home")...)
			if exitCodeOf(err) != 2 || !strings.Contains(out, e.defaultLocation("claude")) {
				t.Fatalf("exit %d, want 2 naming the location\n%s", exitCodeOf(err), out)
			}
			assertNothingCreated(t, name, before, entrySnapshot(t, e.root))
		})
	}
}

// T018 (US2): own groups keep their history in the data dir; groups and isolated
// profiles do not see each other's sessions.
func TestHistoryGroupUS2SeparateGroups(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	claudeHome := e.defaultLocation("claude")

	e.run(nil, "group", "create", "claude/work")
	store := e.groupStore("claude", "work")
	for _, d := range []string{"projects", "paste-cache", "file-history", "todos", "plans", "tasks"} {
		if fi, err := os.Stat(filepath.Join(store, d)); err != nil || !fi.IsDir() {
			t.Errorf("store directory %s not created: %v", d, err)
		}
	}
	rec := readFile(t, filepath.Join(e.root, ".config", "aprofile", "groups", "claude", "work.yaml"))
	for _, want := range []string{"harness: claude", "name: work", "created: "} {
		if !strings.Contains(rec, want) {
			t.Errorf("group record lacks %q:\n%s", want, rec)
		}
	}

	e.run(nil, addArgs("claude", "acme", "--group", "work")...)
	e.run(nil, addArgs("claude", "corp", "--group", "work")...)
	e.run(nil, addArgs("claude", "lab", "--group", "home")...)
	e.run(nil, addArgs("claude", "client")...)
	e.run(nil, "sync")
	for _, name := range []string{"acme", "corp"} {
		for _, entry := range historyEntries["claude"] {
			assertLinkTo(t, filepath.Join(e.configDir("claude", name), entry), filepath.Join(store, entry))
		}
	}
	assertRealEntry(t, filepath.Join(e.configDir("claude", "client"), "projects"))

	at := func(name string) string { return filepath.Join(e.configDir("claude", name), "projects", "x") }
	writeFile(t, filepath.Join(at("acme"), "work.jsonl"), "{}")
	writeFile(t, filepath.Join(at("lab"), "home.jsonl"), "{}")
	writeFile(t, filepath.Join(at("client"), "client.jsonl"), "{}")
	visible := func(name, file string) bool {
		_, err := os.Stat(filepath.Join(at(name), file))
		return err == nil
	}
	want := map[string]map[string]bool{
		"work.jsonl":   {"acme": true, "corp": true, "lab": false, "client": false},
		"home.jsonl":   {"acme": false, "corp": false, "lab": true, "client": false},
		"client.jsonl": {"acme": false, "corp": false, "lab": false, "client": true},
	}
	for file, who := range want {
		for name, ok := range who {
			if visible(name, file) != ok {
				t.Errorf("%s visible through %s = %v, want %v", file, name, !ok, ok)
			}
		}
		if n := countNamed([]string{e.root}, file); n != 1 {
			t.Errorf("%s exists %d times on disk, want 1", file, n)
		}
	}
	if _, err := os.Stat(filepath.Join(claudeHome, "projects", "x", "work.jsonl")); err == nil {
		t.Error("a work session leaked into the default setup")
	}

	// An unknown group creates nothing and lists the harness's groups (SC-004).
	before := entrySnapshot(t, e.root)
	out, err := e.tryRun(nil, addArgs("claude", "test", "--group", "wrok")...)
	if exitCodeOf(err) != 2 || !strings.Contains(out, "home") || !strings.Contains(out, "work") {
		t.Fatalf("add --group wrok: exit %d\n%s", exitCodeOf(err), out)
	}
	assertNothingCreated(t, "add --group wrok", before, entrySnapshot(t, e.root))

	for _, args := range [][]string{
		{"group", "create", "claude/home"},
		{"group", "create", "claude/work"},
		{"group", "create", "opencode/x"},
		{"group", "create", "claude/Bad_Name"},
	} {
		before := entrySnapshot(t, e.root)
		out, err := e.tryRun(nil, args...)
		if exitCodeOf(err) != 2 {
			t.Errorf("%s: exit %d, want 2\n%s", strings.Join(args, " "), exitCodeOf(err), out)
		}
		assertNothingCreated(t, strings.Join(args, " "), before, entrySnapshot(t, e.root))
	}
}

// membershipLines extracts the plan lines a membership change prints after its
// "history group change for" header, with set-aside timestamps normalized so a
// dry run and the real run (a moment later) compare equal.
func membershipLines(out string) []string {
	ts := regexp.MustCompile(`\d{8}T\d{6}Z`)
	var lines []string
	in := false
	for _, l := range strings.Split(out, "\n") {
		switch {
		case strings.Contains(l, "history group change for "):
			in = true
		case in && strings.HasPrefix(l, "  "):
			lines = append(lines, ts.ReplaceAllString(l, "<ts>"))
		case in:
			in = false
		}
	}
	return lines
}

// treeTotals counts the regular files and their bytes under the roots, never
// following links.
func treeTotals(roots ...string) (files int, bytes int64) {
	for _, r := range roots {
		_ = filepath.Walk(r, func(path string, fi os.FileInfo, err error) error {
			if err == nil && fi.Mode().IsRegular() {
				files++
				bytes += fi.Size()
			}
			return nil
		})
	}
	return files, bytes
}

func (e *env) setAsideRoot(harnessName, name string) string {
	return filepath.Join(e.root, ".local", "share", "aprofile", "set-aside", harnessName, name)
}

// T023 (US3, SC-003, SC-005): join home with merge and set-aside, leave, and the
// refusals.
func TestHistoryGroupUS3JoinAndLeave(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	claudeHome := e.defaultLocation("claude")
	e.run(nil, addArgs("claude", "solo")...)
	e.run(nil, "sync")
	solo := e.configDir("claude", "solo")
	writeFile(t, filepath.Join(solo, "projects", "-tmp-demo", "s-solo.jsonl"), "{\"solo\":1}\n")
	writeFile(t, filepath.Join(solo, "projects", "-tmp-demo", "memory", "MEMORY.md"), "# solo memory\n")
	writeFile(t, filepath.Join(solo, "history.jsonl"), "solo prompt\n")
	writeFile(t, filepath.Join(claudeHome, "projects", "-tmp-demo", "memory", "MEMORY.md"), "# default memory\n")

	// Unknown group: exit 2, nothing created (SC-004).
	before := entrySnapshot(t, e.root)
	out, err := e.tryRun(nil, "edit", "claude/solo", "--group", "ghost")
	if exitCodeOf(err) != 2 || !strings.Contains(out, "home") {
		t.Fatalf("edit --group ghost: exit %d\n%s", exitCodeOf(err), out)
	}
	assertNothingCreated(t, "edit --group ghost", before, entrySnapshot(t, e.root))

	// --group and --no-group together: usage error.
	if _, err := e.tryRun(nil, "edit", "claude/solo", "--group", "home", "--no-group"); err == nil {
		t.Error("--group with --no-group must fail")
	}

	// Dry run: lists the plan, changes nothing.
	dry := e.run(nil, "edit", "claude/solo", "--group", "home", "--dry-run")
	planned := membershipLines(dry)
	for _, want := range []string{"move-to-store projects/-tmp-demo/s-solo.jsonl", "set-aside history.jsonl", "set-aside projects/-tmp-demo/memory/MEMORY.md", "link-history projects"} {
		found := false
		for _, l := range planned {
			if strings.Contains(l, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("dry run lacks %q:\n%s", want, dry)
		}
	}
	assertNothingCreated(t, "dry run", before, entrySnapshot(t, e.root))

	// Off a terminal without --yes: exit 3, nothing changed.
	out, err = e.tryRun(nil, "edit", "claude/solo", "--group", "home")
	if exitCodeOf(err) != 3 {
		t.Fatalf("without --yes: exit %d, want 3\n%s", exitCodeOf(err), out)
	}
	assertNothingCreated(t, "exit 3", before, entrySnapshot(t, e.root))

	// Running profile: exit 4, nothing changed.
	out, err = e.tryRun([]string{"APROFILE_TEST_RUNNING=" + strconv.Itoa(os.Getpid())}, "edit", "claude/solo", "--group", "home", "--yes")
	if exitCodeOf(err) != 4 {
		t.Fatalf("running: exit %d, want 4\n%s", exitCodeOf(err), out)
	}
	assertNothingCreated(t, "exit 4", before, entrySnapshot(t, e.root))

	// Apply: exactly the dry run's steps; no store file overwritten; nothing lost.
	files0, bytes0 := treeTotals(claudeHome, solo)
	defaultMemory := readFile(t, filepath.Join(claudeHome, "projects", "-tmp-demo", "memory", "MEMORY.md"))
	out = e.run(nil, "edit", "claude/solo", "--group", "home", "--yes")
	if got := membershipLines(out); strings.Join(got, "\n") != strings.Join(planned, "\n") {
		t.Errorf("applied plan differs from the dry run (SC-005):\ndry:\n%s\nreal:\n%s", strings.Join(planned, "\n"), strings.Join(got, "\n"))
	}
	if !strings.Contains(out, "at the same time") {
		t.Errorf("join must print the concurrent-use notice:\n%s", out)
	}
	if readFile(t, filepath.Join(claudeHome, "projects", "-tmp-demo", "s-solo.jsonl")) != "{\"solo\":1}\n" {
		t.Error("s-solo.jsonl not moved into the default location")
	}
	if readFile(t, filepath.Join(claudeHome, "projects", "-tmp-demo", "memory", "MEMORY.md")) != defaultMemory {
		t.Error("a store file was overwritten")
	}
	runs, _ := os.ReadDir(e.setAsideRoot("claude", "solo"))
	if len(runs) != 1 {
		t.Fatalf("want one set-aside run, got %d", len(runs))
	}
	run := filepath.Join(e.setAsideRoot("claude", "solo"), runs[0].Name())
	if readFile(t, filepath.Join(run, "history.jsonl")) != "solo prompt\n" ||
		readFile(t, filepath.Join(run, "projects", "-tmp-demo", "memory", "MEMORY.md")) != "# solo memory\n" {
		t.Error("set-aside content differs")
	}
	manifest := readFile(t, filepath.Join(run, "manifest.json"))
	for _, want := range []string{`"profile": "claude/solo"`, `"group": "home"`, `"path": "history.jsonl"`, `"reason": "not-mergeable"`, `"reason": "exists-in-store"`} {
		if !strings.Contains(manifest, want) {
			t.Errorf("manifest lacks %s:\n%s", want, manifest)
		}
	}
	files1, bytes1 := treeTotals(claudeHome, solo, e.setAsideRoot("claude", "solo"))
	files1-- // the manifest
	bytes1 -= int64(len(manifest))
	if files0 != files1 || bytes0 != bytes1 {
		t.Errorf("files/bytes before %d/%d, after %d/%d (SC-003)", files0, bytes0, files1, bytes1)
	}
	for _, entry := range historyEntries["claude"] {
		assertLinkTo(t, filepath.Join(solo, entry), filepath.Join(claudeHome, entry))
	}
	if again := e.run(nil, "edit", "claude/solo", "--group", "home"); !strings.Contains(again, "nothing to do") {
		t.Errorf("joining the same group again: want nothing to do:\n%s", again)
	}

	// edit --share keeps the membership.
	e.run(nil, "edit", "claude/solo", "--share", "none", "--yes")
	if !strings.Contains(readFile(t, e.profileStorePath("claude", "solo")), "history_group: home") {
		t.Error("edit --share dropped history_group")
	}

	// Leave: the store is byte-identical, the profile gets empty private entries.
	storeBefore := entrySnapshot(t, claudeHome)
	dry = e.run(nil, "edit", "claude/solo", "--no-group", "--dry-run")
	out = e.run(nil, "edit", "claude/solo", "--no-group", "--yes")
	if a, b := membershipLines(dry), membershipLines(out); strings.Join(a, "\n") != strings.Join(b, "\n") || len(a) == 0 {
		t.Errorf("leave: dry run and real run differ:\n%v\n%v", a, b)
	}
	assertNothingCreated(t, "leave (store)", storeBefore, entrySnapshot(t, claudeHome))
	for _, entry := range []string{"projects", "todos", "plans"} {
		assertRealEntry(t, filepath.Join(solo, entry))
		if ents, _ := os.ReadDir(filepath.Join(solo, entry)); len(ents) != 0 {
			t.Errorf("%s must be empty after leaving", entry)
		}
	}
	if strings.Contains(readFile(t, e.profileStorePath("claude", "solo")), "history_group") {
		t.Error("history_group still stored after --no-group")
	}
}

// T023 (SC-005): moving between groups relinks only; the old store is unchanged.
func TestHistoryGroupUS3MoveBetweenGroups(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	claudeHome := e.defaultLocation("claude")
	e.run(nil, "group", "create", "claude/work")
	e.run(nil, addArgs("claude", "mover", "--group", "home")...)
	homeBefore := entrySnapshot(t, claudeHome)
	dry := e.run(nil, "edit", "claude/mover", "--group", "work", "--dry-run")
	out := e.run(nil, "edit", "claude/mover", "--group", "work", "--yes")
	a, b := membershipLines(dry), membershipLines(out)
	if strings.Join(a, "\n") != strings.Join(b, "\n") || len(a) == 0 {
		t.Errorf("move: dry run and real run differ:\n%v\n%v", a, b)
	}
	for _, l := range b {
		if !strings.Contains(l, "relink-history") {
			t.Errorf("move must only relink, got %q", l)
		}
	}
	assertNothingCreated(t, "move (old store)", homeBefore, entrySnapshot(t, claudeHome))
	for _, entry := range historyEntries["claude"] {
		assertLinkTo(t, filepath.Join(e.configDir("claude", "mover"), entry), filepath.Join(e.groupStore("claude", "work"), entry))
	}
}

// T023: a Codex profile with existing history moves its rollouts and sets aside
// what cannot be merged.
func TestHistoryGroupUS3CodexJoin(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "codex")
	codexHome := e.defaultLocation("codex")
	e.run(nil, addArgs("codex", "cx")...)
	e.run(nil, "sync")
	cx := e.configDir("codex", "cx")
	testutil.SeedHistory(t, cx, "codex")
	e.run(nil, "edit", "codex/cx", "--group", "home", "--yes")
	rollout := filepath.Join("sessions", "2026", "11", "07", "rollout-x-"+testutil.HistorySessionID+".jsonl")
	if _, err := os.Stat(filepath.Join(codexHome, rollout)); err != nil {
		t.Errorf("rollout not moved into the default location: %v", err)
	}
	runs, _ := os.ReadDir(e.setAsideRoot("codex", "cx"))
	if len(runs) != 1 {
		t.Fatalf("want one set-aside run, got %d", len(runs))
	}
	run := filepath.Join(e.setAsideRoot("codex", "cx"), runs[0].Name())
	for _, f := range []string{"history.jsonl", "session_index.jsonl"} {
		if _, err := os.Stat(filepath.Join(run, f)); err != nil {
			t.Errorf("%s not set aside: %v", f, err)
		}
		assertLinkTo(t, filepath.Join(cx, f), filepath.Join(codexHome, f))
	}
}

// T025 (FR-019, SC-003): rename keeps the membership and carries the set-aside
// history; remove --purge of a member deletes its links and set-aside history,
// never anything in a store or the default setup.
func TestHistoryGroupUS3RenameAndPurge(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	claudeHome := e.defaultLocation("claude")
	e.run(nil, "group", "create", "claude/work")
	e.run(nil, addArgs("claude", "old")...)
	e.run(nil, "sync")
	writeFile(t, filepath.Join(e.configDir("claude", "old"), "history.jsonl"), "old prompt\n")
	writeFile(t, filepath.Join(e.configDir("claude", "old"), "projects", "p", "s.jsonl"), "{}\n")
	e.run(nil, "edit", "claude/old", "--group", "work", "--yes")
	if _, err := os.Stat(e.setAsideRoot("claude", "old")); err != nil {
		t.Fatalf("no set-aside history after the join: %v", err)
	}
	store := e.groupStore("claude", "work")
	storeBefore := entrySnapshot(t, store)
	homeBefore := entrySnapshot(t, claudeHome)

	e.run(nil, "rename", "claude/old", "new")
	if !strings.Contains(readFile(t, e.profileStorePath("claude", "new")), "history_group: work") {
		t.Error("rename dropped history_group")
	}
	if _, err := os.Stat(e.setAsideRoot("claude", "old")); !os.IsNotExist(err) {
		t.Error("the old set-aside dir is still there")
	}
	if runs, _ := os.ReadDir(e.setAsideRoot("claude", "new")); len(runs) != 1 {
		t.Errorf("set-aside history did not move with the profile: %d runs", len(runs))
	}
	assertNothingCreated(t, "rename (store)", storeBefore, entrySnapshot(t, store))
	assertLinkTo(t, filepath.Join(e.configDir("claude", "new"), "projects"), filepath.Join(store, "projects"))

	// rename refuses when a set-aside dir exists at the new name; nothing moves.
	writeFile(t, filepath.Join(e.setAsideRoot("claude", "taken"), "x", "manifest.json"), "{}")
	if _, err := e.tryRun(nil, "rename", "claude/new", "taken"); err == nil {
		t.Error("rename over an existing set-aside dir must fail")
	}
	if !e.profileExists("claude", "new") {
		t.Error("a refused rename changed the profile")
	}

	e.run(nil, "remove", "claude", "new", "--purge", "--yes")
	assertNothingCreated(t, "purge (store)", storeBefore, entrySnapshot(t, store))
	assertNothingCreated(t, "purge (default setup)", homeBefore, entrySnapshot(t, claudeHome))
	if _, err := os.Stat(e.setAsideRoot("claude", "new")); !os.IsNotExist(err) {
		t.Error("remove --purge left the set-aside history")
	}
	if _, err := os.Lstat(e.configDir("claude", "new")); !os.IsNotExist(err) {
		t.Error("remove --purge left the config dir")
	}

	// A home member: purge never follows its links into the default setup.
	e.run(nil, addArgs("claude", "member", "--group", "home")...)
	homeBefore = entrySnapshot(t, claudeHome)
	e.run(nil, "remove", "claude", "member", "--purge", "--yes")
	assertNothingCreated(t, "purge home member (default setup)", homeBefore, entrySnapshot(t, claudeHome))
}

func (e *env) profileExists(harnessName, name string) bool {
	_, err := os.Stat(e.profileStorePath(harnessName, name))
	return err == nil
}

// Checkpoint 2 review: --no-group clears a group that does not exist; a combined
// --share and --group change that needs confirmation exits 3 without any change;
// add --adopt --group is refused.
func TestHistoryGroupUS3ReviewRegressions(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")

	e.run(nil, addArgs("claude", "ghosty")...)
	store := e.profileStorePath("claude", "ghosty")
	writeFile(t, store, readFile(t, store)+"history_group: ghost\n")
	// A membership-only change (no history links to remove) is still refused while
	// the profile runs (FR-013).
	out0, err0 := e.tryRun([]string{"APROFILE_TEST_RUNNING=" + strconv.Itoa(os.Getpid())}, "edit", "claude/ghosty", "--no-group", "--yes")
	if exitCodeOf(err0) != 4 || !strings.Contains(readFile(t, store), "history_group: ghost") {
		t.Fatalf("membership-only change while running: exit %d, want 4 and no change\n%s", exitCodeOf(err0), out0)
	}
	e.run(nil, "edit", "claude/ghosty", "--no-group", "--yes")
	if strings.Contains(readFile(t, store), "history_group") {
		t.Error("--no-group kept a group that does not exist")
	}

	e.run(nil, addArgs("claude", "both", "--share", "none")...)
	e.run(nil, "sync")
	writeFile(t, filepath.Join(e.configDir("claude", "both"), "projects", "-tmp", "a.jsonl"), "{}\n")
	before := entrySnapshot(t, e.root)
	out, err := e.tryRun(nil, "edit", "claude/both", "--share", "content", "--group", "home")
	if exitCodeOf(err) != 3 {
		t.Fatalf("--share with --group off a terminal: exit %d, want 3\n%s", exitCodeOf(err), out)
	}
	assertNothingCreated(t, "--share with --group, exit 3", before, entrySnapshot(t, e.root))

	if err := os.MkdirAll(filepath.Join(e.configDir("claude", "adopted"), "projects", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	before = entrySnapshot(t, e.root)
	out, err = e.tryRun(nil, addArgs("claude", "adopted", "--adopt", "--group", "home")...)
	if exitCodeOf(err) != 2 || !strings.Contains(out, "edit claude/adopted --group home") {
		t.Fatalf("add --adopt --group: exit %d\n%s", exitCodeOf(err), out)
	}
	assertNothingCreated(t, "add --adopt --group", before, entrySnapshot(t, e.root))
}

type groupListEntry struct {
	Harness            string   `json:"harness"`
	Name               string   `json:"name"`
	Builtin            bool     `json:"builtin"`
	Store              string   `json:"store"`
	Members            []string `json:"members"`
	DefaultSetupMember bool     `json:"default_setup_member"`
	Entries            []struct {
		Entry     string `json:"entry"`
		Merge     string `json:"merge"`
		SizeBytes int64  `json:"size_bytes"`
		Exists    bool   `json:"exists"`
	} `json:"entries"`
}

// T026 (US4): list, show and delete groups; home is permanent.
func TestHistoryGroupUS4ManageGroups(t *testing.T) {
	e := newEnv(t)
	for _, h := range allHarnesses {
		testutil.SeedDefaultsAt(t, e.root, h)
	}
	text := e.run(nil, "group", "list")
	for _, h := range []string{"claude", "codex", "pi"} {
		if !regexp.MustCompile(`(?m)^` + h + `\s+home\s`).MatchString(text) {
			t.Errorf("group list lacks %s home:\n%s", h, text)
		}
	}
	if strings.Contains(text, "opencode") {
		t.Errorf("group list shows opencode:\n%s", text)
	}
	var list []groupListEntry
	out, _, err := e.tryRunSplit(nil, "group", "list", "--json")
	if err != nil || json.Unmarshal([]byte(out), &list) != nil {
		t.Fatalf("group list --json: %v\n%s", err, out)
	}
	if len(list) != 3 {
		t.Fatalf("group list --json = %+v", list)
	}
	for _, g := range list {
		if g.Name != "home" || !g.Builtin || !g.DefaultSetupMember || g.Store != e.defaultLocation(g.Harness) {
			t.Errorf("home group = %+v", g)
		}
	}

	e.run(nil, "group", "create", "claude/work")
	e.run(nil, addArgs("claude", "acme", "--group", "work")...)
	writeFile(t, filepath.Join(e.configDir("claude", "acme"), "projects", "p", "s.jsonl"), "12345")
	var shown groupListEntry
	out, _, err = e.tryRunSplit(nil, "group", "show", "claude/work", "--json")
	if err != nil || json.Unmarshal([]byte(out), &shown) != nil {
		t.Fatalf("group show --json: %v\n%s", err, out)
	}
	if shown.Store != e.groupStore("claude", "work") || strings.Join(shown.Members, ",") != "acme" || shown.DefaultSetupMember {
		t.Errorf("group show = %+v", shown)
	}
	entries := map[string]string{}
	for _, en := range shown.Entries {
		entries[en.Entry] = en.Merge
		if en.Entry == "projects" && (!en.Exists || en.SizeBytes != 5) {
			t.Errorf("projects entry = %+v", en)
		}
		if en.Entry == "history.jsonl" && en.Exists {
			t.Errorf("history.jsonl must not exist yet: %+v", en)
		}
	}
	if entries["projects"] != "per-file" || entries["history.jsonl"] != "none" || len(entries) != len(historyEntries["claude"]) {
		t.Errorf("group show entries = %v", entries)
	}
	if text := e.run(nil, "group", "show", "claude/work"); !strings.Contains(text, "acme") || !strings.Contains(text, e.groupStore("claude", "work")) {
		t.Errorf("group show text:\n%s", text)
	}
	out, _, _ = e.tryRunSplit(nil, "group", "list", "--json")
	_ = json.Unmarshal([]byte(out), &list)
	for _, g := range list {
		if g.Name != "home" && g.DefaultSetupMember {
			t.Errorf("the default setup is listed in %s/%s (FR-017)", g.Harness, g.Name)
		}
	}

	// Delete with members: exit 2 listing them.
	out, err = e.tryRun(nil, "group", "delete", "claude/work")
	if exitCodeOf(err) != 2 || !strings.Contains(out, "acme") {
		t.Fatalf("delete with members: exit %d\n%s", exitCodeOf(err), out)
	}
	e.run(nil, "edit", "claude/acme", "--no-group", "--yes")
	store := e.groupStore("claude", "work")
	storeBefore := entrySnapshot(t, store)
	out = e.run(nil, "group", "delete", "claude/work")
	if !strings.Contains(out, store) {
		t.Errorf("delete must report the kept store:\n%s", out)
	}
	assertNothingCreated(t, "delete without --purge (store)", storeBefore, entrySnapshot(t, store))
	if _, err := os.Stat(filepath.Join(e.root, ".config", "aprofile", "groups", "claude", "work.yaml")); !os.IsNotExist(err) {
		t.Error("the group record is still there")
	}
	out, err = e.tryRun(nil, "group", "create", "claude/work")
	if exitCodeOf(err) != 2 || !strings.Contains(out, store) {
		t.Errorf("create over a kept store: exit %d\n%s", exitCodeOf(err), out)
	}

	// home is built in.
	out, err = e.tryRun(nil, "group", "delete", "claude/home")
	if exitCodeOf(err) != 2 || !strings.Contains(out, "built in") {
		t.Errorf("delete home: exit %d\n%s", exitCodeOf(err), out)
	}
	// Groups cannot be renamed; home is built in (US4 AS4).
	out, err = e.tryRun(nil, "group", "rename", "claude/home", "house")
	if exitCodeOf(err) != 2 || !strings.Contains(out, "built in") {
		t.Errorf("rename home: exit %d\n%s", exitCodeOf(err), out)
	}
	out, err = e.tryRun(nil, "group", "rename", "claude/work", "job")
	if exitCodeOf(err) != 2 || !strings.Contains(out, "cannot be renamed") {
		t.Errorf("rename work: exit %d\n%s", exitCodeOf(err), out)
	}

	// --purge deletes an empty group's store after confirmation.
	e.run(nil, "group", "create", "claude/tmp")
	if _, err := e.tryRun(nil, "group", "delete", "claude/tmp", "--purge"); exitCodeOf(err) != 3 {
		t.Errorf("--purge without --yes off a terminal: exit %d, want 3", exitCodeOf(err))
	}
	if _, err := os.Stat(e.groupStore("claude", "tmp")); err != nil {
		t.Error("exit 3 removed the store")
	}
	e.run(nil, "group", "delete", "claude/tmp", "--purge", "--yes")
	if _, err := os.Stat(e.groupStore("claude", "tmp")); !os.IsNotExist(err) {
		t.Error("--purge --yes kept the store")
	}

	// Unknown groups: exit 2, nothing created.
	for _, args := range [][]string{{"group", "show", "claude/ghost"}, {"group", "delete", "claude/ghost"}} {
		before := entrySnapshot(t, e.root)
		if _, err := e.tryRun(nil, args...); exitCodeOf(err) != 2 {
			t.Errorf("%s: exit %d, want 2", strings.Join(args, " "), exitCodeOf(err))
		}
		assertNothingCreated(t, strings.Join(args, " "), before, entrySnapshot(t, e.root))
	}
}

// findingWith reports whether a doctor finding of the profile (or of no profile
// when profile is empty) with the severity contains every substring.
func findingWith(rep doctorReport, profile, severity string, subs ...string) bool {
	for _, f := range rep.Findings {
		if profile != "" && f.Profile != profile {
			continue
		}
		if severity != "" && f.Severity != severity {
			continue
		}
		text := f.Message + " " + f.Fix
		ok := true
		for _, s := range subs {
			if !strings.Contains(text, s) {
				ok = false
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// T028 (US5): drift detection, non-destructive sync, relink.
func TestHistoryGroupUS5Drift(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	claudeHome := e.defaultLocation("claude")
	e.run(nil, "group", "create", "claude/work")
	e.run(nil, addArgs("claude", "mm", "--group", "home")...)
	e.run(nil, addArgs("claude", "w1", "--group", "work")...)
	e.run(nil, addArgs("claude", "w2", "--group", "work")...)
	e.run(nil, "sync")
	mm := e.configDir("claude", "mm")
	store := e.groupStore("claude", "work")

	// (a) a member's projects/ and history.jsonl became real entries.
	if err := os.Remove(filepath.Join(mm, "projects")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(mm, "projects", "p", "drift.jsonl"), "{}\n")
	if err := os.Remove(filepath.Join(mm, "history.jsonl")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(mm, "history.jsonl"), "saved by temp-and-rename\n")
	rep, _ := doctorJSON(t, e)
	for _, entry := range []string{"projects", "history.jsonl"} {
		if !findingWith(rep, "claude/mm", "problem", entry, "sync --relink") {
			t.Errorf("doctor does not report the real %s with the sync --relink fix: %+v", entry, rep.Findings)
		}
	}
	homeBefore := entrySnapshot(t, claudeHome)
	out := e.run(nil, "sync")
	assertRealEntry(t, filepath.Join(mm, "projects"))
	if !strings.Contains(out, "aprofile edit claude/mm --group home") {
		t.Errorf("sync must name edit --group for the real entry:\n%s", out)
	}
	assertNothingCreated(t, "sync (default setup)", homeBefore, entrySnapshot(t, claudeHome))
	e.run(nil, "sync", "--relink", "--yes")
	assertLinkTo(t, filepath.Join(mm, "projects"), filepath.Join(claudeHome, "projects"))
	assertLinkTo(t, filepath.Join(mm, "history.jsonl"), filepath.Join(claudeHome, "history.jsonl"))
	if n := countNamed([]string{filepath.Join(e.root, ".local", "share", "aprofile", "backups")}, "drift.jsonl"); n != 1 {
		t.Errorf("the replaced real entry was not backed up (%d copies)", n)
	}

	// (b) a deleted link is recreated; (c) a link to another store is relinked.
	if err := os.Remove(filepath.Join(mm, "todos")); err != nil {
		t.Fatal(err)
	}
	w1 := e.configDir("claude", "w1")
	if err := os.Remove(filepath.Join(w1, "projects")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(claudeHome, "projects"), filepath.Join(w1, "projects")); err != nil {
		t.Fatal(err)
	}
	homeBefore = entrySnapshot(t, claudeHome)
	e.run(nil, "sync")
	assertLinkTo(t, filepath.Join(mm, "todos"), filepath.Join(claudeHome, "todos"))
	assertLinkTo(t, filepath.Join(w1, "projects"), filepath.Join(store, "projects"))
	assertNothingCreated(t, "sync (default setup)", homeBefore, entrySnapshot(t, claudeHome))

	// (d) a profile of a group that does not exist.
	e.run(nil, addArgs("claude", "gg")...)
	e.run(nil, "sync")
	gg := e.configDir("claude", "gg")
	pf := e.profileStorePath("claude", "gg")
	writeFile(t, pf, readFile(t, pf)+"history_group: ghost\n")
	if err := os.Remove(filepath.Join(gg, "todos")); err != nil {
		t.Fatal(err)
	}
	ggBefore := entrySnapshot(t, gg)
	rep, _ = doctorJSON(t, e)
	if !findingWith(rep, "claude/gg", "problem", "ghost") {
		t.Errorf("doctor does not report the unknown group: %+v", rep.Findings)
	}
	out = e.run(nil, "sync")
	if !strings.Contains(out, "claude/ghost does not exist") {
		t.Errorf("sync does not report the unknown group:\n%s", out)
	}
	for p := range diffSnapshots(ggBefore, entrySnapshot(t, gg)) {
		for _, h := range historyEntries["claude"] {
			if p == h || strings.HasPrefix(p, h+"/") {
				t.Errorf("sync changed history entry %s of a profile of an unknown group", p)
			}
		}
	}
	e.run(nil, "edit", "claude/gg", "--no-group", "--yes")

	// (e) a non-home store removed by hand.
	if err := os.RemoveAll(store); err != nil {
		t.Fatal(err)
	}
	rep, _ = doctorJSON(t, e)
	if !findingWith(rep, "", "problem", "store", "claude/work") {
		t.Errorf("doctor does not report the missing store: %+v", rep.Findings)
	}
	if out := e.run(nil, "sync"); !strings.Contains(out, "is missing") {
		t.Errorf("sync does not report the missing store:\n%s", out)
	}
	for _, d := range []string{"projects", "todos", "plans"} {
		if fi, err := os.Stat(filepath.Join(store, d)); err != nil || !fi.IsDir() {
			t.Errorf("sync did not recreate store entry %s: %v", d, err)
		}
	}

	// (f) a kept store of a deleted group and set-aside history are information.
	e.run(nil, "group", "create", "claude/gone")
	e.run(nil, "group", "delete", "claude/gone")
	writeFile(t, filepath.Join(e.configDir("claude", "gg"), "history.jsonl"), "x\n")
	e.run(nil, "edit", "claude/gg", "--group", "work", "--yes")
	rep, _ = doctorJSON(t, e)
	if !findingWith(rep, "", "info", e.groupStore("claude", "gone")) {
		t.Errorf("doctor does not list the kept store as information: %+v", rep.Findings)
	}
	if !findingWith(rep, "claude/gg", "info", "set-aside") {
		t.Errorf("doctor does not list the set-aside history as information: %+v", rep.Findings)
	}

	// (g) definition changes: teams/ becomes history, plans/ stops being history.
	writeFile(t, filepath.Join(mm, "teams", "t1.json"), "{}\n")
	e.writeHarness("claude.yaml", "name: claude\nclassification:\n"+
		"  - { entry: \"teams/\", layer: state, history: true, merge: per-file }\n"+
		"  - { entry: \"plans/\", layer: state }\n")
	homeBefore = entrySnapshot(t, claudeHome)
	out = e.run(nil, "sync")
	assertRealEntry(t, filepath.Join(mm, "teams"))
	if readFile(t, filepath.Join(mm, "teams", "t1.json")) != "{}\n" {
		t.Error("sync moved data of a newly declared history entry")
	}
	if !strings.Contains(out, "aprofile edit claude/mm --group home") {
		t.Errorf("sync must name edit --group for teams/:\n%s", out)
	}
	assertRealEntry(t, filepath.Join(mm, "plans"))
	if ents, _ := os.ReadDir(filepath.Join(mm, "plans")); len(ents) != 0 {
		t.Error("plans/ must be an empty private entry")
	}
	assertNothingCreated(t, "sync after definition change (default setup)", homeBefore, entrySnapshot(t, claudeHome))
	e.run(nil, "edit", "claude/mm", "--group", "home", "--yes")
	assertLinkTo(t, filepath.Join(mm, "teams"), filepath.Join(claudeHome, "teams"))
	if readFile(t, filepath.Join(claudeHome, "teams", "t1.json")) != "{}\n" {
		t.Error("edit --group did not merge teams/ into the store")
	}

	// (h) sync twice: the second run changes nothing; (i) never writes ~/.claude.
	e.run(nil, "sync")
	homeBefore = entrySnapshot(t, claudeHome)
	if out := e.run(nil, "sync"); !strings.Contains(out, "(0 changes)") {
		t.Errorf("second sync changed something:\n%s", out)
	}
	assertNothingCreated(t, "second sync (default setup)", homeBefore, entrySnapshot(t, claudeHome))
}

// T032 (FR-018): copy leaves the target isolated unless --group, never transfers
// history, looks --group up in the target's harness and hints at the source's group.
func TestHistoryGroupCopy(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	testutil.SeedDefaultsAt(t, e.root, "codex")
	e.run(nil, "group", "create", "claude/work")
	e.run(nil, addArgs("claude", "src", "--group", "work")...)
	writeFile(t, filepath.Join(e.configDir("claude", "src"), "projects", "p", "s.jsonl"), "{}\n")
	store := e.groupStore("claude", "work")
	storeBefore := entrySnapshot(t, store)

	out := e.run(nil, "copy", "claude/src", "claude/plain")
	if !strings.Contains(out, "--group work") {
		t.Errorf("copy of a member must name --group work:\n%s", out)
	}
	if strings.Contains(readFile(t, e.profileStorePath("claude", "plain")), "history_group") {
		t.Error("copy carried the membership")
	}
	assertRealEntry(t, filepath.Join(e.configDir("claude", "plain"), "projects"))
	if n := countNamed([]string{e.configDir("claude", "plain")}, "s.jsonl"); n != 0 {
		t.Error("copy transferred history")
	}
	assertNothingCreated(t, "copy (store)", storeBefore, entrySnapshot(t, store))

	e.run(nil, "copy", "claude/src", "claude/joined", "--group", "work")
	assertLinkTo(t, filepath.Join(e.configDir("claude", "joined"), "projects"), filepath.Join(store, "projects"))
	if !strings.Contains(readFile(t, e.profileStorePath("claude", "joined")), "history_group: work") {
		t.Error("copy --group did not store the membership")
	}

	for _, args := range [][]string{
		{"copy", "claude/src", "claude/ghosted", "--group", "ghost"},
		{"copy", "claude/src", "codex/cx", "--group", "work"},
	} {
		before := entrySnapshot(t, e.root)
		out, err := e.tryRun(nil, args...)
		if exitCodeOf(err) != 2 {
			t.Errorf("%s: exit %d, want 2\n%s", strings.Join(args, " "), exitCodeOf(err), out)
		}
		assertNothingCreated(t, strings.Join(args, " "), before, entrySnapshot(t, e.root))
	}
	if out := e.run(nil, "copy", "claude/src", "codex/cx2"); strings.Contains(out, "--group work") {
		t.Errorf("no hint when the target's harness has no group of that name:\n%s", out)
	}
	var rep struct {
		HistoryGroupHint string `json:"history_group_hint"`
	}
	js, _, err := e.tryRunSplit(nil, "copy", "claude/src", "claude/js", "--json")
	if err != nil || json.Unmarshal([]byte(js), &rep) != nil || !strings.Contains(rep.HistoryGroupHint, "--group work") {
		t.Errorf("copy --json history_group_hint = %q (%v)\n%s", rep.HistoryGroupHint, err, js)
	}
}
