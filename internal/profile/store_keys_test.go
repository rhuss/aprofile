package profile_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/profile"
)

// TestStoreKeys covers T089/FR-002: Keys lists the (harness, name) of every store
// file, sorted, including a file that fails to load, so a wrapper-name clash check
// in cli and importer sees it too. Non-YAML files and directories are ignored.
func TestStoreKeys(t *testing.T) {
	store := newStore(t)
	if keys, err := store.Keys(); err != nil || len(keys) != 0 {
		t.Fatalf("empty store: Keys = %v, %v", keys, err)
	}
	for _, p := range []*profile.Profile{
		{Harness: "codex", Name: "work"},
		{Harness: "claude", Name: "work"},
		{Harness: "claude", Name: "private"},
	} {
		if err := store.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	broken := store.Path("claude", "broken")
	if err := os.WriteFile(broken, []byte("name: [not, a, string\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(broken)
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}

	keys, err := store.Keys()
	if err != nil {
		t.Fatal(err)
	}
	want := []harness.ProfileKey{
		{Harness: "claude", Name: "broken"},
		{Harness: "claude", Name: "private"},
		{Harness: "claude", Name: "work"},
		{Harness: "codex", Name: "work"},
	}
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("Keys = %v, want %v", keys, want)
	}

	// List agrees: the broken file is a load failure, the rest load.
	profiles, failures, err := store.List()
	if err != nil || len(profiles) != 3 || len(failures) != 1 || failures[0].ID() != "claude/broken" {
		t.Errorf("List = %d profiles, failures %v, err %v", len(profiles), failures, err)
	}
}
