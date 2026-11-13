package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// Feature 004 US2: a Codex API-key profile folder launches on its own.

const codexGW = `# provider for the work gateway
model_provider = "enmaas"

[model_providers.enmaas]
name = "enmaas"
base_url = "https://gw.example/v1"
env_key = "OPENAI_API_KEY"
`

func (e *env) writeCodexConfig(name, content string) string {
	e.t.Helper()
	cfg := e.configDir("codex", name)
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		e.t.Fatal(err)
	}
	p := filepath.Join(cfg, "config.toml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
	return p
}

func readTOML(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]any{}
	if err := toml.Unmarshal(data, &m); err != nil {
		t.Fatalf("%s: %v\n%s", path, err, data)
	}
	return m
}

func providerBlock(m map[string]any, name string) map[string]any {
	ps, _ := m["model_providers"].(map[string]any)
	b, _ := ps[name].(map[string]any)
	return b
}

func TestLaunchCodexProviderAuth(t *testing.T) {
	e := newEnv(t)
	key := e.keyFile("glm", "sk-codex-glm\n")
	e.run(nil, "add", "codex", "work-glm", "--share", "content", "--auth-kind", "api-key", "--auth-key", "file:"+key, "--model", "1.5")
	cfgPath := e.writeCodexConfig("work-glm", codexGW)

	dry := e.run(nil, "sync", "--dry-run")
	for _, want := range []string{
		"codex/work-glm: config.toml: set model_providers.enmaas.auth (replaces env_key)",
		"config.toml is re-encoded: comments are dropped",
	} {
		if !strings.Contains(dry, want) {
			t.Errorf("dry run lacks %q:\n%s", want, dry)
		}
	}
	if data, _ := os.ReadFile(cfgPath); string(data) != codexGW {
		t.Fatal("the dry run wrote config.toml")
	}

	e.run(nil, "sync")
	m := readTOML(t, cfgPath)
	blk := providerBlock(m, "enmaas")
	if _, has := blk["env_key"]; has {
		t.Error("env_key not replaced")
	}
	auth, _ := blk["auth"].(map[string]any)
	if auth["command"] != "sh" {
		t.Fatalf("auth block = %v", blk)
	}
	var args []string
	for _, a := range auth["args"].([]any) {
		args = append(args, a.(string))
	}
	if args[len(args)-1] != key {
		t.Errorf("the key path is not a separate argument: %v", args)
	}
	if got := execSh(t, "sh", args); got != "sk-codex-glm" {
		t.Errorf("auth command printed %q", got)
	}
	if data, _ := os.ReadFile(cfgPath); strings.Contains(string(data), "sk-codex-glm") {
		t.Error("config.toml holds the key value")
	}
	if _, has := m["model"]; has {
		t.Error("aprofile must not write model into config.toml")
	}

	// The wrapper passes the model before the caller's args and exports no key.
	out, err := e.runWrapper("codex-work-glm", []string{"OPENAI_API_KEY=leak"}, "-m", "x")
	if err != nil {
		t.Fatalf("wrapper: %v\n%s", err, out)
	}
	if !strings.Contains(out, "STUB_ARGS: [-c model=\"1.5\" -m x]") {
		t.Errorf("wrapper args:\n%s", out)
	}
	if strings.Contains(out, "STUB_ENV: OPENAI_API_KEY=") {
		t.Errorf("OPENAI_API_KEY exported although the folder carries the key:\n%s", out)
	}

	// The provider switch moves the block and restores the old provider's env_key.
	data, _ := os.ReadFile(cfgPath)
	switched := strings.Replace(string(data), "model_provider = 'enmaas'", "model_provider = 'other'", 1)
	switched = strings.Replace(switched, `model_provider = "enmaas"`, `model_provider = "other"`, 1)
	switched += "\n[model_providers.other]\nname = \"other\"\nbase_url = \"https://other/v1\"\nenv_key = \"OTHER_KEY\"\n"
	if err := os.WriteFile(cfgPath, []byte(switched), 0o644); err != nil {
		t.Fatal(err)
	}
	e.run(nil, "sync")
	m = readTOML(t, cfgPath)
	if old := providerBlock(m, "enmaas"); old["auth"] != nil || old["env_key"] != "OPENAI_API_KEY" {
		t.Errorf("old provider after the switch: %v", old)
	}
	if nw := providerBlock(m, "other"); nw["auth"] == nil || nw["env_key"] != nil {
		t.Errorf("new provider after the switch: %v", nw)
	}

	// Switching to chatgpt-login removes the auth block and restores env_key.
	e.run(nil, "edit", "codex/work-glm", "--auth-kind", "chatgpt-login")
	out = e.run(nil, "sync")
	if !strings.Contains(out, "codex/work-glm: config.toml: removed model_providers.other.auth (restored env_key)") {
		t.Errorf("sync output lacks the removal:\n%s", out)
	}
	m = readTOML(t, cfgPath)
	if nw := providerBlock(m, "other"); nw["auth"] != nil || nw["env_key"] != "OTHER_KEY" {
		t.Errorf("after chatgpt-login: %v", nw)
	}
}

func TestLaunchCodexNoCustomProvider(t *testing.T) {
	e := newEnv(t)
	key := e.keyFile("plain", "sk-plain\n")
	e.run(nil, "add", "codex", "plain", "--share", "content", "--auth-kind", "api-key", "--auth-key", "file:"+key)
	cfgPath := e.writeCodexConfig("plain", "model = \"o4\"\n")
	e.run(nil, "sync")
	if data, _ := os.ReadFile(cfgPath); string(data) != "model = \"o4\"\n" {
		t.Errorf("config.toml without a custom provider changed:\n%s", data)
	}
	out, err := e.runWrapper("codex-plain", nil)
	if err != nil || !strings.Contains(out, "STUB_ENV: OPENAI_API_KEY=sk-plain") {
		t.Errorf("the wrapper carries the key without a custom provider: %v\n%s", err, out)
	}
	if !strings.Contains(out, "STUB_ARGS: []") {
		t.Errorf("no model, no model argument:\n%s", out)
	}
}
