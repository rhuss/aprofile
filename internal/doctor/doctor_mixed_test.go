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

func findingsWith(findings []Finding, substr string) []Finding {
	var out []Finding
	for _, f := range findings {
		if strings.Contains(f.Message, substr) {
			out = append(out, f)
		}
	}
	return out
}

// T071/FR-044: every seeded key of a mixed file that differs from the default setup
// is reported as information; owned, credentials and state keys are not, and a
// profile at share level none (nothing seeded) has no drift findings.
func TestDoctorMixedSeededKeyDrift(t *testing.T) {
	h := testutil.HermeticHome(t)
	testutil.SeedDefaults(t, h, "claude")
	paths, _ := config.ResolvePaths()
	set, _ := harness.Load(paths.HarnessesDir())
	store := profile.NewStore(paths)

	work := prepared(t, paths, set, store, "claude", "work", profile.ShareContent)
	none := prepared(t, paths, set, store, "claude", "bare", profile.ShareNone)
	// The profile changed a preference, its MCP servers, its account and a counter.
	edited := `{"autoUpdates":true,"showSpinnerTree":true,"mcpServers":{},"oauthAccount":{"email":"x"},"numStartups":99}`
	testutil.WriteFile(t, filepath.Join(work, ".claude.json"), edited)
	testutil.WriteFile(t, filepath.Join(none, ".claude.json"), edited)

	findings, err := Run(paths, set, store)
	if err != nil {
		t.Fatal(err)
	}
	drift := findingsWith(findings, "differs from the default setup")
	if len(drift) != 1 {
		t.Fatalf("want one drift finding, got:\n%s", dump(drift))
	}
	f := drift[0]
	if f.Profile != "claude/work" || !strings.Contains(f.Message, `"autoUpdates"`) || f.Severity != SeverityInfo {
		t.Errorf("unexpected drift finding: %+v", f)
	}
}

// T081/FR-013: undeclared keys of a mixed file in the default setup are reported as
// information once per harness (not per profile, not in sync), naming the keys
// that are not seeded because their name looks like a credential.
func TestDoctorUndeclaredMixedKeys(t *testing.T) {
	h := testutil.HermeticHome(t)
	testutil.SeedDefaults(t, h, "claude")
	paths, _ := config.ResolvePaths()
	set, _ := harness.Load(paths.HarnessesDir())
	store := profile.NewStore(paths)

	// Only a none profile: nothing is seeded, so nothing to report.
	_ = prepared(t, paths, set, store, "claude", "bare", profile.ShareNone)
	home := filepath.Join(h.Root, ".claude.json")
	data, err := os.ReadFile(home)
	if err != nil {
		t.Fatal(err)
	}
	// The token value is a distinctive canary: a one-letter value would also match
	// inside ordinary words, so a leak could go unnoticed.
	const canary = "CANARY-vendor-token-7f3c9e"
	withNew := strings.TrimSuffix(strings.TrimSpace(string(data)), "}") + `,"brandNewPref":1,"newVendorToken":"` + canary + `"}`
	testutil.WriteFile(t, home, withNew)
	findings, _ := Run(paths, set, store)
	if got := findingsWith(findings, "not declared"); len(got) != 0 {
		t.Errorf("no borrowing profile, want no undeclared-key finding, got:\n%s", dump(got))
	}

	_ = prepared(t, paths, set, store, "claude", "alpha", profile.ShareContent)
	_ = prepared(t, paths, set, store, "claude", "beta", profile.ShareAll)
	findings, _ = Run(paths, set, store)
	got := findingsWith(findings, "not declared")
	if len(got) != 1 {
		t.Fatalf("want the undeclared keys once per harness, got:\n%s", dump(got))
	}
	f := got[0]
	if f.Profile != "" || f.Severity != SeverityInfo || !strings.HasPrefix(f.Message, "claude:") ||
		!strings.Contains(f.Message, "brandNewPref") || !strings.Contains(f.Message, "newVendorToken") {
		t.Errorf("unexpected finding: %+v", f)
	}
	for _, f := range findings {
		if strings.Contains(f.Message, canary) || strings.Contains(f.Fix, canary) {
			t.Errorf("findings must name keys, never values: %+v", f)
		}
	}
	if f.Fix == "" {
		t.Error("the finding needs a fix")
	}
}
