package launch_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/launch"
	"github.com/cc-deck/aprofile/internal/profile"
)

// Removing private modes would expose the saved Authorization header.
func TestRecoveryRecordIsPrivateAndRepairsUnchangedSync(t *testing.T) {
	e := newApplyEnv(t)
	settings := filepath.Join(e.cfg, "config.toml")
	const original = "[otel.exporter.otlp-http]\nendpoint = \"https://old.example\"\n[otel.exporter.otlp-http.headers]\nAuthorization = \"Bearer fake-recovery-canary\"\n"
	if err := os.WriteFile(settings, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	p := &profile.Profile{Harness: "codex", Name: "work", AuthKind: "chatgpt-login", Share: profile.ShareContent, Features: []profile.FeatureState{{Feature: "telemetry"}}}
	d := builtin(t, "codex")
	if _, err := launch.Apply(p, d, e.cfg, e.rec, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(e.rec)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "fake-recovery-canary") {
		t.Fatal("recovery record lost original Authorization header")
	}
	paths := map[string]os.FileMode{e.rec: 0o600, filepath.Dir(e.rec): 0o700, filepath.Dir(filepath.Dir(e.rec)): 0o700}
	for path, mode := range paths {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != mode {
			t.Errorf("%s mode = %o, want %o", path, fi.Mode().Perm(), mode)
		}
		if err := os.Chmod(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	before, err := os.Stat(e.rec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := launch.Apply(p, d, e.cfg, e.rec, true); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(e.rec); fi.Mode().Perm() != 0o755 {
		t.Fatal("dry run repaired permissions")
	}
	if _, err := launch.Apply(p, d, e.cfg, e.rec, false); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(e.rec)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Error("permission repair rewrote unchanged record")
	}
	for path, mode := range paths {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != mode {
			t.Errorf("repaired %s mode = %o, want %o", path, fi.Mode().Perm(), mode)
		}
	}
	p.Features = nil
	if _, err := launch.Apply(p, d, e.cfg, e.rec, false); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(restored), "Bearer fake-recovery-canary") {
		t.Fatal("disable lost original Authorization header")
	}
}

// Invalid paths must fail even for in-memory callers that never use Load.
func TestPlanRejectsUnsafeRecoveryPaths(t *testing.T) {
	for _, path := range []string{"../../escape.json", "/tmp/escape.json", "", "nested/../escape.json", "./settings.json", "nested//settings.json"} {
		t.Run(path, func(t *testing.T) {
			rec := launch.Record{Entries: []launch.Entry{{File: path, Location: "env.AUTOMODE_DECISION_LOG", Replaced: map[string]string{"": `"original"`}}}}
			res, err := launch.Plan(loginProfile(profile.ShareContent), builtin(t, "claude"), launch.Doc{}, rec)
			if err == nil {
				t.Errorf("unsafe record path %q accepted, planned %d writes", path, len(res.Writes()))
			}
		})
	}
}

func TestApplyRejectsUnsafeRecordBeforeSettingsWrites(t *testing.T) {
	e := newApplyEnv(t)
	outside := filepath.Join(filepath.Dir(e.cfg), "escape.json")
	const sentinel = `{"keep":"user's existing settings"}`
	if err := os.WriteFile(outside, []byte(sentinel), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(e.rec), 0o700); err != nil {
		t.Fatal(err)
	}
	rec := launch.Record{FormatVersion: 1, Entries: []launch.Entry{{File: "../escape.json", Location: "env.AUTOMODE_DECISION_LOG", Replaced: map[string]string{"": `"original"`}}}}
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.rec, data, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, dryRun := range []bool{true, false} {
		if _, err := launch.Apply(loginProfile(profile.ShareContent), builtin(t, "claude"), e.cfg, e.rec, dryRun); err == nil {
			t.Errorf("dryRun=%v accepted unsafe record", dryRun)
		}
	}
	got, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != sentinel {
		t.Fatalf("outside file overwritten: %s", got)
	}
	got, err = os.ReadFile(e.rec)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Error("invalid record changed")
	}
}
