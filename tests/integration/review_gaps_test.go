package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/testutil"
)

func backupsOf(t *testing.T, e *env, h, n, file string) []string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(e.root, ".local", "share", "aprofile", "backups", h, n, "*", file))
	return m
}

// FR-007a: copy keeps the source's level by default, starts the target's settings
// as a copy of the source's (hooks excluded), honors --share, and carries hook sets
// only where the target harness has a body.
func TestCopyAcrossLevels(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	writeFile(t, filepath.Join(e.root, ".config", "aprofile", "hooks.d", "jira.yaml"),
		"name: jira\nbodies:\n  claude:\n    Stop: [{matcher: x, hooks: []}]\n")
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login", "--share", "content", "--hook-sets", "jira")
	e.run(nil, "sync")
	src := filepath.Join(e.configDir("claude", "work"), "settings.json")
	writeFile(t, src, `{"permissions":{"allow":["Bash(work-only)"]},"hooks":{"Stop":[{"matcher":"mine","hooks":[]}]}}`)

	out, _, err := e.tryRunSplit(nil, "copy", "claude/work", "claude/twin", "--json")
	if err != nil {
		t.Fatalf("copy: %v\n%s", err, out)
	}
	var rep struct {
		Share           string   `json:"share"`
		SettingsCarried []string `json:"settings_carried"`
		HookSetsCarried []string `json:"hook_sets_carried"`
	}
	_ = json.Unmarshal([]byte(out), &rep)
	if rep.Share != "content" || len(rep.HookSetsCarried) != 1 {
		t.Errorf("copy report: %+v", rep)
	}
	twin := readFile(t, filepath.Join(e.configDir("claude", "twin"), "settings.json"))
	if !strings.Contains(twin, "work-only") || strings.Contains(twin, `"hooks"`) {
		t.Errorf("target settings start as the source's, without hooks:\n%s", twin)
	}
	if !strings.Contains(readFile(t, filepath.Join(e.root, ".config", "aprofile", "profiles", "claude", "twin.yaml")), "jira") {
		t.Error("the named hook set travels with the copy")
	}

	e.run(nil, "copy", "claude/work", "claude/bare", "--share", "none")
	if l := links(t, e.configDir("claude", "bare")); len(l) != 0 {
		t.Errorf("a none copy links nothing: %v", l)
	}

	out, _, err = e.tryRunSplit(nil, "copy", "claude/work", "codex/cx", "--json")
	if err != nil {
		t.Fatalf("cross-harness copy: %v\n%s", err, out)
	}
	if !strings.Contains(out, "jira (named; no body for codex)") {
		t.Errorf("a set without a codex body is reported as left out:\n%s", out)
	}
	if strings.Contains(readFile(t, filepath.Join(e.root, ".config", "aprofile", "profiles", "codex", "cx.yaml")), "jira") {
		t.Error("a set without a body is not carried")
	}

	// A level change on an existing target goes through edit --share.
	out2, err := e.tryRun(nil, "copy", "claude/work", "claude/bare", "--overwrite")
	if exitCodeOf(err) != 2 || !strings.Contains(out2, "aprofile edit claude/bare --share content") {
		t.Errorf("copy --overwrite across levels must refuse (exit %d):\n%s", exitCodeOf(err), out2)
	}
}

// FR-017a: a level changed by hand in the profile file: sync performs only the
// additive part and names the command that completes it; that command works.
func TestHandEditedLevel(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login", "--share", "content")
	e.run(nil, "sync")
	settings := filepath.Join(e.configDir("claude", "work"), "settings.json")
	writeFile(t, settings, `{"own":true}`)
	store := filepath.Join(e.root, ".config", "aprofile", "profiles", "claude", "work.yaml")
	writeFile(t, store, strings.Replace(readFile(t, store), "share: content", "share: all", 1))

	out := e.run(nil, "sync")
	if !strings.Contains(out, "aprofile edit claude/work --share all") {
		t.Errorf("sync names the completing command:\n%s", out)
	}
	// The settings file is still the profile's own until edit --share completes the
	// level change, so sync restores its placed neutralizing entries there (spec 005
	// FR-001, the same linked-file rule as every launch entry) but never replaces
	// the file.
	if fi, _ := os.Lstat(settings); fi.Mode()&os.ModeSymlink != 0 {
		t.Error("sync must not replace profile data with a link")
	}
	if m := readSettings(t, settings); m["own"] != true || len(m) != 2 {
		t.Errorf("sync must not replace profile data: %v", m)
	} else if delete(m, "own"); !onlyNeutralizing(t, m) {
		t.Errorf("sync added more than the neutralizing entries: %v", m)
	}
	if fi, _ := os.Lstat(filepath.Join(e.configDir("claude", "work"), "CLAUDE.md")); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("content links stay")
	}
	e.run(nil, "edit", "claude/work", "--share", "all", "--yes")
	if fi, _ := os.Lstat(settings); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("edit --share with the current level completes the replace")
	}
	if b := backupsOf(t, e, "claude", "work", "settings.json"); len(b) != 1 || readSettings(t, b[0])["own"] != true {
		t.Errorf("the profile's settings are backed up first: %v", b)
	}
}

// FR-018/SC-006/SC-007: settings pull applies exactly what its dry run listed,
// --replace applies an exact copy without the hooks, a backup holds the previous
// content, and other harnesses' settings work too.
func TestSettingsPullApplies(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	testutil.SeedDefaultsAt(t, e.root, "pi")
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login", "--share", "content")
	e.run(nil, "add", "pi", "pp", "--auth-kind", "login", "--share", "content")
	e.run(nil, "sync")
	settings := filepath.Join(e.configDir("claude", "work"), "settings.json")
	before := `{"permissions":{"allow":["Bash(work-only)"]},"hooks":{"Stop":[{"matcher":"mine","hooks":[]}]}}`
	writeFile(t, settings, before)

	e.run(nil, "settings", "pull", "claude/work", "--replace", "--yes")
	var m map[string]any
	_ = json.Unmarshal([]byte(readFile(t, settings)), &m)
	var d map[string]any
	_ = json.Unmarshal([]byte(testutil.ClaudeSettingsJSON), &d)
	for k := range d {
		if k == "hooks" {
			continue
		}
		if _, ok := m[k]; !ok {
			t.Errorf("--replace copies %s", k)
		}
	}
	if strings.Contains(readFile(t, settings), "work-only") || !strings.Contains(readFile(t, settings), `"mine"`) {
		t.Errorf("--replace drops profile-only keys and keeps the profile's hooks:\n%s", readFile(t, settings))
	}
	if b := backupsOf(t, e, "claude", "work", "settings.json"); len(b) != 1 || readFile(t, b[0]) != before {
		t.Errorf("the backup holds the previous content: %v", b)
	}
	// The pull places the neutralizing entries again right after the copy.
	if env, _ := m["env"].(map[string]any); env == nil || env["CLAUDE_CODE_USE_VERTEX"] != "" {
		t.Errorf("the neutralizing entries are not placed again after the pull: %v", m["env"])
	}
	// Nothing left to pull but the env block of the neutralizing entries aprofile
	// placed (spec 005 FR-001), which the pull names and places again.
	out := e.run(nil, "settings", "pull", "claude/work", "--replace", "--dry-run")
	if !strings.Contains(out, "removed: env") || strings.Contains(out, "changed:") ||
		!strings.Contains(out, "launch settings aprofile placed are placed again after the pull") {
		t.Errorf("a second pull lists only the placed env block:\n%s", out)
	}

	pi := filepath.Join(e.configDir("pi", "pp"), "agent", "settings.json")
	writeFile(t, pi, `{"theme":"light","mine":1}`)
	dry := e.run(nil, "settings", "pull", "pi/pp", "theme", "--dry-run")
	if !strings.Contains(dry, "changed: theme") || strings.Contains(dry, "lastChangelogVersion") {
		t.Errorf("a named-key pull lists only that key:\n%s", dry)
	}
	e.run(nil, "settings", "pull", "pi/pp", "theme", "--yes")
	if got := readFile(t, pi); !strings.Contains(got, `"dark"`) || !strings.Contains(got, `"mine"`) || strings.Contains(got, "lastChangelogVersion") {
		t.Errorf("pi pull applies exactly the named key:\n%s", got)
	}
}

// SC-007 for hook-sets reapply, and the missing-script edge case for sync.
func TestReapplyNeedsConfirmationAndMissingScript(t *testing.T) {
	e := newEnv(t)
	script := filepath.Join(e.root, "no-such-dir", "hook.sh")
	writeFile(t, filepath.Join(e.root, ".config", "aprofile", "hooks.d", "base.yaml"),
		"name: base\nbodies:\n  claude:\n    Stop:\n      - matcher: m\n        hooks: [{type: command, command: "+script+"}]\n")
	e.run(nil, "add", "claude", "ci", "--auth-kind", "oauth-login", "--share", "none", "--hook-sets", "base")
	out := e.run(nil, "sync")
	if !strings.Contains(out, script) || !strings.Contains(out, "does not exist") {
		t.Errorf("sync reports the missing script:\n%s", out)
	}
	settings := filepath.Join(e.configDir("claude", "ci"), "settings.json")
	if !strings.Contains(readFile(t, settings), "hook.sh") {
		t.Error("the managed entry is still written")
	}
	writeFile(t, settings, strings.Replace(readFile(t, settings), "hook.sh", "hook.sh --mine", 1))
	edited := readFile(t, settings)
	if out, err := e.tryRun(nil, "hook-sets", "reapply", "claude/ci"); exitCodeOf(err) != 3 {
		t.Fatalf("reapply off a TTY without --yes: exit %d\n%s", exitCodeOf(err), out)
	}
	if readFile(t, settings) != edited {
		t.Error("nothing changes without confirmation")
	}
	e.run(nil, "hook-sets", "reapply", "claude/ci", "--yes")
	if b := backupsOf(t, e, "claude", "ci", "settings.json"); len(b) != 1 || !strings.Contains(readFile(t, b[0]), "--mine") {
		t.Errorf("the edited file is backed up first: %v", b)
	}
}

// FR-007 for import, FR-022 for harness show, FR-020 exit status.
func TestImportShareHarnessShowDoctorExit(t *testing.T) {
	e := newEnv(t)
	source := ccFixture(t, e.root)
	e.run(nil, "import", "cc-deck", "--source", source, "--share", "none")
	if !strings.Contains(readFile(t, e.profileStorePath("claude", "work")), "share: none") {
		t.Error("import --share none stores the level")
	}
	codex := e.run(nil, "harness", "show", "codex")
	if !strings.Contains(codex, "config.toml is private at every level") || !strings.Contains(codex, "~/.agents/skills") {
		t.Errorf("harness show codex states what all shares and the $HOME escape:\n%s", codex)
	}
	if !strings.Contains(e.run(nil, "harness", "show", "opencode"), "~/.claude/skills") {
		t.Error("harness show opencode lists its $HOME escapes")
	}

	e2 := newEnv(t)
	testutil.SeedDefaultsAt(t, e2.root, "opencode")
	e2.run(nil, "add", "opencode", "oc", "--auth-kind", "login", "--share", "none")
	e2.run(nil, "sync")
	rep, _ := doctorJSON(t, e2)
	for _, f := range rep.Findings {
		if f.Profile == "opencode/oc" && f.Severity == "problem" && strings.Contains(f.Message, "skills") {
			t.Errorf("$HOME escapes are information, not problems: %+v", f)
		}
	}
}

// Re-review fix: settings pull --replace copies a non-JSON settings file whole
// (OpenCode tui.jsonc) instead of writing it empty.
func TestSettingsPullReplacesNonJSONFile(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "opencode")
	e.run(nil, "add", "opencode", "oc", "--auth-kind", "login", "--share", "content")
	e.run(nil, "sync")
	tui := filepath.Join(e.configDir("opencode", "oc"), "config", "opencode", "tui.jsonc")
	writeFile(t, tui, `{"theme":"light"}`)
	e.run(nil, "settings", "pull", "opencode/oc", "--replace", "--yes")
	if got := readFile(t, tui); got != `{"theme":"dark"}` {
		t.Errorf("tui.jsonc should be the default setup's copy, got %q", got)
	}
}

// FR-028: reapply confirms and backs up even when no entry diverged (a set whose
// body changed is re-placed only after a confirmation, with a backup first).
func TestReapplyAlwaysConfirms(t *testing.T) {
	e := newEnv(t)
	set := filepath.Join(e.root, ".config", "aprofile", "hooks.d", "base.yaml")
	writeFile(t, set, "name: base\nbodies:\n  claude:\n    Stop:\n      - matcher: m\n        hooks: [{type: command, command: /bin/true}]\n")
	e.run(nil, "add", "claude", "ci", "--auth-kind", "oauth-login", "--share", "none", "--hook-sets", "base")
	e.run(nil, "sync")
	settings := filepath.Join(e.configDir("claude", "ci"), "settings.json")
	before := readFile(t, settings)
	writeFile(t, set, "name: base\nbodies:\n  claude:\n    Stop:\n      - matcher: m\n        hooks: [{type: command, command: /usr/bin/true}]\n")
	if out, err := e.tryRun(nil, "hook-sets", "reapply", "claude/ci"); exitCodeOf(err) != 3 {
		t.Fatalf("reapply off a TTY without --yes: exit %d\n%s", exitCodeOf(err), out)
	}
	if readFile(t, settings) != before {
		t.Error("nothing changes without confirmation")
	}
	e.run(nil, "hook-sets", "reapply", "claude/ci", "--yes")
	if !strings.Contains(readFile(t, settings), "/usr/bin/true") {
		t.Errorf("the changed set is re-placed:\n%s", readFile(t, settings))
	}
	if b := backupsOf(t, e, "claude", "ci", "settings.json"); len(b) != 1 || readFile(t, b[0]) != before {
		t.Errorf("the file is backed up before it is rewritten: %v", b)
	}
}
