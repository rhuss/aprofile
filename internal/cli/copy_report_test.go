package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/secret"
)

func copyTestDef(t *testing.T, name string) *harness.Definition {
	t.Helper()
	set, err := harness.LoadBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	def, ok := set.Get(name)
	if !ok {
		t.Fatalf("no builtin %q", name)
	}
	return def
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestOwnedSecretOptions(t *testing.T) {
	for v, want := range map[string]bool{
		"":                                      false,
		"o3":                                    false,
		"env:OPENAI_API_KEY":                    false,
		"${TOKEN}":                              false,
		"sk-ant-abc":                            true,
		"Bearer ghp_abcdefghijklmnopqrstuvwxyz": true,
		"AbCdEfGhIjKlMnOpQrStUvWxYz0123456789":  true,
		"https://llm.example.com/v1/some/very/long/path/segment": false,
		"/Users/someone/projects/a/very/long/path/to/a/file.txt": false,
		// Same rule as MCP args: path segments and reference defaults are still checked.
		"https://llm.example.com/v1?api_key=sk-abc": true,
		"${TOKEN:-ghp_fallback}":                    true,
	} {
		if got := secret.LooksLikeLiteralSecret(v, ownedSecretOptions); got != want {
			t.Errorf("LooksLikeLiteralSecret(%q, owned) = %v, want %v", v, got, want)
		}
	}
}

// Mixed files: only owned keys are scanned, and the MCP key is left to mcp's per-server
// warnings.
func TestOwnedSecretWarningsMixedFile(t *testing.T) {
	def := copyTestDef(t, "claude")
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".claude.json"),
		`{"oauthAccount":{"token":"sk-ant-CREDENTIAL"},"mcpServers":{"x":{"env":{"K":"sk-ant-MCP"}}}}`)
	if got := ownedSecretWarnings(def, dir); len(got) != 0 {
		t.Errorf("credentials and MCP keys must not be reported here: %v", got)
	}
}

func TestOwnedSecretWarningsWholeOwnedFile(t *testing.T) {
	def := copyTestDef(t, "pi")
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "agent", "mcp.json"),
		`{"settings":{"apiToken":"sk-ant-OWNED"},"mcpServers":{"x":{"env":{"K":"sk-ant-MCP"}}}}`)
	want := []copySecretWarning{{Source: "owned", Entry: "agent/mcp.json", Key: "settings.apiToken"}}
	if got := ownedSecretWarnings(def, dir); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestOwnedNotConverted(t *testing.T) {
	pi := copyTestDef(t, "pi")
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "agent", "mcp.json"), `{"settings":{},"mcpServers":{}}`)
	if got, want := ownedNotConverted(pi, dir), []string{"agent/mcp.json#settings"}; !reflect.DeepEqual(got, want) {
		t.Errorf("pi: got %v, want %v", got, want)
	}

	claude := copyTestDef(t, "claude")
	dir = t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".claude.json"), `{"oauthAccount":{},"mcpServers":{},"projects":{}}`)
	if got := ownedNotConverted(claude, dir); len(got) != 0 {
		t.Errorf("claude: only mcpServers is owned and it is converted: %v", got)
	}
}

func TestFindPluginsManifestFallback(t *testing.T) {
	def := copyTestDef(t, "claude")
	dir := t.TempDir()
	root := filepath.Join(dir, "plugins", "local", "helper")
	writeTestFile(t, filepath.Join(root, ".claude-plugin", "plugin.json"), `{"name":"helper-plugin"}`)
	writeTestFile(t, filepath.Join(root, "skills", "s1", "SKILL.md"), "# s1\n")
	writeTestFile(t, filepath.Join(root, "mcp.json"), `{"srv":{"command":"x"}}`)
	got := findPlugins(def, dir)
	want := []skippedPlugin{{Name: "helper-plugin", Skills: []string{"s1"}, MCPServers: []string{"srv"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if d := describePlugin(got[0]); !strings.Contains(d, "helper-plugin") || !strings.Contains(d, "s1") || !strings.Contains(d, "srv") {
		t.Errorf("describePlugin = %q", d)
	}
}

func TestResolveCopyAuth(t *testing.T) {
	claude := copyTestDef(t, "claude")
	if _, err := resolveCopyAuth(claude, nil, "", "", "claude/t"); err == nil || !strings.Contains(err.Error(), "--auth-key") {
		t.Errorf("default auth-token without a key must fail naming --auth-key: %v", err)
	}
	got, err := resolveCopyAuth(claude, nil, "", "env:TOK", "claude/t")
	if err != nil || got.kind != "auth-token" || got.refs["ANTHROPIC_AUTH_TOKEN"].String() != "env:TOK" {
		t.Errorf("got %+v, %v", got, err)
	}
	if got, err := resolveCopyAuth(claude, nil, "oauth-login", "", "claude/t"); err != nil || got.kind != "oauth-login" || got.refs != nil {
		t.Errorf("interactive kind: %+v, %v", got, err)
	}
	if _, err := resolveCopyAuth(claude, nil, "oauth-login", "env:TOK", "claude/t"); err == nil {
		t.Error("a key for an interactive kind must fail")
	}
	if _, err := resolveCopyAuth(claude, nil, "nope", "", "claude/t"); err == nil {
		t.Error("an unknown kind must fail")
	}
	// An overwritten target keeps its auth unless --auth or --auth-key is given.
	existing := &profile.Profile{AuthKind: "api-key", AuthRefs: map[string]secret.Reference{"ANTHROPIC_API_KEY": mustRef(t, "env:A")}}
	if got, err := resolveCopyAuth(claude, existing, "", "", "claude/t"); err != nil || got.set {
		t.Errorf("existing target keeps its auth: %+v, %v", got, err)
	}
	if got, err := resolveCopyAuth(claude, existing, "", "env:B", "claude/t"); err != nil || got.kind != "api-key" || got.refs["ANTHROPIC_API_KEY"].String() != "env:B" {
		t.Errorf("--auth-key on an existing target replaces its key: %+v, %v", got, err)
	}
}

func mustRef(t *testing.T, s string) secret.Reference {
	t.Helper()
	r, err := secret.ParseReference(s)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// A same-harness copy merges owned keys into the target's mixed file. A target file
// that exists but cannot be read (permission denied, a directory in its place) fails
// the copy and is left unchanged, instead of being replaced by the source's owned
// keys alone.
func TestCarryOwnedLayerUnreadableTarget(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads files regardless of their mode")
	}
	def := copyTestDef(t, "claude")
	src := t.TempDir()
	writeTestFile(t, filepath.Join(src, ".claude.json"), `{"mcpServers":{"x":{"command":"run"}}}`)

	t.Run("permission denied", func(t *testing.T) {
		dst := t.TempDir()
		target := filepath.Join(dst, ".claude.json")
		const orig = `{"oauthAccount":{"id":"keep"},"theme":"dark"}`
		writeTestFile(t, target, orig)
		if err := os.Chmod(target, 0o200); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(target, 0o600) })
		if _, err := carryOwnedLayer(def, src, dst); err == nil {
			t.Fatal("an unreadable target must fail the copy")
		}
		_ = os.Chmod(target, 0o600)
		if got, _ := os.ReadFile(target); string(got) != orig {
			t.Errorf("target changed: %s", got)
		}
	})

	t.Run("directory in its place", func(t *testing.T) {
		dst := t.TempDir()
		target := filepath.Join(dst, ".claude.json")
		writeTestFile(t, filepath.Join(target, "inside"), "x")
		if _, err := carryOwnedLayer(def, src, dst); err == nil {
			t.Fatal("a directory in place of the target must fail the copy")
		}
		if fi, err := os.Stat(target); err != nil || !fi.IsDir() {
			t.Errorf("target directory must stay: %v", err)
		}
	})
}
