package history_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/history"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/testutil"
)

type env struct {
	home     testutil.Home
	paths    *config.Paths
	defs     *harness.Set
	profiles *profile.Store
	mgr      *history.Manager
}

func newEnv(t *testing.T) *env {
	t.Helper()
	h := testutil.HermeticHome(t)
	paths, err := config.ResolvePaths()
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}
	defs, err := harness.LoadBuiltins()
	if err != nil {
		t.Fatalf("LoadBuiltins: %v", err)
	}
	store := profile.NewStore(paths)
	return &env{home: h, paths: paths, defs: defs, profiles: store, mgr: history.NewManager(paths, defs, store)}
}

func (e *env) def(t *testing.T, name string) *harness.Definition {
	t.Helper()
	d, ok := e.defs.Get(name)
	if !ok {
		t.Fatalf("no built-in %q", name)
	}
	return d
}

// writeRecord writes a group record by hand (group create is a later task).
func (e *env) writeRecord(t *testing.T, h, g string) {
	t.Helper()
	testutil.WriteFile(t, e.paths.GroupRecordPath(h, g), "harness: "+h+"\nname: "+g+"\ncreated: 2026-11-07T15:04:05Z\n")
}

func TestListSynthesizesHomePerSupportedHarness(t *testing.T) {
	e := newEnv(t)
	groups, err := e.mgr.List("")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var ids []string
	for _, g := range groups {
		ids = append(ids, g.Harness+"/"+g.Name)
		if !g.Builtin || !g.DefaultSetupMember {
			t.Errorf("%s/%s: builtin=%v default member=%v", g.Harness, g.Name, g.Builtin, g.DefaultSetupMember)
		}
	}
	if got, want := strings.Join(ids, ","), "claude/home,codex/home,pi/home"; got != want {
		t.Fatalf("List = %s, want %s", got, want)
	}
	if groups[0].Store != filepath.Join(e.home.Root, ".claude") {
		t.Errorf("claude/home store = %q", groups[0].Store)
	}
	if _, err := e.mgr.List("opencode"); !errors.Is(err, history.ErrNotSupported) {
		t.Errorf("List(opencode) = %v, want ErrNotSupported", err)
	}
}

func TestGetUnknownGroupListsExistingGroups(t *testing.T) {
	e := newEnv(t)
	e.writeRecord(t, "claude", "work")
	_, err := e.mgr.Get("claude", "wrok")
	if !errors.Is(err, history.ErrUnknownGroup) {
		t.Fatalf("Get(wrok) = %v, want ErrUnknownGroup", err)
	}
	if !history.IsInvalid(err) {
		t.Error("unknown group must be an exit-2 class error")
	}
	for _, want := range []string{"claude/wrok", "home", "work"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if _, err := e.mgr.Get("opencode", "home"); !errors.Is(err, history.ErrNotSupported) {
		t.Errorf("Get(opencode/home) = %v, want ErrNotSupported", err)
	}
}

func TestGroupMembersAndStores(t *testing.T) {
	e := newEnv(t)
	e.writeRecord(t, "claude", "work")
	for _, p := range []*profile.Profile{
		{Harness: "claude", Name: "acme", HistoryGroup: "work"},
		{Harness: "claude", Name: "corp", HistoryGroup: "work"},
		{Harness: "claude", Name: "lab", HistoryGroup: "home"},
		{Harness: "claude", Name: "client"},
	} {
		if err := e.profiles.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	work, err := e.mgr.Get("claude", "work")
	if err != nil {
		t.Fatalf("Get(work): %v", err)
	}
	if work.Builtin || work.DefaultSetupMember {
		t.Error("work must not be built in and must not contain the default setup (FR-017)")
	}
	if strings.Join(work.Members, ",") != "acme,corp" {
		t.Errorf("work members = %v", work.Members)
	}
	if work.Store != e.paths.GroupStoreDir("claude", "work") {
		t.Errorf("work store = %q", work.Store)
	}
	home, _ := e.mgr.Get("claude", "home")
	if strings.Join(home.Members, ",") != "lab" || !home.DefaultSetupMember {
		t.Errorf("home members = %v default=%v", home.Members, home.DefaultSetupMember)
	}

	claude := e.def(t, "claude")
	lab, _ := e.profiles.Load("claude", "lab")
	st, err := e.mgr.StoreFor(claude, lab)
	if err != nil || st == nil {
		t.Fatalf("StoreFor(lab) = %v, %v", st, err)
	}
	if !st.IsHome() || st.Name() != "home" || st.EntryPath("projects") != filepath.Join(e.home.Root, ".claude", "projects") {
		t.Errorf("home store: home=%v name=%q projects=%q", st.IsHome(), st.Name(), st.EntryPath("projects"))
	}
	acme, _ := e.profiles.Load("claude", "acme")
	st, _ = e.mgr.StoreFor(claude, acme)
	if st.IsHome() || st.Name() != "work" || st.EntryPath("projects/") != filepath.Join(e.paths.GroupStoreDir("claude", "work"), "projects") {
		t.Errorf("work store: home=%v name=%q projects=%q", st.IsHome(), st.Name(), st.EntryPath("projects/"))
	}
	if st.Root() != e.paths.GroupStoreDir("claude", "work") {
		t.Errorf("work root = %q", st.Root())
	}
	client, _ := e.profiles.Load("claude", "client")
	if st, err := e.mgr.StoreFor(claude, client); st != nil || err != nil {
		t.Errorf("StoreFor(isolated) = %v, %v; want nil, nil", st, err)
	}
	ghost := &profile.Profile{Harness: "claude", Name: "g", HistoryGroup: "ghost"}
	if _, err := e.mgr.StoreFor(claude, ghost); !errors.Is(err, history.ErrUnknownGroup) {
		t.Errorf("StoreFor(ghost) = %v, want ErrUnknownGroup", err)
	}
}

func TestPiHomeStoreUsesDefaultLocation(t *testing.T) {
	e := newEnv(t)
	st := e.mgr.HomeStore(e.def(t, "pi"))
	want := filepath.Join(e.home.Root, ".pi", "agent", "sessions")
	if got := st.EntryPath("agent/sessions/"); got != want {
		t.Errorf("pi home agent/sessions = %q, want %q", got, want)
	}
}

func TestCheckHomeLocation(t *testing.T) {
	e := newEnv(t)
	claude := *e.def(t, "claude")
	loc := filepath.Join(e.home.Root, ".claude")

	// Missing default location.
	if err := history.CheckHomeLocation(&claude, e.home.Root, e.paths, nil); err == nil || !history.IsInvalid(err) || !strings.Contains(err.Error(), loc) {
		t.Errorf("missing location: %v", err)
	}
	if err := os.MkdirAll(loc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := history.CheckHomeLocation(&claude, e.home.Root, e.paths, nil); err != nil {
		t.Errorf("good location refused: %v", err)
	}
	// Equal to a profile config dir.
	if err := history.CheckHomeLocation(&claude, e.home.Root, e.paths, []string{loc}); err == nil || !strings.Contains(err.Error(), "config dir of a profile") {
		t.Errorf("profile dir location: %v", err)
	}
	// A symbolic link.
	real := filepath.Join(e.home.Root, "real-claude")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(loc); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, loc); err != nil {
		t.Fatal(err)
	}
	if err := history.CheckHomeLocation(&claude, e.home.Root, e.paths, nil); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Errorf("symlink location: %v", err)
	}
	// Inside aprofile's data directory.
	inside := filepath.Join(e.paths.DataDir, "fake-default")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	claude.DefaultConfigLocation = inside
	if err := history.CheckHomeLocation(&claude, e.home.Root, e.paths, nil); err == nil || !strings.Contains(err.Error(), "data directory") {
		t.Errorf("data dir location: %v", err)
	}
}

func TestCreateGroup(t *testing.T) {
	e := newEnv(t)
	now := time.Date(2026, 11, 7, 15, 4, 5, 0, time.UTC)
	g, err := e.mgr.Create("claude", "work", now)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if g.Builtin || g.Store != e.paths.GroupStoreDir("claude", "work") || !g.Created.Equal(now) {
		t.Errorf("group = %+v", g)
	}
	rec, err := os.ReadFile(e.paths.GroupRecordPath("claude", "work"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"harness: claude", "name: work", "created: 2026-11-07T15:04:05Z"} {
		if !strings.Contains(string(rec), want) {
			t.Errorf("record lacks %q:\n%s", want, rec)
		}
	}
	for _, r := range e.def(t, "claude").HistoryRules() {
		p := filepath.Join(g.Store, r.Path())
		fi, err := os.Stat(p)
		if r.IsDir() {
			if err != nil || !fi.IsDir() {
				t.Errorf("store dir %s: %v", r.Path(), err)
			}
		} else if err == nil {
			t.Errorf("store file %s must not be created", r.Path())
		}
	}
	got, err := e.mgr.Get("claude", "work")
	if err != nil || got.Name != "work" {
		t.Errorf("Get after Create = %+v, %v", got, err)
	}

	cases := []struct {
		h, g string
		want error
	}{
		{"claude", "home", history.ErrBuiltinGroup},
		{"claude", "work", history.ErrGroupExists},
		{"opencode", "x", history.ErrNotSupported},
		{"claude", "Bad_Name", history.ErrInvalidName},
		{"nosuch", "x", history.ErrUnknownHarness},
	}
	for _, c := range cases {
		if _, err := e.mgr.Create(c.h, c.g, now); !errors.Is(err, c.want) || !history.IsInvalid(err) {
			t.Errorf("Create(%s/%s) = %v, want %v", c.h, c.g, err, c.want)
		}
	}
}

// FR-006: a kept store of a deleted group is never adopted implicitly.
func TestCreateRefusesKeptStore(t *testing.T) {
	e := newEnv(t)
	kept := e.paths.GroupStoreDir("claude", "old")
	testutil.WriteFile(t, filepath.Join(kept, "projects", "x", "s.jsonl"), "{}")
	_, err := e.mgr.Create("claude", "old", time.Now())
	if !errors.Is(err, history.ErrKeptStore) || !strings.Contains(err.Error(), kept) {
		t.Fatalf("Create over a kept store = %v", err)
	}
	if _, err := os.Stat(e.paths.GroupRecordPath("claude", "old")); !os.IsNotExist(err) {
		t.Error("a record was written")
	}
}

func TestDeleteGroup(t *testing.T) {
	e := newEnv(t)
	if _, err := e.mgr.Create("claude", "work", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := e.profiles.Save(&profile.Profile{Harness: "claude", Name: "acme", HistoryGroup: "work"}); err != nil {
		t.Fatal(err)
	}
	store := e.paths.GroupStoreDir("claude", "work")
	testutil.WriteFile(t, filepath.Join(store, "projects", "p", "s.jsonl"), "{}")

	if _, err := e.mgr.Delete("claude", "work", false); !errors.Is(err, history.ErrHasMembers) || !strings.Contains(err.Error(), "acme") {
		t.Fatalf("Delete with members = %v", err)
	}
	if _, err := e.mgr.Delete("claude", "home", true); !errors.Is(err, history.ErrBuiltinGroup) {
		t.Errorf("Delete(home) = %v", err)
	}
	if _, err := e.mgr.Delete("claude", "ghost", false); !errors.Is(err, history.ErrUnknownGroup) {
		t.Errorf("Delete(ghost) = %v", err)
	}
	if err := e.profiles.Remove("claude", "acme"); err != nil {
		t.Fatal(err)
	}
	kept, err := e.mgr.Delete("claude", "work", false)
	if err != nil || kept != store {
		t.Fatalf("Delete = %q, %v", kept, err)
	}
	if _, err := os.Stat(filepath.Join(store, "projects", "p", "s.jsonl")); err != nil {
		t.Error("Delete without purge removed store content")
	}
	if got := e.mgr.KeptStores("claude"); len(got) != 1 || got[0] != store {
		t.Errorf("KeptStores = %v", got)
	}
	if _, err := e.mgr.Get("claude", "work"); !errors.Is(err, history.ErrUnknownGroup) {
		t.Errorf("Get after Delete = %v", err)
	}

	if _, err := e.mgr.Create("claude", "tmp", time.Now()); err != nil {
		t.Fatal(err)
	}
	kept, err = e.mgr.Delete("claude", "tmp", true)
	if err != nil || kept != "" {
		t.Fatalf("Delete --purge = %q, %v", kept, err)
	}
	if _, err := os.Stat(e.paths.GroupStoreDir("claude", "tmp")); !os.IsNotExist(err) {
		t.Error("purge kept the store")
	}
}

// Deep review: a nested history file gets its store directory on group create, so
// the harness's first write through the member's link succeeds.
func TestCreateGroupNestedHistoryFile(t *testing.T) {
	e := newEnv(t)
	def := []byte("name: hist\nbinary: hist\nwrapper_prefix: hist\nconfig_env:\n  HIST_DIR: \".\"\n" +
		"classification:\n  - { entry: \"archive/history.jsonl\", layer: state, history: true }\n")
	set := harness.LoadFromBytes(map[string][]byte{"hist.yaml": def})
	if errs := set.Errors(); len(errs) != 0 {
		t.Fatalf("load: %v", errs)
	}
	mgr := history.NewManager(e.paths, set, e.profiles)
	if _, err := mgr.Create("hist", "work", time.Now()); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if fi, err := os.Stat(filepath.Join(e.paths.GroupStoreDir("hist", "work"), "archive")); err != nil || !fi.IsDir() {
		t.Errorf("store directory of a nested history file not created: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(e.paths.GroupStoreDir("hist", "work"), "archive", "history.jsonl")); !os.IsNotExist(err) {
		t.Error("the history file itself must not be created")
	}
}
