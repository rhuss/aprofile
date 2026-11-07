package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/testutil"
)

type showCredsJSON struct {
	HasCredentials bool `json:"has_credentials"`
	Credentials    struct {
		Present    bool     `json:"present"`
		Sources    []string `json:"sources"`
		References []struct {
			Variable  string `json:"variable"`
			Reference string `json:"reference"`
			Resolves  bool   `json:"resolves"`
			Problem   string `json:"problem"`
		} `json:"references"`
		AuthStatus *struct {
			Status string `json:"status"`
			Detail string `json:"detail"`
			Error  string `json:"error"`
		} `json:"auth_status"`
	} `json:"credentials"`
}

func showCreds(t *testing.T, id string) (showCredsJSON, string) {
	t.Helper()
	js, err := runCLI(t, "show", id, "--json")
	if err != nil {
		t.Fatalf("show --json: %v\n%s", err, js)
	}
	var sj showCredsJSON
	if err := json.Unmarshal([]byte(js), &sj); err != nil {
		t.Fatalf("decode: %v\n%s", err, js)
	}
	text, err := runCLI(t, "show", id)
	if err != nil {
		t.Fatalf("show: %v\n%s", err, text)
	}
	return sj, text
}

// installCLIHook puts aprofile-harness-<name> on PATH for the test.
func installCLIHook(t *testing.T, name, script string) {
	t.Helper()
	dir := t.TempDir()
	testutil.WriteFile(t, filepath.Join(dir, "aprofile-harness-"+name), script)
	if err := os.Chmod(filepath.Join(dir, "aprofile-harness-"+name), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/usr/bin:/bin")
}

// T066 (FR-039): a reference profile has credentials when its references resolve;
// show says whether each one resolves and never prints the value.
func TestShowReportsReferenceCredentials(t *testing.T) {
	testutil.HermeticHome(t)
	t.Setenv("PATH", "/usr/bin:/bin")
	if out, err := runCLI(t, "add", "claude", "work", "--auth-kind", "oauth-token", "--auth-key", "env:WORK_TOKEN_T066"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	t.Setenv("WORK_TOKEN_T066", "")
	sj, text := showCreds(t, "claude/work")
	if sj.HasCredentials || sj.Credentials.Present || len(sj.Credentials.References) != 1 || sj.Credentials.References[0].Resolves {
		t.Errorf("an unset reference is not a credential: %+v", sj)
	}
	if sj.Credentials.References[0].Problem == "" || !strings.Contains(text, "does not resolve") {
		t.Errorf("show should say why the reference does not resolve:\n%s", text)
	}

	t.Setenv("WORK_TOKEN_T066", "secret-value-never-printed")
	sj, text = showCreds(t, "claude/work")
	if !sj.HasCredentials || !sj.Credentials.Present || !sj.Credentials.References[0].Resolves {
		t.Errorf("a resolving reference is a credential: %+v", sj)
	}
	if r := sj.Credentials.References[0]; r.Reference != "env:WORK_TOKEN_T066" || r.Variable == "" {
		t.Errorf("reference entry: %+v", r)
	}
	if !strings.Contains(text, "credentials:    present") || !strings.Contains(text, "env:WORK_TOKEN_T066") || !strings.Contains(text, "resolves") {
		t.Errorf("text show should report the resolving reference:\n%s", text)
	}
	js, _ := runCLI(t, "show", "claude/work", "--json")
	if strings.Contains(text+js, "secret-value-never-printed") {
		t.Error("show must never print a resolved value")
	}
	// list --json agrees on has_credentials.
	lj, _ := runCLI(t, "list", "--json")
	if !strings.Contains(lj, `"has_credentials": true`) {
		t.Errorf("list --json should report the resolving reference:\n%s", lj)
	}
}

// T066 (FR-011, FR-039): a Keychain-backed login has no file in the config dir;
// the hook's auth-status tells show the credentials exist.
func TestShowReportsKeychainLoginViaAuthStatus(t *testing.T) {
	h := testutil.HermeticHome(t)
	testutil.WriteFile(t, filepath.Join(h.ConfigHome, "aprofile", "harnesses.d", "claude.yaml"),
		"name: claude\nexec_hook:\n  operations: [auth-status]\n")
	installCLIHook(t, "claude", "#!/bin/sh\ncat >/dev/null\necho status=ok\necho 'detail=keychain'\n")
	if out, err := runCLI(t, "add", "claude", "kc", "--auth-kind", "oauth-login"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	sj, text := showCreds(t, "claude/kc")
	if !sj.HasCredentials || !sj.Credentials.Present || sj.Credentials.AuthStatus == nil || sj.Credentials.AuthStatus.Status != "ok" {
		t.Errorf("auth-status ok means credentials present: %+v", sj)
	}
	if !strings.Contains(strings.Join(sj.Credentials.Sources, ","), "auth-status") {
		t.Errorf("sources should name auth-status: %v", sj.Credentials.Sources)
	}
	if !strings.Contains(text, "auth-status") || !strings.Contains(text, "keychain") {
		t.Errorf("text show should report the hook status:\n%s", text)
	}

	// status=missing: no credentials, and show says so.
	installCLIHook(t, "claude", "#!/bin/sh\ncat >/dev/null\necho status=missing\n")
	sj, _ = showCreds(t, "claude/kc")
	if sj.HasCredentials || sj.Credentials.AuthStatus.Status != "missing" {
		t.Errorf("auth-status missing: %+v", sj)
	}

	// Hook not installed: show still works and says the hook is not installed.
	t.Setenv("PATH", "/usr/bin:/bin")
	sj, text = showCreds(t, "claude/kc")
	if sj.HasCredentials || sj.Credentials.AuthStatus == nil || sj.Credentials.AuthStatus.Status != "not-installed" {
		t.Errorf("missing hook: %+v", sj)
	}
	if !strings.Contains(text, "aprofile-harness-claude") {
		t.Errorf("text should name the missing hook:\n%s", text)
	}

	// A failing hook does not fail show; the error is reported.
	installCLIHook(t, "claude", "#!/bin/sh\nexit 3\n")
	sj, _ = showCreds(t, "claude/kc")
	if sj.Credentials.AuthStatus == nil || sj.Credentials.AuthStatus.Status != "error" || sj.Credentials.AuthStatus.Error == "" {
		t.Errorf("failing hook: %+v", sj.Credentials.AuthStatus)
	}
}

// T066: sync calls write-config after preparing the config dir and honors
// changed=; a dry run does not call it.
func TestSyncCallsWriteConfigHook(t *testing.T) {
	h := testutil.HermeticHome(t)
	testutil.WriteFile(t, filepath.Join(h.ConfigHome, "aprofile", "harnesses.d", "claude.yaml"),
		"name: claude\nexec_hook:\n  operations: [write-config]\n")
	calls := filepath.Join(h.Root, "calls")
	// The hook writes a marker file once and reports changed=true only then.
	installCLIHook(t, "claude", "#!/bin/sh\necho call >>"+calls+"\nin=$(cat)\ndir=$(printf '%s\\n' \"$in\" | sed -n 's/^config_dir=//p')\n"+
		"[ -d \"$dir\" ] || { echo 'config dir not prepared' >&2; exit 5; }\n"+
		"if [ -f \"$dir/hook-marker\" ]; then echo changed=false; else touch \"$dir/hook-marker\"; echo changed=true; fi\n")
	if out, err := runCLI(t, "add", "claude", "wc", "--auth-kind", "oauth-login"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	out, err := runCLI(t, "sync", "--dry-run", "--json")
	if err != nil {
		t.Fatalf("sync --dry-run: %v\n%s", err, out)
	}
	if _, err := os.Stat(calls); err == nil {
		t.Error("a dry run must not call write-config")
	}
	if !strings.Contains(out, `"action": "write-config"`) {
		t.Errorf("the dry-run plan should list write-config:\n%s", out)
	}

	out, err = runCLI(t, "sync")
	if err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}
	if !strings.Contains(out, "write-config") {
		t.Errorf("sync should report the hook's change:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(h.DataHome, "aprofile", "profiles", "claude", "wc", "hook-marker")); err != nil {
		t.Errorf("the hook did not run: %v", err)
	}
	out, err = runCLI(t, "sync")
	if err != nil || !strings.Contains(out, "already up to date (0 changes)") {
		t.Errorf("changed=false must not count as a change: %v\n%s", err, out)
	}

	// A missing executable is reported, not fatal.
	t.Setenv("PATH", "/usr/bin:/bin")
	out, err = runCLI(t, "sync")
	if err != nil || !strings.Contains(out, "aprofile-harness-claude") {
		t.Errorf("a missing write-config hook is a note: %v\n%s", err, out)
	}
	// A failing hook is a problem: sync exits non-zero naming hook and operation.
	installCLIHook(t, "claude", "#!/bin/sh\nexit 3\n")
	out, err = runCLI(t, "sync")
	if err == nil || !strings.Contains(out, "write-config") {
		t.Errorf("a failing write-config hook must fail sync: %v\n%s", err, out)
	}
}

// T076: a declared env operation whose hook is missing fails the profile's sync
// (its wrapper is kept as it was) instead of writing a wrapper without the hook's
// variables.
func TestSyncFailsOnMissingEnvHook(t *testing.T) {
	h := testutil.HermeticHome(t)
	testutil.WriteFile(t, filepath.Join(h.ConfigHome, "aprofile", "harnesses.d", "claude.yaml"),
		"name: claude\nexec_hook:\n  operations: [env]\n")
	t.Setenv("PATH", "/usr/bin:/bin")
	if out, err := runCLI(t, "add", "claude", "envh", "--auth-kind", "oauth-login"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	out, err := runCLI(t, "sync")
	if err == nil || !strings.Contains(out, "aprofile-harness-claude") || !strings.Contains(out, "env") {
		t.Errorf("sync must fail naming the missing env hook: %v\n%s", err, out)
	}
	if _, serr := os.Stat(filepath.Join(h.DataHome, "aprofile", "bin", "claude-envh")); serr == nil {
		t.Error("no wrapper may be written without the env hook's variables")
	}
}

// T071: sync applies config_patches merge-only and reports each applied patch;
// the second sync changes nothing.
func TestSyncAppliesConfigPatches(t *testing.T) {
	h := testutil.HermeticHome(t)
	t.Setenv("PATH", "/usr/bin:/bin")
	testutil.WriteFile(t, filepath.Join(h.Root, ".claude", "settings.json"), `{"theme":"dark","env":{"KEEP":"1"}}`)
	testutil.WriteFile(t, filepath.Join(h.ConfigHome, "aprofile", "harnesses.d", "claude.yaml"),
		"name: claude\nconfig_patches:\n  - file: settings.json\n    merge:\n      env: { APROFILE_PATCHED: \"1\" }\n")
	if out, err := runCLI(t, "add", "claude", "pc", "--auth-kind", "oauth-login", "--share", "content"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	out, err := runCLI(t, "sync")
	if err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}
	file := filepath.Join(h.DataHome, "aprofile", "profiles", "claude", "pc", "settings.json")
	data, _ := os.ReadFile(file)
	if !strings.Contains(string(data), "APROFILE_PATCHED") || !strings.Contains(string(data), "KEEP") || !strings.Contains(string(data), "theme") {
		t.Errorf("patched settings.json should keep the other keys:\n%s", data)
	}
	if !strings.Contains(out, "patch settings.json (set env.APROFILE_PATCHED)") {
		t.Errorf("sync should report the patch:\n%s", out)
	}
	out, err = runCLI(t, "sync")
	if err != nil || !strings.Contains(out, "already up to date (0 changes)") {
		t.Errorf("a second sync must not re-apply the patch: %v\n%s", err, out)
	}
	// The default setup's file was never touched.
	if d, _ := os.ReadFile(filepath.Join(h.Root, ".claude", "settings.json")); strings.Contains(string(d), "APROFILE_PATCHED") {
		t.Error("the default setup must not be patched")
	}
}
