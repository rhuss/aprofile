package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/testutil"
)

// Phase 11 copy fixes: T067 (target load errors, leftover config dir, early auth
// validation), T069 (wrapper name clash), T072 (complete reports), T075 (--auth-key)
// and T078 (skills present/missing across harnesses).

// copyReport11 is the part of the copy --json report these tests read.
type copyReport11 struct {
	Mode       string   `json:"mode"`
	MCPCarried []string `json:"mcp_carried"`
	MCP        struct {
		Converted []string `json:"converted"`
	} `json:"mcp"`
	SecretWarnings []struct {
		Server string `json:"server"`
		Field  string `json:"field"`
	} `json:"secret_warnings"`
	OwnedSecretWarnings []string `json:"owned_secret_warnings"`
	OwnedNotConverted   []string `json:"owned_not_converted"`
	PluginsSkipped      []string `json:"plugins_skipped"`
	SkippedPlugins      []struct {
		Name       string   `json:"name"`
		Skills     []string `json:"skills"`
		MCPServers []string `json:"mcp_servers"`
	} `json:"skipped_plugins"`
	SkillsInTarget    []string `json:"skills_in_target"`
	SkillsMissing     []string `json:"skills_missing_in_target"`
	SkillsHomeEscapes []string `json:"skills_home_escapes"`
}

func copyJSON(t *testing.T, e *env, args ...string) copyReport11 {
	t.Helper()
	stdout, stderr, err := e.tryRunSplit(nil, append([]string{"copy"}, append(args, "--json")...)...)
	if err != nil {
		t.Fatalf("copy %v failed: %v\n%s%s", args, err, stdout, stderr)
	}
	var rep copyReport11
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		t.Fatalf("copy --json not valid JSON: %v\n%s", err, stdout)
	}
	return rep
}

// T067: only "not found" means the target is absent; a target that fails to load is
// never overwritten.
func TestCopyTargetLoadErrorFails(t *testing.T) {
	e := newEnv(t)
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login")
	store := filepath.Join(e.root, ".config", "aprofile", "profiles", "claude", "broken.yaml")
	writeFile(t, store, "harness: claude\nname: broken\nshare: [not, a, level\n")

	for _, extra := range [][]string{nil, {"--overwrite"}} {
		args := append([]string{"copy", "claude/work", "claude/broken"}, extra...)
		out, err := e.tryRun(nil, args...)
		if err == nil {
			t.Fatalf("copy onto a target that fails to load must fail (%v):\n%s", extra, out)
		}
		if !strings.Contains(out, "claude/broken") {
			t.Errorf("error must name the target:\n%s", out)
		}
	}
	if got := readFile(t, store); !strings.Contains(got, "[not, a, level") {
		t.Errorf("the unloadable target file was rewritten:\n%s", got)
	}
}

// T067 / FR-004: a leftover config dir without a profile needs --adopt.
func TestCopyLeftoverConfigDirNeedsAdopt(t *testing.T) {
	e := newEnv(t)
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login")
	leftover := e.configDir("claude", "old")
	writeFile(t, filepath.Join(leftover, ".credentials.json"), `{"kept":true}`)

	out, err := e.tryRun(nil, "copy", "claude/work", "claude/old")
	if err == nil || !strings.Contains(out, leftover) || !strings.Contains(out, "--adopt") {
		t.Fatalf("copy onto a leftover config dir must fail naming it and --adopt (err %v):\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(e.root, ".config", "aprofile", "profiles", "claude", "old.yaml")); !os.IsNotExist(err) {
		t.Error("a refused copy must not create the target profile")
	}

	e.run(nil, "copy", "claude/work", "claude/old", "--adopt")
	if got := readFile(t, filepath.Join(leftover, ".credentials.json")); got != `{"kept":true}` {
		t.Errorf("--adopt must keep the leftover credentials, got %q", got)
	}
}

// T067: --auth is validated before anything is written for the target.
func TestCopyInvalidAuthWritesNothing(t *testing.T) {
	e := newEnv(t)
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login")
	out, err := e.tryRun(nil, "copy", "claude/work", "codex/cx", "--auth", "bogus")
	if err == nil || !strings.Contains(out, "bogus") {
		t.Fatalf("an unknown --auth must fail naming it (err %v):\n%s", err, out)
	}
	if _, err := os.Stat(e.configDir("codex", "cx")); !os.IsNotExist(err) {
		t.Error("a refused copy must not create the target config dir")
	}
}

// T075: a target auth kind that needs a reference requires --auth-key.
func TestCopyAuthKeyRequired(t *testing.T) {
	e := newEnv(t)
	e.run(nil, "add", "codex", "src")

	// claude's default kind (auth-token) carries its credential in a variable.
	out, err := e.tryRun(nil, "copy", "codex/src", "claude/dst")
	if err == nil || !strings.Contains(out, "--auth-key") {
		t.Fatalf("copy to a kind needing a reference must fail naming --auth-key (err %v):\n%s", err, out)
	}
	if _, err := os.Stat(e.configDir("claude", "dst")); !os.IsNotExist(err) {
		t.Error("a refused copy must not create the target config dir")
	}

	// A literal secret is rejected as on add.
	if out, err := e.tryRun(nil, "copy", "codex/src", "claude/dst", "--auth-key", "sk-ant-LITERALSECRETVALUE00000000000"); err == nil {
		t.Fatalf("a literal --auth-key must be rejected:\n%s", out)
	}

	e.run(nil, "copy", "codex/src", "claude/dst", "--auth-key", "env:CLAUDE_TOK")
	store := readFile(t, filepath.Join(e.root, ".config", "aprofile", "profiles", "claude", "dst.yaml"))
	if !strings.Contains(store, "auth-token") || !strings.Contains(store, "env:CLAUDE_TOK") {
		t.Errorf("target must get the default kind with the supplied reference:\n%s", store)
	}

	// An interactive kind needs no key.
	e.run(nil, "copy", "codex/src", "claude/login", "--auth", "oauth-login")

	// Changing the kind of an existing target clears its refs: a new key is required.
	out, err = e.tryRun(nil, "copy", "codex/src", "claude/dst", "--overwrite", "--auth", "api-key")
	if err == nil || !strings.Contains(out, "--auth-key") {
		t.Errorf("--auth api-key without --auth-key must fail (err %v):\n%s", err, out)
	}
	e.run(nil, "copy", "codex/src", "claude/dst", "--overwrite", "--yes", "--auth", "api-key", "--auth-key", "env:KEY2")

	// A key for an interactive kind is an error.
	if out, err := e.tryRun(nil, "copy", "claude/dst", "codex/cy", "--auth-key", "env:X"); err == nil {
		t.Errorf("--auth-key for an interactive kind must fail:\n%s", out)
	}
}

// T069 / FR-002: a wrapper name another profile already has is rejected, naming it.
func TestCopyWrapperNameClash(t *testing.T) {
	e := newEnv(t)
	writeFile(t, filepath.Join(e.root, ".config", "aprofile", "harnesses.d", "claudex.yaml"),
		"name: claudex\nbinary: claudex\nwrapper_prefix: claude-x\nconfig_env:\n  CLAUDEX_DIR: \".\"\n")
	e.run(nil, "add", "claudex", "work")
	e.run(nil, "add", "claude", "src", "--auth-kind", "oauth-login")

	out, err := e.tryRun(nil, "copy", "claude/src", "claude/x-work")
	if err == nil || !strings.Contains(out, "claude-x-work") || !strings.Contains(out, "claudex/work") {
		t.Fatalf("a wrapper name clash must fail naming the wrapper and the profile (err %v):\n%s", err, out)
	}
	if _, err := os.Stat(e.configDir("claude", "x-work")); !os.IsNotExist(err) {
		t.Error("a refused copy must not create the target config dir")
	}
}

const codexOwnedTOML = `model = "o3"

[model_providers.corp]
name = "Corp"
base_url = "https://llm.corp.example.com/v1/openai/compat/endpoint/path"
experimental_bearer_token = "sk-proj-OWNEDSECRET000000000000000000"

[mcp_servers.tool]
command = "mcp-tool"
env = { API_KEY = "sk-ant-MCPSECRET0000000000000000000000" }
`

// T072: a same-harness copy warns about every secret-looking owned value, not only
// MCP ones, and labels the servers as carried.
func TestCopySameHarnessOwnedSecretsAndCarried(t *testing.T) {
	e := newEnv(t)
	e.run(nil, "add", "codex", "src")
	e.run(nil, "sync")
	writeFile(t, filepath.Join(e.configDir("codex", "src"), "config.toml"), codexOwnedTOML)

	rep := copyJSON(t, e, "codex/src", "codex/twin")
	if !contains(rep.OwnedSecretWarnings, "config.toml: model_providers.corp.experimental_bearer_token") {
		t.Errorf("missing owned-file secret warning: %v", rep.OwnedSecretWarnings)
	}
	for _, w := range rep.OwnedSecretWarnings {
		if strings.Contains(w, "base_url") || strings.Contains(w, "mcp_servers") {
			t.Errorf("unexpected owned warning %q (URLs are not secrets; MCP is reported per server)", w)
		}
	}
	if !hasWarning(rep.SecretWarnings, "tool", "env[API_KEY]") {
		t.Errorf("missing MCP secret warning: %+v", rep.SecretWarnings)
	}
	if !contains(rep.MCPCarried, "tool") || len(rep.MCP.Converted) != 0 {
		t.Errorf("same-harness servers are carried, not converted: carried=%v converted=%v", rep.MCPCarried, rep.MCP.Converted)
	}

	out := e.run(nil, "copy", "codex/src", "codex/twin2")
	if !strings.Contains(out, "carried") || strings.Contains(out, "experimental_bearer_token = ") {
		t.Errorf("text report must say carried and never print values:\n%s", out)
	}
	if !strings.Contains(out, "model_providers.corp.experimental_bearer_token") {
		t.Errorf("text report must warn about the owned secret:\n%s", out)
	}
}

// T072: a cross-harness copy reports the owned keys it does not convert.
func TestCopyCrossReportsUnconvertedOwned(t *testing.T) {
	e := newEnv(t)
	e.run(nil, "add", "codex", "src")
	e.run(nil, "sync")
	writeFile(t, filepath.Join(e.configDir("codex", "src"), "config.toml"), codexOwnedTOML)

	rep := copyJSON(t, e, "codex/src", "claude/dst", "--auth", "oauth-login")
	for _, want := range []string{"config.toml#model", "config.toml#model_providers"} {
		if !contains(rep.OwnedNotConverted, want) {
			t.Errorf("owned_not_converted missing %q: %v", want, rep.OwnedNotConverted)
		}
	}
	if contains(rep.OwnedNotConverted, "config.toml#mcp_servers") {
		t.Errorf("MCP servers are converted, not reported as unconverted: %v", rep.OwnedNotConverted)
	}
	if !contains(rep.MCP.Converted, "tool") {
		t.Errorf("tool should be converted: %v", rep.MCP.Converted)
	}
}

// T072 / FR-036: skipped plugins are listed by name with their skills and MCP servers.
func TestCopyListsSkippedPlugins(t *testing.T) {
	e := newEnv(t)
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login", "--share", "none")
	e.run(nil, "sync")
	dir := e.configDir("claude", "work")
	install := filepath.Join(dir, "plugins", "cache", "market", "superpowers", "1.0.0")
	writeFile(t, filepath.Join(install, "skills", "brainstorming", "SKILL.md"), "# b\n")
	writeFile(t, filepath.Join(install, "skills", "tdd", "SKILL.md"), "# t\n")
	writeFile(t, filepath.Join(install, ".mcp.json"), `{"mcpServers":{"helper":{"command":"h"}}}`)
	rec, _ := json.Marshal(map[string]any{"version": 2, "plugins": map[string]any{
		"superpowers@market": []any{map[string]any{"installPath": install}},
	}})
	writeFile(t, filepath.Join(dir, "plugins", "installed_plugins.json"), string(rec))

	rep := copyJSON(t, e, "claude/work", "codex/cx")
	if len(rep.SkippedPlugins) != 1 {
		t.Fatalf("want one skipped plugin, got %+v", rep.SkippedPlugins)
	}
	p := rep.SkippedPlugins[0]
	if p.Name != "superpowers@market" || strings.Join(p.Skills, ",") != "brainstorming,tdd" || strings.Join(p.MCPServers, ",") != "helper" {
		t.Errorf("skipped plugin = %+v", p)
	}
	if len(rep.PluginsSkipped) != 1 || !strings.Contains(rep.PluginsSkipped[0], "superpowers@market") ||
		!strings.Contains(rep.PluginsSkipped[0], "brainstorming") || !strings.Contains(rep.PluginsSkipped[0], "helper") {
		t.Errorf("plugins_skipped text = %v", rep.PluginsSkipped)
	}
}

// T078 / FR-021, FR-036: a cross-harness target whose skills are linked or read from
// $HOME gets a report of which source skills it already sees and which are missing.
func TestCopyCrossSkillsPresentMissing(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	testutil.SeedDefaultsAt(t, e.root, "opencode")
	writeFile(t, filepath.Join(e.root, ".claude", "skills", "only-claude", "SKILL.md"), "# o\n")
	writeFile(t, filepath.Join(e.root, ".claude", "skills", "shared-agent", "SKILL.md"), "# s\n")
	writeFile(t, filepath.Join(e.root, ".agents", "skills", "shared-agent", "SKILL.md"), "# s\n")
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login", "--share", "content")
	e.run(nil, "sync")

	rep := copyJSON(t, e, "claude/work", "opencode/oc")
	// review: in the opencode default skills; shared-agent: via ~/.agents/skills;
	// only-claude: nowhere the target reads. opencode also reads ~/.claude/skills,
	// so everything there is present.
	for _, s := range []string{"review", "shared-agent", "only-claude"} {
		if !contains(rep.SkillsInTarget, s) {
			t.Errorf("skills_in_target missing %q: %v (missing %v)", s, rep.SkillsInTarget, rep.SkillsMissing)
		}
	}
	for _, esc := range []string{"~/.agents/skills", "~/.claude/skills"} {
		if !contains(rep.SkillsHomeEscapes, esc) {
			t.Errorf("skills_home_escapes missing %q: %v", esc, rep.SkillsHomeEscapes)
		}
	}

	testutil.SeedDefaultsAt(t, e.root, "codex")
	rep = copyJSON(t, e, "claude/work", "codex/cx")
	if !contains(rep.SkillsInTarget, "review") || !contains(rep.SkillsInTarget, "shared-agent") {
		t.Errorf("skills_in_target = %v", rep.SkillsInTarget)
	}
	if !contains(rep.SkillsMissing, "only-claude") || contains(rep.SkillsMissing, "review") {
		t.Errorf("skills_missing_in_target = %v", rep.SkillsMissing)
	}
	if !contains(rep.SkillsHomeEscapes, "~/.agents/skills") {
		t.Errorf("skills_home_escapes = %v", rep.SkillsHomeEscapes)
	}
	out := e.run(nil, "copy", "claude/work", "codex/cx2")
	if !strings.Contains(out, "only-claude") || !strings.Contains(out, "~/.agents/skills") {
		t.Errorf("text report must name missing skills and the escapes:\n%s", out)
	}
}
