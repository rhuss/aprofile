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

// Spec 006, data-model "Feature state": features round-trip sorted by feature id
// through Save/Load.
func TestFeaturesRoundTripSorted(t *testing.T) {
	store := newStore(t)
	in := &profile.Profile{Harness: "claude", Name: "work", AuthKind: "oauth-login"}
	in.Features = []profile.FeatureState{
		{Feature: "telemetry", Params: map[string]string{
			"traces": "on", "logs": "off", "endpoint": "https://otlp.example.com", "protocol": "http/protobuf",
		}},
		{Feature: "automode-log"},
	}
	if err := store.Save(in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(store.Path("claude", "work"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "features:") {
		t.Errorf("features not stored:\n%s", data)
	}
	// Stored sorted by feature id: automode-log before telemetry.
	autoIdx := strings.Index(string(data), "feature: automode-log")
	telIdx := strings.Index(string(data), "feature: telemetry")
	if autoIdx < 0 || telIdx < 0 || autoIdx > telIdx {
		t.Errorf("features not sorted by feature id:\n%s", data)
	}
	out, err := store.Load("claude", "work")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(out.Features) != 2 {
		t.Fatalf("features not round-tripped: %+v", out.Features)
	}
	if out.Features[0].Feature != "automode-log" || out.Features[1].Feature != "telemetry" {
		t.Errorf("features not sorted after load: %+v", out.Features)
	}
	tel := out.Features[1]
	if tel.Params["traces"] != "on" || tel.Params["endpoint"] != "https://otlp.example.com" {
		t.Errorf("telemetry params not round-tripped: %+v", tel.Params)
	}
	// A profile without features stores no features key (SC-002-style minimal file).
	_ = store.Save(&profile.Profile{Harness: "claude", Name: "solo", AuthKind: "oauth-login"})
	data, _ = os.ReadFile(store.Path("claude", "solo"))
	if strings.Contains(string(data), "features") {
		t.Errorf("featureless profile stores a features key:\n%s", data)
	}
}

// Unknown feature ids and out-of-enum stored params each add a LoadWarning and
// place nothing (data-model "Feature state").
func TestFeaturesLoadWarnings(t *testing.T) {
	store := newStore(t)
	var got []string
	store.Warn = func(msg string) { got = append(got, msg) }
	reset := func() { got = nil }
	// Unknown feature id.
	path := store.Path("claude", "work")
	testutil.WriteFile(t, path, "harness: claude\nname: work\nauth_kind: oauth-login\nfeatures:\n  - feature: time-travel\n")
	reset()
	p, err := store.Load("claude", "work")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 1 || !strings.Contains(got[0], "time-travel") {
		t.Errorf("unknown feature id should warn naming it: %v", got)
	}
	if len(p.Features) != 0 {
		t.Errorf("unknown feature id should be dropped from the record: %+v", p.Features)
	}
	// Out-of-enum stored param (not in the catalog's cross-harness union): the
	// whole feature drops, so the harness default cannot re-supply the param and
	// place entries from a malformed state.
	testutil.WriteFile(t, path, "harness: claude\nname: work\nauth_kind: oauth-login\nfeatures:\n  - feature: telemetry\n    params:\n      traces: on\n      logs: on\n      endpoint: https://e\n      protocol: carrier-pigeon\n")
	reset()
	p, err = store.Load("claude", "work")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 1 || !strings.Contains(got[0], "protocol") {
		t.Errorf("out-of-enum param should warn naming it: %v", got)
	}
	if len(p.Features) != 0 {
		t.Fatalf("a malformed feature state should place nothing: %+v", p.Features)
	}
	// Unknown param name and a bad bool value warn the same way and drop the
	// feature.
	testutil.WriteFile(t, path, "harness: claude\nname: work\nauth_kind: oauth-login\nfeatures:\n  - feature: telemetry\n    params:\n      traces: maybe\n      flavor: vanilla\n")
	reset()
	p, err = store.Load("claude", "work")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("bad bool and unknown param should each warn: %v", got)
	}
	if len(p.Features) != 0 {
		t.Fatalf("a malformed feature state should place nothing: %+v", p.Features)
	}
}

func TestHistoryGroupRoundTripAndValidation(t *testing.T) {
	store := newStore(t)
	in := &profile.Profile{Harness: "claude", Name: "lab", HistoryGroup: "home"}
	if err := store.Save(in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(store.Path("claude", "lab"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "history_group: home") {
		t.Errorf("history_group not stored:\n%s", data)
	}
	out, err := store.Load("claude", "lab")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.HistoryGroup != "home" {
		t.Errorf("HistoryGroup = %q, want home", out.HistoryGroup)
	}

	// Isolated profiles store no history_group key (SC-002).
	iso := &profile.Profile{Harness: "claude", Name: "solo"}
	if err := store.Save(iso); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, _ = os.ReadFile(store.Path("claude", "solo"))
	if strings.Contains(string(data), "history_group") {
		t.Errorf("isolated profile stores history_group:\n%s", data)
	}

	for _, bad := range []string{"Work", "work_1", "-x", "a/b"} {
		p := &profile.Profile{Harness: "claude", Name: "x1", HistoryGroup: bad}
		if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "history group") {
			t.Errorf("Validate(history_group %q) = %v, want a history group error", bad, err)
		}
	}
}
