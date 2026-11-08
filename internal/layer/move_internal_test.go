package layer

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// On EXDEV (a store on another file system) moveNoOverwrite copies to an O_EXCL
// target, keeping mode and modification time, then removes the source.
func TestMoveNoOverwriteCrossDevice(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a", "s.jsonl")
	dst := filepath.Join(dir, "b", "s.jsonl")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("data"), 0o640); err != nil {
		t.Fatal(err)
	}
	orig := linkFile
	linkFile = func(string, string) error { return &os.LinkError{Op: "link", Err: syscall.EXDEV} }
	defer func() { linkFile = orig }()
	if err := moveNoOverwrite(src, dst); err != nil {
		t.Fatalf("moveNoOverwrite: %v", err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "data" {
		t.Errorf("dst = %q", b)
	}
	if fi, _ := os.Stat(dst); fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v", fi.Mode().Perm())
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source not removed")
	}
	// An existing destination is never overwritten, also across devices.
	if err := os.WriteFile(src, []byte("new"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := moveNoOverwrite(src, dst); !os.IsExist(err) {
		t.Fatalf("want an exists error, got %v", err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "data" {
		t.Errorf("dst overwritten: %q", b)
	}
}
