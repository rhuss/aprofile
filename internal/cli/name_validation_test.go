package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/testutil"
)

// Sec6: profile references are validated, so a "../" harness or name never reaches
// a path join.
func TestParseProfileRefValidatesNames(t *testing.T) {
	for _, ref := range []string{
		"claude/work",
		"codex/p01",
	} {
		if _, _, err := parseProfileRef(ref); err != nil {
			t.Errorf("parseProfileRef(%q): %v", ref, err)
		}
	}
	for _, ref := range []string{
		"claude/../../victim",
		"../claude/work",
		"claude/Work",
		"claude/work/extra",
		"claude/.",
		"claude/",
		"/work",
		"work",
	} {
		if h, n, err := parseProfileRef(ref); err == nil {
			t.Errorf("parseProfileRef(%q) = %q, %q; want an error", ref, h, n)
		}
	}
}

// plantTraversalTarget creates the store file and config dir a traversal name
// would resolve to, outside aprofile's per-harness directories, and returns the
// config dir.
func plantTraversalTarget(t *testing.T, h, n string) string {
	t.Helper()
	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	store := p.ProfileStorePath(h, n)
	dir := p.ProfileConfigDir(h, n)
	for _, d := range []string{filepath.Dir(store), dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(store, []byte("name: victim\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "keep"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Sec6: remove --purge with a traversal harness or name is rejected before anything
// is deleted.
func TestRemoveRejectsInvalidNames(t *testing.T) {
	for _, tc := range []struct{ h, n string }{
		{"claude", "../../../victim"},
		{"../../victim", "work"},
		{"claude", "Work"},
	} {
		t.Run(tc.h+"_"+tc.n, func(t *testing.T) {
			testutil.HermeticHome(t)
			dir := plantTraversalTarget(t, tc.h, tc.n)
			out, err := runCLIInput(t, "", "remove", tc.h, tc.n, "--purge", "--yes")
			if err == nil || !strings.Contains(err.Error(), "must match") {
				t.Fatalf("remove %s %s: err %v\n%s", tc.h, tc.n, err, out)
			}
			if _, err := os.Stat(filepath.Join(dir, "keep")); err != nil {
				t.Errorf("purge deleted %s: %v", dir, err)
			}
		})
	}
}

// Sec6: rename validates the source reference too, not only the new name.
func TestRenameRejectsInvalidSourceName(t *testing.T) {
	testutil.HermeticHome(t)
	dir := plantTraversalTarget(t, "claude", "../../../victim")
	out, err := runCLIInput(t, "", "rename", "claude/../../../victim", "fresh", "--force")
	if err == nil || !strings.Contains(err.Error(), "must match") {
		t.Fatalf("rename: err %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "keep")); err != nil {
		t.Errorf("rename moved %s: %v", dir, err)
	}
}
