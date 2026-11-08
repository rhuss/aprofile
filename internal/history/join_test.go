package history_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/history"
	"github.com/cc-deck/aprofile/internal/layer"
	"github.com/cc-deck/aprofile/internal/testutil"
)

// stepLines renders steps as "kind entry" lines (store-side steps use the
// absolute store path as entry).
func stepLines(steps []layer.Step) []string {
	var out []string
	for _, s := range steps {
		out = append(out, string(s.Kind)+" "+s.Entry)
	}
	return out
}

func findStep(steps []layer.Step, kind layer.StepKind, entry string) *layer.Step {
	for i := range steps {
		if steps[i].Kind == kind && steps[i].Entry == entry {
			return &steps[i]
		}
	}
	return nil
}

func indexOf(steps []layer.Step, kind layer.StepKind, entry string) int {
	for i := range steps {
		if steps[i].Kind == kind && steps[i].Entry == entry {
			return i
		}
	}
	return -1
}

type joinEnv struct {
	*env
	claude   *harness.Definition
	dir      string // the profile config dir
	setAside string
	home     *history.Store
	work     *history.Store
}

func newJoinEnv(t *testing.T) *joinEnv {
	t.Helper()
	e := newEnv(t)
	testutil.SeedDefaults(t, e.home, "claude")
	j := &joinEnv{env: e, claude: e.def(t, "claude"), dir: e.paths.ProfileConfigDir("claude", "solo"),
		setAside: filepath.Join(e.paths.SetAsideDir("claude", "solo"), "20261107T150405Z")}
	j.home = e.mgr.HomeStore(j.claude)
	if _, err := e.mgr.Create("claude", "work", time.Now()); err != nil {
		t.Fatal(err)
	}
	j.work = e.mgr.StoreOf(j.claude, "work")
	return j
}

// T021: a per-file join moves files absent in the store, sets aside the rest
// (including project memory), sets aside merge-none entries whole, removes the
// emptied directories and links every history entry.
func TestPlanJoinMergeAndSetAside(t *testing.T) {
	j := newJoinEnv(t)
	sid := testutil.HistorySessionID
	testutil.SeedHistory(t, j.dir, "claude")
	// The default setup already has the project memory index and a plan.
	testutil.WriteFile(t, filepath.Join(j.home.Root(), "projects", "-tmp-demo", "memory", "MEMORY.md"), "# default memory\n")
	testutil.WriteFile(t, filepath.Join(j.home.Root(), "plans", "slug.md"), "# default plan\n")

	steps, err := history.PlanJoin(j.claude, j.home, j.dir, j.setAside)
	if err != nil {
		t.Fatal(err)
	}
	moves := map[string]string{
		"projects/-tmp-demo/" + sid + ".jsonl": filepath.Join(j.home.Root(), "projects", "-tmp-demo", sid+".jsonl"),
		"paste-cache/h1.txt":                   filepath.Join(j.home.Root(), "paste-cache", "h1.txt"),
		"file-history/" + sid + "/a@v1":        filepath.Join(j.home.Root(), "file-history", sid, "a@v1"),
		"todos/" + sid + "-agent-x.json":       filepath.Join(j.home.Root(), "todos", sid+"-agent-x.json"),
		"tasks/" + sid + "/1.json":             filepath.Join(j.home.Root(), "tasks", sid, "1.json"),
	}
	for entry, dest := range moves {
		s := findStep(steps, layer.StepMoveToStore, entry)
		if s == nil || s.Dest != dest || s.Fallback != filepath.Join(j.setAside, filepath.FromSlash(entry)) {
			t.Errorf("move-to-store %s: %+v", entry, s)
		}
	}
	asides := map[string]string{
		"projects/-tmp-demo/memory/MEMORY.md": layer.ReasonExistsInStore,
		"plans/slug.md":                       layer.ReasonExistsInStore,
		"history.jsonl":                       layer.ReasonNotMergeable,
	}
	for entry, reason := range asides {
		s := findStep(steps, layer.StepSetAside, entry)
		if s == nil || s.Reason != reason || s.Dest != filepath.Join(j.setAside, filepath.FromSlash(entry)) {
			t.Errorf("set-aside %s: %+v", entry, s)
		}
	}
	for _, r := range j.claude.HistoryRules() {
		e := r.Path()
		link := findStep(steps, layer.StepLinkHistory, e)
		if link == nil || link.Source != j.home.EntryPath(e) {
			t.Errorf("link-history %s missing: %v", e, stepLines(steps))
			continue
		}
		if r.IsDir() {
			rm := indexOf(steps, layer.StepRemoveEmptyDir, e)
			if rm < 0 || rm > indexOf(steps, layer.StepLinkHistory, e) {
				t.Errorf("%s: the emptied directory must be removed before the link: %v", e, stepLines(steps))
			}
		}
	}
	// Nothing in the store is ever a step target except new files.
	for _, s := range steps {
		if s.Kind == layer.StepMoveToStore {
			if _, err := os.Lstat(s.Dest); err == nil {
				t.Errorf("move-to-store targets an existing store file %s", s.Dest)
			}
		}
	}
}

// T021: a missing history directory of the default setup is created (explicit
// join) before anything moves into it; a non-home store gets store-side mkdirs.
func TestPlanJoinCreatesMissingStoreDirs(t *testing.T) {
	j := newJoinEnv(t)
	if err := os.RemoveAll(filepath.Join(j.home.Root(), "todos")); err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, filepath.Join(j.dir, "todos", "t.json"), "[]")
	steps, err := history.PlanJoin(j.claude, j.home, j.dir, j.setAside)
	if err != nil {
		t.Fatal(err)
	}
	storeTodos := j.home.EntryPath("todos")
	mk := findStep(steps, layer.StepMkdir, storeTodos)
	if mk == nil || mk.Note != "default setup" {
		t.Fatalf("want a default setup mkdir of %s: %v", storeTodos, stepLines(steps))
	}
	if indexOf(steps, layer.StepMkdir, storeTodos) > indexOf(steps, layer.StepMoveToStore, "todos/t.json") {
		t.Error("the store directory must be created before the move")
	}

	if err := os.RemoveAll(j.work.EntryPath("plans")); err != nil {
		t.Fatal(err)
	}
	steps, _ = history.PlanJoin(j.claude, j.work, j.dir, j.setAside)
	if mk := findStep(steps, layer.StepMkdir, j.work.EntryPath("plans")); mk == nil || mk.Note == "default setup" {
		t.Errorf("want a history store mkdir of plans: %v", stepLines(steps))
	}
}

// T021: an entry that is already a link to the store gets no step; joining the
// current group again plans only the missing entries (FR-022b), or nothing.
func TestPlanJoinAgainCompletesOnlyMissing(t *testing.T) {
	j := newJoinEnv(t)
	steps, _ := history.PlanJoin(j.claude, j.work, j.dir, j.setAside)
	if err := os.MkdirAll(j.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := layer.Apply(steps, layer.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	again, _ := history.PlanJoin(j.claude, j.work, j.dir, j.setAside)
	if len(again) != 0 {
		t.Fatalf("joining the same group again: want no steps, got %v", stepLines(again))
	}
	if err := os.Remove(filepath.Join(j.dir, "todos")); err != nil {
		t.Fatal(err)
	}
	again, _ = history.PlanJoin(j.claude, j.work, j.dir, j.setAside)
	if got := stepLines(again); len(got) != 1 || got[0] != "link-history todos" {
		t.Errorf("want only link-history todos, got %v", got)
	}
}

// T021: leaving unlinks each history entry (never touching the store) and creates
// empty private entries; moving relinks only, with default setup mkdirs for home.
func TestPlanLeaveAndMove(t *testing.T) {
	j := newJoinEnv(t)
	steps, _ := history.PlanJoin(j.claude, j.work, j.dir, j.setAside)
	if err := os.MkdirAll(j.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := layer.Apply(steps, layer.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}

	leave, err := history.PlanLeave(j.claude, j.dir, j.work)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range j.claude.HistoryRules() {
		e := r.Path()
		u, c := indexOf(leave, layer.StepUnlink, e), -1
		if r.IsDir() {
			c = indexOf(leave, layer.StepMkdir, e)
		} else {
			c = indexOf(leave, layer.StepCreate, e)
		}
		if u < 0 || c < 0 || c < u {
			t.Errorf("leave %s: want unlink then an empty private entry, got %v", e, stepLines(leave))
		}
	}
	for _, s := range leave {
		if strings.HasPrefix(s.Path, j.work.Root()) {
			t.Errorf("leave touches the store: %s %s", s.Kind, s.Path)
		}
	}

	if err := os.RemoveAll(filepath.Join(j.home.Root(), "todos")); err != nil {
		t.Fatal(err)
	}
	move, err := history.PlanJoin(j.claude, j.home, j.dir, j.setAside)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, s := range move {
		switch {
		case s.Kind == layer.StepRelinkHistory:
		case s.Kind == layer.StepMkdir && s.Note == "default setup" && s.Entry == j.home.EntryPath("todos"):
		default:
			kinds = append(kinds, string(s.Kind)+" "+s.Entry)
		}
	}
	if len(kinds) != 0 {
		t.Errorf("move: only relink-history and default setup mkdirs expected, got extra %v", kinds)
	}
	if n := len(move); n != len(j.claude.HistoryRules())+1 {
		t.Errorf("move: %d steps, want one relink per history entry plus one mkdir: %v", n, stepLines(move))
	}
}

// T021/T022: the manifest writer records the profile, group, time and items.
func TestSetAsideManifest(t *testing.T) {
	e := newEnv(t)
	now := time.Date(2026, 11, 7, 15, 4, 5, 0, time.UTC)
	dir := history.SetAsideRunDir(e.paths, "claude", "solo", now)
	if want := filepath.Join(e.paths.SetAsideDir("claude", "solo"), "20261107T150405Z"); dir != want {
		t.Errorf("SetAsideRunDir = %q, want %q", dir, want)
	}
	write := history.ManifestWriter(dir, "claude/solo", "home", now)
	for _, it := range []layer.SetAsideItem{{Path: "history.jsonl", Reason: layer.ReasonNotMergeable},
		{Path: "projects/x/memory/MEMORY.md", Reason: layer.ReasonExistsInStore}} {
		if err := write(it); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Profile, Group, Created string
		Items                   []layer.SetAsideItem
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m.Profile != "claude/solo" || m.Group != "home" || m.Created != "2026-11-07T15:04:05Z" || len(m.Items) != 2 {
		t.Errorf("manifest = %s", data)
	}
	runs, err := history.ListSetAside(e.paths, "claude", "solo")
	if err != nil || len(runs) != 1 || runs[0].Dir != dir || len(runs[0].Items) != 2 {
		t.Errorf("ListSetAside = %+v, %v", runs, err)
	}
	var paths []string
	for _, it := range runs[0].Items {
		paths = append(paths, it.Path)
	}
	sort.Strings(paths)
	if paths[0] != "history.jsonl" {
		t.Errorf("items = %v", paths)
	}
}

// Checkpoint 2 review: a store path blocked by a file is set aside, never moved.
func TestPlanJoinBlockedStorePath(t *testing.T) {
	j := newJoinEnv(t)
	testutil.WriteFile(t, filepath.Join(j.work.Root(), "projects", "x"), "a file in the store\n")
	testutil.WriteFile(t, filepath.Join(j.dir, "projects", "x", "a.jsonl"), "{}")
	steps, err := history.PlanJoin(j.claude, j.work, j.dir, j.setAside)
	if err != nil {
		t.Fatal(err)
	}
	if s := findStep(steps, layer.StepSetAside, "projects/x/a.jsonl"); s == nil || s.Reason != layer.ReasonExistsInStore {
		t.Fatalf("want set-aside of projects/x/a.jsonl, got %v", stepLines(steps))
	}
	if _, _, err := layer.Apply(steps, layer.ApplyOptions{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// The store file is unchanged, the profile's file is set aside and gone.
	if got := readFileT(t, filepath.Join(j.work.Root(), "projects", "x")); got != "a file in the store\n" {
		t.Errorf("store file changed: %q", got)
	}
	if got := readFileT(t, filepath.Join(j.setAside, "projects", "x", "a.jsonl")); got != "{}" {
		t.Errorf("set-aside copy = %q", got)
	}
	if fi, err := os.Lstat(filepath.Join(j.dir, "projects")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("projects must be linked after the join: %v", err)
	}
}

// Deep review: symbolic links are never moved into a store (they would give every
// member a path to what they point at), and nothing is moved through a store
// directory that is a link.
func TestPlanJoinSetsAsideSymlinks(t *testing.T) {
	j := newJoinEnv(t)
	secret := filepath.Join(j.dir, ".credentials.json")
	testutil.WriteFile(t, secret, "token")
	testutil.WriteFile(t, filepath.Join(j.dir, "projects", "p", "real.jsonl"), "{}")
	if err := os.Symlink(secret, filepath.Join(j.dir, "projects", "p", "creds.jsonl")); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(j.home.Root(), "..", "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(j.work.Root(), "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(j.work.Root(), "projects", "q")); err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, filepath.Join(j.dir, "projects", "q", "s.jsonl"), "{}")

	steps, err := history.PlanJoin(j.claude, j.work, j.dir, j.setAside)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []string{"projects/p/creds.jsonl", "projects/q/s.jsonl"} {
		if s := findStep(steps, layer.StepSetAside, e); s == nil || s.Reason != layer.ReasonNotMergeable {
			t.Errorf("%s: want set-aside (not mergeable), got %v", e, stepLines(steps))
		}
	}
	if findStep(steps, layer.StepMoveToStore, "projects/p/real.jsonl") == nil {
		t.Errorf("a regular file must still move: %v", stepLines(steps))
	}
	if _, _, err := layer.Apply(steps, layer.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(j.work.Root(), "projects", "p", "creds.jsonl")); !os.IsNotExist(err) {
		t.Error("a symbolic link was moved into the store")
	}
	if ents, _ := os.ReadDir(outside); len(ents) != 0 {
		t.Errorf("the join wrote outside the store: %d entries", len(ents))
	}
}

// Deep review: a move interrupted after the hard link into the store completes on
// the next join instead of setting the same file aside as a collision.
func TestPlanJoinCompletesInterruptedMove(t *testing.T) {
	j := newJoinEnv(t)
	src := filepath.Join(j.dir, "projects", "p", "s.jsonl")
	testutil.WriteFile(t, src, "{}")
	dst := filepath.Join(j.work.Root(), "projects", "p", "s.jsonl")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(src, dst); err != nil {
		t.Fatal(err)
	}
	steps, err := history.PlanJoin(j.claude, j.work, j.dir, j.setAside)
	if err != nil {
		t.Fatal(err)
	}
	if findStep(steps, layer.StepMoveToStore, "projects/p/s.jsonl") == nil {
		t.Fatalf("want move-to-store completing the move, got %v", stepLines(steps))
	}
	if _, _, err := layer.Apply(steps, layer.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(j.setAside, "projects", "p", "s.jsonl")); !os.IsNotExist(err) {
		t.Error("the interrupted move was set aside as a collision")
	}
	if readFileT(t, dst) != "{}" {
		t.Error("store file lost")
	}
}

// Deep review: two joins within one second get separate set-aside directories.
func TestSetAsideRunDirIsUnique(t *testing.T) {
	e := newEnv(t)
	now := time.Date(2026, 11, 7, 15, 4, 5, 0, time.UTC)
	first := history.SetAsideRunDir(e.paths, "claude", "solo", now)
	if err := os.MkdirAll(first, 0o755); err != nil {
		t.Fatal(err)
	}
	second := history.SetAsideRunDir(e.paths, "claude", "solo", now)
	if second == first || !strings.HasPrefix(second, first+"-") {
		t.Errorf("second run dir %q must differ from %q with a counter", second, first)
	}
}

func readFileT(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// Deep review round 2: an uninspectable set-aside location ends the search for a
// free run directory instead of looping.
func TestSetAsideRunDirStopsOnErrors(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permissions do not stop root")
	}
	e := newEnv(t)
	parent := filepath.Dir(e.paths.SetAsideDir("claude", "solo"))
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })
	done := make(chan string, 1)
	go func() { done <- history.SetAsideRunDir(e.paths, "claude", "solo", time.Now()) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("SetAsideRunDir does not return when the location cannot be inspected")
	}
}
