package integration

// Spec 006: the features command and its placement, end to end against the
// built binary (quickstart scenarios 1-6, T009/T015/T018/T021/T026/T030).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/testutil"
)

// featureSettingsPath returns the profile's settings.json path.
func (e *env) featureSettingsPath(harness, name string) string {
	e.t.Helper()
	return filepath.Join(e.configDir(harness, name), "settings.json")
}

// featureRecordPath returns the profile's managed-launch record path.
func (e *env) featureRecordPath(harness, name string) string {
	e.t.Helper()
	return filepath.Join(e.root, ".local", "share", "aprofile", "managed-launch", harness, name+".json")
}

// featureRecordEntries reads the managed-launch record's entries.
func featureRecordEntries(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read managed-launch record %s: %v", path, err)
	}
	var rec struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("parse record %s: %v", path, err)
	}
	return rec.Entries
}

// Quickstart Scenario 1 (User Story 1): telemetry on Claude Code.
func TestFeaturesTelemetryClaudeEndToEnd(t *testing.T) {
	e := newEnv(t)
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login", "--share", "content")

	out := e.run(nil, "features", "claude/work", "--enable", "telemetry",
		"--traces", "on", "--logs", "on", "--endpoint", "https://otlp.example.com")
	if !strings.Contains(out, "sync") {
		t.Errorf("mutation output should remind to run sync:\n%s", out)
	}
	e.run(nil, "sync")

	m := readSettings(t, e.featureSettingsPath("claude", "work"))
	env := envBlock(t, m)
	want := map[string]any{
		"CLAUDE_CODE_ENABLE_TELEMETRY":        "1",
		"OTEL_TRACES_EXPORTER":                "otlp",
		"OTEL_LOGS_EXPORTER":                  "otlp",
		"OTEL_EXPORTER_OTLP_ENDPOINT":         "https://otlp.example.com",
		"OTEL_EXPORTER_OTLP_PROTOCOL":         "http/protobuf",
		"CLAUDE_CODE_ENHANCED_TELEMETRY_BETA": "1",
	}
	for k, v := range want {
		if got, ok := env[k]; !ok || got != v {
			t.Errorf("env.%s = %v (present %v), want %v", k, got, ok, v)
		}
	}
	// Each entry is recorded as a placed entry.
	entries := featureRecordEntries(t, e.featureRecordPath("claude", "work"))
	recorded := map[string]bool{}
	for _, en := range entries {
		recorded[en["location"].(string)] = true
	}
	for k := range want {
		if !recorded["env."+k] {
			t.Errorf("env.%s not recorded as a placed entry: %v", k, entries)
		}
	}

	// Bare status shows telemetry on with every parameter value (FR-001).
	out = e.run(nil, "features", "claude/work")
	for _, s := range []string{"telemetry", "on", "traces", "logs", "https://otlp.example.com", "http/protobuf"} {
		if !strings.Contains(out, s) {
			t.Errorf("status output lacks %q:\n%s", s, out)
		}
	}
	// show lists the feature among the launch settings (FR-016).
	out = e.run(nil, "show", "claude/work")
	if !strings.Contains(out, "telemetry") {
		t.Errorf("show lacks the features section:\n%s", out)
	}

	// A second sync is a no-op (idempotent placement).
	out = e.run(nil, "sync")
	if strings.Contains(out, "set env") || strings.Contains(out, "removed env") {
		t.Errorf("second sync changed the placed entries:\n%s", out)
	}

	// Disabling and re-syncing removes exactly those entries (SC-002).
	before := readSettings(t, e.featureSettingsPath("claude", "work"))
	e.run(nil, "features", "claude/work", "--disable", "telemetry")
	e.run(nil, "sync")
	after := readSettings(t, e.featureSettingsPath("claude", "work"))
	for k := range want {
		if _, ok := envBlock(t, after)[k]; ok {
			t.Errorf("env.%s still present after disable", k)
		}
	}
	if len(before) != len(after) {
		t.Errorf("settings.json top-level keys changed: %v -> %v", before, after)
	}
	if len(envBlock(t, before)) != len(envBlock(t, after))+len(want) {
		t.Errorf("more than the feature's entries were removed: %v -> %v", envBlock(t, before), envBlock(t, after))
	}
}

// Quickstart Scenario 2 (User Story 2): the Auto-mode decision log.
func TestFeaturesAutomodeLogLifecycle(t *testing.T) {
	e := newEnv(t)
	e.run(nil, "add", "claude", "private", "--auth-kind", "oauth-login", "--share", "content")

	e.run(nil, "features", "claude/private", "--enable", "automode-log")
	e.run(nil, "sync")
	m := readSettings(t, e.featureSettingsPath("claude", "private"))
	if got, ok := envBlock(t, m)["AUTOMODE_DECISION_LOG"]; !ok || got != "1" {
		t.Fatalf("env.AUTOMODE_DECISION_LOG = %v (present %v), want 1", got, ok)
	}
	entries := featureRecordEntries(t, e.featureRecordPath("claude", "private"))
	found := false
	for _, en := range entries {
		if en["location"] == "env.AUTOMODE_DECISION_LOG" {
			found = true
		}
	}
	if !found {
		t.Errorf("automode-log entry not recorded as placed: %v", entries)
	}

	// Disabling removes it on the next sync.
	e.run(nil, "features", "claude/private", "--disable", "automode-log")
	e.run(nil, "sync")
	m = readSettings(t, e.featureSettingsPath("claude", "private"))
	if _, ok := envBlock(t, m)["AUTOMODE_DECISION_LOG"]; ok {
		t.Error("env.AUTOMODE_DECISION_LOG still present after disable")
	}
}

// Quickstart Scenario 3 (User Story 3): discovery and gating.
func TestFeaturesDiscoveryAndGating(t *testing.T) {
	e := newEnv(t)
	e.run(nil, "add", "codex", "work", "--auth-kind", "api-key", "--auth-key", "file:/keys/x", "--share", "content")

	out := e.run(nil, "features", "codex/work")
	if !strings.Contains(out, "telemetry") {
		t.Errorf("codex status should list telemetry:\n%s", out)
	}
	if strings.Contains(out, "automode-log") {
		t.Errorf("codex status must not list automode-log:\n%s", out)
	}
	// The per-harness default protocol shows (FR-004, AS3).
	if _, err := runFeaturesErr(e, "features", "codex/work", "--enable", "telemetry",
		"--traces", "on", "--logs", "on", "--endpoint", "https://otlp.example.com"); err != nil {
		t.Fatal(err)
	}
	out = e.run(nil, "features", "codex/work")
	if !strings.Contains(out, "otlp-http json") {
		t.Errorf("codex status should show the default protocol otlp-http json:\n%s", out)
	}
	// An unsupported flag fails naming the harness, changing nothing (FR-007).
	out, err := runFeaturesErr(e, "features", "codex/work", "--enable", "automode-log")
	if err == nil {
		t.Fatal("enabling automode-log on codex should fail")
	}
	if !strings.Contains(out, "codex") || !strings.Contains(out, "automode-log") {
		t.Errorf("error should name the harness and the flag:\n%s", out)
	}
	// Nothing changed by the failed mutation: the record still holds only the
	// telemetry feature enabled earlier in this test.
	data, _ := os.ReadFile(e.profileFile("codex", "work"))
	if strings.Contains(string(data), "automode-log") {
		t.Errorf("a failed mutation stored state:\n%s", data)
	}
}

// runFeaturesErr runs a command expecting failure and returns its output and error.
func runFeaturesErr(e *env, args ...string) (string, error) {
	e.t.Helper()
	return e.tryRun(nil, args...)
}

// Quickstart Scenario 4 (User Story 4): Codex telemetry in config.toml, with
// hand-written key preservation.
func TestFeaturesCodexTelemetryEndToEnd(t *testing.T) {
	e := newEnv(t)
	e.run(nil, "add", "codex", "work", "--auth-kind", "api-key", "--auth-key", "file:/keys/x", "--share", "content")

	e.run(nil, "features", "codex/work", "--enable", "telemetry",
		"--traces", "on", "--logs", "on", "--endpoint", "https://otlp.example.com")
	e.run(nil, "sync")
	cfgPath := filepath.Join(e.configDir("codex", "work"), "config.toml")
	m := readTOML(t, cfgPath)
	otel, ok := m["otel"].(map[string]any)
	if !ok {
		t.Fatalf("no [otel] section: %v", m)
	}
	exp, ok := otel["exporter"].(map[string]any)
	if !ok || exp["otlp-http"] == nil {
		t.Fatalf("exporter not in the inline-table encoding: %v", otel["exporter"])
	}
	inner := exp["otlp-http"].(map[string]any)
	if inner["endpoint"] != "https://otlp.example.com" || inner["protocol"] != "json" {
		t.Errorf("exporter fields = %v", inner)
	}
	if _, ok := otel["trace_exporter"]; !ok {
		t.Error("trace_exporter missing")
	}
	entries := featureRecordEntries(t, e.featureRecordPath("codex", "work"))
	locations := map[string]bool{}
	for _, en := range entries {
		locations[en["location"].(string)] = true
	}
	if !locations["otel.exporter"] || !locations["otel.trace_exporter"] {
		t.Errorf("nested TOML locations not recorded: %v", entries)
	}

	// Hand-write a sibling key, disable, re-sync: only aprofile's keys go.
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	// Add a hand-written key to the existing [otel] section (a second [otel]
	// header would be invalid TOML once subtables exist).
	patched := strings.Replace(string(data), "[otel]\n", "[otel]\nenvironment = \"prod\"\n", 1)
	if err := os.WriteFile(cfgPath, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
	e.run(nil, "features", "codex/work", "--disable", "telemetry")
	e.run(nil, "sync")
	m = readTOML(t, cfgPath)
	otel, ok = m["otel"].(map[string]any)
	if !ok || otel["environment"] != "prod" {
		t.Fatalf("hand-written [otel] key lost: %v", m["otel"])
	}
	if _, has := otel["exporter"]; has {
		t.Error("aprofile-placed exporter still present after disable")
	}
	if _, has := otel["trace_exporter"]; has {
		t.Error("aprofile-placed trace_exporter still present after disable")
	}
}

// Quickstart Scenario 5 (User Story 5): features respect isolation and copy.
func TestFeaturesIsolationAndCopy(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login", "--share", "content")
	e.run(nil, "features", "claude/work", "--enable", "telemetry",
		"--traces", "on", "--logs", "on", "--endpoint", "https://otlp.example.com")
	e.run(nil, "sync")

	// Mutation refused at share level all with the hint; bare status still works.
	e.run(nil, "add", "claude", "tmp", "--auth-kind", "oauth-login", "--share", "all")
	if out, err := e.tryRun(nil, "features", "claude/tmp", "--enable", "automode-log"); err == nil {
		t.Fatalf("mutation at all should be refused:\n%s", out)
	} else if !strings.Contains(out, "--share content") {
		t.Errorf("refusal should hint at --share content:\n%s", out)
	}
	if out, err := e.tryRun(nil, "features", "claude/tmp"); err != nil {
		t.Fatalf("bare status at all must work: %v\n%s", err, out)
	}

	// The copy carries the feature state.
	e.run(nil, "copy", "claude/work", "claude/work2")
	out := e.run(nil, "features", "claude/work2")
	if !strings.Contains(out, "telemetry") || !strings.Contains(out, "on") {
		t.Errorf("copy did not carry the feature state:\n%s", out)
	}
	data, _ := os.ReadFile(e.profileFile("claude", "work2"))
	if !strings.Contains(string(data), "https://otlp.example.com") {
		t.Errorf("copy lost the endpoint param:\n%s", data)
	}

	// edit --share all warns, clears the feature state and reports it (FR-014).
	out = e.run(nil, "edit", "claude/work2", "--share", "all", "--yes")
	if !strings.Contains(out, "features disabled") || !strings.Contains(out, "telemetry") {
		t.Errorf("edit --share all should report the cleared features:\n%s", out)
	}
	data, _ = os.ReadFile(e.profileFile("claude", "work2"))
	if strings.Contains(string(data), "features:") {
		t.Errorf("feature state not cleared from the record:\n%s", data)
	}
	// The next sync removes the orphaned placed entries: settings.json is now a
	// link to the default setup, and the feature's entries went with the
	// profile's own file.
	e.run(nil, "sync")
	if raw := readFile(t, e.featureSettingsPath("claude", "work2")); strings.Contains(raw, "OTEL_TRACES_EXPORTER") {
		t.Error("orphaned feature entry still placed after the clearing sync")
	}

	// Cross-harness copy: telemetry carries, automode-log is named as left out.
	e.run(nil, "features", "claude/work", "--enable", "automode-log")
	out = e.run(nil, "copy", "claude/work", "codex/ported")
	if !strings.Contains(out, "features carried: telemetry") {
		t.Errorf("cross-harness copy should carry telemetry:\n%s", out)
	}
	if !strings.Contains(out, "automode-log (harness codex does not support it)") {
		t.Errorf("cross-harness copy should name the dropped feature:\n%s", out)
	}
	if !strings.Contains(out, "protocol") {
		t.Errorf("cross-harness copy should report the dropped protocol param:\n%s", out)
	}
	data, _ = os.ReadFile(e.profileFile("codex", "ported"))
	if strings.Contains(string(data), "automode-log") {
		t.Errorf("unsupported feature carried to codex:\n%s", data)
	}
	if strings.Contains(string(data), "http/protobuf") {
		t.Errorf("invalid protocol value carried to codex:\n%s", data)
	}
}
