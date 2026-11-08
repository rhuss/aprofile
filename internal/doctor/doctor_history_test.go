package doctor

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

// spec 003 T013: a member's history link to its group store is not a leak; an
// isolated profile's history link still is.
func TestDoctorMemberHistoryLinkIsNotALeak(t *testing.T) {
	h := testutil.HermeticHome(t)
	paths, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	set, _ := harness.LoadBuiltins()
	store := profile.NewStore(paths)
	testutil.SeedDefaults(t, h, "claude")
	home := filepath.Join(h.Root, ".claude")

	for _, p := range []*profile.Profile{
		{Harness: "claude", Name: "lab", Share: profile.ShareAll, HistoryGroup: "home"},
		{Harness: "claude", Name: "solo", Share: profile.ShareAll},
	} {
		if err := store.Save(p); err != nil {
			t.Fatal(err)
		}
		dir := paths.ProfileConfigDir(p.Harness, p.Name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(home, "projects"), filepath.Join(dir, "projects")); err != nil {
			t.Fatal(err)
		}
	}
	findings, err := Run(paths, set, store)
	if err != nil {
		t.Fatal(err)
	}
	var labLeak, soloLeak bool
	for _, f := range findings {
		if !strings.Contains(f.Message, `"projects/"`) || !strings.Contains(f.Message, "leak") {
			continue
		}
		switch f.Profile {
		case "claude/lab":
			labLeak = true
		case "claude/solo":
			soloLeak = true
		}
	}
	if labLeak {
		t.Error("a member's history link to its store must not be reported as a leak")
	}
	if !soloLeak {
		t.Error("an isolated profile's history link must still be reported as a leak")
	}
}
