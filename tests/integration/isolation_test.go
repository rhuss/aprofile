package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestScenario3Isolation covers quickstart Scenario 3 (US2, SC-002): two profiles of
// one harness each keep real, per-profile state and credentials, a shared entry is a
// link into the default setup visible in both, and nothing of one profile is
// reachable through the other or the default setup.
func TestScenario3Isolation(t *testing.T) {
	e := newEnv(t)

	// A default setup with a content file (CLAUDE.md is classified as content).
	defaultSetup := filepath.Join(e.root, ".claude")
	writeFile(t, filepath.Join(defaultSetup, "CLAUDE.md"), "v1\n")

	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-token", "--auth-key", "env:WORK_TOKEN")
	e.run(nil, "add", "claude", "private", "--auth-kind", "oauth-token", "--auth-key", "env:PRIV_TOKEN")
	e.run(nil, "sync")

	work := e.configDir("claude", "work")
	priv := e.configDir("claude", "private")

	// State entries are real per-profile directories, never links (FR-015).
	for _, dir := range []string{work, priv} {
		fi, err := os.Lstat(filepath.Join(dir, "projects"))
		if err != nil {
			t.Fatalf("projects/ not created in %s: %v", dir, err)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			t.Errorf("projects/ in %s must be a real dir, not a symlink (SC-002)", dir)
		}
	}

	// The shared entry is a symlink into the default setup, visible in both profiles.
	for _, dir := range []string{work, priv} {
		li, err := os.Lstat(filepath.Join(dir, "CLAUDE.md"))
		if err != nil {
			t.Fatalf("shared CLAUDE.md not linked in %s: %v", dir, err)
		}
		if li.Mode()&os.ModeSymlink == 0 {
			t.Errorf("CLAUDE.md in %s must be a symlink to the default setup (FR-017)", dir)
		}
		if got := readFile(t, filepath.Join(dir, "CLAUDE.md")); got != "v1\n" {
			t.Errorf("shared CLAUDE.md in %s = %q, want v1", dir, got)
		}
	}

	// Simulate harness sessions writing distinct state + credentials into each profile.
	writeFile(t, filepath.Join(work, ".credentials.json"), `{"token":"work-cred"}`)
	writeFile(t, filepath.Join(priv, ".credentials.json"), `{"token":"priv-cred"}`)
	writeFile(t, filepath.Join(work, "projects", "session-work.json"), `{"id":"w"}`)
	writeFile(t, filepath.Join(priv, "projects", "session-private.json"), `{"id":"p"}`)

	// Credentials differ and are isolated (FR-014).
	if readFile(t, filepath.Join(work, ".credentials.json")) == readFile(t, filepath.Join(priv, ".credentials.json")) {
		t.Error("credentials must differ between profiles")
	}

	// 0 cross-profile reachability: one profile's session is not visible through the
	// other profile's config dir nor the default setup (SC-002).
	if _, err := os.Stat(filepath.Join(work, "projects", "session-private.json")); !os.IsNotExist(err) {
		t.Error("private session reachable through work's config dir")
	}
	if _, err := os.Stat(filepath.Join(priv, "projects", "session-work.json")); !os.IsNotExist(err) {
		t.Error("work session reachable through private's config dir")
	}
	if _, err := os.Stat(filepath.Join(defaultSetup, "projects", "session-work.json")); !os.IsNotExist(err) {
		t.Error("work session reachable through the default setup")
	}

	// Editing the shared entry in the default setup is visible in both without a sync.
	writeFile(t, filepath.Join(defaultSetup, "CLAUDE.md"), "v2\n")
	for _, dir := range []string{work, priv} {
		if got := readFile(t, filepath.Join(dir, "CLAUDE.md")); got != "v2\n" {
			t.Errorf("edited shared CLAUDE.md not visible in %s: %q", dir, got)
		}
	}
}

// TestDoctorAndFixLinks covers the US2 doctor isolation checks and sync --fix-links:
// a leaking state link is detected by doctor, replaced with an empty real entry by
// sync --fix-links, and the link target is never deleted (FR-018, edge cases).
func TestDoctorAndFixLinks(t *testing.T) {
	e := newEnv(t)
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-token", "--auth-key", "env:WORK_TOKEN")
	e.run(nil, "sync")

	work := e.configDir("claude", "work")
	defaultProjects := filepath.Join(e.root, ".claude", "projects")
	leaked := filepath.Join(defaultProjects, "leaked.json")
	writeFile(t, leaked, `{"from":"default"}`)

	// Introduce a leak: replace the real projects/ dir with a symlink to the default.
	if err := os.RemoveAll(filepath.Join(work, "projects")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(defaultProjects, filepath.Join(work, "projects")); err != nil {
		t.Fatal(err)
	}

	// doctor reports the leak and exits non-zero (JSON stays clean on stdout). The
	// profile's env reference must resolve so the unresolved-reference check (FR-044)
	// does not also fire and mask the clean-after-repair assertion below.
	out, _, err := e.tryRunSplit([]string{"WORK_TOKEN=x"}, "doctor", "--json")
	if err == nil {
		t.Fatalf("doctor should exit non-zero when it finds a leak\n%s", out)
	}
	var report struct {
		FormatVersion int `json:"format_version"`
		Findings      []struct {
			Profile string `json:"profile"`
			Message string `json:"message"`
			Fix     string `json:"fix"`
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("doctor --json not valid JSON: %v\n%s", err, out)
	}
	found := false
	for _, f := range report.Findings {
		if f.Profile == "claude/work" && strings.Contains(f.Message, "projects") {
			found = true
			if !strings.Contains(f.Fix, "fix-links") {
				t.Errorf("leak finding should suggest sync --fix-links, got %q", f.Fix)
			}
		}
	}
	if !found {
		t.Errorf("doctor did not report the projects leak: %+v", report.Findings)
	}

	// sync --fix-links replaces the leaking link with an empty real dir.
	e.run(nil, "sync", "--fix-links")

	fi, err := os.Lstat(filepath.Join(work, "projects"))
	if err != nil {
		t.Fatalf("projects/ missing after fix-links: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Error("projects/ should be a real dir after --fix-links, not a symlink")
	}
	if _, err := os.Stat(filepath.Join(work, "projects", "leaked.json")); !os.IsNotExist(err) {
		t.Error("the repaired projects/ dir must be empty (not the leaked content)")
	}
	// The link target is never deleted (Constitution II).
	if _, err := os.Stat(leaked); err != nil {
		t.Errorf("fix-links must not delete the link target: %v", err)
	}

	// doctor is now clean and exits zero.
	cleanOut, _, cleanErr := e.tryRunSplit([]string{"WORK_TOKEN=x"}, "doctor", "--json")
	if cleanErr != nil {
		t.Errorf("doctor should exit zero after repair: %v\n%s", cleanErr, cleanOut)
	}
	if strings.Contains(cleanOut, "projects") {
		t.Errorf("doctor still reports a projects leak after repair:\n%s", cleanOut)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
