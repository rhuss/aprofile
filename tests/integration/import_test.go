package integration

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ccFixture builds a fixture cc-deck layout under the hermetic root and returns the
// cc-deck source directory. It mirrors the layout the importer package documents as
// the assumed cc-deck schema (a verification item against the real cc-deck repo).
func ccFixture(t *testing.T, root string) string {
	t.Helper()
	source := filepath.Join(root, "ccdeck")
	live := filepath.Join(source, "live", "claude", "work")
	// Metadata file.
	writeFixtureFile(t, filepath.Join(source, "profiles", "claude", "work.yaml"),
		"harness: claude\n"+
			"name: work\n"+
			"label: Work\n"+
			"color: blue\n"+
			"icon: W\n"+
			"primary: true\n"+
			"auth_kind: auth-token\n"+
			"auth_refs:\n"+
			"  ANTHROPIC_AUTH_TOKEN: env:WORK_TOKEN\n"+
			"config_dir: "+live+"\n")
	// Live config dir: credentials, a mixed file, an owned dir, and state.
	writeFixtureFile(t, filepath.Join(live, ".credentials.json"), `{"token":"SRC_CRED"}`+"\n")
	writeFixtureFile(t, filepath.Join(live, ".claude.json"),
		`{"oauthAccount":{"id":"SRC"},"mcpServers":{"srv":{"command":"x"}},"projects":{"/x":{}},"numStartups":3}`+"\n")
	writeFixtureFile(t, filepath.Join(live, "skills", "skill1.md"), "SRC_SKILL\n")
	writeFixtureFile(t, filepath.Join(live, "projects", "p1", "session"), "STATE\n")
	return source
}

func writeFixtureFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// snapshotTree returns a relpath->content map of every file under root.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		rel, _ := filepath.Rel(root, path)
		out[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return out
}

func assertTreeUnchanged(t *testing.T, label string, before, after map[string]string) {
	t.Helper()
	if len(before) != len(after) {
		t.Errorf("%s: file count changed %d -> %d", label, len(before), len(after))
	}
	for k, v := range before {
		av, ok := after[k]
		if !ok {
			t.Errorf("%s: file %q removed", label, k)
			continue
		}
		if av != v {
			t.Errorf("%s: file %q changed", label, k)
		}
	}
	for k := range after {
		if _, ok := before[k]; !ok {
			t.Errorf("%s: file %q added", label, k)
		}
	}
}

func (e *env) profileConfigDir(harness, name string) string {
	return filepath.Join(e.root, ".local", "share", "aprofile", "profiles", harness, name)
}

func (e *env) profileStorePath(harness, name string) string {
	return filepath.Join(e.root, ".config", "aprofile", "profiles", harness, name+".yaml")
}

func TestImportDryRunChangesNothing(t *testing.T) {
	e := newEnv(t)
	source := ccFixture(t, e.root)

	aprofileConfig := filepath.Join(e.root, ".config", "aprofile")
	aprofileData := filepath.Join(e.root, ".local", "share", "aprofile")
	_ = os.MkdirAll(aprofileConfig, 0o755)
	_ = os.MkdirAll(aprofileData, 0o755)

	ccBefore := snapshotTree(t, source)
	cfgBefore := snapshotTree(t, aprofileConfig)
	dataBefore := snapshotTree(t, aprofileData)

	out := e.run(nil, "import", "cc-deck", "--source", source, "--dry-run")
	if !strings.Contains(out, "claude/work") {
		t.Errorf("dry-run should list the profile it would create:\n%s", out)
	}

	assertTreeUnchanged(t, "cc-deck", ccBefore, snapshotTree(t, source))
	assertTreeUnchanged(t, "aprofile config", cfgBefore, snapshotTree(t, aprofileConfig))
	assertTreeUnchanged(t, "aprofile data", dataBefore, snapshotTree(t, aprofileData))
}

func TestImportCreatesProfilesAndPreservesCCDeck(t *testing.T) {
	e := newEnv(t)
	source := ccFixture(t, e.root)
	ccBefore := snapshotTree(t, source)

	e.run(nil, "import", "cc-deck", "--source", source)

	// Profile store file exists.
	if _, err := os.Stat(e.profileStorePath("claude", "work")); err != nil {
		t.Fatalf("profile store not created: %v", err)
	}
	cfg := e.profileConfigDir("claude", "work")

	// Credentials copied verbatim.
	if got := readFile(t, filepath.Join(cfg, ".credentials.json")); !strings.Contains(got, "SRC_CRED") {
		t.Errorf("credentials not copied: %q", got)
	}
	// Mixed file carries credential + owned keys, not state.
	mixed := readFile(t, filepath.Join(cfg, ".claude.json"))
	if !strings.Contains(mixed, "oauthAccount") || !strings.Contains(mixed, "mcpServers") {
		t.Errorf("mixed file missing credential/owned keys: %s", mixed)
	}
	if strings.Contains(mixed, "numStartups") || strings.Contains(mixed, `"/x"`) {
		t.Errorf("mixed file should not carry state keys: %s", mixed)
	}
	// Skills are content since feature 002 (FR-008): they are never imported; sync
	// links them from the default setup according to the share level.
	if _, err := os.Stat(filepath.Join(cfg, "skills", "skill1.md")); !os.IsNotExist(err) {
		t.Errorf("skills are content and must not be imported (err=%v)", err)
	}
	// State NOT carried (fresh state layer).
	if _, err := os.Stat(filepath.Join(cfg, "projects")); !os.IsNotExist(err) {
		t.Errorf("state should be fresh/empty, found projects dir (err=%v)", err)
	}

	// No cc-deck file was modified.
	assertTreeUnchanged(t, "cc-deck", ccBefore, snapshotTree(t, source))
}

func TestImportSkipsExistingAndExitsNonZero(t *testing.T) {
	e := newEnv(t)
	source := ccFixture(t, e.root)

	e.run(nil, "import", "cc-deck", "--source", source) // first import creates claude/work

	out, err := e.tryRun(nil, "import", "cc-deck", "--source", source)
	if err == nil {
		t.Fatalf("re-import of an existing profile must exit non-zero:\n%s", out)
	}
	if !strings.Contains(out, "claude/work") || !strings.Contains(strings.ToLower(out), "skip") {
		t.Errorf("re-import should report the skipped profile:\n%s", out)
	}
}

func TestImportOverwriteKeepsCredentials(t *testing.T) {
	e := newEnv(t)
	source := ccFixture(t, e.root)
	e.run(nil, "import", "cc-deck", "--source", source)

	cfg := e.profileConfigDir("claude", "work")
	// Simulate a local credential the user does not want replaced.
	writeFixtureFile(t, filepath.Join(cfg, ".credentials.json"), `{"token":"LOCAL_CRED"}`+"\n")

	out, err := e.tryRun(nil, "import", "cc-deck", "--source", source, "--overwrite")
	if err != nil {
		t.Fatalf("--overwrite should succeed: %v\n%s", err, out)
	}
	if got := readFile(t, filepath.Join(cfg, ".credentials.json")); !strings.Contains(got, "LOCAL_CRED") {
		t.Errorf("--overwrite must keep existing credentials, got %q", got)
	}
}

func TestImportOverwriteCredentialsBacksUpAndReplaces(t *testing.T) {
	e := newEnv(t)
	source := ccFixture(t, e.root)
	e.run(nil, "import", "cc-deck", "--source", source)

	cfg := e.profileConfigDir("claude", "work")
	writeFixtureFile(t, filepath.Join(cfg, ".credentials.json"), `{"token":"LOCAL_CRED"}`+"\n")
	// Local change to the mixed file's credential key.
	writeFixtureFile(t, filepath.Join(cfg, ".claude.json"),
		`{"oauthAccount":{"id":"LOCAL_ACCT"},"mcpServers":{"old":{}},"projects":{"/y":{}}}`+"\n")

	out, err := e.tryRun(nil, "import", "cc-deck", "--source", source, "--overwrite-credentials")
	if err != nil {
		t.Fatalf("--overwrite-credentials should succeed: %v\n%s", err, out)
	}

	// Credentials replaced with the imported ones.
	if got := readFile(t, filepath.Join(cfg, ".credentials.json")); !strings.Contains(got, "SRC_CRED") {
		t.Errorf("--overwrite-credentials must replace credentials, got %q", got)
	}
	// The mixed file's credential key replaced; state key preserved.
	mixed := readFile(t, filepath.Join(cfg, ".claude.json"))
	if !strings.Contains(mixed, `"SRC"`) {
		t.Errorf("mixed credential key not replaced: %s", mixed)
	}
	if !strings.Contains(mixed, `"/y"`) {
		t.Errorf("mixed state key should be preserved: %s", mixed)
	}

	// Backups exist and hold the old content.
	backupsRoot := filepath.Join(e.root, ".local", "share", "aprofile", "backups", "claude", "work")
	found := findBackupWith(t, backupsRoot, ".credentials.json", "LOCAL_CRED")
	if !found {
		t.Errorf("credential backup with old content not found under %s", backupsRoot)
	}
	if !findBackupWith(t, backupsRoot, ".claude.json", "LOCAL_ACCT") {
		t.Errorf("mixed-file backup with old content not found under %s", backupsRoot)
	}
}

func TestImportReportsWrapperCollision(t *testing.T) {
	e := newEnv(t)
	source := ccFixture(t, e.root)

	// A cc-deck wrapper with the same name lives elsewhere on PATH.
	collisionDir := filepath.Join(e.root, "ccwrappers")
	writeExecFile(t, filepath.Join(collisionDir, "claude-work"), "#!/bin/sh\necho old-cc-deck-wrapper\n")
	for i, v := range e.vars {
		if strings.HasPrefix(v, "PATH=") {
			e.vars[i] = "PATH=" + collisionDir + string(os.PathListSeparator) + strings.TrimPrefix(v, "PATH=")
		}
	}

	out := e.run(nil, "import", "cc-deck", "--source", source)
	if !strings.Contains(out, "claude-work") {
		t.Errorf("import should report the wrapper-name collision on PATH:\n%s", out)
	}
}

// findBackupWith reports whether any <backupsRoot>/<ts>/<name> file contains want.
func findBackupWith(t *testing.T, backupsRoot, name, want string) bool {
	t.Helper()
	entries, err := os.ReadDir(backupsRoot)
	if err != nil {
		return false
	}
	var timestamps []string
	for _, e := range entries {
		if e.IsDir() {
			timestamps = append(timestamps, e.Name())
		}
	}
	sort.Strings(timestamps)
	for _, ts := range timestamps {
		data, err := os.ReadFile(filepath.Join(backupsRoot, ts, name))
		if err == nil && strings.Contains(string(data), want) {
			return true
		}
	}
	return false
}
