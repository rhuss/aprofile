package harness_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
)

// --- T062: login_command ---

func TestBuiltinLoginCommands(t *testing.T) {
	set, err := harness.LoadBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"claude":   {"auth", "login"},
		"codex":    {"login"},
		"opencode": {"auth", "login"},
		// pi has no login subcommand: sign-in is the /login command inside a session,
		// so the declared command is the bare binary.
		"pi": {},
	}
	for name, args := range want {
		d, ok := set.Get(name)
		if !ok {
			t.Fatalf("%s not loaded", name)
		}
		if d.LoginCommand == nil {
			t.Fatalf("%s: login_command not declared", name)
		}
		if len(d.LoginCommand.Args) != len(args) || (len(args) > 0 && !reflect.DeepEqual(d.LoginCommand.Args, args)) {
			t.Errorf("%s: login_command.args = %v, want %v", name, d.LoginCommand.Args, args)
		}
		if got := d.LoginArgv(); got[0] != d.Binary || len(got) != len(args)+1 {
			t.Errorf("%s: LoginArgv = %v", name, got)
		}
	}
	pi, _ := set.Get("pi")
	if !strings.Contains(pi.LoginCommand.Note, "/login") {
		t.Errorf("pi login_command.note should tell the user to run /login, got %q", pi.LoginCommand.Note)
	}
}

func TestLoginCommandValidation(t *testing.T) {
	base := "name: lc\nbinary: lc\nwrapper_prefix: lc\nconfig_env:\n  LC_DIR: \".\"\n"
	cases := map[string]string{
		"empty arg":   base + "login_command:\n  args: [auth, \"\"]\n",
		"newline arg": base + "login_command:\n  args: [\"a\\nb\"]\n",
	}
	for name, src := range cases {
		set := harness.LoadFromBytes(map[string][]byte{"lc.yaml": []byte(src)})
		if _, ok := set.Get("lc"); ok {
			t.Errorf("%s: definition should be rejected", name)
			continue
		}
		errs := set.Errors()
		if len(errs) != 1 || errs[0].Field != "login_command" {
			t.Errorf("%s: want one login_command error, got %v", name, errs)
		}
	}
	// No login_command at all is valid (login then needs an exec hook).
	set := harness.LoadFromBytes(map[string][]byte{"lc.yaml": []byte(base)})
	d, ok := set.Get("lc")
	if !ok || d.LoginCommand != nil || d.LoginArgv() != nil {
		t.Errorf("definition without login_command: ok=%v errs=%v", ok, set.Errors())
	}
}

func TestOverrideLoginCommand(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "claude.yaml")
	writeExec(t, file, "name: claude\nlogin_command:\n  args: [login, --sso]\n")
	set, _ := harness.Load(dir)
	d, _ := set.Get("claude")
	if !reflect.DeepEqual(d.LoginCommand.Args, []string{"login", "--sso"}) {
		t.Errorf("override not applied: %v", d.LoginCommand.Args)
	}
	if d.Source["login_command"] != file {
		t.Errorf("login_command source = %q, want %q", d.Source["login_command"], file)
	}

	// An explicit null removes the inherited login command.
	writeExec(t, file, "name: claude\nlogin_command: null\n")
	set, _ = harness.Load(dir)
	d, _ = set.Get("claude")
	if d.LoginCommand != nil {
		t.Errorf("login_command: null should remove the built-in command, got %+v", d.LoginCommand)
	}
}

// --- T063: auth_clears ---

var claudeProviderVars = []string{
	"CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_FOUNDRY",
	"ANTHROPIC_VERTEX_PROJECT_ID", "CLOUD_ML_REGION", "ANTHROPIC_BEDROCK_BASE_URL",
	"ANTHROPIC_VERTEX_BASE_URL", "AWS_BEARER_TOKEN_BEDROCK",
	"CLAUDE_CODE_SKIP_VERTEX_AUTH", "CLAUDE_CODE_SKIP_BEDROCK_AUTH",
}

func TestClaudeAuthClears(t *testing.T) {
	set, _ := harness.LoadBuiltins()
	d, _ := set.Get("claude")
	for _, v := range claudeProviderVars {
		if !contains(d.AuthClears, v) {
			t.Errorf("claude auth_clears missing %s", v)
		}
	}
	// The unset list is every auth kind's variables plus the clears, sorted, unique.
	unset := d.UnsetAuthVars()
	for _, v := range append([]string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN"}, claudeProviderVars...) {
		if !contains(unset, v) {
			t.Errorf("UnsetAuthVars missing %s: %v", v, unset)
		}
	}
	seen := map[string]bool{}
	for i, v := range unset {
		if seen[v] {
			t.Errorf("UnsetAuthVars has duplicate %s", v)
		}
		seen[v] = true
		if i > 0 && unset[i-1] > v {
			t.Errorf("UnsetAuthVars not sorted: %v", unset)
		}
	}
	// Harnesses without clears unset only their auth vars.
	codex, _ := set.Get("codex")
	if !reflect.DeepEqual(codex.UnsetAuthVars(), codex.AllAuthVars()) {
		t.Errorf("codex UnsetAuthVars = %v, want %v", codex.UnsetAuthVars(), codex.AllAuthVars())
	}
}

func TestAuthClearsValidation(t *testing.T) {
	base := "name: ac\nbinary: ac\nwrapper_prefix: ac\nconfig_env:\n  AC_DIR: \".\"\nmodel_var: AC_MODEL\nendpoint_var: AC_URL\n"
	cases := map[string]string{
		"bad name":   base + "auth_clears: [\"1BAD\"]\n",
		"config_env": base + "auth_clears: [AC_DIR]\n",
		"contract":   base + "auth_clears: [APROFILE_NAME]\n",
	}
	for name, src := range cases {
		set := harness.LoadFromBytes(map[string][]byte{"ac.yaml": []byte(src)})
		if _, ok := set.Get("ac"); ok {
			t.Errorf("%s: definition should be rejected", name)
			continue
		}
		if errs := set.Errors(); len(errs) != 1 || errs[0].Field != "auth_clears" {
			t.Errorf("%s: want one auth_clears error, got %v", name, errs)
		}
	}
	// A clear that an auth kind also sets is valid: the kind still sets it.
	ok := base + "auth_clears: [AC_USE_CLOUD]\nauth_kinds:\n  - name: cloud\n    vars: { AC_USE_CLOUD: ref }\n"
	set := harness.LoadFromBytes(map[string][]byte{"ac.yaml": []byte(ok)})
	if _, found := set.Get("ac"); !found {
		t.Errorf("clear shared with a kind's var should be valid: %v", set.Errors())
	}
}

func TestEnvHookCannotSetClearedVar(t *testing.T) {
	dir := t.TempDir()
	writeExec(t, filepath.Join(dir, "aprofile-harness-ach"), "#!/bin/sh\necho AC_USE_CLOUD=1\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	src := "name: ach\nbinary: ach\nwrapper_prefix: ach\nconfig_env:\n  ACH_DIR: \".\"\nauth_clears: [AC_USE_CLOUD]\nexec_hook:\n  operations: [env]\n"
	set := harness.LoadFromBytes(map[string][]byte{"ach.yaml": []byte(src)})
	d, ok := set.Get("ach")
	if !ok {
		t.Fatalf("load: %v", set.Errors())
	}
	_, err := harness.RunEnvHook(d, harness.HookContext{Harness: "ach", Profile: "p", ConfigDir: "/x"})
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Errorf("hook setting a cleared var must be rejected as reserved, got %v", err)
	}
}

// --- T062: the exec hook login operation ---

func loginHookDef(t *testing.T, script string) *harness.Definition {
	t.Helper()
	dir := t.TempDir()
	writeExec(t, filepath.Join(dir, "aprofile-harness-lh"), script)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	src := "name: lh\nbinary: lh\nwrapper_prefix: lh\nconfig_env:\n  LH_DIR: \".\"\nexec_hook:\n  operations: [login]\n"
	set := harness.LoadFromBytes(map[string][]byte{"lh.yaml": []byte(src)})
	d, ok := set.Get("lh")
	if !ok {
		t.Fatalf("load: %v", set.Errors())
	}
	return d
}

func TestRunLoginHookPassesThrough(t *testing.T) {
	d := loginHookDef(t, "#!/bin/sh\necho \"op=$1\"\ncat\necho \"env=$LOGIN_MARK\"\necho \"to-stderr\" >&2\n")
	var out, errOut bytes.Buffer
	ctx := harness.HookContext{Harness: "lh", Profile: "work", ConfigDir: "/cfg", AuthKind: "login"}
	err := harness.RunLoginHook(d, ctx, []string{"PATH=" + os.Getenv("PATH"), "LOGIN_MARK=yes"}, &out, &errOut)
	if err != nil {
		t.Fatalf("RunLoginHook: %v", err)
	}
	// Output is not parsed as key=value: it goes straight to the terminal.
	for _, want := range []string{"op=login", "harness=lh", "profile=work", "config_dir=/cfg", "auth_kind=login", "env=yes"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("hook stdout missing %q:\n%s", want, out.String())
		}
	}
	if !strings.Contains(errOut.String(), "to-stderr") {
		t.Errorf("hook stderr not passed through: %q", errOut.String())
	}
}

func TestRunLoginHookFailures(t *testing.T) {
	d := loginHookDef(t, "#!/bin/sh\nexit 64\n")
	err := harness.RunLoginHook(d, harness.HookContext{Harness: "lh"}, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if !errors.Is(err, harness.ErrHookUnsupported) {
		t.Errorf("exit 64: want ErrHookUnsupported, got %v", err)
	}

	d = loginHookDef(t, "#!/bin/sh\nexit 3\n")
	err = harness.RunLoginHook(d, harness.HookContext{Harness: "lh"}, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || errors.Is(err, harness.ErrHookUnsupported) || !strings.Contains(err.Error(), "login") {
		t.Errorf("exit 3: want an error naming the operation, got %v", err)
	}

	// Not declared: treated as unsupported, the hook is never called.
	set, _ := harness.LoadBuiltins()
	claude, _ := set.Get("claude")
	if err := harness.RunLoginHook(claude, harness.HookContext{Harness: "claude"}, nil, &bytes.Buffer{}, &bytes.Buffer{}); !errors.Is(err, harness.ErrHookUnsupported) {
		t.Errorf("undeclared login op: want ErrHookUnsupported, got %v", err)
	}
}

func TestRunLoginHookMissingExecutable(t *testing.T) {
	src := "name: nohook\nbinary: nohook\nwrapper_prefix: nohook\nconfig_env:\n  NH_DIR: \".\"\nexec_hook:\n  operations: [login]\n"
	set := harness.LoadFromBytes(map[string][]byte{"nh.yaml": []byte(src)})
	d, _ := set.Get("nohook")
	t.Setenv("PATH", t.TempDir())
	err := harness.RunLoginHook(d, harness.HookContext{Harness: "nohook"}, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if !errors.Is(err, harness.ErrHookNotFound) {
		t.Errorf("missing hook executable: want ErrHookNotFound, got %v", err)
	}
}
