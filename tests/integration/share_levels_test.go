package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/testutil"
)

// TestUS2DefaultLevelNamedOnAdd covers US2 scenario 1 and FR-007: without --share a
// profile gets content (or settings.yaml default_share), and add names the level and
// its description.
func TestUS2DefaultLevelNamedOnAdd(t *testing.T) {
	e := newEnv(t)
	out := e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login")
	if !strings.Contains(out, "share level: content") || !strings.Contains(out, "Uses your instructions and skills") {
		t.Errorf("add should name the level and its description:\n%s", out)
	}
	if got := readFile(t, filepath.Join(e.root, ".config", "aprofile", "profiles", "claude", "work.yaml")); !strings.Contains(got, "share: content") {
		t.Errorf("profile file should store the level:\n%s", got)
	}

	writeFile(t, filepath.Join(e.root, ".config", "aprofile", "settings.yaml"), "default_share: none\n")
	out = e.run(nil, "add", "claude", "ci", "--auth-kind", "oauth-login")
	if !strings.Contains(out, "share level: none") {
		t.Errorf("settings.yaml default_share should apply:\n%s", out)
	}
	out = e.run(nil, "add", "claude", "lab", "--auth-kind", "oauth-login", "--share", "all")
	if !strings.Contains(out, "share level: all") || !strings.Contains(out, "logins, history and memory stay private") {
		t.Errorf("--share all should be named with its description:\n%s", out)
	}
	if out, err := e.tryRun(nil, "add", "claude", "bad", "--auth-kind", "oauth-login", "--share", "most"); err == nil {
		t.Errorf("an unknown level must be rejected:\n%s", out)
	}
	help := e.run(nil, "add", "--help")
	for _, d := range []string{"Uses your whole setup live", "Uses your instructions and skills", "Shares nothing with your setup"} {
		if !strings.Contains(strings.Join(strings.Fields(help), " "), d) {
			t.Errorf("add --help should carry the level description %q", d)
		}
	}
}

// TestUS2HelpSharing covers US2 scenario 3 and FR-013.
func TestUS2HelpSharing(t *testing.T) {
	e := newEnv(t)
	out := e.run(nil, "help", "sharing")
	for _, want := range []string{
		"logins, history, sessions and memory are always private",
		"UC1", "UC7", "What each level does", "Plugin store", "Per-harness differences", "codex:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("help sharing missing %q:\n%s", want, out)
		}
	}
}

// TestUS2NoneProfile covers US2 scenario 4: a none profile has no content links,
// its own minimal settings and private state.
func TestUS2NoneProfile(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	e.run(nil, "add", "claude", "ci", "--auth-kind", "oauth-login", "--share", "none")
	e.run(nil, "sync")
	dir := e.configDir("claude", "ci")
	if l := links(t, dir); len(l) != 0 {
		t.Errorf("a none profile links nothing, found %v", l)
	}
	if got := strings.TrimSpace(readFile(t, filepath.Join(dir, "settings.json"))); got != "{}" {
		t.Errorf("settings at none start minimal, got %s", got)
	}
	if got := strings.TrimSpace(readFile(t, filepath.Join(dir, ".claude.json"))); got != "{}" {
		t.Errorf("nothing is seeded at none, not even MCP servers (FR-011): %s", got)
	}
	if fi, err := os.Stat(filepath.Join(dir, "projects")); err != nil || !fi.IsDir() {
		t.Error("state dirs are private and created")
	}
}

// TestUS2ContentSettingsCopy covers US2 scenario 5: a content profile's settings
// start as a copy of the default setup's, without its hooks.
func TestUS2ContentSettingsCopy(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login", "--share", "content")
	out := e.run(nil, "sync")
	if !strings.Contains(out, "hook-sets import") {
		t.Errorf("sync should point to hook-sets import for the default hooks left out:\n%s", out)
	}
	path := filepath.Join(e.configDir("claude", "work"), "settings.json")
	if fi, err := os.Lstat(path); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("settings.json must be the profile's own file (err=%v)", err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(readFile(t, path)), &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["permissions"]; !ok {
		t.Error("permissions should be copied")
	}
	if _, ok := m["statusLine"]; !ok {
		t.Error("statusLine should be copied")
	}
	if _, ok := m["hooks"]; ok {
		t.Error("the default setup's hooks must not be copied")
	}
	// Editing the profile's copy never reaches the default setup.
	writeFile(t, path, `{"permissions":{"allow":["Bash(work-only)"]}}`)
	if strings.Contains(readFile(t, filepath.Join(e.root, ".claude", "settings.json")), "work-only") {
		t.Error("a permission granted in the profile reached the default setup")
	}
}

// FR-022/level table: MCP servers start as a copy of the default setup's at all
// and content on every harness, including those that keep them in a plain owned
// file (Codex config.toml, pi agent/mcp.json); at none the file starts absent.
func TestOwnedMCPFileSeededAtAllAndContent(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "codex")
	testutil.SeedDefaultsAt(t, e.root, "pi")
	e.run(nil, "add", "codex", "cc", "--auth-kind", "chatgpt-login", "--share", "content")
	e.run(nil, "add", "codex", "cn", "--auth-kind", "chatgpt-login", "--share", "none")
	e.run(nil, "add", "pi", "pa", "--auth-kind", "login", "--share", "all")
	e.run(nil, "sync")

	cfg := filepath.Join(e.configDir("codex", "cc"), "config.toml")
	if got := readFile(t, cfg); got != readFile(t, filepath.Join(e.root, ".codex", "config.toml")) {
		t.Errorf("codex content profile: config.toml starts as the default setup's copy, got %q", got)
	}
	if fi, err := os.Lstat(cfg); err != nil || fi.Mode()&os.ModeSymlink != 0 || fi.Mode().Perm() != 0o600 {
		t.Errorf("the seeded config.toml is a private 0600 file: %v %v", fi, err)
	}
	if _, err := os.Lstat(filepath.Join(e.configDir("codex", "cn"), "config.toml")); !os.IsNotExist(err) {
		t.Errorf("codex none profile: config.toml starts absent: %v", err)
	}
	if _, err := os.Stat(filepath.Join(e.configDir("pi", "pa"), "agent", "mcp.json")); err != nil {
		t.Errorf("pi all profile: agent/mcp.json starts as a copy: %v", err)
	}
	help := e.run(nil, "help", "sharing")
	if !strings.Contains(help, "config.toml is private at every level, all included: it holds the MCP servers; at all and content it starts as a copy") {
		t.Errorf("help sharing states the seeding:\n%s", help)
	}
}
