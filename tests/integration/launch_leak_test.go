package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/testutil"
)

// TestSC006NoLaunchVariableLeaks (spec 004 SC-006, FR-014): for every ordered pair
// of fixture profiles, the second's wrapper started from an environment holding the
// first's launch variables and APROFILE_SET_VARS (as the first's wrapper or folder
// session leaves them) passes none of the first's launch variables to the harness.
func TestSC006NoLaunchVariableLeaks(t *testing.T) {
	e := newEnv(t)
	key := e.keyFile("k", "sk-leak\n")
	e.run(nil, "add", "claude", "deep", "--share", "content", "--auth-kind", "api-key", "--auth-key", "file:"+key,
		"--model", "deep-model", "--endpoint", "http://deep",
		"--env", "ANTHROPIC_DEFAULT_HAIKU_MODEL=deep-haiku", "--env", "DEEP_ONLY=1")
	e.run(nil, "add", "claude", "private", "--share", "content", "--auth-kind", "oauth-login")
	e.run(nil, "add", "codex", "glm", "--share", "content", "--auth-kind", "api-key", "--auth-key", "file:"+key,
		"--model", "glm-5", "--endpoint", "http://glm", "--env", "GLM_ONLY=1")
	e.run(nil, "add", "opencode", "oc", "--share", "content", "--auth-kind", openCodeKind(t, e), "--env", "OC_ONLY=1")
	cfg := e.configDir("codex", "glm")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "config.toml"), []byte("model_provider = \"gw\"\n[model_providers.gw]\nbase_url = \"http://glm\"\nenv_key = \"OPENAI_API_KEY\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.run(nil, "sync")

	// Each profile's session environment is taken from what aprofile produced: the
	// variables its wrapper exports plus, for a Claude folder, the non-empty entries
	// of its env block (a session started from the folder alone exports them). The
	// session's launch variables are the names its APROFILE_SET_VARS lists.
	ocStub := filepath.Dir(testutil.BuildBinary(t, "github.com/cc-deck/aprofile/tests/fixtures/stub", "opencode"))
	for i, v := range e.vars {
		if p, ok := strings.CutPrefix(v, "PATH="); ok {
			e.vars[i] = "PATH=" + ocStub + string(os.PathListSeparator) + p
		}
	}
	session := func(wrapperName, harnessName, name string) []string {
		out, err := e.runWrapper(wrapperName, []string{"HOME=" + e.root})
		if err != nil {
			t.Fatalf("%s: %v\n%s", wrapperName, err, out)
		}
		vals := map[string]string{}
		for _, l := range strings.Split(out, "\n") {
			if kv, ok := strings.CutPrefix(l, "STUB_ENV: "); ok {
				k, v, _ := strings.Cut(kv, "=")
				vals[k] = v
			}
		}
		if harnessName == "claude" {
			block, _ := readSettings(t, filepath.Join(e.configDir("claude", name), "settings.json"))["env"].(map[string]any)
			for k, v := range block {
				if s, ok := v.(string); ok && s != "" {
					vals[k] = s
				}
			}
		}
		env := []string{"APROFILE_SET_VARS=" + vals["APROFILE_SET_VARS"]}
		for _, n := range strings.Fields(vals["APROFILE_SET_VARS"]) {
			env = append(env, n+"="+vals[n])
		}
		return env
	}
	sessions := map[string][]string{
		"claude-deep":    session("claude-deep", "claude", "deep"),
		"claude-private": session("claude-private", "claude", "private"),
		"codex-glm":      session("codex-glm", "codex", "glm"),
		"opencode-oc":    session("opencode-oc", "opencode", "oc"),
	}
	wantSetVars := map[string]string{
		"claude-deep":    "APROFILE_SET_VARS=ANTHROPIC_MODEL ANTHROPIC_BASE_URL ANTHROPIC_DEFAULT_HAIKU_MODEL DEEP_ONLY",
		"claude-private": "APROFILE_SET_VARS=",
		"codex-glm":      "APROFILE_SET_VARS=OPENAI_BASE_URL GLM_ONLY",
		"opencode-oc":    "APROFILE_SET_VARS=OC_ONLY",
	}
	for w, want := range wantSetVars {
		if got := sessions[w][0]; got != want {
			t.Errorf("%s session carries %q, want %q", w, got, want)
		}
	}
	for first, firstEnv := range sessions {
		for second := range sessions {
			if first == second {
				continue
			}
			out, err := e.runWrapper(second, append([]string{"HOME=" + e.root}, firstEnv...))
			if err != nil {
				t.Fatalf("%s after %s: %v\n%s", second, first, err, out)
			}
			for _, kv := range firstEnv {
				name, val, _ := strings.Cut(kv, "=")
				if name == "APROFILE_SET_VARS" || val == "" {
					continue
				}
				if strings.Contains(out, "STUB_ENV: "+kv+"\n") && !ownSetting(second, kv) {
					t.Errorf("%s started from a %s session: %s leaked", second, first, kv)
				}
			}
		}
	}

	// A hostile inherited list never clears PATH or HOME.
	out, err := e.runWrapper("claude-private", []string{"HOME=" + e.root, "APROFILE_SET_VARS=PATH HOME CLAUDE_CONFIG_DIR APROFILE_NAME"})
	if err != nil {
		t.Fatalf("hostile APROFILE_SET_VARS broke the wrapper: %v\n%s", err, out)
	}
	for _, want := range []string{"STUB_ENV: PATH=", "STUB_ENV: HOME=" + e.root, "STUB_ENV: CLAUDE_CONFIG_DIR=", "STUB_ENV: APROFILE_NAME=private"} {
		if !strings.Contains(out, want) {
			t.Errorf("hostile APROFILE_SET_VARS removed %q:\n%s", want, out)
		}
	}
}

// ownSetting reports whether kv is a value the second profile itself supplies (the
// codex profile's endpoint is the same in both fixtures only by name).
func ownSetting(wrapper, kv string) bool {
	return wrapper == "codex-glm" && kv == "OPENAI_BASE_URL=http://glm"
}

// openCodeKind returns an OpenCode auth kind without a reference.
func openCodeKind(t *testing.T, e *env) string {
	t.Helper()
	out := e.run(nil, "harness", "show", "opencode")
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "default_auth_kind:") {
			f := strings.Fields(l)
			if len(f) >= 2 {
				return f[1]
			}
		}
	}
	return "login"
}
