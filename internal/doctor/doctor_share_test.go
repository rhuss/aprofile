package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/layer"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/testutil"
)

func findingWith(findings []Finding, substr string) *Finding {
	for i := range findings {
		if strings.Contains(findings[i].Message, substr) {
			return &findings[i]
		}
	}
	return nil
}

func prepared(t *testing.T, paths *config.Paths, set *harness.Set, store *profile.Store, h, n string, share profile.Share, sets ...string) string {
	t.Helper()
	p := &profile.Profile{Harness: h, Name: n, Share: share, AuthKind: map[string]string{"claude": "oauth-login", "opencode": "login"}[h], HookSets: sets}
	if err := store.Save(p); err != nil {
		t.Fatal(err)
	}
	def, _ := set.Get(h)
	dir := paths.ProfileConfigDir(h, n)
	if _, err := layer.PrepareConfigDir(def, p, dir, false); err != nil {
		t.Fatal(err)
	}
	return dir
}

// T051/FR-020/FR-001a: each new check with its severity; the exit status counts
// only problems.
func TestDoctorShareChecks(t *testing.T) {
	h := testutil.HermeticHome(t)
	testutil.SeedDefaults(t, h, "claude")
	testutil.SeedDefaults(t, h, "opencode")
	paths, _ := config.ResolvePaths()
	harnessesD := paths.HarnessesDir()
	// A user override with the deprecated alias and a catch-all.
	testutil.WriteFile(t, filepath.Join(harnessesD, "claude.yaml"),
		"name: claude\nclassification:\n  - { entry: \"RTK.md\", layer: shared }\n")
	testutil.WriteFile(t, filepath.Join(harnessesD, "toy.yaml"),
		"name: toy\nbinary: toy\nwrapper_prefix: toy\nconfig_env: { TOY_HOME: \".\" }\nclassification:\n  - { entry: \"*\", layer: content }\n")
	// A hook set whose script does not exist, used by a profile, plus a missing set.
	testutil.WriteFile(t, filepath.Join(paths.HooksDir(), "jira.yaml"),
		"name: jira\nbodies:\n  claude:\n    Stop: [{matcher: x, hooks: [{type: command, command: /nonexistent/jira.sh}]}]\n")
	set, _ := harness.Load(harnessesD)
	store := profile.NewStore(paths)

	work := prepared(t, paths, set, store, "claude", "work", profile.ShareContent)
	_ = prepared(t, paths, set, store, "claude", "ci", profile.ShareNone, "jira", "gone")
	_ = prepared(t, paths, set, store, "opencode", "oc", profile.ShareNone)
	// A hand-edited all profile that still names a hook set.
	_ = prepared(t, paths, set, store, "claude", "same", profile.ShareAll, "jira")

	// A linked content entry replaced by a real file (atomic rewrite by a harness).
	claudeMD := filepath.Join(work, "CLAUDE.md")
	_ = os.Remove(claudeMD)
	testutil.WriteFile(t, claudeMD, "real")
	// The content profile's settings differ from the default setup.
	testutil.WriteFile(t, filepath.Join(work, "settings.json"), `{"own":true}`)

	findings, err := Run(paths, set, store)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		substr, severity, fix string
	}{
		{"stopped sharing CLAUDE.md", SeverityProblem, "sync --relink"},
		{`hook set "gone" is referenced but missing`, SeverityProblem, ""},
		{"/nonexistent/jira.sh", SeverityProblem, ""},
		{"deprecated layer shared", SeverityProblem, ""},
		{"every unknown entry is linked", SeverityProblem, ""},
		{"settings differ from the default setup", SeverityInfo, "settings pull"},
		{"~/.agents/skills", SeverityInfo, ""},
		{"not borrowed by any profile", SeverityInfo, "harness classify"},
		{"hook sets jira are attached but ignored", SeverityInfo, "--share content"},
	}
	for _, c := range cases {
		f := findingWith(findings, c.substr)
		if f == nil {
			t.Errorf("missing finding %q in:\n%s", c.substr, dump(findings))
			continue
		}
		if f.Severity != c.severity {
			t.Errorf("%q: severity %q, want %q", c.substr, f.Severity, c.severity)
		}
		if c.fix != "" && !strings.Contains(f.Fix, c.fix) {
			t.Errorf("%q: fix %q should mention %q", c.substr, f.Fix, c.fix)
		}
	}
	// The unborrowed info is reported once per harness, not per profile.
	n := 0
	for _, f := range findings {
		if strings.Contains(f.Message, "not borrowed by any profile") && strings.HasPrefix(f.Message, "claude") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("unborrowed default entries reported %d times for claude, want once", n)
	}
	if !HasProblems(findings) {
		t.Error("problems were found")
	}
	if HasProblems([]Finding{{Message: "x", Severity: SeverityInfo}}) {
		t.Error("information alone is not a problem")
	}
}
