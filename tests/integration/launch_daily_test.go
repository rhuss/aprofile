package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// TestSC001DailyProfileFixtures (SC-001): nine fixtures shaped like the API-key
// daily profiles start from their folder alone with the correct endpoint and key.
// The files aprofile writes are checked here, the key command is run with
// /bin/sh, and a folder-only Claude launch from a hostile environment is emulated
// with the verified harness facts; launching the real harnesses against a stub
// endpoint is manual (tests never run real harnesses; walkthrough, research R10).
func TestSC001DailyProfileFixtures(t *testing.T) {
	type fixture struct {
		harness, name, model, endpoint string
		aliases                        bool
	}
	aliasEnv := []string{
		"ANTHROPIC_DEFAULT_OPUS_MODEL=gw-opus",
		"ANTHROPIC_DEFAULT_SONNET_MODEL=gw-sonnet",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL=gw-haiku",
	}
	fixtures := []fixture{
		{"claude", "deepseek", "deepseek-v4", "https://api.deepseek.example/anthropic", false},
		{"claude", "glm", "glm-5", "https://open.glm.example/api/anthropic", false},
		{"claude", "work", "", "https://gw.work.example", false},
		{"claude", "work-deepseek", "rits/deepseek-ai/DeepSeek-V4.1-Flash-1017[1m]", "https://gw.work.example/deepseek", true},
		{"claude", "work-glm", "rits/glm-5.1", "https://gw.work.example/glm", true},
		{"codex", "deepseek", "deepseek-chat", "https://api.deepseek.example/v1", false},
		{"codex", "glm", "glm-5", "https://open.glm.example/v1", false},
		{"codex", "work-deepseek", "1.5", "https://gw.work.example/v1", false},
		{"codex", "work-glm", "rits/glm-5.1", "https://gw.work.example/v1", false},
	}
	e := newEnv(t)
	for _, f := range fixtures {
		key := e.keyFile(f.harness+"-"+f.name, "sk-"+f.harness+"-"+f.name+"\n")
		args := []string{"add", f.harness, f.name, "--share", "content", "--auth-kind", "api-key", "--auth-key", "file:" + key}
		if f.model != "" {
			args = append(args, "--model", f.model)
		}
		if f.harness == "claude" {
			args = append(args, "--endpoint", f.endpoint)
			if f.aliases {
				for _, a := range aliasEnv {
					args = append(args, "--env", a)
				}
			}
		}
		e.run(nil, args...)
		if f.harness == "codex" {
			cfg := e.configDir("codex", f.name)
			if err := os.MkdirAll(cfg, 0o755); err != nil {
				t.Fatal(err)
			}
			toml := "model_provider = \"enmaas\"\n\n[model_providers.enmaas]\nname = \"enmaas\"\nbase_url = \"" + f.endpoint + "\"\nenv_key = \"OPENAI_API_KEY\"\nwire_api = \"responses\"\n"
			if err := os.WriteFile(filepath.Join(cfg, "config.toml"), []byte(toml), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	e.run(nil, "sync")

	passed := 0
	for _, f := range fixtures {
		id := f.harness + "/" + f.name
		wantKey := "sk-" + f.harness + "-" + f.name
		ok := true
		fail := func(format string, a ...any) {
			ok = false
			t.Errorf(id+": "+format, a...)
		}
		cfg := e.configDir(f.harness, f.name)
		if f.harness == "claude" {
			m := readSettings(t, filepath.Join(cfg, "settings.json"))
			env := envBlock(t, m)
			if env["ANTHROPIC_BASE_URL"] != f.endpoint {
				fail("endpoint = %v", env["ANTHROPIC_BASE_URL"])
			}
			if f.model != "" && env["ANTHROPIC_MODEL"] != f.model {
				fail("model = %v", env["ANTHROPIC_MODEL"])
			}
			if f.model == "" {
				if _, has := env["ANTHROPIC_MODEL"]; has {
					fail("a profile without a model gets no model entry")
				}
			}
			if f.aliases && (env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] != "gw-haiku" || env["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "gw-opus") {
				fail("aliases = %v", env)
			}
			helper, _ := m["apiKeyHelper"].(string)
			if got := runHelper(t, helper); got != wantKey {
				fail("key command printed %q", got)
			}
			// Folder-only launch from a hostile tool environment, emulated with the
			// verified harness facts (spec Context): the env block is applied over
			// the launching environment, and an empty auth entry neutralizes an
			// exported key so the key command's key is sent (FR-006a).
			hostile := map[string]string{
				"ANTHROPIC_API_KEY": "sk-wrong", "ANTHROPIC_AUTH_TOKEN": "sk-wrong",
				"ANTHROPIC_BASE_URL": "http://wrong.example", "ANTHROPIC_MODEL": "wrong-model",
				"CLAUDE_CODE_USE_BEDROCK": "1", "CLAUDE_CODE_USE_VERTEX": "1",
			}
			for k, v := range env {
				if s, isStr := v.(string); isStr {
					hostile[k] = s
				}
			}
			for _, k := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX"} {
				if hostile[k] != "" {
					fail("a launching tool's %s is not neutralized by the folder", k)
				}
			}
			if hostile["ANTHROPIC_BASE_URL"] != f.endpoint || (f.model != "" && hostile["ANTHROPIC_MODEL"] != f.model) {
				fail("folder-only launch uses endpoint %q, model %q", hostile["ANTHROPIC_BASE_URL"], hostile["ANTHROPIC_MODEL"])
			}
		} else {
			data, err := os.ReadFile(filepath.Join(cfg, "config.toml"))
			if err != nil {
				fail("config.toml: %v", err)
				continue
			}
			doc := map[string]any{}
			if err := toml.Unmarshal(data, &doc); err != nil {
				fail("config.toml invalid: %v", err)
				continue
			}
			blk, _ := doc["model_providers"].(map[string]any)["enmaas"].(map[string]any)
			if blk["base_url"] != f.endpoint || blk["env_key"] != nil {
				fail("provider block = %v", blk)
			}
			auth, _ := blk["auth"].(map[string]any)
			var args []string
			for _, a := range auth["args"].([]any) {
				args = append(args, a.(string))
			}
			c := execSh(t, auth["command"].(string), args)
			if c != wantKey {
				fail("auth command printed %q", c)
			}
			w, _ := os.ReadFile(filepath.Join(e.root, ".local", "share", "aprofile", "bin", "codex-"+f.name))
			if !strings.Contains(string(w), `exec 'codex' '-c' 'model="`+f.model+`"' "$@"`) {
				fail("wrapper lacks the model argument:\n%s", w)
			}
		}
		if ok {
			passed++
		}
	}
	if passed != len(fixtures) {
		t.Errorf("SC-001: %d of %d daily fixtures pass", passed, len(fixtures))
	}
}
