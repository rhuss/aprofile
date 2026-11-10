package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/testutil"
)

// loginPath puts a stub harness binary `claude` and, when hookScript is not empty,
// an aprofile-harness-claude hook on PATH.
func loginPath(t *testing.T, hookScript string) {
	t.Helper()
	dir := t.TempDir()
	testutil.WriteFile(t, filepath.Join(dir, "claude"), "#!/bin/sh\n")
	if hookScript != "" {
		testutil.WriteFile(t, filepath.Join(dir, "aprofile-harness-claude"), hookScript)
	}
	for _, f := range []string{"claude", "aprofile-harness-claude"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			if err := os.Chmod(filepath.Join(dir, f), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/usr/bin:/bin")
}

// recordLogin swaps execLogin for a recorder and returns a pointer to the env it got.
func recordLogin(t *testing.T) *[]string {
	t.Helper()
	var got []string
	orig := execLogin
	execLogin = func(_ string, _, env []string) error {
		got = env
		return nil
	}
	t.Cleanup(func() { execLogin = orig })
	return &got
}

// M8 (FR-011, FR-039): login runs with the wrapper's environment, which includes
// the exec hook's env variables (set unconditionally, listed in APROFILE_SET_VARS). The hook gets the full input (model, endpoint).
func TestLoginEnvIncludesHookEnv(t *testing.T) {
	h := testutil.HermeticHome(t)
	testutil.WriteFile(t, filepath.Join(h.ConfigHome, "aprofile", "harnesses.d", "claude.yaml"),
		"name: claude\nexec_hook:\n  operations: [env]\n")
	in := filepath.Join(h.Root, "hook-input")
	loginPath(t, "#!/bin/sh\ncat >"+in+"\necho HOOK_REGION=eu-west\necho HOOK_FROM=env:HOOK_SRC\n")
	t.Setenv("HOOK_SRC", "from-shell")
	got := recordLogin(t)

	if out, err := runCLI(t, "add", "claude", "work", "--auth-kind", "oauth-login", "--model", "opus"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	if out, err := runCLI(t, "login", "claude/work"); err != nil {
		t.Fatalf("login: %v\n%s", err, out)
	}
	m := envMap(*got)
	if m["HOOK_REGION"] != "eu-west" || m["HOOK_FROM"] != "from-shell" {
		t.Errorf("login env lacks the hook's variables: HOOK_REGION=%q HOOK_FROM=%q", m["HOOK_REGION"], m["HOOK_FROM"])
	}
	// The folder carries the model (share level content), so it also carries
	// APROFILE_SET_VARS; the login environment does not export it (spec 004 FR-014).
	if _, has := m["APROFILE_SET_VARS"]; has {
		t.Errorf("APROFILE_SET_VARS = %q exported although the folder carries it", m["APROFILE_SET_VARS"])
	}
	if _, has := m["ANTHROPIC_MODEL"]; has {
		t.Error("the folder-carried model must not be in the login environment")
	}
	data, _ := os.ReadFile(in)
	if !strings.Contains(string(data), "model=opus\n") {
		t.Errorf("the env hook should get the profile's model:\n%s", data)
	}

	// At share level all the wrapper carries everything and lists the hook's
	// variables in APROFILE_SET_VARS.
	if out, err := runCLI(t, "add", "claude", "shared", "--auth-kind", "oauth-login", "--model", "opus", "--share", "all"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	if out, err := runCLI(t, "login", "claude/shared"); err != nil {
		t.Fatalf("login: %v\n%s", err, out)
	}
	m = envMap(*got)
	if sv := " " + m["APROFILE_SET_VARS"] + " "; !strings.Contains(sv, " HOOK_REGION ") || !strings.Contains(sv, " HOOK_FROM ") || !strings.Contains(sv, " ANTHROPIC_MODEL ") {
		t.Errorf("APROFILE_SET_VARS = %q, want the model and the hook's variables listed", m["APROFILE_SET_VARS"])
	}
}

// M8: a declared env operation whose hook is missing fails the login like it fails
// sync, instead of logging in with an incomplete environment.
func TestLoginFailsOnMissingEnvHook(t *testing.T) {
	h := testutil.HermeticHome(t)
	testutil.WriteFile(t, filepath.Join(h.ConfigHome, "aprofile", "harnesses.d", "claude.yaml"),
		"name: claude\nexec_hook:\n  operations: [env]\n")
	loginPath(t, "")
	orig := execLogin
	execLogin = func(string, []string, []string) error { return errors.New("must not exec") }
	t.Cleanup(func() { execLogin = orig })

	if out, err := runCLI(t, "add", "claude", "work", "--auth-kind", "oauth-login"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	_, err := runCLI(t, "login", "claude/work")
	if err == nil || !strings.Contains(err.Error(), "aprofile-harness-claude") || !strings.Contains(err.Error(), "env") {
		t.Errorf("login must fail naming the missing env hook, got %v", err)
	}
}

// Corr8: `--env CLOUD_ML_REGION=env:CLOUD_ML_REGION` passes the caller's value of a
// cleared provider variable through, in the login environment as in the wrapper.
func TestLoginEnvPassesClearedVariableThrough(t *testing.T) {
	def := builtinDef(t, "claude")
	p := &profile.Profile{Harness: "claude", Name: "vx", AuthKind: "oauth-login",
		ExtraEnv: []profile.ExtraEnvVar{{Name: "CLOUD_ML_REGION", Value: "env:CLOUD_ML_REGION"}}}
	env, err := loginEnv(p, def, "/cfg", nil, []string{"PATH=/usr/bin", "CLOUD_ML_REGION=us-east5", "CLAUDE_CODE_USE_VERTEX=1"})
	if err != nil {
		t.Fatalf("loginEnv: %v", err)
	}
	m := envMap(env)
	if m["CLOUD_ML_REGION"] != "us-east5" {
		t.Errorf("CLOUD_ML_REGION = %q, want the caller's us-east5", m["CLOUD_ML_REGION"])
	}
	if _, has := m["CLAUDE_CODE_USE_VERTEX"]; has {
		t.Error("a cleared variable the profile does not ask for stays cleared")
	}
}
