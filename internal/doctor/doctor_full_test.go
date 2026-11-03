package doctor

import (
	"os"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/secret"
	"github.com/cc-deck/aprofile/internal/testutil"
)

// ghostDefinition is a data-only harness whose binary does not exist, so the
// missing-binary check fires deterministically regardless of what is installed on the
// test machine (no real harness name is used).
const ghostDefinition = `name: ghost
binary: aprofile-nonexistent-binary-xyz
wrapper_prefix: ghost
config_env:
  GHOST_DIR: "."
auth_kinds:
  - name: token
    vars: { GHOST_TOKEN: ref }
default_auth_kind: token
`

// TestDoctorFullChecks exercises the FR-044 checks added in T054: missing harness
// binary, a reference that cannot be resolved now (FR-028a), and a missing wrapper. It
// also asserts no finding leaks a resolved secret value (SC-006).
func TestDoctorFullChecks(t *testing.T) {
	testutil.HermeticHome(t)
	paths, err := config.ResolvePaths()
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	set := harness.LoadFromBytes(map[string][]byte{"ghost.yaml": []byte(ghostDefinition)})
	if errs := set.Errors(); len(errs) > 0 {
		t.Fatalf("unexpected definition errors: %v", errs)
	}
	store := profile.NewStore(paths)

	// Ensure the referenced variable is unset so the reference cannot resolve now.
	os.Unsetenv("MISSING_TOKEN_XYZ")
	p := &profile.Profile{
		Harness:  "ghost",
		Name:     "g1",
		AuthKind: "token",
		AuthRefs: map[string]secret.Reference{
			"GHOST_TOKEN": mustRef(t, "env:MISSING_TOKEN_XYZ"),
		},
	}
	if err := store.Save(p); err != nil {
		t.Fatalf("save profile: %v", err)
	}

	findings, err := Run(paths, set, store)
	if err != nil {
		t.Fatalf("doctor run: %v", err)
	}

	wantSubstrings := []string{
		"aprofile-nonexistent-binary-xyz", // missing binary
		"env:MISSING_TOKEN_XYZ",           // unresolved reference (reports the ref, not a value)
		`wrapper "ghost-g1" is missing`,   // missing wrapper
	}
	for _, want := range wantSubstrings {
		if !anyFinding(findings, want) {
			t.Errorf("expected a finding containing %q; got:\n%s", want, dump(findings))
		}
	}

	// Every reference/binary finding must name the profile and a fix (FR-044).
	for _, f := range findings {
		if f.Profile == "ghost/g1" && f.Fix == "" {
			t.Errorf("profile finding without a fix: %+v", f)
		}
	}
}

// TestDoctorReferenceNoSecretLeak proves a *resolvable* reference never puts the value
// into a finding: doctor reports nothing for a set variable, and the value never
// appears in any finding (SC-006).
func TestDoctorReferenceNoSecretLeak(t *testing.T) {
	testutil.HermeticHome(t)
	paths, _ := config.ResolvePaths()
	set := harness.LoadFromBytes(map[string][]byte{"ghost.yaml": []byte(ghostDefinition)})
	store := profile.NewStore(paths)

	const canary = "CANARY-doctor-secret-value-should-never-appear"
	t.Setenv("GHOST_SECRET_SRC", canary)
	p := &profile.Profile{
		Harness: "ghost", Name: "g1", AuthKind: "token",
		AuthRefs: map[string]secret.Reference{"GHOST_TOKEN": mustRef(t, "env:GHOST_SECRET_SRC")},
	}
	if err := store.Save(p); err != nil {
		t.Fatal(err)
	}

	findings, err := Run(paths, set, store)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		if strings.Contains(f.Message, canary) || strings.Contains(f.Fix, canary) {
			t.Fatalf("doctor finding leaked the resolved secret value: %+v", f)
		}
	}
	// A set reference must not be reported as unresolved.
	if anyFinding(findings, "GHOST_TOKEN") && anyFinding(findings, "cannot") {
		t.Errorf("a resolvable reference should not be reported unresolved:\n%s", dump(findings))
	}
}

func mustRef(t *testing.T, raw string) secret.Reference {
	t.Helper()
	r, err := secret.ParseReference(raw)
	if err != nil {
		t.Fatalf("parse reference %q: %v", raw, err)
	}
	return r
}

func anyFinding(findings []Finding, substr string) bool {
	for _, f := range findings {
		if strings.Contains(f.Message, substr) {
			return true
		}
	}
	return false
}

func dump(findings []Finding) string {
	var b strings.Builder
	for _, f := range findings {
		b.WriteString("- [" + f.Profile + "] " + f.Message + "\n")
	}
	return b.String()
}
