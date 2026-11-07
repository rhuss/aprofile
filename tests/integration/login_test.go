package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/testutil"
)

// shellProviderVars are Claude Code provider switches a user's shell may export;
// none may reach a profile's harness (T063, FR-027).
var shellProviderVars = []string{
	"CLAUDE_CODE_USE_VERTEX=1", "ANTHROPIC_VERTEX_PROJECT_ID=proj", "CLOUD_ML_REGION=us-east5",
	"CLAUDE_CODE_USE_BEDROCK=1", "AWS_BEARER_TOKEN_BEDROCK=tok",
}

func assertNoProviderVars(t *testing.T, out string) {
	t.Helper()
	for _, kv := range shellProviderVars {
		name := kv[:strings.IndexByte(kv, '=')]
		if strings.Contains(out, "STUB_ENV: "+name+"=") || strings.Contains(out, "HOOK_ENV: "+name+"=") {
			t.Errorf("%s leaked into the harness:\n%s", name, out)
		}
	}
}

// T062 + T063: aprofile login execs the declared login command (claude auth login)
// with the profile environment, and shell provider switches are cleared there and
// in the wrapper.
func TestLoginRunsHarnessLoginCommand(t *testing.T) {
	e := newEnv(t)
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login")
	e.run(nil, "sync")

	out := e.run(shellProviderVars, "login", "claude/work")
	for _, want := range []string{
		"STUB_ARGS: [auth login]",
		"STUB_ENV: CLAUDE_CONFIG_DIR=" + e.configDir("claude", "work"),
		"STUB_ENV: APROFILE_NAME=work",
		"STUB_ENV: APROFILE_SET_VARS=",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("login output missing %q\n---\n%s", want, out)
		}
	}
	assertNoProviderVars(t, out)

	wout, err := e.runWrapper("claude-work", shellProviderVars)
	if err != nil {
		t.Fatalf("wrapper: %v\n%s", err, wout)
	}
	assertNoProviderVars(t, wout)
}

// hookPath installs an aprofile-harness-claude script in its own directory and
// returns a PATH value that finds it before the stub.
func (e *env) hookPath(script string) string {
	e.t.Helper()
	dir := e.t.TempDir()
	p := filepath.Join(dir, "aprofile-harness-claude")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		e.t.Fatal(err)
	}
	binDir := filepath.Join(e.root, ".local", "share", "aprofile", "bin")
	return "PATH=" + strings.Join([]string{dir, e.stubDir, binDir, "/usr/bin", "/bin"}, string(os.PathListSeparator))
}

// T062: a definition whose exec hook declares login runs the hook instead of the
// login command; exit 64 falls back to the command.
func TestLoginUsesExecHook(t *testing.T) {
	e := newEnv(t)
	testutil.WriteFile(t, filepath.Join(e.root, ".config", "aprofile", "harnesses.d", "claude.yaml"),
		"name: claude\nexec_hook:\n  operations: [login]\n")
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login")

	path := e.hookPath("#!/bin/sh\necho \"HOOK_OP: $1\"\nsed 's/^/HOOK_IN: /'\nenv | sed 's/^/HOOK_ENV: /'\n")
	out := e.run(append([]string{path}, shellProviderVars...), "login", "claude/work")
	for _, want := range []string{
		"HOOK_OP: login",
		"HOOK_IN: auth_kind=oauth-login",
		"HOOK_IN: profile=work",
		"HOOK_IN: config_dir=" + e.configDir("claude", "work"),
		"HOOK_ENV: CLAUDE_CONFIG_DIR=" + e.configDir("claude", "work"),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("hook login output missing %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, "STUB_ARGS") {
		t.Errorf("the login command must not run when the hook handled login:\n%s", out)
	}
	assertNoProviderVars(t, out)

	// Exit 64: not supported, fall back to the declared login command.
	path = e.hookPath("#!/bin/sh\nexit 64\n")
	out = e.run([]string{path}, "login", "claude/work")
	if !strings.Contains(out, "STUB_ARGS: [auth login]") {
		t.Errorf("exit 64 should fall back to the login command:\n%s", out)
	}

	// Any other failure aborts, naming the hook and operation.
	path = e.hookPath("#!/bin/sh\nexit 5\n")
	out, err := e.tryRun([]string{path}, "login", "claude/work")
	if err == nil || !strings.Contains(out, "login") || strings.Contains(out, "STUB_ARGS") {
		t.Errorf("a failing login hook must abort without running the command: err=%v\n%s", err, out)
	}
}

// T062: neither a login command nor a login hook is a clear error.
func TestLoginWithoutFlowFails(t *testing.T) {
	e := newEnv(t)
	testutil.WriteFile(t, filepath.Join(e.root, ".config", "aprofile", "harnesses.d", "claude.yaml"),
		"name: claude\nlogin_command: null\n")
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login")
	out, err := e.tryRun(nil, "login", "claude/work")
	if err == nil || !strings.Contains(out, "login_command") || strings.Contains(out, "STUB_ARGS") {
		t.Errorf("want a clear error and no launch: err=%v\n%s", err, out)
	}
}
