package integration

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ccFixture builds a fixture cc-deck layout under the hermetic root, following
// cc-deck's real on-disk schema (cc-deck internal/config and internal/profile):
// <config>/cc-deck/config.yaml with profiles.<harness>.<name> and default_profile,
// cc-deck's credential copies under <config>/cc-deck/profiles/<harness>/<name>/,
// and the live harness config dir under <data>/cc-deck/profiles/<name>/<harness>/.
// Both cc-deck dirs sit where import looks by default. It returns the cc-deck
// config dir. All values are fake.
func ccFixture(t *testing.T, root string) string {
	t.Helper()
	source := filepath.Join(root, ".config", "cc-deck")
	writeFixtureFile(t, filepath.Join(source, "config.yaml"),
		"default_profile: claude/work\n"+
			"profiles:\n"+
			"  claude:\n"+
			"    work:\n"+
			"      auth:\n"+
			"        login: true\n"+
			"      color: blue\n"+
			"      icon: W\n"+
			"    vertex:\n"+
			"      backend: vertex\n"+
			"      project: fake-project\n"+
			"      region: us-east5\n"+
			"      auth:\n"+
			"        credentials:\n"+
			"          file: ~/gcloud/adc.json\n"+
			"      color: '#E27C5A'\n"+
			"  codex:\n"+
			"    private:\n"+
			"      auth:\n"+
			"        api_key:\n"+
			"          env: FAKE_OPENAI_KEY\n")
	writeFixtureFile(t, filepath.Join(source, "profiles", "claude", "vertex", "credentials"), `{"type":"FAKE_ADC"}`+"\n")
	live := filepath.Join(ccDataDir(root), "profiles", "work", "claude")
	// Live config dir: credentials, a mixed file, a content dir, and state.
	writeFixtureFile(t, filepath.Join(live, ".credentials.json"), `{"token":"SRC_CRED"}`+"\n")
	writeFixtureFile(t, filepath.Join(live, ".claude.json"),
		`{"oauthAccount":{"id":"SRC"},"mcpServers":{"srv":{"command":"x"}},"projects":{"/x":{}},"numStartups":3}`+"\n")
	writeFixtureFile(t, filepath.Join(live, "skills", "skill1.md"), "SRC_SKILL\n")
	writeFixtureFile(t, filepath.Join(live, "projects", "p1", "session"), "STATE\n")
	return source
}

// ccDataDir is the cc-deck data dir of the fixture (the import default).
func ccDataDir(root string) string { return filepath.Join(root, ".local", "share", "cc-deck") }

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
	ccDataBefore := snapshotTree(t, ccDataDir(e.root))
	cfgBefore := snapshotTree(t, aprofileConfig)
	dataBefore := snapshotTree(t, aprofileData)

	out := e.run(nil, "import", "cc-deck", "--source", source, "--dry-run")
	if !strings.Contains(out, "claude/work") {
		t.Errorf("dry-run should list the profile it would create:\n%s", out)
	}

	assertTreeUnchanged(t, "cc-deck", ccBefore, snapshotTree(t, source))
	assertTreeUnchanged(t, "cc-deck data", ccDataBefore, snapshotTree(t, ccDataDir(e.root)))
	assertTreeUnchanged(t, "aprofile config", cfgBefore, snapshotTree(t, aprofileConfig))
	assertTreeUnchanged(t, "aprofile data", dataBefore, snapshotTree(t, aprofileData))
}

func TestImportCreatesProfilesAndPreservesCCDeck(t *testing.T) {
	e := newEnv(t)
	source := ccFixture(t, e.root)
	ccBefore := snapshotTree(t, source)
	ccDataBefore := snapshotTree(t, ccDataDir(e.root))

	out := e.run(nil, "import", "cc-deck", "--source", source)
	if strings.Contains(out, "FAKE_ADC") || strings.Contains(out, "SRC_CRED") {
		t.Errorf("import output must not show credential contents:\n%s", out)
	}

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
	assertTreeUnchanged(t, "cc-deck data", ccDataBefore, snapshotTree(t, ccDataDir(e.root)))
}

// SC-010: the cc-deck profiles import with no manual edits; each store file carries
// the mapped auth, backend environment, appearance and primary flag.
func TestImportMapsCCDeckSchema(t *testing.T) {
	e := newEnv(t)
	ccFixture(t, e.root)
	writeFixtureFile(t, filepath.Join(e.root, "gcloud", "adc.json"), `{"type":"DECLARED"}`+"\n")

	// No --source/--source-data: both cc-deck dirs are found at their defaults.
	e.run(nil, "import", "cc-deck")

	work := readFile(t, e.profileStorePath("claude", "work"))
	for _, want := range []string{"auth_kind: oauth-login", "primary: true", "color: blue", "icon: W"} {
		if !strings.Contains(work, want) {
			t.Errorf("claude/work store missing %q:\n%s", want, work)
		}
	}
	vertex := readFile(t, e.profileStorePath("claude", "vertex"))
	credCopy := filepath.Join(e.root, ".local", "share", "aprofile", "credentials", "claude", "vertex", "credentials")
	for _, want := range []string{"CLAUDE_CODE_USE_VERTEX", "ANTHROPIC_VERTEX_PROJECT_ID", "fake-project", "CLOUD_ML_REGION", "us-east5", "GOOGLE_APPLICATION_CREDENTIALS", credCopy} {
		if !strings.Contains(vertex, want) {
			t.Errorf("claude/vertex store missing %q:\n%s", want, vertex)
		}
	}
	if strings.Contains(vertex, "FAKE_ADC") {
		t.Errorf("store file must not contain credential contents")
	}
	// cc-deck's own credential copy wins over the declared source file.
	if got := readFile(t, credCopy); !strings.Contains(got, "FAKE_ADC") {
		t.Errorf("credential copy = %q", got)
	}
	if fi, err := os.Stat(credCopy); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("credential copy mode: %v %v", fi, err)
	}
	codex := readFile(t, e.profileStorePath("codex", "private"))
	if !strings.Contains(codex, "auth_kind: api-key") || !strings.Contains(codex, "OPENAI_API_KEY: env:FAKE_OPENAI_KEY") {
		t.Errorf("codex/private store:\n%s", codex)
	}
}

// FR-001: import moves the primary flag from the harness's previous primary and
// reports it.
func TestImportMovesPrimaryAndReportsIt(t *testing.T) {
	e := newEnv(t)
	source := ccFixture(t, e.root)
	e.run(nil, "add", "claude", "old", "--auth-kind", "oauth-login", "--primary")

	out := e.run(nil, "import", "cc-deck", "--source", source, "--source-data", ccDataDir(e.root))
	if !strings.Contains(out, "moved primary flag from claude/old to claude/work") {
		t.Errorf("import should report the primary move:\n%s", out)
	}
	if strings.Contains(readFile(t, e.profileStorePath("claude", "old")), "primary: true") {
		t.Errorf("claude/old should no longer be primary")
	}

	jsonOut := e.run(nil, "import", "cc-deck", "--source", source, "--overwrite", "--json")
	if !strings.Contains(jsonOut, `"source_data_dir"`) {
		t.Errorf("--json should report the cc-deck data dir:\n%s", jsonOut)
	}
}

// FR-045: a profile that already exists is skipped and reported while the others
// are imported in the same run, which then exits non-zero.
func TestImportSkipsExistingAndExitsNonZero(t *testing.T) {
	e := newEnv(t)
	source := ccFixture(t, e.root)
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login")

	out, err := e.tryRun(nil, "import", "cc-deck", "--source", source)
	if err == nil {
		t.Fatalf("import with an existing profile must exit non-zero:\n%s", out)
	}
	if !strings.Contains(out, "skip     claude/work") {
		t.Errorf("import should report the skipped profile:\n%s", out)
	}
	for _, id := range [][2]string{{"claude", "vertex"}, {"codex", "private"}} {
		if _, err := os.Stat(e.profileStorePath(id[0], id[1])); err != nil {
			t.Errorf("%s/%s should still be imported in the same run: %v", id[0], id[1], err)
		}
	}
	if strings.Contains(readFile(t, e.profileStorePath("claude", "work")), "primary: true") {
		t.Errorf("the skipped existing profile must stay as it was")
	}
}

// FR-045: a profile that fails partway is reported as failed and rolled back; the
// others are imported and the command exits non-zero.
func TestImportFailedProfileReportedOthersImported(t *testing.T) {
	e := newEnv(t)
	source := ccFixture(t, e.root)
	live := filepath.Join(ccDataDir(e.root), "profiles", "work", "claude")
	writeFixtureFile(t, filepath.Join(live, ".claude.json"), "{not json\n")

	out, err := e.tryRun(nil, "import", "cc-deck", "--source", source)
	if err == nil {
		t.Fatalf("a failed profile must make import exit non-zero:\n%s", out)
	}
	if !strings.Contains(out, "failed   claude/work") || !strings.Contains(out, "1 profile(s) failed to import") {
		t.Errorf("import should report the failed profile:\n%s", out)
	}
	if _, err := os.Stat(e.profileStorePath("claude", "work")); !os.IsNotExist(err) {
		t.Errorf("the failed profile must leave no store file (err=%v)", err)
	}
	if _, err := os.Stat(e.profileConfigDir("claude", "work")); !os.IsNotExist(err) {
		t.Errorf("the failed profile must leave no config dir (err=%v)", err)
	}
	for _, id := range [][2]string{{"claude", "vertex"}, {"codex", "private"}} {
		if _, err := os.Stat(e.profileStorePath(id[0], id[1])); err != nil {
			t.Errorf("%s/%s should still be imported: %v", id[0], id[1], err)
		}
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
