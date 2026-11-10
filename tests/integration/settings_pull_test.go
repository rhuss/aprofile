package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/testutil"
)

// TestSettingsPull covers US3 scenarios 4 and 6 and FR-018.
func TestSettingsPull(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login", "--share", "content")
	e.run(nil, "sync")
	settings := filepath.Join(e.configDir("claude", "work"), "settings.json")
	writeFile(t, settings, `{"permissions":{"allow":["Bash(ls:*)","Bash(work-only)"]},"hooks":{"Stop":[{"matcher":"mine","hooks":[]}]}}`)

	// The difference is visible in show (FR-015).
	if out := e.run(nil, "show", "claude/work"); !strings.Contains(out, "differ from the default setup") {
		t.Errorf("show should say the settings differ:\n%s", out)
	}

	dry := e.run(nil, "settings", "pull", "claude/work", "--dry-run")
	for _, want := range []string{"changed: statusLine", "changed: enabledPlugins", `changed: permissions.allow["Read(~/notes/**)"]`} {
		if !strings.Contains(dry, want) {
			t.Errorf("dry run should list %q:\n%s", want, dry)
		}
	}
	if strings.Contains(dry, "removed:") {
		t.Errorf("a merge removes nothing:\n%s", dry)
	}
	if out, err := e.tryRun(nil, "settings", "pull", "claude/work"); exitCodeOf(err) != 3 {
		t.Fatalf("without --yes off a TTY: exit %d, want 3\n%s", exitCodeOf(err), out)
	}
	out := e.run(nil, "settings", "pull", "claude/work", "--yes")
	if !strings.Contains(out, "backed up to") {
		t.Errorf("pull backs up first:\n%s", out)
	}
	got := readFile(t, settings)
	if !strings.Contains(got, "Bash(work-only)") || !strings.Contains(got, "statusLine") {
		t.Errorf("merge keeps the profile-only permission and adds the default's keys:\n%s", got)
	}
	if !strings.Contains(got, `"mine"`) || strings.Contains(got, "rtk.sh") {
		t.Errorf("the profile's hooks are untouched and the default's are not pulled:\n%s", got)
	}

	// --replace lists the profile-only permission as removed.
	rep := e.run(nil, "settings", "pull", "claude/work", "--replace", "--dry-run")
	if !strings.Contains(rep, `removed: permissions.allow["Bash(work-only)"]`) {
		t.Errorf("replace dry run should list the profile-only permission as removed:\n%s", rep)
	}

	// all and none profiles are refused with exit 2.
	e.run(nil, "add", "claude", "lab", "--auth-kind", "oauth-login", "--share", "all")
	e.run(nil, "add", "claude", "ci", "--auth-kind", "oauth-login", "--share", "none")
	for _, ref := range []string{"claude/lab", "claude/ci"} {
		if out, err := e.tryRun(nil, "settings", "pull", ref, "--dry-run"); exitCodeOf(err) != 2 {
			t.Errorf("%s: exit %d, want 2\n%s", ref, exitCodeOf(err), out)
		}
	}
}

// Feature 004 FR-010: after settings pull (merge or --replace) the profile's
// folder-carried launch entries are present again.
func TestSettingsPullKeepsLaunchEntries(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login", "--share", "content", "--model", "gw-model")
	e.run(nil, "sync")
	settings := filepath.Join(e.configDir("claude", "work"), "settings.json")
	for _, args := range [][]string{
		{"settings", "pull", "claude/work", "--yes"},
		{"settings", "pull", "claude/work", "--replace", "--yes"},
	} {
		out := e.run(nil, args...)
		m := readSettings(t, settings)
		env, _ := m["env"].(map[string]any)
		if env["ANTHROPIC_MODEL"] != "gw-model" || env["APROFILE_SET_VARS"] != "ANTHROPIC_MODEL" {
			t.Errorf("%v: launch entries missing after the pull: %v\n%s", args, m["env"], out)
		}
		if strings.Contains(strings.Join(args, " "), "--replace") && !strings.Contains(out, "launch settings: settings.json: set env ANTHROPIC_MODEL") {
			t.Errorf("%v: the re-placed entries are not listed:\n%s", args, out)
		}
	}
	// The folder is in sync afterwards: the next sync changes nothing.
	if out := e.run(nil, "sync"); !strings.Contains(out, "0 changes") && !strings.Contains(out, "already up to date") {
		t.Errorf("sync after pull reports changes:\n%s", out)
	}
}

// FR-010 with a key command: when the default setup holds its own apiKeyHelper and
// endpoint, the profile's key command, endpoint and FR-006a neutralizing entries
// win again after the pull, and a merge pull keeps the user's own entries (SC-004).
func TestSettingsPullRestoresKeyCommandOverDefault(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	e.writeDefaultClaudeSettings(`{"apiKeyHelper":"echo sk-default","env":{"ANTHROPIC_BASE_URL":"http://default","USER_X":"1"}}`)
	key := e.keyFile("pull", "sk-pull-profile\n")
	e.run(nil, "add", "claude", "work", "--auth-kind", "api-key", "--auth-key", "file:"+key, "--share", "content",
		"--endpoint", "http://profile", "--model", "gw-model")
	e.run(nil, "sync")
	settings := filepath.Join(e.configDir("claude", "work"), "settings.json")
	m := readSettings(t, settings)
	envBlock(t, m)["MY_TRACE"] = "1"
	data, _ := json.Marshal(m)
	if err := os.WriteFile(settings, data, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"settings", "pull", "claude/work", "--yes"},
		{"settings", "pull", "claude/work", "--replace", "--yes"},
	} {
		out := e.run(nil, args...)
		m := readSettings(t, settings)
		env := envBlock(t, m)
		helper, _ := m["apiKeyHelper"].(string)
		if got := runHelper(t, helper); got != "sk-pull-profile" {
			t.Errorf("%v: key command prints %q:\n%s", args, got, out)
		}
		if env["ANTHROPIC_BASE_URL"] != "http://profile" || env["ANTHROPIC_MODEL"] != "gw-model" {
			t.Errorf("%v: endpoint/model = %v / %v", args, env["ANTHROPIC_BASE_URL"], env["ANTHROPIC_MODEL"])
		}
		if v, has := env["ANTHROPIC_API_KEY"]; !has || v != "" {
			t.Errorf("%v: ANTHROPIC_API_KEY neutralizer missing: %v", args, env)
		}
		if len(args) == 4 && env["MY_TRACE"] != "1" {
			t.Errorf("merge pull dropped the user's MY_TRACE: %v", env)
		}
	}
	if out := e.run(nil, "sync"); !strings.Contains(out, "0 changes") && !strings.Contains(out, "already up to date") {
		t.Errorf("sync after pull reports changes:\n%s", out)
	}
}
