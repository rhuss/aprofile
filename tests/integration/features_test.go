package integration

// Spec 006: the features command and its placement, end to end against the
// built binary (quickstart scenarios 1-6, T009/T015/T018/T021/T026/T030).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
