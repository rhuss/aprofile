package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/testutil"
)

// runCLISplit runs the CLI and returns stdout and stderr separately.
func runCLISplit(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	root := NewRootCmd()
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetIn(strings.NewReader(""))
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), errOut.String(), err
}

// followupProfiles adds and syncs profiles given as "harness/name" and returns their
// config dirs by reference.
func followupProfiles(t *testing.T, refs ...string) map[string]string {
	t.Helper()
	testutil.HermeticHome(t)
	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	dirs := map[string]string{}
	for _, ref := range refs {
		h, n, _ := strings.Cut(ref, "/")
		args := []string{"add", h, n}
		if h == "claude" {
			args = append(args, "--auth-kind", "oauth-login")
		}
		mustCLI(t, args...)
		dirs[ref] = p.ProfileConfigDir(h, n)
	}
	mustCLI(t, "sync")
	return dirs
}

// putFile writes data at path, replacing a link there rather than writing through it.
func putFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(path)
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

// T084: the cli reads and writes the servers under the key the definition's location
// names, not a key hardcoded per flavor.
func TestMCPDocUsesDefinitionKey(t *testing.T) {
	def := &harness.Definition{Name: "x", MCP: &harness.MCPLocation{Location: "cfg.json#servers", Flavor: "claude-json"}}
	dir := t.TempDir()
	putFile(t, filepath.Join(dir, "cfg.json"), `{"mcpServers":{"no":{"command":"no"}},"servers":{"gh":{"command":"mcp-gh"}}}`)
	names, err := mcpServerNames(def, dir)
	if err != nil || strings.Join(names, ",") != "gh" {
		t.Fatalf("servers = %v (%v), want gh from key servers", names, err)
	}
	if got := ownedNotConverted(&harness.Definition{MCP: def.MCP, Classification: []harness.EntryRule{{Entry: "cfg.json", Layer: harness.LayerOwned}}}, dir); strings.Join(got, ",") != "cfg.json#mcpServers" {
		t.Errorf("owned not converted = %v, want only the non-MCP key", got)
	}
}

// T085: a cross-harness copy --overwrite names the replaced servers; mcp copy prints
// the same MCP report lines.
func TestCopyReportsReplacedServers(t *testing.T) {
	dirs := followupProfiles(t, "claude/src", "codex/dst")
	putFile(t, filepath.Join(dirs["claude/src"], ".claude.json"), `{"mcpServers":{"gh":{"command":"mcp-gh"},"fs":{"command":"mcp-fs"}}}`)
	putFile(t, filepath.Join(dirs["codex/dst"], "config.toml"), "[mcp_servers.gh]\ncommand = \"old\"\n")
	out := mustCLI(t, "copy", "claude/src", "codex/dst", "--overwrite", "--yes")
	if !strings.Contains(out, "MCP servers replaced: gh") || !strings.Contains(out, "MCP servers converted: fs") {
		t.Errorf("copy --overwrite must name replaced and converted servers:\n%s", out)
	}
	out = mustCLI(t, "mcp", "copy", "claude/src", "codex/dst", "--overwrite")
	if !strings.Contains(out, "MCP servers replaced: fs, gh") {
		t.Errorf("mcp copy uses the same report lines:\n%s", out)
	}
}

// T086: copy --json has one secret_warnings list with a source per warning, keeps the
// structured skipped_plugins and has no plugins_skipped, owned_secret_warnings or
// settings_warnings fields.
func TestCopyJSONSecretWarningsMerged(t *testing.T) {
	testutil.HermeticHome(t)
	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	mustCLI(t, "add", "codex", "src", "--share", "content")
	mustCLI(t, "sync")
	src := p.ProfileConfigDir("codex", "src")
	putFile(t, filepath.Join(src, "config.toml"), `model = "m"

[model_providers.corp]
experimental_bearer_token = "sk-ant-OWNEDCANARY0000000000000000000AAAA"

[mcp_servers.tool]
command = "mcp-tool"
env = { API_KEY = "sk-ant-MCPCANARY00000000000000000000AAAA" }
`)
	// A settings entry of codex, if the definition has one, carries a secret too.
	codex := copyTestDef(t, "codex")
	settingsRel := ""
	for _, r := range codex.Classification {
		if r.Layer == harness.LayerSettings && !r.IsGlob() && !r.IsDir() && strings.HasSuffix(r.Path(), ".json") {
			settingsRel = r.Path()
			break
		}
	}
	if settingsRel != "" {
		putFile(t, filepath.Join(src, filepath.FromSlash(settingsRel)), `{"api_key":"sk-ant-SETTINGSCANARY000000000000000000AAAA"}`)
	}
	out, _, err := runCLISplit(t, "copy", "codex/src", "codex/twin", "--json")
	if err != nil {
		t.Fatalf("copy: %v\n%s", err, out)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	for _, gone := range []string{"plugins_skipped", "owned_secret_warnings", "settings_warnings"} {
		if _, ok := raw[gone]; ok {
			t.Errorf("field %s must be gone:\n%s", gone, out)
		}
	}
	var rep struct {
		SecretWarnings []copySecretWarning `json:"secret_warnings"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatal(err)
	}
	has := func(w copySecretWarning) bool {
		for _, g := range rep.SecretWarnings {
			if g == w {
				return true
			}
		}
		return false
	}
	if !has(copySecretWarning{Source: "mcp", Server: "tool", Field: "env[API_KEY]"}) {
		t.Errorf("missing mcp warning: %+v", rep.SecretWarnings)
	}
	if !has(copySecretWarning{Source: "owned", Entry: "config.toml", Key: "model_providers.corp.experimental_bearer_token"}) {
		t.Errorf("missing owned warning: %+v", rep.SecretWarnings)
	}
	if settingsRel != "" && !has(copySecretWarning{Source: "settings", Entry: settingsRel, Key: "api_key"}) {
		t.Errorf("missing settings warning: %+v", rep.SecretWarnings)
	}
	if strings.Contains(out, "CANARY") {
		t.Errorf("a warning carries a value:\n%s", out)
	}
}

// T086: settings warnings use the merged shape (claude has a settings.json).
func TestCopySettingsWarningSource(t *testing.T) {
	testutil.HermeticHome(t)
	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	mustCLI(t, "add", "claude", "src", "--auth-kind", "oauth-login", "--share", "content")
	mustCLI(t, "sync")
	putFile(t, filepath.Join(p.ProfileConfigDir("claude", "src"), "settings.json"), `{"env":{"API_KEY":"sk-ant-SETTINGSCANARY000000000000000000AAAA"}}`)
	out, _, err := runCLISplit(t, "copy", "claude/src", "claude/twin", "--json")
	if err != nil {
		t.Fatalf("copy: %v\n%s", err, out)
	}
	var rep struct {
		SecretWarnings []copySecretWarning `json:"secret_warnings"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatal(err)
	}
	want := copySecretWarning{Source: "settings", Entry: "settings.json", Key: "env.API_KEY"}
	found := false
	for _, w := range rep.SecretWarnings {
		found = found || w == want
	}
	if !found {
		t.Errorf("missing %+v in %+v", want, rep.SecretWarnings)
	}
	text, errText, err := runCLISplit(t, "copy", "claude/src", "claude/twin2")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errText, "settings value settings.json: env.API_KEY looks like a secret") || strings.Contains(text+errText, "CANARY") {
		t.Errorf("text warning:\n%s\n%s", text, errText)
	}
}

// T090: a failed copy into a new target removes the config dir it prepared, so the
// retry after fixing the source works without --adopt.
func TestCopyFailureRollsBackNewTarget(t *testing.T) {
	dirs := followupProfiles(t, "claude/src")
	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	mcpFile := filepath.Join(dirs["claude/src"], ".claude.json")
	putFile(t, mcpFile, `{"mcpServers": {"gh": not json`)
	dst := p.ProfileConfigDir("codex", "dst")
	if out, err := runCLI(t, "copy", "claude/src", "codex/dst"); err == nil {
		t.Fatalf("copy from an unparseable source MCP file succeeded:\n%s", out)
	}
	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		t.Fatalf("failed copy left the target config dir %s (%v)", dst, err)
	}
	putFile(t, mcpFile, `{"mcpServers":{"gh":{"command":"mcp-gh"}}}`)
	if out, err := runCLI(t, "copy", "claude/src", "codex/dst"); err != nil {
		t.Fatalf("retry without --adopt failed: %v\n%s", err, out)
	}
	if data, err := os.ReadFile(filepath.Join(dst, "config.toml")); err != nil || !strings.Contains(string(data), "mcp-gh") {
		t.Errorf("retry did not convert: %v\n%s", err, data)
	}
}

// T090: a failed copy never removes a config dir it did not create (an adopted
// leftover dir stays).
func TestCopyFailureKeepsAdoptedDir(t *testing.T) {
	dirs := followupProfiles(t, "claude/src")
	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	putFile(t, filepath.Join(dirs["claude/src"], ".claude.json"), `{"mcpServers": not json`)
	dst := p.ProfileConfigDir("codex", "dst")
	keep := filepath.Join(dst, "mine.txt")
	putFile(t, keep, "mine")
	if out, err := runCLI(t, "copy", "claude/src", "codex/dst", "--adopt"); err == nil {
		t.Fatalf("copy succeeded:\n%s", out)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("adopted dir was removed: %v", err)
	}
}

// T095: the text report says a server was written without a dropped --flag=value
// option, naming the flag and never the value.
func TestCopyReportsDroppedOption(t *testing.T) {
	const tok = "sk-ant-OPTIONCANARY000000000000000000AAAA"
	dirs := followupProfiles(t, "claude/src")
	putFile(t, filepath.Join(dirs["claude/src"], ".claude.json"),
		`{"mcpServers":{"files":{"command":"mcp-files","args":["--root","/tmp","--token=`+tok+`"]}}}`)
	out := mustCLI(t, "copy", "claude/src", "codex/dst")
	if !strings.Contains(out, "MCP server written without a dropped option: files (args[2] --token=…)") {
		t.Errorf("text report must name the dropped option:\n%s", out)
	}
	if strings.Contains(out, tok) {
		t.Errorf("value printed:\n%s", out)
	}
	js, _, err := runCLISplit(t, "copy", "claude/src", "codex/dst2", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(js, `"option": "--token"`) || strings.Contains(js, tok) {
		t.Errorf("json drop must carry the option name only:\n%s", js)
	}
}
