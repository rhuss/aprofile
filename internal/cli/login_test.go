package cli

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/secret"
	"github.com/cc-deck/aprofile/internal/testutil"
)

func builtinDef(t *testing.T, name string) *harness.Definition {
	t.Helper()
	set, err := harness.LoadBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	d, ok := set.Get(name)
	if !ok {
		t.Fatalf("%s not loaded", name)
	}
	return d
}

func envMap(kvs []string) map[string]string {
	m := map[string]string{}
	for _, kv := range kvs {
		if i := strings.IndexByte(kv, '='); i > 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}

// T063: provider switches exported in the caller's shell never reach a login.
func TestLoginEnvClearsProviderSwitches(t *testing.T) {
	def := builtinDef(t, "claude")
	p := &profile.Profile{Harness: "claude", Name: "work", AuthKind: "oauth-login"}
	caller := []string{
		"PATH=/usr/bin", "CLAUDE_CODE_USE_VERTEX=1", "ANTHROPIC_VERTEX_PROJECT_ID=proj",
		"CLOUD_ML_REGION=us-east5", "CLAUDE_CODE_USE_BEDROCK=1", "AWS_BEARER_TOKEN_BEDROCK=tok",
		"ANTHROPIC_API_KEY=leak",
	}
	env, err := loginEnv(p, def, "/cfg/claude/work", nil, caller)
	if err != nil {
		t.Fatal(err)
	}
	m := envMap(env)
	for _, v := range []string{"CLAUDE_CODE_USE_VERTEX", "ANTHROPIC_VERTEX_PROJECT_ID", "CLOUD_ML_REGION", "CLAUDE_CODE_USE_BEDROCK", "AWS_BEARER_TOKEN_BEDROCK", "ANTHROPIC_API_KEY"} {
		if _, has := m[v]; has {
			t.Errorf("login env must not carry %s", v)
		}
	}
	if m["CLAUDE_CONFIG_DIR"] != "/cfg/claude/work" || m["APROFILE_NAME"] != "work" || m["PATH"] != "/usr/bin" {
		t.Errorf("login env missing redirect/contract/PATH: %v", m)
	}
}

// T063: a kind that sets a cleared variable still sets it.
func TestLoginEnvClearsExceptOwnKind(t *testing.T) {
	src := "name: cloudy\nbinary: cloudy\nwrapper_prefix: cloudy\nconfig_env:\n  CLOUDY_DIR: \".\"\n" +
		"auth_clears: [CLOUDY_USE_CLOUD, CLOUDY_REGION]\nauth_kinds:\n  - name: cloud\n    vars: { CLOUDY_USE_CLOUD: ref }\n"
	set := harness.LoadFromBytes(map[string][]byte{"c.yaml": []byte(src)})
	def, ok := set.Get("cloudy")
	if !ok {
		t.Fatal(set.Errors())
	}
	ref, _ := secret.ParseReference("env:MY_CLOUD_FLAG")
	p := &profile.Profile{Harness: "cloudy", Name: "b", AuthKind: "cloud",
		AuthRefs: map[string]secret.Reference{"CLOUDY_USE_CLOUD": ref}}
	t.Setenv("MY_CLOUD_FLAG", "1")
	env, err := loginEnv(p, def, "/cfg", nil, []string{"MY_CLOUD_FLAG=1", "CLOUDY_USE_CLOUD=shell", "CLOUDY_REGION=shell"})
	if err != nil {
		t.Fatal(err)
	}
	m := envMap(env)
	if m["CLOUDY_USE_CLOUD"] != "1" {
		t.Errorf("own kind's var = %q, want 1", m["CLOUDY_USE_CLOUD"])
	}
	if _, has := m["CLOUDY_REGION"]; has {
		t.Errorf("a clear the kind does not set must stay cleared")
	}
}

// T075: login applies the wrapper's nested-session cleanup and exports
// APROFILE_SET_VARS like the wrapper (FR-027).
func TestLoginEnvNestedSessionCleanup(t *testing.T) {
	def := builtinDef(t, "claude")
	p := &profile.Profile{Harness: "claude", Name: "work", AuthKind: "oauth-login",
		Model: "opus", ExtraEnv: []profile.ExtraEnvVar{{Name: "TEAM", Value: "platform", Literal: true}}}
	prev := []string{
		"APROFILE_NAME=other", "APROFILE_HARNESS=claude",
		"APROFILE_SET_VARS=ANTHROPIC_MODEL ANTHROPIC_BASE_URL OTHER_ONLY TEAM",
		"ANTHROPIC_MODEL=other-model", "ANTHROPIC_BASE_URL=https://other", "OTHER_ONLY=x", "TEAM=other-team",
		"UNRELATED=keep",
	}
	env, err := loginEnv(p, def, "/cfg", nil, prev)
	if err != nil {
		t.Fatal(err)
	}
	m := envMap(env)
	if _, has := m["OTHER_ONLY"]; has {
		t.Error("a variable the previous profile set must be dropped")
	}
	if _, has := m["ANTHROPIC_BASE_URL"]; has {
		t.Error("the previous profile's endpoint must be dropped")
	}
	if m["ANTHROPIC_MODEL"] != "opus" || m["TEAM"] != "platform" {
		t.Errorf("own values must replace the previous profile's: model=%q team=%q", m["ANTHROPIC_MODEL"], m["TEAM"])
	}
	if m["UNRELATED"] != "keep" {
		t.Error("unrelated variables must survive")
	}
	if m["APROFILE_SET_VARS"] != "ANTHROPIC_MODEL TEAM" {
		t.Errorf("APROFILE_SET_VARS = %q, want %q", m["APROFILE_SET_VARS"], "ANTHROPIC_MODEL TEAM")
	}
	if m["APROFILE_NAME"] != "work" {
		t.Errorf("APROFILE_NAME = %q", m["APROFILE_NAME"])
	}
}

// T075: within the same profile's session (or outside any), caller values of
// model, endpoint and extra variables win, as in the wrapper.
func TestLoginEnvCallerOverridesInSameProfile(t *testing.T) {
	def := builtinDef(t, "claude")
	p := &profile.Profile{Harness: "claude", Name: "work", AuthKind: "oauth-login", Model: "opus",
		ExtraEnv: []profile.ExtraEnvVar{{Name: "TEAM", Value: "platform", Literal: true}}}
	for _, caller := range [][]string{
		{"APROFILE_NAME=work", "APROFILE_HARNESS=claude", "APROFILE_SET_VARS=TEAM", "ANTHROPIC_MODEL=haiku", "TEAM=mine"},
		{"ANTHROPIC_MODEL=haiku", "TEAM=mine"},
	} {
		env, err := loginEnv(p, def, "/cfg", nil, caller)
		if err != nil {
			t.Fatal(err)
		}
		m := envMap(env)
		if m["ANTHROPIC_MODEL"] != "haiku" || m["TEAM"] != "mine" {
			t.Errorf("caller overrides lost (%v): model=%q team=%q", caller, m["ANTHROPIC_MODEL"], m["TEAM"])
		}
		if m["APROFILE_SET_VARS"] != "ANTHROPIC_MODEL TEAM" {
			t.Errorf("APROFILE_SET_VARS = %q", m["APROFILE_SET_VARS"])
		}
	}
	// An empty caller value counts as unset, like ${VAR:-} in the wrapper.
	env, _ := loginEnv(p, def, "/cfg", nil, []string{"TEAM="})
	if envMap(env)["TEAM"] != "platform" {
		t.Errorf("empty caller value should not win: %v", envMap(env)["TEAM"])
	}
}

// T062: login runs the declared login command with the profile env.
func TestLoginRunsDeclaredCommand(t *testing.T) {
	testutil.HermeticHome(t)
	bin := t.TempDir()
	testutil.WriteFile(t, filepath.Join(bin, "claude"), "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(bin, "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin:/bin")
	t.Setenv("CLAUDE_CODE_USE_VERTEX", "1")

	var gotArgv, gotEnv []string
	orig := execLogin
	execLogin = func(path string, argv, env []string) error {
		gotArgv, gotEnv = argv, env
		return nil
	}
	t.Cleanup(func() { execLogin = orig })

	if out, err := runCLI(t, "add", "claude", "work", "--auth-kind", "oauth-login"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	out, err := runCLI(t, "login", "claude/work")
	if err != nil {
		t.Fatalf("login: %v\n%s", err, out)
	}
	if !reflect.DeepEqual(gotArgv, []string{"claude", "auth", "login"}) {
		t.Errorf("argv = %v, want [claude auth login]", gotArgv)
	}
	m := envMap(gotEnv)
	if !strings.HasSuffix(m["CLAUDE_CONFIG_DIR"], filepath.Join("profiles", "claude", "work")) {
		t.Errorf("CLAUDE_CONFIG_DIR = %q", m["CLAUDE_CONFIG_DIR"])
	}
	if _, has := m["CLAUDE_CODE_USE_VERTEX"]; has {
		t.Error("login must clear CLAUDE_CODE_USE_VERTEX")
	}
}

// T062: a definition with neither a login command nor a login hook fails clearly.
func TestLoginFailsWithoutCommandOrHook(t *testing.T) {
	h := testutil.HermeticHome(t)
	testutil.WriteFile(t, filepath.Join(h.ConfigHome, "aprofile", "harnesses.d", "claude.yaml"),
		"name: claude\nlogin_command: null\n")
	orig := execLogin
	execLogin = func(string, []string, []string) error { return errors.New("must not exec") }
	t.Cleanup(func() { execLogin = orig })

	if out, err := runCLI(t, "add", "claude", "work", "--auth-kind", "oauth-login"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	_, err := runCLI(t, "login", "claude/work")
	if err == nil || !strings.Contains(err.Error(), "login_command") || !strings.Contains(err.Error(), "login operation") {
		t.Errorf("want a clear error naming login_command and the hook login operation, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(h.DataHome, "aprofile", "profiles", "claude", "work", ".claude.json")); statErr == nil {
		t.Error("a failed login must not prepare the config dir")
	}
}
