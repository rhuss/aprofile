package layer_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/layer"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/testutil"
)

// tempNames are the names aprofile's own atomic writes use: config.WriteFileAtomic
// and the importer's file copy (.aprofile-tmp-*), and apply.go's directory copy and
// swap (.<entry>.aprofile-tmp-*, .<entry>.aprofile-new-*, ...-old).
var tempNames = []string{
	".aprofile-tmp-123456",
	".settings.json.aprofile-tmp-42",
	".skills.aprofile-new-77",
	".skills.aprofile-new-77-old",
}

func TestIsTempName(t *testing.T) {
	for _, n := range tempNames {
		if !layer.IsTempName(n) {
			t.Errorf("IsTempName(%q) = false, want true", n)
		}
	}
	for _, n := range []string{"settings.json", "aprofile-tmp-1", "RTK.md", ".aprofile", ".claude.json", "my.aprofile-tmp-notes"} {
		if layer.IsTempName(n) {
			t.Errorf("IsTempName(%q) = true, want false", n)
		}
	}
}

// T091 (Prod6): an interrupted atomic write leaves aprofile's own temp names behind;
// the config-dir walk never reports them as unclassified entries (doctor, sync, show
// and the default-setup listing all use it).
func TestWalkIgnoresAprofileTempNames(t *testing.T) {
	h := testutil.HermeticHome(t)
	testutil.SeedDefaults(t, h, "claude")
	testutil.SeedDefaults(t, h, "opencode")
	set, err := harness.LoadBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	claude, _ := set.Get("claude")
	dir := filepath.Join(h.DataHome, "cl")
	for _, n := range tempNames {
		testutil.WriteFile(t, filepath.Join(dir, n), "partial")
	}
	if err := os.MkdirAll(filepath.Join(dir, ".agents.aprofile-tmp-9", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	unc, err := layer.Unclassified(claude, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(unc) != 0 {
		t.Errorf("temp names reported as unclassified: %v", unc)
	}
	steps, err := layer.Plan(claude, profile.ShareAll, h.Root, dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range steps {
		if strings.Contains(s.Path, "aprofile-tmp") || strings.Contains(s.Path, "aprofile-new") {
			t.Errorf("plan step for a temp name: %s", s)
		}
	}
	entries, err := layer.Treatments(claude, &profile.Profile{Harness: "claude", Name: "x", Share: profile.ShareAll}, h.Root, dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if layer.IsTempName(filepath.Base(e.Entry)) {
			t.Errorf("treatment for a temp name: %+v", e)
		}
	}

	// Inside a container directory (OpenCode's XDG spread) too.
	oc, _ := set.Get("opencode")
	odir := filepath.Join(h.DataHome, "oc")
	testutil.WriteFile(t, filepath.Join(odir, "config", "opencode", ".opencode.json.aprofile-tmp-5"), "{}")
	if unc, _ := layer.Unclassified(oc, odir); len(unc) != 0 {
		t.Errorf("temp name in a container reported: %v", unc)
	}

	// A temp name left in the default setup is not an unborrowed default entry.
	testutil.WriteFile(t, filepath.Join(h.Root, ".claude", ".aprofile-tmp-8"), "x")
	unb, err := layer.UnborrowedDefaultEntries(claude)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range unb {
		if layer.IsTempName(filepath.Base(e)) {
			t.Errorf("temp name listed as unborrowed default entry: %v", unb)
		}
	}
}

// T091: sync removes stale temp names (older than the cutoff), files and
// directories, and leaves fresh ones (a concurrent write may own them), other
// entries and link targets alone. A dry run removes nothing.
func TestRemoveStaleTemps(t *testing.T) {
	h := testutil.HermeticHome(t)
	set, err := harness.LoadBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	oc, _ := set.Get("opencode")
	dir := filepath.Join(h.DataHome, "oc")
	old := time.Now().Add(-2 * time.Minute)
	stale := []string{
		".aprofile-tmp-1",
		"config/opencode/.opencode.json.aprofile-tmp-2",
		".skills.aprofile-new-3-old",
	}
	for _, rel := range stale {
		testutil.WriteFile(t, filepath.Join(dir, rel), "partial")
	}
	staleDir := filepath.Join(dir, ".agent.aprofile-tmp-4")
	testutil.WriteFile(t, filepath.Join(staleDir, "helper.md"), "x")
	stale = append(stale, ".agent.aprofile-tmp-4")
	for _, rel := range stale {
		ageTree(t, filepath.Join(dir, rel), old)
	}
	// The swap that left ".skills.aprofile-new-3-old" completed: its entry exists,
	// so the temp is a leftover (TestRemoveStaleTempsRestoresInterruptedSwap covers
	// the interrupted case).
	testutil.WriteFile(t, filepath.Join(dir, "skills", "s.md"), "x")
	fresh := filepath.Join(dir, ".aprofile-tmp-fresh")
	testutil.WriteFile(t, fresh, "in progress")
	keep := filepath.Join(dir, "config", "opencode", "foo.json")
	testutil.WriteFile(t, keep, "{}")
	if err := os.Chtimes(keep, old, old); err != nil {
		t.Fatal(err)
	}
	// A temp name inside a linked directory belongs to the link target: not touched.
	target := filepath.Join(h.Root, "elsewhere")
	testutil.WriteFile(t, filepath.Join(target, ".aprofile-tmp-9"), "x")
	if err := os.Chtimes(filepath.Join(target, ".aprofile-tmp-9"), old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "config", "opencode", "agent")); err != nil {
		t.Fatal(err)
	}

	cutoff := time.Now().Add(-time.Minute)
	res, err := layer.RemoveStaleTemps(oc, dir, cutoff, true)
	if err != nil {
		t.Fatal(err)
	}
	got := res.Removed
	sort.Strings(stale)
	if strings.Join(got, ",") != strings.Join(stale, ",") {
		t.Errorf("dry run = %v, want %v", got, stale)
	}
	for _, rel := range stale {
		if _, err := os.Lstat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("dry run removed %s", rel)
		}
	}

	res, err = layer.RemoveStaleTemps(oc, dir, cutoff, false)
	if err != nil {
		t.Fatal(err)
	}
	got = res.Removed
	if len(res.Failed) != 0 || len(res.Restored) != 0 {
		t.Errorf("unexpected failures or restores: %+v", res)
	}
	if strings.Join(got, ",") != strings.Join(stale, ",") {
		t.Errorf("removed = %v, want %v", got, stale)
	}
	for _, rel := range stale {
		if _, err := os.Lstat(filepath.Join(dir, rel)); !os.IsNotExist(err) {
			t.Errorf("stale %s still present", rel)
		}
	}
	for _, p := range []string{fresh, keep, filepath.Join(target, ".aprofile-tmp-9")} {
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("%s must stay: %v", p, err)
		}
	}
	if got, _ := layer.RemoveStaleTemps(oc, dir, cutoff, false); len(got.Removed) != 0 {
		t.Errorf("second run removed %v", got.Removed)
	}
	if got, err := layer.RemoveStaleTemps(oc, filepath.Join(h.DataHome, "missing"), cutoff, false); err != nil || len(got.Removed) != 0 {
		t.Errorf("missing config dir: %v, %v", got, err)
	}
}

// ageTree sets the modification time of every entry under root to at.
func ageTree(t *testing.T, root string, at time.Time) {
	t.Helper()
	err := filepath.WalkDir(root, func(p string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(p, at, at)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Review follow-up: a swap interrupted between its two renames leaves the old
// directory under ".<entry>.aprofile-new-*-old" and no <entry>. That temp holds
// the only copy of the entry (and keeps the entry's old mtime), so sync renames it
// back instead of deleting it; a dry run only names the restore.
func TestRemoveStaleTempsRestoresInterruptedSwap(t *testing.T) {
	h := testutil.HermeticHome(t)
	set, err := harness.LoadBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	oc, _ := set.Get("opencode")
	dir := filepath.Join(h.DataHome, "oc")
	old := time.Now().Add(-time.Hour)
	swapOld := filepath.Join(dir, "config", "opencode", ".agent.aprofile-new-55-old")
	swapNew := filepath.Join(dir, "config", "opencode", ".agent.aprofile-new-55")
	testutil.WriteFile(t, filepath.Join(swapOld, "helper.md"), "the only copy")
	testutil.WriteFile(t, filepath.Join(swapNew, "helper.md"), "new copy")
	ageTree(t, swapOld, old)
	ageTree(t, swapNew, old)
	// A completed swap whose cleanup was interrupted: the entry exists, the -old
	// temp is leftover and goes.
	testutil.WriteFile(t, filepath.Join(dir, "config", "opencode", "command", "x.md"), "x")
	leftover := filepath.Join(dir, "config", "opencode", ".command.aprofile-new-7-old")
	testutil.WriteFile(t, filepath.Join(leftover, "x.md"), "old")
	ageTree(t, leftover, old)

	cutoff := time.Now().Add(-time.Minute)
	res, err := layer.RemoveStaleTemps(oc, dir, cutoff, true)
	if err != nil {
		t.Fatal(err)
	}
	want := layer.TempRestore{Temp: "config/opencode/.agent.aprofile-new-55-old", Entry: "config/opencode/agent"}
	if len(res.Restored) != 1 || res.Restored[0] != want {
		t.Errorf("dry run restored = %+v, want %+v", res.Restored, want)
	}
	if strings.Join(res.Removed, ",") != "config/opencode/.agent.aprofile-new-55,config/opencode/.command.aprofile-new-7-old" {
		t.Errorf("dry run removed = %v", res.Removed)
	}
	if _, err := os.Lstat(swapOld); err != nil {
		t.Error("a dry run must not move the swap temp")
	}

	res, err = layer.RemoveStaleTemps(oc, dir, cutoff, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Restored) != 1 || res.Restored[0] != want || len(res.Failed) != 0 {
		t.Errorf("restored = %+v, failed = %v", res.Restored, res.Failed)
	}
	data, err := os.ReadFile(filepath.Join(dir, "config", "opencode", "agent", "helper.md"))
	if err != nil || string(data) != "the only copy" {
		t.Errorf("the entry should be restored from the -old temp: %q, %v", data, err)
	}
	for _, p := range []string{swapOld, swapNew, leftover} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("%s should be gone", p)
		}
	}
}

// Review follow-up: a swap temp that cannot be renamed back is kept (it holds the
// only copy) and reported as a failure naming its path; a stale temp that cannot
// be removed is reported the same way.
func TestRemoveStaleTempsReportsFailures(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks do not apply to root")
	}
	h := testutil.HermeticHome(t)
	set, err := harness.LoadBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	oc, _ := set.Get("opencode")
	dir := filepath.Join(h.DataHome, "oc")
	parent := filepath.Join(dir, "config", "opencode")
	old := time.Now().Add(-time.Hour)
	swapOld := filepath.Join(parent, ".agent.aprofile-new-55-old")
	testutil.WriteFile(t, filepath.Join(swapOld, "helper.md"), "the only copy")
	tmp := filepath.Join(parent, ".opencode.json.aprofile-tmp-3")
	testutil.WriteFile(t, tmp, "partial")
	ageTree(t, swapOld, old)
	ageTree(t, tmp, old)
	if err := os.Chmod(parent, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	res, err := layer.RemoveStaleTemps(oc, dir, time.Now().Add(-time.Minute), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Restored) != 0 || len(res.Removed) != 0 {
		t.Errorf("nothing can change in a read-only dir: %+v", res)
	}
	msgs := ""
	for _, e := range res.Failed {
		msgs += e.Error() + "\n"
	}
	if !strings.Contains(msgs, swapOld) || !strings.Contains(msgs, tmp) || len(res.Failed) != 2 {
		t.Errorf("failures should name both paths:\n%s", msgs)
	}
	if _, err := os.Lstat(swapOld); err != nil {
		t.Error("the swap temp holds the only copy: keep it")
	}
}

// Review follow-up: a temp directory is judged by the newest modification time
// within it, so a long running copy whose top-level directory is old but which is
// still writing files deep inside is not deleted mid-copy.
func TestRemoveStaleTempsJudgesDirsByNewestEntry(t *testing.T) {
	h := testutil.HermeticHome(t)
	set, err := harness.LoadBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	oc, _ := set.Get("opencode")
	dir := filepath.Join(h.DataHome, "oc")
	old := time.Now().Add(-time.Hour)
	busy := filepath.Join(dir, ".agent.aprofile-tmp-4")
	testutil.WriteFile(t, filepath.Join(busy, "a", "b", "old.md"), "x")
	ageTree(t, busy, old)
	testutil.WriteFile(t, filepath.Join(busy, "a", "b", "new.md"), "just written")
	if err := os.Chtimes(filepath.Join(busy, "a", "b"), old, old); err != nil {
		t.Fatal(err)
	}
	idle := filepath.Join(dir, ".skills.aprofile-tmp-5")
	testutil.WriteFile(t, filepath.Join(idle, "a", "old.md"), "x")
	ageTree(t, idle, old)

	res, err := layer.RemoveStaleTemps(oc, dir, time.Now().Add(-time.Minute), false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Removed, ",") != ".skills.aprofile-tmp-5" {
		t.Errorf("removed = %v, want only the idle temp dir", res.Removed)
	}
	if _, err := os.Lstat(busy); err != nil {
		t.Error("a temp dir with a fresh entry inside must stay")
	}
}
