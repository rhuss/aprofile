package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestScenario2CallerOverrideAndIdempotentSync covers quickstart Scenario 2
// (SC-003) as amended by spec 004 (FR-013, US4): a caller's exported
// ANTHROPIC_MODEL or ANTHROPIC_BASE_URL never reaches the harness (the folder
// carries the profile's values; a wrapper-only harness gets the profile's value),
// a one-run change goes through --model, and a second sync with no changes reports
// 0 changes and writes 0 files.
func TestScenario2CallerOverrideAndIdempotentSync(t *testing.T) {
	e := newEnv(t)
	e.run(nil, "add", "claude", "work", "--model", "X", "--auth-kind", "oauth-token", "--auth-key", "env:WORK_TOKEN")
	e.run(nil, "sync")

	out, err := e.runWrapper("claude-work", []string{"WORK_TOKEN=tok", "ANTHROPIC_MODEL=other", "ANTHROPIC_BASE_URL=http://other"}, "--model", "one-run")
	if err != nil {
		t.Fatalf("wrapper failed: %v\n%s", err, out)
	}
	if strings.Contains(out, "STUB_ENV: ANTHROPIC_MODEL=") || strings.Contains(out, "STUB_ENV: ANTHROPIC_BASE_URL=") {
		t.Errorf("a caller's model or endpoint reached the harness:\n%s", out)
	}
	if !strings.Contains(out, "STUB_ARGS: [--model one-run]") {
		t.Errorf("--model not passed through:\n%s", out)
	}

	// A profile whose wrapper carries the settings (share level all: settings.json
	// is linked) gets the profile's values, never the caller's.
	e.run(nil, "add", "claude", "shared", "--share", "all", "--model", "X", "--endpoint", "http://profile", "--auth-kind", "oauth-login")
	e.run(nil, "sync")
	out, err = e.runWrapper("claude-shared", []string{"ANTHROPIC_MODEL=other", "ANTHROPIC_BASE_URL=http://other"})
	if err != nil {
		t.Fatalf("wrapper failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "STUB_ENV: ANTHROPIC_MODEL=X") || !strings.Contains(out, "STUB_ENV: ANTHROPIC_BASE_URL=http://profile") {
		t.Errorf("wrapper-carried settings must be the profile's:\n%s", out)
	}

	// Record mtimes of all files under the aprofile data dir.
	dataDir := filepath.Join(e.root, ".local", "share", "aprofile")
	before := snapshotMtimes(t, dataDir)

	// Second sync: must report 0 changes.
	out2 := e.run(nil, "sync")
	if !strings.Contains(out2, "0 changes") && !strings.Contains(out2, "already up to date") {
		t.Errorf("second sync should report no changes:\n%s", out2)
	}

	after := snapshotMtimes(t, dataDir)
	for path, mt := range before {
		if after[path] != mt {
			t.Errorf("file %s was rewritten by an unchanged sync (SC-003)", path)
		}
	}
	if len(before) != len(after) {
		t.Errorf("file set changed across an unchanged sync: before=%d after=%d", len(before), len(after))
	}
}

func snapshotMtimes(t *testing.T, root string) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		out[path] = info.ModTime().UnixNano()
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}
