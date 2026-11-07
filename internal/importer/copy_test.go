package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustWrite(t *testing.T, path, content string, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), perm); err != nil {
		t.Fatal(err)
	}
}

// A cc-deck config dir links shared entries to the default setup, so an entry (or
// something inside it) may be a symlink to a directory. The copy follows it and
// writes real files.
func TestCopyFollowsSymlinkedDirectories(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	mustWrite(t, filepath.Join(real, "a.txt"), "A\n", 0o644)
	other := filepath.Join(root, "other")
	mustWrite(t, filepath.Join(other, "b.txt"), "B\n", 0o644)
	if err := os.Symlink(other, filepath.Join(real, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(real, "dangling")); err != nil {
		t.Fatal(err)
	}
	// A cycle back to the tree root must not recurse forever.
	if err := os.Symlink(real, filepath.Join(real, "loop")); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(root, "entry")
	if err := os.Symlink(real, src); err != nil {
		t.Fatal(err)
	}

	var warnings []string
	c := copier{warn: func(s string) { warnings = append(warnings, s) }}
	dst := filepath.Join(root, "dst", "entry")
	if err := c.copy(src, dst); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if fi, err := os.Lstat(dst); err != nil || fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		t.Fatalf("dst should be a real directory: %v %v", fi, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "a.txt")); string(b) != "A\n" {
		t.Errorf("a.txt = %q", b)
	}
	if fi, err := os.Lstat(filepath.Join(dst, "linked")); err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("linked dir should be copied as a real dir: %v %v", fi, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "linked", "b.txt")); string(b) != "B\n" {
		t.Errorf("linked/b.txt = %q", b)
	}
	if _, err := os.Lstat(filepath.Join(dst, "dangling")); !os.IsNotExist(err) {
		t.Errorf("dangling link should be skipped")
	}
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "dangling") || !strings.Contains(joined, "loop") {
		t.Errorf("dangling link and cycle should be reported: %v", warnings)
	}
}

func TestCopyPrivateWritesAtomically0600(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src", "creds")
	mustWrite(t, filepath.Join(src, "token.json"), "T\n", 0o644)
	dst := filepath.Join(root, "dst", "creds")
	// An existing dst file that is a symlink must be replaced, never written through.
	target := filepath.Join(root, "outside.json")
	mustWrite(t, target, "OUTSIDE\n", 0o644)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dst, "token.json")); err != nil {
		t.Fatal(err)
	}

	c := copier{private: true, warn: func(string) {}}
	if err := c.copy(src, dst); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(filepath.Join(dst, "token.json"))
	if err != nil || fi.Mode()&os.ModeSymlink != 0 || fi.Mode().Perm() != 0o600 {
		t.Fatalf("token.json should be a real 0600 file: %v %v", fi, err)
	}
	if b, _ := os.ReadFile(target); string(b) != "OUTSIDE\n" {
		t.Errorf("copy wrote through a symlink")
	}
	if di, _ := os.Stat(dst); di.Mode().Perm() != 0o700 {
		t.Errorf("private dir mode = %v, want 0700", di.Mode().Perm())
	}
	// Single-file copy onto a symlinked dst replaces the link, not its target.
	single := filepath.Join(root, "single.json")
	if err := os.Symlink(target, single); err != nil {
		t.Fatal(err)
	}
	if err := c.copy(filepath.Join(src, "token.json"), single); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(single); err != nil || fi.Mode()&os.ModeSymlink != 0 || fi.Mode().Perm() != 0o600 {
		t.Errorf("single dst should be a real 0600 file: %v %v", fi, err)
	}
	if b, _ := os.ReadFile(target); string(b) != "OUTSIDE\n" {
		t.Errorf("single-file copy wrote through a symlink")
	}
	entries, _ := os.ReadDir(dst)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".aprofile-tmp-") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}
