package wrapper_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/secret"
	"github.com/cc-deck/aprofile/internal/wrapper"
)

// Feature 004 T019: the carrier-aware wrapper.

func builtinDef(t *testing.T, name string) *harness.Definition {
	t.Helper()
	set, err := harness.LoadBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	d, ok := set.Get(name)
	if !ok {
		t.Fatalf("no builtin %s", name)
	}
	return d
}

// stubBin puts a stub for binary on a fresh PATH dir: it prints ARG=<arg> per
// argument, then its environment.
func stubBin(t *testing.T, binary string) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nfor a in \"$@\"; do printf 'ARG=%s\\n' \"$a\"; done\nexec /usr/bin/env\n"
	if err := os.WriteFile(filepath.Join(dir, binary), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

type launched struct {
	env  map[string]string
	args []string
	out  string
}

// launch renders the wrapper for p with cfg as config dir and runs it with exactly
// env (plus PATH to the stub), returning what the stub saw.
func launch(t *testing.T, p *profile.Profile, def *harness.Definition, cfg string, opts wrapper.RenderOptions, env []string, args ...string) (launched, error) {
	t.Helper()
	s, err := wrapper.RenderWith(p, def, cfg, opts)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "w")
	if err := os.WriteFile(path, []byte(s), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", append([]string{path}, args...)...)
	cmd.Env = append([]string{"PATH=" + stubBin(t, def.Binary) + ":/usr/bin:/bin"}, env...)
	out, err := cmd.CombinedOutput()
	l := launched{env: map[string]string{}, out: string(out)}
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if a, ok := strings.CutPrefix(line, "ARG="); ok {
			l.args = append(l.args, a)
			continue
		}
		if i := strings.IndexByte(line, '='); i > 0 {
			l.env[line[:i]] = line[i+1:]
		}
	}
	return l, err
}

func mustLaunch(t *testing.T, p *profile.Profile, def *harness.Definition, cfg string, opts wrapper.RenderOptions, env []string, args ...string) launched {
	t.Helper()
	l, err := launch(t, p, def, cfg, opts, env, args...)
	if err != nil {
		t.Fatalf("wrapper failed: %v\n%s", err, l.out)
	}
	return l
}

// folderCfg returns a config dir with its own settings.json, so a content profile's
// folder carries its launch settings.
func folderCfg(t *testing.T) string {
	t.Helper()
	cfg := t.TempDir()
	if err := os.WriteFile(filepath.Join(cfg, "settings.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func keyFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func folderProfile(t *testing.T, key string) *profile.Profile {
	return &profile.Profile{Harness: "claude", Name: "deep", AuthKind: "api-key", Share: profile.ShareContent,
		AuthRefs: map[string]secret.Reference{"ANTHROPIC_API_KEY": mustRef(t, "file:"+key)},
		Model:    "gw-model", Endpoint: "http://gw",
		ExtraEnv: []profile.ExtraEnvVar{
			{Name: "ANTHROPIC_DEFAULT_HAIKU_MODEL", Value: "gw-haiku"},
			{Name: "CLOUD_ML_REGION", Value: "env:CLOUD_ML_REGION"},
		}}
}

func TestWrapperFolderCarriedOnlyCleared(t *testing.T) {
	def := builtinDef(t, "claude")
	cfg := folderCfg(t)
	p := folderProfile(t, keyFile(t, "sk-test\n"))
	s, _ := wrapper.RenderWith(p, def, cfg, wrapper.RenderOptions{})
	for _, bad := range []string{"ANTHROPIC_MODEL='gw-model'", "ANTHROPIC_BASE_URL='http://gw'", "export ANTHROPIC_API_KEY", "export APROFILE_SET_VARS"} {
		if strings.Contains(s, bad) {
			t.Errorf("wrapper sets a folder-carried setting (%s):\n%s", bad, s)
		}
	}
	l := mustLaunch(t, p, def, cfg, wrapper.RenderOptions{}, []string{
		"ANTHROPIC_MODEL=caller", "ANTHROPIC_BASE_URL=http://caller", "ANTHROPIC_API_KEY=leak",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL=caller-haiku", "CLOUD_ML_REGION=us-east5", "APROFILE_SET_VARS=X",
	}, "--model", "x")
	for _, v := range []string{"ANTHROPIC_MODEL", "ANTHROPIC_BASE_URL", "ANTHROPIC_API_KEY", "ANTHROPIC_DEFAULT_HAIKU_MODEL", "APROFILE_SET_VARS"} {
		if val, ok := l.env[v]; ok {
			t.Errorf("%s=%q reached the harness; the folder carries it", v, val)
		}
	}
	// A self-referencing pass-through survives the clearing (captured first).
	if l.env["CLOUD_ML_REGION"] != "us-east5" {
		t.Errorf("CLOUD_ML_REGION = %q, want the caller's us-east5", l.env["CLOUD_ML_REGION"])
	}
	if strings.Join(l.args, " ") != "--model x" {
		t.Errorf("args = %v", l.args)
	}
	for k := range l.env {
		if strings.HasPrefix(k, "_aprofile") {
			t.Errorf("wrapper leaks its temporary %s", k)
		}
	}
}

func TestWrapperFolderKeyFailsClosed(t *testing.T) {
	def := builtinDef(t, "claude")
	cfg := folderCfg(t)
	missing := filepath.Join(t.TempDir(), "nope")
	p := folderProfile(t, missing)
	l, err := launch(t, p, def, cfg, wrapper.RenderOptions{}, []string{"CLOUD_ML_REGION=x"})
	if err == nil {
		t.Fatalf("wrapper should fail for a missing key file:\n%s", l.out)
	}
	if !strings.Contains(l.out, "aprofile: profile claude/deep: reference file:"+missing+" is missing, unreadable or empty") {
		t.Errorf("failure message = %q", l.out)
	}
	s, _ := wrapper.RenderWith(p, def, cfg, wrapper.RenderOptions{})
	if strings.Contains(s, "export ANTHROPIC_API_KEY") || strings.Contains(s, "export _aprofile") {
		t.Errorf("the folder-carried key must never be exported:\n%s", s)
	}
}

func TestWrapperClearedSourceKeyExported(t *testing.T) {
	def := builtinDef(t, "claude")
	cfg := folderCfg(t)
	p := &profile.Profile{Harness: "claude", Name: "tok", AuthKind: "auth-token", Share: profile.ShareContent,
		AuthRefs: map[string]secret.Reference{"ANTHROPIC_AUTH_TOKEN": mustRef(t, "env:ANTHROPIC_AUTH_TOKEN")}}
	l := mustLaunch(t, p, def, cfg, wrapper.RenderOptions{}, []string{"ANTHROPIC_AUTH_TOKEN=tok-from-shell"})
	if l.env["ANTHROPIC_AUTH_TOKEN"] != "tok-from-shell" {
		t.Errorf("cleared-source key not exported from the captured source: %q", l.env["ANTHROPIC_AUTH_TOKEN"])
	}
}

func TestWrapperCleanupInherited(t *testing.T) {
	def := builtinDef(t, "claude")
	cfg := folderCfg(t)
	p := &profile.Profile{Harness: "claude", Name: "private", AuthKind: "oauth-login", Share: profile.ShareContent}
	// Started from a shell inside another (or even the same) profile's session.
	l := mustLaunch(t, p, def, cfg, wrapper.RenderOptions{}, []string{
		"APROFILE_NAME=private", "APROFILE_HARNESS=claude",
		"APROFILE_SET_VARS=FOO ANTHROPIC_DEFAULT_HAIKU_MODEL bad-name PATH HOME CLAUDE_CONFIG_DIR APROFILE_LABEL APROFILE_TOKEN 1X *",
		"FOO=1", "ANTHROPIC_DEFAULT_HAIKU_MODEL=leak", "ANTHROPIC_MODEL=leak", "CLAUDE_CODE_SUBAGENT_MODEL=leak",
		"APROFILE_TOKEN=leak", "HOME=/home/me",
	})
	// APROFILE_TOKEN is a profile's own variable, not a contract variable: only the
	// contract variables are protected (review finding: the APROFILE_* prefix let it
	// leak into another profile's launch).
	for _, v := range []string{"FOO", "ANTHROPIC_DEFAULT_HAIKU_MODEL", "ANTHROPIC_MODEL", "CLAUDE_CODE_SUBAGENT_MODEL", "APROFILE_TOKEN"} {
		if _, ok := l.env[v]; ok {
			t.Errorf("%s leaked into the launch", v)
		}
	}
	envr, err := wrapper.Environ(p, def, cfg, wrapper.RenderOptions{}, []string{
		"APROFILE_SET_VARS=APROFILE_TOKEN PATH APROFILE_LABEL", "APROFILE_TOKEN=leak", "PATH=/bin",
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := "\n" + strings.Join(envr, "\n") + "\n"
	if strings.Contains(joined, "\nAPROFILE_TOKEN=") || !strings.Contains(joined, "\nPATH=/bin\n") {
		t.Errorf("Environ applies a different protection rule than the wrapper:\n%s", joined)
	}
	if l.env["HOME"] != "/home/me" || l.env["PATH"] == "" || l.env["CLAUDE_CONFIG_DIR"] != cfg || l.env["APROFILE_LABEL"] != "private" {
		t.Errorf("protected variables lost: HOME=%q PATH=%q CLAUDE_CONFIG_DIR=%q", l.env["HOME"], l.env["PATH"], l.env["CLAUDE_CONFIG_DIR"])
	}
}

func TestWrapperHookEnvUnconditionalAndListed(t *testing.T) {
	def := builtinDef(t, "claude")
	p := &profile.Profile{Harness: "claude", Name: "h", AuthKind: "oauth-login", Model: "opus"} // share all, file absent: wrapper-carried
	opts := wrapper.RenderOptions{HookEnv: []harness.EnvVar{{Name: "HOOK_REGION", Value: "eu"}}}
	l := mustLaunch(t, p, def, t.TempDir(), opts, []string{"HOOK_REGION=mine", "ANTHROPIC_MODEL=mine"})
	if l.env["HOOK_REGION"] != "eu" || l.env["ANTHROPIC_MODEL"] != "opus" {
		t.Errorf("wrapper-carried values not set unconditionally: HOOK_REGION=%q model=%q", l.env["HOOK_REGION"], l.env["ANTHROPIC_MODEL"])
	}
	if l.env["APROFILE_SET_VARS"] != "ANTHROPIC_MODEL HOOK_REGION" {
		t.Errorf("APROFILE_SET_VARS = %q", l.env["APROFILE_SET_VARS"])
	}
}

func TestWrapperCodexModelArg(t *testing.T) {
	def := builtinDef(t, "codex")
	for model, want := range map[string]string{"1.5": `model="1.5"`, `a"b\c`: `model="a\"b\\c"`} {
		p := &profile.Profile{Harness: "codex", Name: "glm", AuthKind: "chatgpt-login", Model: model}
		s, _ := wrapper.RenderWith(p, def, t.TempDir(), wrapper.RenderOptions{})
		if !strings.Contains(s, "exec 'codex' '-c' "+secret.ShellQuote(want)+` "$@"`) {
			t.Errorf("exec line for %q:\n%s", model, s)
		}
		l := mustLaunch(t, p, def, t.TempDir(), wrapper.RenderOptions{}, nil, "-m", "x")
		if strings.Join(l.args, "|") != "-c|"+want+"|-m|x" {
			t.Errorf("args for %q = %q", model, l.args)
		}
	}
	p := &profile.Profile{Harness: "codex", Name: "nomodel", AuthKind: "chatgpt-login"}
	s, _ := wrapper.RenderWith(p, def, t.TempDir(), wrapper.RenderOptions{})
	if !strings.Contains(s, `exec 'codex' "$@"`) {
		t.Errorf("a profile without a model passes no model arg:\n%s", s)
	}
}

func TestWrapperCodexFolderKeyNotExported(t *testing.T) {
	def := builtinDef(t, "codex")
	cfg := t.TempDir()
	toml := "model_provider = \"gw\"\n[model_providers.gw]\nbase_url = \"http://gw\"\nenv_key = \"OPENAI_API_KEY\"\n"
	if err := os.WriteFile(filepath.Join(cfg, "config.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &profile.Profile{Harness: "codex", Name: "glm", AuthKind: "api-key", Share: profile.ShareContent,
		AuthRefs: map[string]secret.Reference{"OPENAI_API_KEY": mustRef(t, "file:"+keyFile(t, "sk-x\n"))}}
	l := mustLaunch(t, p, def, cfg, wrapper.RenderOptions{}, []string{"OPENAI_API_KEY=leak"})
	if _, ok := l.env["OPENAI_API_KEY"]; ok {
		t.Error("OPENAI_API_KEY exported although the folder carries the key")
	}
	// Without a provider in config.toml the wrapper carries the key.
	cfg2 := t.TempDir()
	l = mustLaunch(t, p, def, cfg2, wrapper.RenderOptions{}, nil)
	if l.env["OPENAI_API_KEY"] != "sk-x" {
		t.Errorf("wrapper-carried key = %q", l.env["OPENAI_API_KEY"])
	}
}

// TestEnvironMatchesFolderWrapper extends the Environ guard to folder-carried
// profiles and inherited sessions.
func TestEnvironMatchesFolderWrapper(t *testing.T) {
	def := builtinDef(t, "claude")
	cfg := folderCfg(t)
	key := keyFile(t, "sk-test\n")
	profiles := map[string]*profile.Profile{
		"folder": folderProfile(t, key),
		"share-all": {Harness: "claude", Name: "all", AuthKind: "api-key", Share: profile.ShareAll,
			AuthRefs: map[string]secret.Reference{"ANTHROPIC_API_KEY": mustRef(t, "file:"+key)}, Model: "m",
			ExtraEnv: []profile.ExtraEnvVar{{Name: "CLOUD_ML_REGION", Value: "env:CLOUD_ML_REGION"}}},
		"token": {Harness: "claude", Name: "tok", AuthKind: "auth-token", Share: profile.ShareContent,
			AuthRefs: map[string]secret.Reference{"ANTHROPIC_AUTH_TOKEN": mustRef(t, "env:ANTHROPIC_AUTH_TOKEN")}},
	}
	callers := [][]string{
		{"CLOUD_ML_REGION=r", "ANTHROPIC_AUTH_TOKEN=t", "ANTHROPIC_MODEL=caller"},
		{"CLOUD_ML_REGION=r", "ANTHROPIC_AUTH_TOKEN=t", "APROFILE_NAME=x", "APROFILE_SET_VARS=FOO PATH ANTHROPIC_DEFAULT_OPUS_MODEL", "FOO=1", "ANTHROPIC_DEFAULT_OPUS_MODEL=o"},
	}
	for name, p := range profiles {
		pcfg := cfg
		if p.Share == profile.ShareAll {
			pcfg = t.TempDir()
		}
		for i, caller := range callers {
			full := append([]string{"PATH=" + stubBin(t, "claude") + ":/usr/bin:/bin"}, caller...)
			got := mustLaunch(t, p, def, pcfg, wrapper.RenderOptions{}, caller).env
			env, err := wrapper.Environ(p, def, pcfg, wrapper.RenderOptions{}, full)
			if err != nil {
				t.Fatalf("%s/%d: Environ: %v", name, i, err)
			}
			want := toMap(env)
			for _, shellOnly := range []string{"PWD", "OLDPWD", "SHLVL", "_", "PATH"} {
				delete(got, shellOnly)
				delete(want, shellOnly)
			}
			var diffs []string
			for k, v := range want {
				if g, ok := got[k]; !ok || g != v {
					diffs = append(diffs, k+": Environ="+v+" wrapper="+g)
				}
			}
			for k, v := range got {
				if _, ok := want[k]; !ok {
					diffs = append(diffs, k+": only in wrapper ("+v+")")
				}
			}
			sort.Strings(diffs)
			if len(diffs) > 0 {
				t.Errorf("%s/%d: Environ differs from the wrapper:\n  %s", name, i, strings.Join(diffs, "\n  "))
			}
		}
	}
}

// Checkpoint 2 finding 5: Environ resolves a file: key like the shell's
// `IFS= read -r` (a carriage return is kept), and a folder-carried env: key's source
// survives a hostile inherited APROFILE_SET_VARS.
func TestEnvironMatchesShellForCRLFAndKeySource(t *testing.T) {
	def := builtinDef(t, "claude")
	key := keyFile(t, "sk-crlf\r\n")
	p := &profile.Profile{Harness: "claude", Name: "all", AuthKind: "api-key", Share: profile.ShareAll,
		AuthRefs: map[string]secret.Reference{"ANTHROPIC_API_KEY": mustRef(t, "file:"+key)}}
	cfg := t.TempDir()
	got := mustLaunch(t, p, def, cfg, wrapper.RenderOptions{}, nil).env
	env, err := wrapper.Environ(p, def, cfg, wrapper.RenderOptions{}, []string{"PATH=/usr/bin:/bin"})
	if err != nil {
		t.Fatal(err)
	}
	if want := toMap(env)["ANTHROPIC_API_KEY"]; want != "sk-crlf\r" || got["ANTHROPIC_API_KEY"] != want {
		t.Errorf("Environ=%q wrapper=%q, want %q", want, got["ANTHROPIC_API_KEY"], "sk-crlf\r")
	}

	fp := &profile.Profile{Harness: "claude", Name: "envkey", AuthKind: "api-key", Share: profile.ShareContent,
		AuthRefs: map[string]secret.Reference{"ANTHROPIC_API_KEY": mustRef(t, "env:MY_KEY")}}
	fcfg := folderCfg(t)
	caller := []string{"MY_KEY=k", "APROFILE_SET_VARS=MY_KEY"}
	l := mustLaunch(t, fp, def, fcfg, wrapper.RenderOptions{}, caller)
	if l.env["MY_KEY"] != "k" {
		t.Errorf("the folder key's source was cleared: %q", l.env["MY_KEY"])
	}
	env, _ = wrapper.Environ(fp, def, fcfg, wrapper.RenderOptions{}, append([]string{"PATH=/usr/bin:/bin"}, caller...))
	if toMap(env)["MY_KEY"] != "k" {
		t.Error("Environ cleared the folder key's source")
	}
}
