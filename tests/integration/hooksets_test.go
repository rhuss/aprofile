package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/testutil"
)

func profileHooks(t *testing.T, path string) map[string][]map[string]any {
	t.Helper()
	var doc struct {
		Hooks map[string][]map[string]any `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(readFile(t, path)), &doc); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return doc.Hooks
}

func groupMatchers(groups []map[string]any) string {
	var out []string
	for _, g := range groups {
		m, _ := g["matcher"].(string)
		out = append(out, m)
	}
	return strings.Join(out, ",")
}

// TestUS4HookSets follows quickstart section 4 and covers US4 scenarios 1 to 6,
// FR-024 to FR-031 and SC-008.
func TestUS4HookSets(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	testutil.SeedDefaultsAt(t, e.root, "opencode")
	defaultSettings := filepath.Join(e.root, ".claude", "settings.json")
	before := readFile(t, defaultSettings)

	// Scenario 6: import the default setup's hooks; the default setup is untouched.
	out := e.run(nil, "hook-sets", "import", "claude", "--name", "base")
	if !strings.Contains(out, "created hook set base") {
		t.Errorf("import output:\n%s", out)
	}
	if readFile(t, defaultSettings) != before {
		t.Error("hook-sets import must not modify the default setup")
	}
	if out, err := e.tryRun(nil, "hook-sets", "import", "claude", "--name", "base"); err == nil {
		t.Errorf("importing an existing set must fail:\n%s", out)
	}
	// A files-form import copies the plugin files into the library.
	e.run(nil, "hook-sets", "import", "opencode", "--name", "ocbase")
	if _, err := os.Stat(filepath.Join(e.root, ".config", "aprofile", "hooks.d", "files", "ocbase", "x.ts")); err != nil {
		t.Errorf("files-form import should copy x.ts into the library: %v", err)
	}

	// A second set, every profile, Claude only.
	writeFile(t, filepath.Join(e.root, ".config", "aprofile", "hooks.d", "rtk.yaml"),
		"name: rtk\nevery_profile: true\nbodies:\n  claude:\n    PreToolUse:\n      - matcher: Edit\n        hooks: [{type: command, command: rtk-edit}]\n")

	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login", "--share", "content")
	e.run(nil, "add", "claude", "ci", "--auth-kind", "oauth-login", "--share", "none")
	e.run(nil, "add", "claude", "lab", "--auth-kind", "oauth-login", "--share", "all")
	e.run(nil, "sync")
	work := filepath.Join(e.configDir("claude", "work"), "settings.json")
	ci := filepath.Join(e.configDir("claude", "ci"), "settings.json")

	// Scenario 5: the every-profile set applies to content, not to none.
	if got := groupMatchers(profileHooks(t, work)["PreToolUse"]); got != "Edit" {
		t.Errorf("work gets rtk (every profile): %q", got)
	}
	if got := profileHooks(t, ci); len(got) != 0 {
		t.Errorf("a none profile gets no every-profile set: %v", got)
	}
	show := e.run(nil, "show", "claude/work")
	if !strings.Contains(show, "rtk (applies to every profile)") {
		t.Errorf("show lists the every-profile reason:\n%s", show)
	}

	// A hand-added user group (pre-existing entries are kept).
	var doc map[string]any
	_ = json.Unmarshal([]byte(readFile(t, work)), &doc)
	hooks := doc["hooks"].(map[string]any)
	hooks["PreToolUse"] = append([]any{map[string]any{"matcher": "mine", "hooks": []any{}}}, hooks["PreToolUse"].([]any)...)
	b, _ := json.Marshal(doc)
	writeFile(t, work, string(b))

	// Scenario 1: attach base; user and managed entries side by side.
	e.run(nil, "edit", "claude/work", "--hook-sets", "base")
	e.run(nil, "sync")
	if got := groupMatchers(profileHooks(t, work)["PreToolUse"]); got != "mine,Bash,Edit" {
		t.Errorf("user groups first, then sets alphabetically (base, rtk): %q", got)
	}
	ciBefore := readFile(t, ci)

	// Scenario 2: detach base; only base's entries go.
	e.run(nil, "edit", "claude/work", "--hook-sets", "")
	e.run(nil, "sync")
	if got := groupMatchers(profileHooks(t, work)["PreToolUse"]); got != "mine,Edit" {
		t.Errorf("only base's managed group is removed: %q", got)
	}
	if readFile(t, ci) != ciBefore {
		t.Error("a hook-set change touches only that profile (SC-008)")
	}

	// Scenario 3: a set without a body for the harness fails without changes.
	e.run(nil, "add", "codex", "cx", "--auth-kind", "chatgpt-login", "--share", "content")
	out, err := e.tryRun(nil, "edit", "codex/cx", "--hook-sets", "base")
	if err == nil || !strings.Contains(out, "base") || !strings.Contains(out, "codex") {
		t.Errorf("attaching a set without a codex body must fail naming both:\n%s", out)
	}
	if strings.Contains(readFile(t, filepath.Join(e.root, ".config", "aprofile", "profiles", "codex", "cx.yaml")), "base") {
		t.Error("the failed attach must change nothing")
	}

	// Scenario 4: an all profile cannot attach hook sets (exit 2).
	out, err = e.tryRun(nil, "edit", "claude/lab", "--hook-sets", "base")
	if exitCodeOf(err) != 2 || !strings.Contains(out, "--share content") {
		t.Errorf("all profile: exit %d\n%s", exitCodeOf(err), out)
	}

	// A none profile gets only named sets.
	e.run(nil, "edit", "claude/ci", "--hook-sets", "rtk")
	e.run(nil, "sync")
	if got := groupMatchers(profileHooks(t, ci)["PreToolUse"]); got != "Edit" {
		t.Errorf("a none profile gets the sets it names: %q", got)
	}

	// Excluding the every-profile set from a content profile.
	e.run(nil, "edit", "claude/work", "--exclude-hook-sets", "rtk")
	e.run(nil, "sync")
	if got := groupMatchers(profileHooks(t, work)["PreToolUse"]); got != "mine" {
		t.Errorf("an excluded every-profile set is detached: %q", got)
	}

	// US5 scenario 3: a managed entry edited by hand is left alone and reported.
	e.run(nil, "edit", "claude/ci", "--hook-sets", "rtk")
	raw := readFile(t, ci)
	writeFile(t, ci, strings.Replace(raw, "rtk-edit", "rtk-edit --mine", 1))
	out = e.run(nil, "sync")
	if !strings.Contains(out, "diverged") {
		t.Errorf("sync reports the diverged entry:\n%s", out)
	}
	if !strings.Contains(readFile(t, ci), "rtk-edit --mine") {
		t.Error("the edited entry is never overwritten by sync")
	}
	rp := e.run(nil, "hook-sets", "reapply", "claude/ci", "--dry-run")
	if !strings.Contains(rp, "re-place diverged") {
		t.Errorf("reapply dry run:\n%s", rp)
	}
	e.run(nil, "hook-sets", "reapply", "claude/ci", "--yes")
	if strings.Contains(readFile(t, ci), "--mine") {
		t.Error("reapply replaces the diverged entry")
	}

	// Spec edge case: a deleted set referenced by a profile is reported and sync
	// exits non-zero; the managed entries stay.
	_ = os.Remove(filepath.Join(e.root, ".config", "aprofile", "hooks.d", "rtk.yaml"))
	out, err = e.tryRun(nil, "sync")
	if err == nil || !strings.Contains(out, `hook set "rtk" is referenced but missing`) {
		t.Errorf("a missing set must fail sync:\n%s", out)
	}
	if !strings.Contains(readFile(t, ci), "rtk-edit") {
		t.Error("the managed entries of a missing set stay unchanged")
	}

	list := e.run(nil, "hook-sets", "list")
	if !strings.Contains(list, "base") || !strings.Contains(list, "ocbase") {
		t.Errorf("hook-sets list:\n%s", list)
	}
}

// TestUS4FilesFormProfile covers FR-029: an OpenCode profile's hook directory holds
// links into the library, and the user's own plugin files stay.
func TestUS4FilesFormProfile(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "opencode")
	e.run(nil, "hook-sets", "import", "opencode", "--name", "ocbase")
	e.run(nil, "add", "opencode", "ww", "--auth-kind", "login", "--share", "none", "--hook-sets", "ocbase")
	e.run(nil, "sync")
	plugins := filepath.Join(e.configDir("opencode", "ww"), "config", "opencode", "plugins")
	target, err := os.Readlink(filepath.Join(plugins, "x.ts"))
	if err != nil || !strings.HasSuffix(target, filepath.Join("hooks.d", "files", "ocbase", "x.ts")) {
		t.Fatalf("x.ts should link into the library: %q %v", target, err)
	}
	writeFile(t, filepath.Join(plugins, "mine.ts"), "mine")
	e.run(nil, "edit", "opencode/ww", "--hook-sets", "")
	e.run(nil, "sync")
	if _, err := os.Lstat(filepath.Join(plugins, "x.ts")); !os.IsNotExist(err) {
		t.Error("detaching removes the managed link")
	}
	if readFile(t, filepath.Join(plugins, "mine.ts")) != "mine" {
		t.Error("the user's own plugin file stays")
	}
}

// FR-017a/FR-016: a level change applies its hook-set part itself, and the dry run
// lists it.
func TestLevelChangePlacesHookSets(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	writeFile(t, filepath.Join(e.root, ".config", "aprofile", "hooks.d", "rtk.yaml"),
		"name: rtk\nevery_profile: true\nbodies:\n  claude:\n    PreToolUse:\n      - matcher: Edit\n        hooks: [{type: command, command: rtk-edit}]\n")
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login", "--share", "content")
	e.run(nil, "sync")
	settings := filepath.Join(e.configDir("claude", "work"), "settings.json")
	if got := groupMatchers(profileHooks(t, settings)["PreToolUse"]); got != "Edit" {
		t.Fatalf("rtk placed at content: %q", got)
	}
	dry := e.run(nil, "edit", "claude/work", "--share", "none", "--dry-run")
	if !strings.Contains(dry, "remove-hook settings.json#hooks (removed managed hook entries of: rtk") {
		t.Errorf("the dry run lists the hook-set removal:\n%s", dry)
	}
	applied := e.run(nil, "edit", "claude/work", "--share", "none")
	if strings.Join(planLines(dry), "\n") != strings.Join(planLines(applied), "\n") {
		t.Errorf("applied lines differ from the dry run:\n%s\n---\n%s", dry, applied)
	}
	if got := profileHooks(t, settings)["PreToolUse"]; len(got) != 0 {
		t.Errorf("the level change removes the every-profile set itself, without a sync: %v", got)
	}
	e.run(nil, "edit", "claude/work", "--share", "content")
	if got := groupMatchers(profileHooks(t, settings)["PreToolUse"]); got != "Edit" {
		t.Errorf("moving back to content places rtk again: %q", got)
	}
}

// FR-016/SC-006: when the profile already holds a group identical to a set's, the
// level change places nothing, and the dry run must not claim it would.
func TestLevelChangeDryRunSkipsIdenticalUserGroup(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	writeFile(t, filepath.Join(e.root, ".config", "aprofile", "hooks.d", "rtk.yaml"),
		"name: rtk\nevery_profile: true\nbodies:\n  claude:\n    PreToolUse:\n      - matcher: Edit\n        hooks: [{type: command, command: rtk-edit}]\n")
	e.run(nil, "add", "claude", "solo", "--auth-kind", "oauth-login", "--share", "none")
	e.run(nil, "sync")
	settings := filepath.Join(e.configDir("claude", "solo"), "settings.json")
	writeFile(t, settings, `{"hooks":{"PreToolUse":[{"matcher":"Edit","hooks":[{"type":"command","command":"rtk-edit"}]}]}}`)

	dry := e.run(nil, "edit", "claude/solo", "--share", "content", "--dry-run")
	if strings.Contains(dry, "place-hook") {
		t.Errorf("the user's identical group is theirs; nothing is placed, so the dry run must not list place-hook:\n%s", dry)
	}
	applied := e.run(nil, "edit", "claude/solo", "--share", "content", "--yes")
	if strings.Join(planLines(dry), "\n") != strings.Join(planLines(applied), "\n") {
		t.Errorf("applied lines differ from the dry run:\n%s\n---\n%s", dry, applied)
	}
	if strings.Contains(applied, "differ from the listed plan") {
		t.Errorf("the applied hook steps must match the preview:\n%s", applied)
	}
}

// FR-028/SC-008: the managed-hooks record moves with a renamed profile, so sync
// still removes the entries it placed; a purged profile leaves no record behind.
func TestHookRecordFollowsRenameAndPurge(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	writeFile(t, filepath.Join(e.root, ".config", "aprofile", "hooks.d", "rtk.yaml"),
		"name: rtk\nevery_profile: true\nbodies:\n  claude:\n    PreToolUse:\n      - matcher: Edit\n        hooks: [{type: command, command: rtk-edit}]\n")
	e.run(nil, "add", "claude", "old", "--auth-kind", "oauth-login", "--share", "content")
	e.run(nil, "sync")
	records := filepath.Join(e.root, ".local", "share", "aprofile", "managed-hooks", "claude")
	if _, err := os.Stat(filepath.Join(records, "old.json")); err != nil {
		t.Fatalf("sync writes a record: %v", err)
	}

	e.run(nil, "rename", "claude/old", "new")
	if _, err := os.Stat(filepath.Join(records, "old.json")); !os.IsNotExist(err) {
		t.Error("rename must not leave the old record behind")
	}
	e.run(nil, "edit", "claude/new", "--exclude-hook-sets", "rtk")
	e.run(nil, "sync")
	settings := filepath.Join(e.configDir("claude", "new"), "settings.json")
	if got := profileHooks(t, settings)["PreToolUse"]; len(got) != 0 {
		t.Errorf("after rename, sync removes the entries it placed: %v", got)
	}

	e.run(nil, "remove", "claude", "new", "--purge", "--yes")
	if _, err := os.Stat(filepath.Join(records, "new.json")); !os.IsNotExist(err) {
		t.Error("remove --purge must delete the profile's record")
	}
}
