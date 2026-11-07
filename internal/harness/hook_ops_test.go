package harness_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
)

// installHook puts an aprofile-harness-<name> script first on PATH.
func installHook(t *testing.T, name, script string) {
	t.Helper()
	dir := t.TempDir()
	writeExec(t, filepath.Join(dir, "aprofile-harness-"+name), script)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/usr/bin:/bin")
}

// hideHooks leaves only the system dirs on PATH, so no aprofile-harness-* resolves.
func hideHooks(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir()+string(os.PathListSeparator)+"/usr/bin:/bin")
}

func hookDef(ops ...string) *harness.Definition {
	return &harness.Definition{Name: "fakeops", ExecHook: &harness.ExecHook{Operations: ops}}
}

var hookCtx = harness.HookContext{Harness: "fakeops", Profile: "demo", ConfigDir: "/d", AuthKind: "login"}

// T076: a declared env operation whose executable is missing is an error, not a
// silent no-op (FR-011), and the error names the hook and the operation.
func TestRunEnvHookMissingExecutableIsError(t *testing.T) {
	hideHooks(t)
	_, err := harness.RunEnvHook(hookDef("env"), hookCtx)
	if err == nil {
		t.Fatal("a declared env hook that is not installed must be an error")
	}
	if !errors.Is(err, harness.ErrHookNotFound) {
		t.Errorf("error should wrap ErrHookNotFound: %v", err)
	}
	if !strings.Contains(err.Error(), "aprofile-harness-fakeops") || !strings.Contains(err.Error(), "env") {
		t.Errorf("error should name the hook executable and operation: %v", err)
	}
}

// A definition that declares no exec hook (or not the env operation) is unaffected
// by a missing executable.
func TestRunEnvHookUndeclaredStaysNoOp(t *testing.T) {
	hideHooks(t)
	for _, def := range []*harness.Definition{{Name: "fakeops"}, hookDef("auth-status")} {
		vars, err := harness.RunEnvHook(def, hookCtx)
		if err != nil || vars != nil {
			t.Errorf("undeclared env: vars=%v err=%v", vars, err)
		}
	}
}

// T066: write-config reports changed=true|false; exit 64 is a no-op; the input
// carries auth_kind/model/endpoint.
func TestRunWriteConfigHook(t *testing.T) {
	dir := t.TempDir()
	inFile := filepath.Join(dir, "stdin")
	installHook(t, "fakeops", "#!/bin/sh\ncat >"+inFile+"\necho \"changed=$WC_CHANGED\"\n")

	ctx := hookCtx
	ctx.Model, ctx.Endpoint = "m1", "https://e"
	t.Setenv("WC_CHANGED", "true")
	changed, err := harness.RunWriteConfigHook(hookDef("write-config"), ctx)
	if err != nil || !changed {
		t.Fatalf("changed=true: changed=%v err=%v", changed, err)
	}
	in, _ := os.ReadFile(inFile)
	for _, want := range []string{"harness=fakeops", "profile=demo", "config_dir=/d", "auth_kind=login", "model=m1", "endpoint=https://e"} {
		if !strings.Contains(string(in), want+"\n") {
			t.Errorf("write-config input lacks %q:\n%s", want, in)
		}
	}

	t.Setenv("WC_CHANGED", "false")
	if changed, err := harness.RunWriteConfigHook(hookDef("write-config"), ctx); err != nil || changed {
		t.Errorf("changed=false: changed=%v err=%v", changed, err)
	}

	t.Setenv("WC_CHANGED", "maybe")
	if _, err := harness.RunWriteConfigHook(hookDef("write-config"), ctx); err == nil || !strings.Contains(err.Error(), "write-config") {
		t.Errorf("an invalid changed value must be an error naming the operation, got %v", err)
	}
}

func TestRunWriteConfigHookMissingChangedIsError(t *testing.T) {
	installHook(t, "fakeops", "#!/bin/sh\ncat >/dev/null\n")
	if _, err := harness.RunWriteConfigHook(hookDef("write-config"), hookCtx); err == nil || !strings.Contains(err.Error(), "changed=") {
		t.Errorf("output without changed= must be an error, got %v", err)
	}
}

func TestRunWriteConfigHookUnsupportedAndUndeclared(t *testing.T) {
	installHook(t, "fakeops", "#!/bin/sh\nexit 64\n")
	if changed, err := harness.RunWriteConfigHook(hookDef("write-config"), hookCtx); err != nil || changed {
		t.Errorf("exit 64: changed=%v err=%v", changed, err)
	}
	// Not declared: the hook is never called (it would fail here).
	installHook(t, "fakeops", "#!/bin/sh\nexit 3\n")
	if changed, err := harness.RunWriteConfigHook(hookDef("env"), hookCtx); err != nil || changed {
		t.Errorf("undeclared: changed=%v err=%v", changed, err)
	}
}

// write-config is optional for the launch: a missing executable is reported as
// ErrHookNotFound so sync can report it without failing the profile.
func TestRunWriteConfigHookMissingExecutable(t *testing.T) {
	hideHooks(t)
	_, err := harness.RunWriteConfigHook(hookDef("write-config"), hookCtx)
	if !errors.Is(err, harness.ErrHookNotFound) {
		t.Errorf("want ErrHookNotFound, got %v", err)
	}
}

// auth-status with a missing executable stays a non-error, but says so.
func TestRunAuthStatusHookMissingExecutable(t *testing.T) {
	hideHooks(t)
	st, err := harness.RunAuthStatusHook(hookDef("auth-status"), hookCtx)
	if err != nil {
		t.Fatalf("auth-status with a missing hook must not fail: %v", err)
	}
	if !st.NotInstalled || st.Status != "" {
		t.Errorf("want NotInstalled and no status, got %+v", st)
	}
}

func TestRunAuthStatusHookOK(t *testing.T) {
	installHook(t, "fakeops", "#!/bin/sh\ncat >/dev/null\necho status=ok\necho 'detail=keychain item present'\n")
	st, err := harness.RunAuthStatusHook(hookDef("auth-status"), hookCtx)
	if err != nil || st.Status != "ok" || st.Detail != "keychain item present" || st.NotInstalled {
		t.Errorf("got %+v, %v", st, err)
	}
}

// T071: config_patches are validated at load: a safe relative path, a JSON or
// TOML file, a non-empty merge without null values.
func TestConfigPatchesValidation(t *testing.T) {
	base := "name: pt\nbinary: pt\nwrapper_prefix: pt\nconfig_env:\n  PT_DIR: \".\"\n"
	cases := map[string]string{
		"absolute":  "config_patches:\n  - { file: /etc/x.json, merge: { a: 1 } }\n",
		"dotdot":    "config_patches:\n  - { file: ../x.json, merge: { a: 1 } }\n",
		"no file":   "config_patches:\n  - { merge: { a: 1 } }\n",
		"extension": "config_patches:\n  - { file: x.yaml, merge: { a: 1 } }\n",
		"empty":     "config_patches:\n  - { file: x.json, merge: {} }\n",
		"null":      "config_patches:\n  - { file: x.json, merge: { a: { b: null } } }\n",
	}
	for name, extra := range cases {
		set := harness.LoadFromBytes(map[string][]byte{"pt.yaml": []byte(base + extra)})
		if _, ok := set.Get("pt"); ok {
			t.Errorf("%s: definition should be rejected", name)
			continue
		}
		joined := ""
		for _, e := range set.Errors() {
			joined += e.Error()
		}
		if !strings.Contains(joined, "config_patches") {
			t.Errorf("%s: error should name config_patches: %s", name, joined)
		}
	}
	ok := harness.LoadFromBytes(map[string][]byte{"pt.yaml": []byte(base +
		"config_patches:\n  - { file: settings.json, merge: { a: { b: 1 } } }\n  - { file: sub/config.TOML, merge: { c: x } }\n")})
	if _, found := ok.Get("pt"); !found {
		t.Errorf("valid patches rejected: %v", ok.Errors())
	}
}
