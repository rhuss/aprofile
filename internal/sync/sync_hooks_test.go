package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/testutil"
)

func setup(t *testing.T, defs map[string]string) (*config.Paths, *harness.Set, *profile.Store) {
	t.Helper()
	testutil.HermeticHome(t)
	t.Setenv("PATH", t.TempDir()+string(os.PathListSeparator)+"/usr/bin:/bin")
	paths, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for name, src := range defs {
		files[name+".yaml"] = []byte(src)
	}
	set := harness.LoadFromBytes(files)
	return paths, set, profile.NewStore(paths)
}

func count(list []string, sub string) int {
	n := 0
	for _, s := range list {
		if strings.Contains(s, sub) {
			n++
		}
	}
	return n
}

func hookDef(ops string) string {
	return "name: hooky\nbinary: sh\nwrapper_prefix: hooky\nconfig_env:\n  HOOKY_DIR: \".\"\nexec_hook:\n  operations: [" + ops + "]\n"
}

// Prod8: a declared hook that is not installed is reported once per harness and
// operation in the run's notes, not once per profile; each profile's plan still
// carries it.
func TestSyncReportsMissingHookOncePerHarness(t *testing.T) {
	paths, set, store := setup(t, map[string]string{"hooky": hookDef("pre-launch, write-config")})
	for _, n := range []string{"pa", "pb", "pc"} {
		if err := store.Save(&profile.Profile{Harness: "hooky", Name: n}); err != nil {
			t.Fatal(err)
		}
	}
	res, err := Run(paths, set, store, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if n := count(res.Notes, "aprofile-harness-hooky"); n != 2 {
		t.Errorf("want one note per missing operation (pre-launch, write-config), got %d:\n%s", n, strings.Join(res.Notes, "\n"))
	}
	for _, pp := range res.Profiles {
		if count(pp.Notes, "aprofile-harness-hooky") != 2 {
			t.Errorf("profile %s plan should carry both notes: %v", pp.Profile, pp.Notes)
		}
	}
}

// Prod8: a missing env hook fails every profile of the harness (wrappers kept), but
// the run reports it once.
func TestSyncReportsMissingEnvHookOncePerHarness(t *testing.T) {
	paths, set, store := setup(t, map[string]string{"hooky": hookDef("env")})
	for _, n := range []string{"pa", "pb"} {
		if err := store.Save(&profile.Profile{Harness: "hooky", Name: n}); err != nil {
			t.Fatal(err)
		}
	}
	res, err := Run(paths, set, store, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if n := count(res.Problems, "aprofile-harness-hooky"); n != 1 {
		t.Errorf("want the missing env hook reported once, got %d:\n%s", n, strings.Join(res.Problems, "\n"))
	}
	for _, pp := range res.Profiles {
		if pp.Status != StatusFailed || count(pp.Problems, "aprofile-harness-hooky") != 1 {
			t.Errorf("profile %s: status %s problems %v; want failed with the hook problem", pp.Profile, pp.Status, pp.Problems)
		}
	}
}

// CodeRabbit: wrappers of a harness without a usable definition are kept only while a
// profile (loaded or failing to load) still names that harness; otherwise they are
// stale and removed.
func TestSyncKeepsWrappersOfUnresolvedHarnessOnlyWithProfiles(t *testing.T) {
	paths, set, store := setup(t, map[string]string{"hooky": hookDef("auth-status")})
	if err := os.MkdirAll(paths.BinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, h string) {
		testutil.WriteFile(t, filepath.Join(paths.BinDir, name), "#!/bin/sh\n# aprofile:wrapper harness="+h+" profile=px\n")
	}
	write("gone-px", "gone")     // no definition, no profile: stale
	write("orphan-px", "orphan") // no definition, but a profile names it: kept
	write("broken-px", "broken") // no definition, a profile that fails to load names it: kept
	if err := store.Save(&profile.Profile{Harness: "orphan", Name: "px"}); err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, store.Path("broken", "px"), "harness: [not valid\n")

	res, err := Run(paths, set, store, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.WrappersRemoved, ",") != "gone-px" {
		t.Errorf("removed = %v, want [gone-px]", res.WrappersRemoved)
	}
	for _, kept := range []string{"orphan-px", "broken-px"} {
		if _, err := os.Stat(filepath.Join(paths.BinDir, kept)); err != nil {
			t.Errorf("%s must be kept: %v", kept, err)
		}
	}
}

// M9: a profile whose file fails to load still claims its wrapper name, so a loaded
// profile with the same wrapper name is skipped, not written over it.
func TestSyncWrapperClashCountsLoadFailures(t *testing.T) {
	paths, set, store := setup(t, map[string]string{
		"ha": "name: ha\nbinary: sh\nwrapper_prefix: foo\nconfig_env:\n  A_DIR: \".\"\n",
		"hb": "name: hb\nbinary: sh\nwrapper_prefix: foo-bar\nconfig_env:\n  B_DIR: \".\"\n",
	})
	if err := store.Save(&profile.Profile{Harness: "ha", Name: "bar-px"}); err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, store.Path("hb", "px"), "harness: [not valid\n")

	res, err := Run(paths, set, store, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(paths.BinDir, "foo-bar-px")); err == nil {
		t.Error("the loaded profile's wrapper must not be written over the failing profile's wrapper name")
	}
	if count(res.Problems, `"foo-bar-px"`) != 1 || count(res.Problems, "hb/px") == 0 {
		t.Errorf("want a clash problem naming the wrapper and hb/px:\n%s", strings.Join(res.Problems, "\n"))
	}
}
