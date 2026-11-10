package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Feature 004 US1: a Claude API-key profile folder launches on its own.

const launchKey = "sk-test-launch-canary"

func (e *env) keyFile(name, content string) string {
	e.t.Helper()
	p := filepath.Join(e.root, "keys", name)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		e.t.Fatal(err)
	}
	return p
}

func (e *env) writeDefaultClaudeSettings(content string) string {
	e.t.Helper()
	p := filepath.Join(e.root, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
	return p
}

func readSettings(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("%s: %v\n%s", path, err, data)
	}
	return m
}

func envBlock(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	env, ok := m["env"].(map[string]any)
	if !ok {
		t.Fatalf("settings.json has no env object: %v", m)
	}
	return env
}

// onlyNeutralizing reports whether a settings.json holds nothing but an env block
// of empty entries (spec 005 neutralizing entries).
func onlyNeutralizing(t *testing.T, m map[string]any) bool {
	t.Helper()
	if len(m) != 1 {
		return false
	}
	env, ok := m["env"].(map[string]any)
	if !ok || len(env) == 0 {
		return false
	}
	for _, v := range env {
		if v != "" {
			return false
		}
	}
	return true
}

// runHelper runs a key command the way the harness does (/bin/sh -c) with an empty
// environment.
func runHelper(t *testing.T, cmd string) string {
	t.Helper()
	c := exec.Command("/bin/sh", "-c", cmd)
	c.Env = []string{"PATH=/usr/bin:/bin"}
	out, err := c.Output()
	if err != nil {
		t.Fatalf("key command %q failed: %v", cmd, err)
	}
	return string(out)
}

func (e *env) profileFile(harness, name string) string {
	return filepath.Join(e.root, ".config", "aprofile", "profiles", harness, name+".yaml")
}

// dropProfileLine removes the profile store line starting with prefix (a hand edit).
func (e *env) dropProfileLine(harness, name, prefix string) {
	e.t.Helper()
	p := e.profileFile(harness, name)
	data, err := os.ReadFile(p)
	if err != nil {
		e.t.Fatal(err)
	}
	var keep []string
	for _, l := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(l, prefix) {
			keep = append(keep, l)
		}
	}
	if err := os.WriteFile(p, []byte(strings.Join(keep, "\n")), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func TestLaunchClaudeFolderCarriesSettings(t *testing.T) {
	e := newEnv(t)
	key := e.keyFile("work", launchKey+"\n")
	e.writeDefaultClaudeSettings(`{"model":"opus","permissions":{"allow":["Bash(ls)"]},` +
		`"env":{"MLFLOW_TRACKING_URI":"http://ml","ANTHROPIC_BASE_URL":"http://user"}}`)
	e.run(nil, "add", "claude", "work", "--share", "content", "--auth-kind", "api-key", "--auth-key", "file:"+key,
		"--model", "gw-model", "--endpoint", "http://127.0.0.1:18801",
		"--env", "ANTHROPIC_DEFAULT_HAIKU_MODEL=gw-haiku")
	out := e.run(nil, "sync")
	for _, want := range []string{
		"claude/work: settings.json: set env ANTHROPIC_MODEL",
		"claude/work: settings.json: set apiKeyHelper",
		"claude/work: settings.json: replaced user entry env ANTHROPIC_BASE_URL",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("sync output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "http://user") || strings.Contains(out, launchKey) {
		t.Errorf("sync printed a value:\n%s", out)
	}

	settings := filepath.Join(e.configDir("claude", "work"), "settings.json")
	m := readSettings(t, settings)
	env := envBlock(t, m)
	for k, v := range map[string]string{
		"ANTHROPIC_MODEL":               "gw-model",
		"ANTHROPIC_BASE_URL":            "http://127.0.0.1:18801",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL": "gw-haiku",
		"APROFILE_SET_VARS":             "ANTHROPIC_MODEL ANTHROPIC_BASE_URL ANTHROPIC_DEFAULT_HAIKU_MODEL",
		"MLFLOW_TRACKING_URI":           "http://ml",
		"ANTHROPIC_API_KEY":             "",
		"ANTHROPIC_AUTH_TOKEN":          "",
		"CLAUDE_CODE_USE_VERTEX":        "",
	} {
		if got, ok := env[k]; !ok || got != v {
			t.Errorf("env.%s = %v (present %v), want %q", k, got, ok, v)
		}
	}
	if m["model"] != "opus" || m["permissions"] == nil {
		t.Errorf("user keys changed: model=%v permissions=%v", m["model"], m["permissions"])
	}
	helper, _ := m["apiKeyHelper"].(string)
	if got := runHelper(t, helper); got != launchKey {
		t.Errorf("apiKeyHelper printed %q", got)
	}
	data, _ := os.ReadFile(settings)
	if strings.Contains(string(data), launchKey) {
		t.Error("settings.json holds the key value")
	}

	// Second sync: zero changes, no file written.
	dataDir := filepath.Join(e.root, ".local", "share", "aprofile")
	before := snapshotMtimes(t, dataDir)
	out2 := e.run(nil, "sync")
	if !strings.Contains(out2, "0 changes") && !strings.Contains(out2, "already up to date") {
		t.Errorf("second sync reports changes:\n%s", out2)
	}
	after := snapshotMtimes(t, dataDir)
	for p, mt := range before {
		if after[p] != mt {
			t.Errorf("%s rewritten by an unchanged sync", p)
		}
	}

	// A /model pick (Claude Code writes the model key) is left alone; an edited placed
	// entry is restored.
	m["model"] = "haiku"
	envBlock(t, m)["ANTHROPIC_MODEL"] = "user-pick"
	edited, _ := json.Marshal(m)
	if err := os.WriteFile(settings, edited, 0o644); err != nil {
		t.Fatal(err)
	}
	e.run(nil, "sync")
	m = readSettings(t, settings)
	if envBlock(t, m)["ANTHROPIC_MODEL"] != "gw-model" || m["model"] != "haiku" {
		t.Errorf("after restore: env.ANTHROPIC_MODEL=%v model=%v", envBlock(t, m)["ANTHROPIC_MODEL"], m["model"])
	}

	// Removing the model from the profile removes env.ANTHROPIC_MODEL only.
	e.dropProfileLine("claude", "work", "model:")
	e.run(nil, "sync")
	m = readSettings(t, settings)
	env = envBlock(t, m)
	if _, ok := env["ANTHROPIC_MODEL"]; ok {
		t.Error("env.ANTHROPIC_MODEL not removed")
	}
	if env["ANTHROPIC_BASE_URL"] != "http://127.0.0.1:18801" || env["MLFLOW_TRACKING_URI"] != "http://ml" || m["apiKeyHelper"] == nil {
		t.Errorf("other entries changed: %v", m)
	}
	if m["model"] != "haiku" {
		t.Error("the harness's model key changed")
	}
}

func TestLaunchClaudeOAuthTokenKeyWrapperOnly(t *testing.T) {
	e := newEnv(t)
	e.run(nil, "add", "claude", "tok", "--share", "content", "--auth-kind", "oauth-token", "--auth-key", "env:TOK", "--model", "m")
	e.run(nil, "sync")
	m := readSettings(t, filepath.Join(e.configDir("claude", "tok"), "settings.json"))
	if _, ok := m["apiKeyHelper"]; ok {
		t.Error("an oauth-token key must stay wrapper-only")
	}
	if envBlock(t, m)["ANTHROPIC_MODEL"] != "m" {
		t.Error("the model is still folder-carried")
	}
	// Spec 005 FR-001, FR-002: the folder neutralizes the auth-clearing list, except
	// the token variable the wrapper supplies.
	if _, ok := envBlock(t, m)["CLAUDE_CODE_OAUTH_TOKEN"]; ok {
		t.Error("the wrapper-supplied token variable must not be blanked")
	}
	if got, ok := envBlock(t, m)["CLAUDE_CODE_USE_VERTEX"]; !ok || got != "" {
		t.Errorf("env.CLAUDE_CODE_USE_VERTEX = %v (present %v), want empty", got, ok)
	}
	out, err := e.runWrapper("claude-tok", []string{"TOK=oauth-value"})
	if err != nil || !strings.Contains(out, "STUB_ENV: CLAUDE_CODE_OAUTH_TOKEN=oauth-value") {
		t.Errorf("wrapper must export the oauth token: %v\n%s", err, out)
	}
}

func TestLaunchClaudeShareAllPlacesNothing(t *testing.T) {
	e := newEnv(t)
	def := e.writeDefaultClaudeSettings(`{"theme":"dark"}`)
	key := e.keyFile("all", launchKey+"\n")
	e.run(nil, "add", "claude", "shared", "--share", "all", "--auth-kind", "api-key", "--auth-key", "file:"+key, "--model", "m")
	e.run(nil, "sync")
	if data, _ := os.ReadFile(def); string(data) != `{"theme":"dark"}` {
		t.Errorf("the default setup's settings.json changed: %s", data)
	}
	out, err := e.runWrapper("claude-shared", nil)
	if err != nil || !strings.Contains(out, "STUB_ENV: ANTHROPIC_MODEL=m") || !strings.Contains(out, "STUB_ENV: ANTHROPIC_API_KEY="+launchKey) {
		t.Errorf("at share level all the wrapper carries the settings: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(e.root, ".local", "share", "aprofile", "managed-launch", "claude", "shared.json")); !os.IsNotExist(err) {
		t.Error("a launch record exists for a profile at share level all")
	}
}

func TestLaunchClaudeBrokenSettingsLeavesProfile(t *testing.T) {
	e := newEnv(t)
	key := e.keyFile("work", launchKey+"\n")
	e.run(nil, "add", "claude", "work", "--share", "content", "--auth-kind", "api-key", "--auth-key", "file:"+key, "--model", "a")
	e.run(nil, "sync")
	settings := filepath.Join(e.configDir("claude", "work"), "settings.json")
	if err := os.WriteFile(settings, []byte(`{broken`), 0o644); err != nil {
		t.Fatal(err)
	}
	wrapperPath := filepath.Join(e.root, ".local", "share", "aprofile", "bin", "claude-work")
	regPath := filepath.Join(e.root, ".local", "share", "aprofile", "registry.json")
	wBefore, _ := os.ReadFile(wrapperPath)
	rBefore, _ := os.ReadFile(regPath)
	e.run(nil, "edit", "claude/work", "--label", "Changed")
	out, err := e.tryRun(nil, "sync")
	if err == nil {
		t.Fatalf("sync must exit non-zero for a broken settings.json:\n%s", out)
	}
	if !regexp.MustCompile(`claude/work: settings\.json: cannot place launch settings: .*\(key env\)`).MatchString(out) {
		t.Errorf("error does not name profile, file and key:\n%s", out)
	}
	if data, _ := os.ReadFile(settings); string(data) != `{broken` {
		t.Error("the broken settings.json changed")
	}
	if wAfter, _ := os.ReadFile(wrapperPath); string(wAfter) != string(wBefore) {
		t.Error("the wrapper changed")
	}
	if rAfter, _ := os.ReadFile(regPath); string(rAfter) != string(rBefore) {
		t.Error("the registry entry changed")
	}
}

// execSh runs command with args and an empty environment and returns its output.
func execSh(t *testing.T, command string, args []string) string {
	t.Helper()
	c := exec.Command(command, args...)
	c.Env = []string{"PATH=/usr/bin:/bin"}
	out, err := c.Output()
	if err != nil {
		t.Fatalf("%s %v: %v", command, args, err)
	}
	return string(out)
}
