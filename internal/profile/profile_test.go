package profile_test

import (
	"os"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/secret"
	"github.com/cc-deck/aprofile/internal/testutil"
)

func newStore(t *testing.T) *profile.Store {
	t.Helper()
	testutil.HermeticHome(t)
	paths, err := config.ResolvePaths()
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}
	return profile.NewStore(paths)
}

func TestStoreRoundTrip(t *testing.T) {
	store := newStore(t)
	ref, _ := secret.ParseReference("env:WORK_TOKEN")
	in := &profile.Profile{
		Harness:  "claude",
		Name:     "work",
		Label:    "Work",
		Color:    "blue",
		Icon:     "W",
		Primary:  true,
		AuthKind: "oauth-token",
		AuthRefs: map[string]secret.Reference{"CLAUDE_CODE_OAUTH_TOKEN": ref},
		Model:    "sonnet",
		Endpoint: "https://api.example.com",
		ExtraEnv: []profile.ExtraEnvVar{
			{Name: "FOO", Value: "env:FOO_REF"},
			{Name: "TEAM", Value: "platform", Literal: true},
		},
		HerdrAgent: "worker",
	}
	if err := store.Save(in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := store.Load("claude", "work")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.Label != "Work" || out.Color != "blue" || out.Icon != "W" || !out.Primary {
		t.Errorf("basic fields not round-tripped: %+v", out)
	}
	if out.AuthRefs["CLAUDE_CODE_OAUTH_TOKEN"].Raw != "env:WORK_TOKEN" {
		t.Errorf("auth ref not round-tripped: %+v", out.AuthRefs)
	}
	if out.HerdrAgent != "worker" {
		t.Errorf("herdr_agent not round-tripped: %q", out.HerdrAgent)
	}
}

func TestLiteralMarkingPersisted(t *testing.T) {
	store := newStore(t)
	in := &profile.Profile{
		Harness: "claude", Name: "work", AuthKind: "oauth-login",
		ExtraEnv: []profile.ExtraEnvVar{{Name: "TEAM", Value: "platform", Literal: true}},
	}
	if err := store.Save(in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := store.Load("claude", "work")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(out.ExtraEnv) != 1 || !out.ExtraEnv[0].Literal {
		t.Errorf("literal flag not persisted: %+v", out.ExtraEnv)
	}
}

func TestNameRegexAcceptReject(t *testing.T) {
	store := newStore(t)
	bad := &profile.Profile{Harness: "claude", Name: "Bad_Name"}
	if err := store.Save(bad); err == nil {
		t.Error("expected name validation error for Bad_Name")
	}
	good := &profile.Profile{Harness: "claude", Name: "good-name", AuthKind: "oauth-login"}
	if err := store.Save(good); err != nil {
		t.Errorf("good-name should be accepted: %v", err)
	}
}

func TestHandEditedLiteralSecretRejectedOnLoad(t *testing.T) {
	store := newStore(t)
	// Simulate a hand-edited store file with a plaintext secret in an auth ref.
	path := store.Path("claude", "work")
	testutil.WriteFile(t, path, "harness: claude\nname: work\nauth_kind: oauth-token\nauth_refs:\n  CLAUDE_CODE_OAUTH_TOKEN: sk-REALSECRET\n")
	_, err := store.Load("claude", "work")
	if err == nil {
		t.Fatal("expected error loading a hand-edited plaintext auth secret")
	}
	if !strings.Contains(err.Error(), "claude/work") {
		t.Errorf("error should name the profile: %v", err)
	}
}

func TestHandEditedLiteralExtraSecretRejectedOnLoad(t *testing.T) {
	store := newStore(t)
	path := store.Path("claude", "work")
	// A non-literal extra env value that trips the length check is rejected on load.
	long := strings.Repeat("a", 40)
	testutil.WriteFile(t, path, "harness: claude\nname: work\nextra_env:\n  - name: X\n    value: "+long+"\n")
	_, err := store.Load("claude", "work")
	if err == nil {
		t.Fatal("expected error loading a long non-literal extra secret")
	}
	// But the same value marked literal loads fine (length check skipped).
	testutil.WriteFile(t, path, "harness: claude\nname: work\nextra_env:\n  - name: X\n    value: "+long+"\n    literal: true\n")
	if _, err := store.Load("claude", "work"); err != nil {
		t.Errorf("literal-marked long value should load: %v", err)
	}
}

func TestListAndFailures(t *testing.T) {
	store := newStore(t)
	_ = store.Save(&profile.Profile{Harness: "claude", Name: "work", AuthKind: "oauth-login"})
	_ = store.Save(&profile.Profile{Harness: "claude", Name: "private", AuthKind: "oauth-login"})
	// Plant a broken file.
	testutil.WriteFile(t, store.Path("claude", "broken"), "harness: claude\nname: broken\nauth_refs:\n  X: not-a-ref\n")

	profiles, failures, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(profiles) != 2 {
		t.Errorf("expected 2 good profiles, got %d", len(profiles))
	}
	if len(failures) != 1 {
		t.Errorf("expected 1 failure, got %d: %v", len(failures), failures)
	}
}

func TestExtraEnvNameValidatedOnLoad(t *testing.T) {
	store := newStore(t)
	path := store.Path("claude", "work")
	// A hand-edited store file (FR-006) whose extra-env name carries shell
	// metacharacters must be rejected on load, before it can reach a generated
	// wrapper as an assignment target / export / unset (FR-028b).
	testutil.WriteFile(t, path, "harness: claude\nname: work\nauth_kind: oauth-login\nextra_env:\n  - name: \"X=1; touch /tmp/pwned; Y\"\n    value: v\n")
	if _, err := store.Load("claude", "work"); err == nil {
		t.Fatal("expected error for an extra-env name with shell metacharacters")
	}
	// A valid identifier loads fine.
	testutil.WriteFile(t, path, "harness: claude\nname: work\nauth_kind: oauth-login\nextra_env:\n  - name: GOOD_NAME\n    value: v\n")
	if _, err := store.Load("claude", "work"); err != nil {
		t.Errorf("valid extra-env name should load: %v", err)
	}
}

func TestAuthVarNameValidated(t *testing.T) {
	ref, _ := secret.ParseReference("env:TOK")
	p := &profile.Profile{
		Harness: "claude", Name: "work", AuthKind: "oauth-token",
		AuthRefs: map[string]secret.Reference{"BAD;NAME": ref},
	}
	if err := p.Validate(); err == nil {
		t.Fatal("expected error for an auth variable name with a shell metacharacter")
	}
}

func TestRemove(t *testing.T) {
	store := newStore(t)
	_ = store.Save(&profile.Profile{Harness: "claude", Name: "work", AuthKind: "oauth-login"})
	if err := store.Remove("claude", "work"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if store.Exists("claude", "work") {
		t.Error("profile should be gone after Remove")
	}
	if _, err := os.Stat(store.Path("claude", "work")); !os.IsNotExist(err) {
		t.Error("store file should not exist")
	}
}
