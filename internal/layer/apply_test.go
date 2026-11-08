package layer_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cc-deck/aprofile/internal/layer"
	"github.com/cc-deck/aprofile/internal/testutil"
)

// spec 003 T011: the history step kinds.

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func gone(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("%s still exists", path)
	}
}

func TestApplyMoveToStore(t *testing.T) {
	h := testutil.HermeticHome(t)
	src := filepath.Join(h.DataHome, "p", "sessions", "a", "s.jsonl")
	dst := filepath.Join(h.DataHome, "store", "sessions", "a", "s.jsonl")
	aside := filepath.Join(h.DataHome, "aside", "sessions", "a", "s.jsonl")
	testutil.WriteFile(t, src, "session\n")
	if err := os.Chmod(src, 0o600); err != nil {
		t.Fatal(err)
	}
	mtime := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(src, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	var items []layer.SetAsideItem
	opts := layer.ApplyOptions{SetAsideManifest: func(it layer.SetAsideItem) error { items = append(items, it); return nil }}
	steps := []layer.Step{{Kind: layer.StepMoveToStore, Path: src, Entry: "sessions/a/s.jsonl", Dest: dst, Fallback: aside}}
	if _, _, err := layer.Apply(steps, opts); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if readFile(t, dst) != "session\n" {
		t.Error("store file content differs")
	}
	fi, _ := os.Stat(dst)
	if fi.Mode().Perm() != 0o600 || !fi.ModTime().Equal(mtime) {
		t.Errorf("mode %v mtime %v not kept", fi.Mode().Perm(), fi.ModTime())
	}
	gone(t, src)
	if len(items) != 0 {
		t.Errorf("no item set aside expected, got %v", items)
	}
	// Second apply of the same step: nothing left to move, no change.
	if _, _, err := layer.Apply(steps, opts); err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if readFile(t, dst) != "session\n" {
		t.Error("second apply changed the store")
	}
}

// A store file that appears between planning and apply is never overwritten: the
// profile's file is set aside instead.
func TestApplyMoveToStoreNeverOverwrites(t *testing.T) {
	h := testutil.HermeticHome(t)
	src := filepath.Join(h.DataHome, "p", "projects", "x", "MEMORY.md")
	dst := filepath.Join(h.DataHome, "store", "projects", "x", "MEMORY.md")
	aside := filepath.Join(h.DataHome, "aside", "projects", "x", "MEMORY.md")
	testutil.WriteFile(t, src, "profile memory\n")
	steps := []layer.Step{{Kind: layer.StepMoveToStore, Path: src, Entry: "projects/x/MEMORY.md", Dest: dst, Fallback: aside}}
	testutil.WriteFile(t, dst, "store memory\n") // created by a running member after planning
	var items []layer.SetAsideItem
	applied, _, err := layer.Apply(steps, layer.ApplyOptions{SetAsideManifest: func(it layer.SetAsideItem) error {
		if _, err := os.Stat(src); err != nil {
			t.Errorf("manifest must be written before the move: %v", err)
		}
		items = append(items, it)
		return nil
	}})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if readFile(t, dst) != "store memory\n" {
		t.Fatal("store file was overwritten")
	}
	if readFile(t, aside) != "profile memory\n" {
		t.Error("profile file not set aside")
	}
	gone(t, src)
	if len(items) != 1 || items[0].Path != "projects/x/MEMORY.md" || items[0].Reason != layer.ReasonExistsInStore {
		t.Errorf("manifest items = %v", items)
	}
	// The applied list reports what happened: a set-aside, not a move.
	if len(applied) != 1 || applied[0].Kind != layer.StepSetAside || applied[0].Dest != aside || applied[0].Reason != layer.ReasonExistsInStore {
		t.Errorf("applied = %+v", applied)
	}
}

func TestApplySetAside(t *testing.T) {
	h := testutil.HermeticHome(t)
	p := filepath.Join(h.DataHome, "p")
	aside := filepath.Join(h.DataHome, "aside")
	testutil.WriteFile(t, filepath.Join(p, "history.jsonl"), "h\n")
	testutil.WriteFile(t, filepath.Join(p, "db", "x.sqlite"), "db")
	var items []layer.SetAsideItem
	steps := []layer.Step{
		{Kind: layer.StepSetAside, Path: filepath.Join(p, "history.jsonl"), Entry: "history.jsonl", Dest: filepath.Join(aside, "history.jsonl"), Reason: layer.ReasonNotMergeable},
		{Kind: layer.StepSetAside, Path: filepath.Join(p, "db"), Entry: "db", Dest: filepath.Join(aside, "db"), IsDir: true, Reason: layer.ReasonNotMergeable},
	}
	opts := layer.ApplyOptions{SetAsideManifest: func(it layer.SetAsideItem) error { items = append(items, it); return nil }}
	if _, _, err := layer.Apply(steps, opts); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if readFile(t, filepath.Join(aside, "history.jsonl")) != "h\n" || readFile(t, filepath.Join(aside, "db", "x.sqlite")) != "db" {
		t.Error("set-aside content differs")
	}
	gone(t, filepath.Join(p, "history.jsonl"))
	gone(t, filepath.Join(p, "db"))
	if len(items) != 2 || items[0].Reason != layer.ReasonNotMergeable || items[1].Path != "db" {
		t.Errorf("manifest items = %v", items)
	}
	// A failing manifest stops before anything moves.
	testutil.WriteFile(t, filepath.Join(p, "history.jsonl"), "again\n")
	bad := layer.ApplyOptions{SetAsideManifest: func(layer.SetAsideItem) error { return errors.New("disk full") }}
	_, _, err := layer.Apply([]layer.Step{{Kind: layer.StepSetAside, Path: filepath.Join(p, "history.jsonl"), Entry: "history.jsonl",
		Dest: filepath.Join(aside, "2", "history.jsonl"), Reason: layer.ReasonNotMergeable}}, bad)
	if err == nil {
		t.Fatal("a manifest error must stop the apply")
	}
	if readFile(t, filepath.Join(p, "history.jsonl")) != "again\n" {
		t.Error("the item moved although the manifest failed")
	}
}

func TestApplyRemoveEmptyDirAndLinks(t *testing.T) {
	h := testutil.HermeticHome(t)
	p := filepath.Join(h.DataHome, "p")
	store := filepath.Join(h.DataHome, "store")
	other := filepath.Join(h.DataHome, "other")
	if err := os.MkdirAll(filepath.Join(p, "projects", "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, filepath.Join(other, "todos", "t.json"), "{}")
	if err := os.Symlink(filepath.Join(other, "todos"), filepath.Join(p, "todos")); err != nil {
		t.Fatal(err)
	}
	steps := []layer.Step{
		{Kind: layer.StepRemoveEmptyDir, Path: filepath.Join(p, "projects"), Entry: "projects", IsDir: true},
		{Kind: layer.StepLinkHistory, Path: filepath.Join(p, "projects"), Entry: "projects", Source: filepath.Join(store, "projects")},
		{Kind: layer.StepLinkHistory, Path: filepath.Join(p, "history.jsonl"), Entry: "history.jsonl", Source: filepath.Join(store, "history.jsonl")},
		{Kind: layer.StepRelinkHistory, Path: filepath.Join(p, "todos"), Entry: "todos", Source: filepath.Join(store, "todos")},
	}
	if _, _, err := layer.Apply(steps, layer.ApplyOptions{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	for e, want := range map[string]string{"projects": "projects", "history.jsonl": "history.jsonl", "todos": "todos"} {
		if got, _ := os.Readlink(filepath.Join(p, e)); got != filepath.Join(store, want) {
			t.Errorf("%s -> %q", e, got)
		}
	}
	// link-history only creates the link: the store side is not created.
	gone(t, filepath.Join(store, "projects"))
	gone(t, filepath.Join(store, "history.jsonl"))
	// relink never touches the old target.
	if readFile(t, filepath.Join(other, "todos", "t.json")) != "{}" {
		t.Error("old link target changed")
	}
	// Second apply: no-op.
	if _, _, err := layer.Apply(steps, layer.ApplyOptions{}); err != nil {
		t.Fatalf("second Apply: %v", err)
	}
}

func TestApplyRemoveEmptyDirRefusesFiles(t *testing.T) {
	h := testutil.HermeticHome(t)
	p := filepath.Join(h.DataHome, "p")
	testutil.WriteFile(t, filepath.Join(p, "projects", "a", "left.jsonl"), "x")
	steps := []layer.Step{
		{Kind: layer.StepMkdir, Path: filepath.Join(h.DataHome, "first"), IsDir: true},
		{Kind: layer.StepRemoveEmptyDir, Path: filepath.Join(p, "projects"), Entry: "projects", IsDir: true},
		{Kind: layer.StepMkdir, Path: filepath.Join(h.DataHome, "never"), IsDir: true},
	}
	applied, _, err := layer.Apply(steps, layer.ApplyOptions{})
	if err == nil {
		t.Fatal("remove-empty-dir must fail when a file remains")
	}
	if len(applied) != 1 || applied[0].Kind != layer.StepMkdir {
		t.Errorf("applied = %v, want only the first step", applied)
	}
	if readFile(t, filepath.Join(p, "projects", "a", "left.jsonl")) != "x" {
		t.Error("a file was removed")
	}
	gone(t, filepath.Join(h.DataHome, "never"))
}
