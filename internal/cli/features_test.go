package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/testutil"
)

// Spec 006 T014: the features command's status, mutation and validation errors.

func addFeatureProfile(t *testing.T, args ...string) {
	t.Helper()
	h := testutil.HermeticHome(t)
	testutil.SeedDefaults(t, h, "claude")
	if _, err := runCLI(t, "add", "claude", "work", "--auth-kind", "oauth-login", "--share", "content"); err != nil {
		t.Fatal(err)
	}
}

// FR-001: bare status lists the harness's features, grouped, with state and
// effective defaults.
func TestFeaturesStatusGroupedWithDefaults(t *testing.T) {
	addFeatureProfile(t)
	out, err := runCLI(t, "features", "claude/work")
	if err != nil {
		t.Fatalf("features: %v\n%s", err, out)
	}
	for _, want := range []string{"telemetry:", "automode-log:", "logging:"} {
		if !strings.Contains(out, want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}
	if tel := strings.Index(out, "telemetry:"); tel < 0 || !strings.Contains(out[tel:], "off") {
		t.Errorf("telemetry should be off before enabling:\n%s", out)
	}
	// Effective defaults show for enabled features with unset params (contract).
	if _, err := runCLI(t, "features", "claude/work", "--enable", "telemetry",
		"--traces", "on", "--logs", "on", "--endpoint", "https://otlp.example.com"); err != nil {
		t.Fatal(err)
	}
	out, err = runCLI(t, "features", "claude/work")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "http/protobuf") {
		t.Errorf("status should show the effective protocol default:\n%s", out)
	}

	// FR-017: the stable --json schema.
	out, err = runCLI(t, "features", "claude/work", "--json")
	if err != nil {
		t.Fatalf("features --json: %v\n%s", err, out)
	}
	var st struct {
		Profile string `json:"profile"`
		Harness string `json:"harness"`
		Groups  []struct {
			Name     string `json:"name"`
			Features []struct {
				ID      string            `json:"id"`
				Enabled bool              `json:"enabled"`
				Params  map[string]string `json:"params"`
			} `json:"features"`
		} `json:"groups"`
	}
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		t.Fatalf("features --json: %v\n%s", err, out)
	}
	if st.Profile != "claude/work" || st.Harness != "claude" {
		t.Errorf("json header = %s/%s", st.Harness, st.Profile)
	}
	var tel, am struct {
		ID      string
		Enabled bool
		Params  map[string]string
	}
	for _, g := range st.Groups {
		for _, f := range g.Features {
			switch f.ID {
			case "telemetry":
				tel.ID, tel.Enabled, tel.Params = f.ID, f.Enabled, f.Params
			case "automode-log":
				am.ID, am.Enabled, am.Params = f.ID, f.Enabled, f.Params
			}
		}
	}
	if !tel.Enabled || tel.Params["protocol"] != "http/protobuf" {
		t.Errorf("telemetry json = %+v", tel)
	}
	if am.Enabled || am.Params == nil {
		t.Errorf("automode-log json = %+v, want disabled with params {}", am)
	}
}

// FR-002: a mutation stores the state and prints the sync reminder.
func TestFeaturesMutationStoresAndReminds(t *testing.T) {
	addFeatureProfile(t)
	out, err := runCLI(t, "features", "claude/work", "--enable", "automode-log")
	if err != nil {
		t.Fatalf("features: %v\n%s", err, out)
	}
	if !strings.Contains(out, "automode-log: enabled") || !strings.Contains(out, "sync") {
		t.Errorf("mutation output = %q", out)
	}
	out, err = runCLI(t, "features", "claude/work")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "automode-log:") || !strings.Contains(out, "on") {
		t.Errorf("stored state not visible in status:\n%s", out)
	}
	// The mutation --json schema (contracts/features-cli.md).
	out, err = runCLI(t, "features", "claude/work", "--disable", "automode-log", "--json")
	if err != nil {
		t.Fatalf("features --disable --json: %v\n%s", err, out)
	}
	var mj struct {
		Changed []struct {
			ID      string            `json:"id"`
			Enabled bool              `json:"enabled"`
			Params  map[string]string `json:"params"`
		} `json:"changed"`
		Reminder string `json:"reminder"`
	}
	if err := json.Unmarshal([]byte(out), &mj); err != nil {
		t.Fatalf("mutation json: %v\n%s", err, out)
	}
	if len(mj.Changed) != 1 || mj.Changed[0].ID != "automode-log" || mj.Changed[0].Enabled {
		t.Errorf("changed = %+v", mj.Changed)
	}
	if mj.Reminder != "sync" {
		t.Errorf("reminder = %q", mj.Reminder)
	}
}

// FR-007 / FR-004 and the spec's edge cases: every validation error names what
// it should, and nothing is stored.
func TestFeaturesValidationErrors(t *testing.T) {
	addFeatureProfile(t)
	cases := []struct {
		name   string
		args   []string
		wantIn string
	}{
		{"unsupported feature", []string{"--enable", "time-travel"}, "unknown feature"},
		{"neither signal", []string{"--enable", "telemetry", "--endpoint", "https://e"}, "--traces"},
		{"missing endpoint", []string{"--enable", "telemetry", "--traces", "on"}, "--endpoint"},
		{"bad enum value", []string{"--enable", "telemetry", "--traces", "on", "--endpoint", "https://e", "--protocol", "carrier-pigeon"}, "carrier-pigeon"},
		{"bad bool value", []string{"--enable", "telemetry", "--traces", "maybe"}, "on or off"},
		{"unknown feature param flag on wrong feature", []string{"--enable", "automode-log", "--traces", "on"}, "combined with telemetry"},
	}
	for _, tc := range cases {
		out, err := runCLI(t, append([]string{"features", "claude/work"}, tc.args...)...)
		if err == nil {
			t.Errorf("%s: expected an error", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantIn) {
			t.Errorf("%s: error %q should contain %q", tc.name, err.Error(), tc.wantIn)
		}
		_ = out
	}
	// Nothing was stored by the failed mutations.
	out, err := runCLI(t, "features", "claude/work")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "on\n") {
		t.Errorf("a failed mutation stored state:\n%s", out)
	}
}

// FR-013: mutations are refused at share level all with the way out; bare
// status still works.
func TestFeaturesShareAllRefusal(t *testing.T) {
	h := testutil.HermeticHome(t)
	testutil.SeedDefaults(t, h, "claude")
	_ = h
	if _, err := runCLI(t, "add", "claude", "shared", "--auth-kind", "oauth-login", "--share", "all"); err != nil {
		t.Fatal(err)
	}
	_, err := runCLI(t, "features", "claude/shared", "--enable", "automode-log")
	if err == nil || !strings.Contains(err.Error(), "--share content") {
		t.Errorf("mutation at all should be refused with the hint: %v", err)
	}
	out, err := runCLI(t, "features", "claude/shared")
	if err != nil {
		t.Fatalf("bare status at all must still work: %v\n%s", err, out)
	}
	if !strings.Contains(out, "automode-log") {
		t.Errorf("status at all should still list features:\n%s", out)
	}
}

// FR-015 (this direction): enabling a feature whose env entries collide with a
// hand-set extra env var fails naming the variable.
func TestFeaturesEnvCollision(t *testing.T) {
	addFeatureProfile(t)
	if _, err := runCLI(t, "edit", "claude/work", "--env-literal", "AUTOMODE_DECISION_LOG=1"); err != nil {
		t.Fatal(err)
	}
	_, err := runCLI(t, "features", "claude/work", "--enable", "automode-log")
	if err == nil || !strings.Contains(err.Error(), "AUTOMODE_DECISION_LOG") {
		t.Errorf("collision should fail naming the variable: %v", err)
	}
}

// --dry-run reports the would-be change without storing it.
func TestFeaturesDryRun(t *testing.T) {
	addFeatureProfile(t)
	out, err := runCLI(t, "features", "claude/work", "--enable", "automode-log", "--dry-run")
	if err != nil {
		t.Fatalf("dry-run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "(dry-run)") || !strings.Contains(out, "automode-log: enabled") {
		t.Errorf("dry-run output = %q", out)
	}
	out, err = runCLI(t, "features", "claude/work")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Replace(out, "off", "", -1), "on") {
		t.Errorf("dry-run stored state:\n%s", out)
	}
}

// T020 (User Story 3): the status feature sets and defaults match the harness
// definitions, cross-harness.
func TestFeaturesStatusCrossHarness(t *testing.T) {
	h := testutil.HermeticHome(t)
	testutil.SeedDefaults(t, h, "claude")
	testutil.SeedDefaults(t, h, "codex")
	if _, err := runCLI(t, "add", "claude", "c1", "--auth-kind", "oauth-login", "--share", "content"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "add", "codex", "x1", "--auth-kind", "api-key", "--auth-key", "file:/k", "--share", "content"); err != nil {
		t.Fatal(err)
	}
	status := func(ref string) string {
		out, err := runCLI(t, "features", ref, "--json")
		if err != nil {
			t.Fatalf("features %s: %v\n%s", ref, err, out)
		}
		return out
	}
	var parse func(string) map[string]map[string]any
	parse = func(out string) map[string]map[string]any {
		var st struct {
			Groups []struct {
				Name     string `json:"name"`
				Features []struct {
					ID     string            `json:"id"`
					Params map[string]string `json:"params"`
				} `json:"features"`
			} `json:"groups"`
		}
		if err := json.Unmarshal([]byte(out), &st); err != nil {
			t.Fatalf("features --json: %v\n%s", err, out)
		}
		feats := map[string]map[string]any{}
		for _, g := range st.Groups {
			for _, f := range g.Features {
				m := map[string]any{"group": g.Name, "params": f.Params}
				feats[f.ID] = m
			}
		}
		return feats
	}
	claude := parse(status("claude/c1"))
	codex := parse(status("codex/x1"))
	// Both harnesses support telemetry; only claude supports automode-log.
	if _, ok := claude["telemetry"]; !ok {
		t.Error("claude status lacks telemetry")
	}
	if _, ok := codex["telemetry"]; !ok {
		t.Error("codex status lacks telemetry")
	}
	if _, ok := claude["automode-log"]; !ok {
		t.Error("claude status lacks automode-log")
	}
	if _, ok := codex["automode-log"]; ok {
		t.Error("codex status must not list automode-log")
	}
	// The protocol enum is per harness: claude accepts http/protobuf, codex
	// rejects it and accepts otlp-grpc (FR-004).
	if _, err := runCLI(t, "features", "claude/c1", "--enable", "telemetry", "--traces", "on",
		"--endpoint", "https://e", "--protocol", "http/protobuf"); err != nil {
		t.Errorf("claude should accept http/protobuf: %v", err)
	}
	if _, err := runCLI(t, "features", "codex/x1", "--enable", "telemetry", "--traces", "on",
		"--endpoint", "https://e", "--protocol", "http/protobuf"); err == nil {
		t.Error("codex should reject http/protobuf")
	}
	if _, err := runCLI(t, "features", "codex/x1", "--enable", "telemetry", "--traces", "on",
		"--endpoint", "https://e", "--protocol", "otlp-grpc"); err != nil {
		t.Errorf("codex should accept otlp-grpc: %v", err)
	}
}
