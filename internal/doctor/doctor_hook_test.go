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

const hookyDefinition = `name: hooky
binary: sh
wrapper_prefix: hooky
config_env:
  HOOKY_DIR: "."
exec_hook:
  operations: [env, pre-launch, auth-status]
`

// hookySetup stores two hooky profiles with a wrapper file each, so the stale check
// runs, and returns what doctor.Run needs.
func hookySetup(t *testing.T) (*config.Paths, *harness.Set, *profile.Store) {
	t.Helper()
	testutil.HermeticHome(t)
	paths, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	set := harness.LoadFromBytes(map[string][]byte{"hooky.yaml": []byte(hookyDefinition)})
	if errs := set.Errors(); len(errs) > 0 {
		t.Fatal(errs)
	}
	store := profile.NewStore(paths)
	for _, n := range []string{"p1", "p2"} {
		if err := store.Save(&profile.Profile{Harness: "hooky", Name: n}); err != nil {
			t.Fatal(err)
		}
		testutil.WriteFile(t, filepath.Join(paths.BinDir, "hooky-"+n), "#!/bin/sh\n# aprofile:wrapper harness=hooky profile="+n+"\n")
	}
	return paths, set, store
}

func hookFindingsOf(findings []Finding) []Finding {
	var out []Finding
	for _, f := range findings {
		if strings.Contains(f.Message+f.Fix, "aprofile-harness-hooky") || strings.Contains(f.Message, "exec hook") || strings.Contains(f.Message, "auth-status") {
			out = append(out, f)
		}
	}
	return out
}

// M15 (FR-044): a declared exec hook whose executable is not on PATH is a problem,
// reported once per harness, naming the hook, the operations and a fix.
func TestDoctorReportsMissingDeclaredHookOncePerHarness(t *testing.T) {
	paths, set, store := hookySetup(t)
	t.Setenv("PATH", t.TempDir()+string(os.PathListSeparator)+"/usr/bin:/bin")

	findings, err := Run(paths, set, store)
	if err != nil {
		t.Fatal(err)
	}
	hf := hookFindingsOf(findings)
	if len(hf) != 1 {
		t.Fatalf("want exactly one hook finding, got %d: %+v", len(hf), hf)
	}
	f := hf[0]
	if f.Severity != SeverityProblem || !strings.Contains(f.Message, "not on PATH") || f.Fix == "" {
		t.Errorf("finding: %+v", f)
	}
	for _, op := range []string{"env", "pre-launch", "auth-status"} {
		if !strings.Contains(f.Message, op) {
			t.Errorf("finding should name the declared operation %s: %s", op, f.Message)
		}
	}
}

// M15: a failing env operation is reported per profile with the hook's error,
// instead of silently skipping the stale-wrapper check; a failing auth-status names
// the hook executable in its fix.
func TestDoctorReportsFailingHookOperations(t *testing.T) {
	paths, set, store := hookySetup(t)
	dir := t.TempDir()
	testutil.WriteFile(t, filepath.Join(dir, "aprofile-harness-hooky"),
		"#!/bin/sh\ncat >/dev/null\ncase \"$1\" in\n  env) echo 'env broke' >&2; exit 3 ;;\n  auth-status) echo 'status broke' >&2; exit 4 ;;\n  *) exit 64 ;;\nesac\n")
	if err := os.Chmod(filepath.Join(dir, "aprofile-harness-hooky"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/usr/bin:/bin")

	findings, err := Run(paths, set, store)
	if err != nil {
		t.Fatal(err)
	}
	var env, auth int
	for _, f := range hookFindingsOf(findings) {
		switch {
		case strings.Contains(f.Message, "env broke"):
			env++
			if f.Profile == "" || !strings.Contains(f.Fix, "aprofile-harness-hooky") {
				t.Errorf("env finding should name the profile and the hook in its fix: %+v", f)
			}
		case strings.Contains(f.Message, "status broke"):
			auth++
			if !strings.Contains(f.Fix, "aprofile-harness-hooky") {
				t.Errorf("auth-status finding should name the hook in its fix: %+v", f)
			}
		case strings.Contains(f.Message, "not on PATH"):
			t.Errorf("the hook is installed: %+v", f)
		}
	}
	if env != 2 || auth != 2 {
		t.Errorf("want one env and one auth-status finding per profile, got env=%d auth=%d: %+v", env, auth, findings)
	}
}
