package harness_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cc-deck/aprofile/internal/harness"
)

// writeExec writes an executable script under dir and returns its path.
func writeExec(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

// --- T044: harnesses.d override-merge + source marking ---

func TestOverrideMergeMarksSource(t *testing.T) {
	dir := t.TempDir()
	// Override a single field of the built-in claude definition.
	writeExec(t, filepath.Join(dir, "claude.yaml"),
		"name: claude\ndefault_config_location: \"/custom/location\"\n")

	set, err := harness.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if errs := set.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected load errors: %v", errs)
	}
	d, ok := set.Get("claude")
	if !ok {
		t.Fatal("claude not loaded")
	}
	if d.DefaultConfigLocation != "/custom/location" {
		t.Errorf("override not applied: %q", d.DefaultConfigLocation)
	}
	// The overridden field is sourced from the file; an untouched field stays builtin.
	overrideFile := filepath.Join(dir, "claude.yaml")
	if got := d.Source["default_config_location"]; got != overrideFile {
		t.Errorf("default_config_location source = %q, want %q", got, overrideFile)
	}
	if got := d.Source["model_var"]; got != "builtin" {
		t.Errorf("model_var source = %q, want builtin", got)
	}
	// Untouched built-in fields are still present from the built-in.
	if d.ModelVar != "ANTHROPIC_MODEL" {
		t.Errorf("model_var lost after merge: %q", d.ModelVar)
	}
	if d.Binary != "claude" {
		t.Errorf("binary lost after merge: %q", d.Binary)
	}
}

func TestOverrideMergeMapsMergeByKey(t *testing.T) {
	dir := t.TempDir()
	// config_env is a map: the override adds a key and the built-in key survives.
	writeExec(t, filepath.Join(dir, "claude.yaml"),
		"name: claude\nconfig_env:\n  EXTRA_DIR: \"sub\"\n")

	set, err := harness.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	d, _ := set.Get("claude")
	var haveBuiltin, haveExtra bool
	for _, e := range d.ConfigEnv {
		if e.Var == "CLAUDE_CONFIG_DIR" {
			haveBuiltin = true
		}
		if e.Var == "EXTRA_DIR" && e.Path == "sub" {
			haveExtra = true
		}
	}
	if !haveBuiltin || !haveExtra {
		t.Errorf("maps did not merge by key: builtin=%v extra=%v (%+v)", haveBuiltin, haveExtra, d.ConfigEnv)
	}
}

func TestNewHarnessFromFile(t *testing.T) {
	dir := t.TempDir()
	writeExec(t, filepath.Join(dir, "fake.yaml"),
		"name: fake\nbinary: fakebin\nwrapper_prefix: fake\nconfig_env:\n  FAKE_DIR: \".\"\n")

	set, err := harness.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	d, ok := set.Get("fake")
	if !ok {
		t.Fatal("fake not added")
	}
	file := filepath.Join(dir, "fake.yaml")
	if d.Source["binary"] != file {
		t.Errorf("new harness binary source = %q, want %q", d.Source["binary"], file)
	}
	// Built-ins still present.
	if _, ok := set.Get("claude"); !ok {
		t.Error("built-ins must remain available alongside a new harness")
	}
}

func TestDuplicateNameIsAnError(t *testing.T) {
	dir := t.TempDir()
	writeExec(t, filepath.Join(dir, "a.yaml"),
		"name: dup\nbinary: a\nwrapper_prefix: dupa\nconfig_env:\n  A_DIR: \".\"\n")
	writeExec(t, filepath.Join(dir, "b.yaml"),
		"name: dup\nbinary: b\nwrapper_prefix: dupb\nconfig_env:\n  B_DIR: \".\"\n")

	set, err := harness.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	errs := set.Errors()
	if len(errs) == 0 {
		t.Fatal("expected an error for the duplicate name")
	}
	joined := ""
	for _, e := range errs {
		joined += e.Error() + "\n"
	}
	if !strings.Contains(joined, "a.yaml") || !strings.Contains(joined, "b.yaml") {
		t.Errorf("duplicate error should name both files: %s", joined)
	}
	// Built-ins keep working.
	if _, ok := set.Get("claude"); !ok {
		t.Error("built-ins must keep working despite a duplicate-name error")
	}
}

func TestInvalidOverrideFileIsIsolated(t *testing.T) {
	dir := t.TempDir()
	// A new harness missing config_env is invalid (validation reports the field).
	writeExec(t, filepath.Join(dir, "broken.yaml"),
		"name: broken\nbinary: broken\nwrapper_prefix: broken\n")
	// A valid sibling still loads.
	writeExec(t, filepath.Join(dir, "ok.yaml"),
		"name: okharness\nbinary: ok\nwrapper_prefix: ok\nconfig_env:\n  OK_DIR: \".\"\n")

	set, err := harness.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := set.Get("okharness"); !ok {
		t.Error("valid sibling should load despite an invalid file")
	}
	var named bool
	for _, e := range set.Errors() {
		if strings.Contains(e.File, "broken.yaml") && e.Field == "config_env" {
			named = true
		}
	}
	if !named {
		t.Errorf("invalid file not reported with file+field: %v", set.Errors())
	}
}

func TestLoadWithoutDirKeepsBuiltins(t *testing.T) {
	set, err := harness.Load(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := set.Get("claude"); !ok {
		t.Error("built-ins must load when harnesses.d is absent")
	}
	d, _ := set.Get("claude")
	if d.Source["binary"] != "builtin" {
		t.Errorf("pure built-in binary source = %q, want builtin", d.Source["binary"])
	}
}

// --- T045: exec-hook client ---

func TestHookCallParsesKeyValue(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "hook")
	writeExec(t, bin, "#!/bin/sh\ncat >/dev/null\necho 'FOO=bar'\necho 'BAZ=qux'\n")

	h := harness.NewHookForTest("x", bin, 0)
	res, err := h.Call("env", map[string]string{"harness": "x", "profile": "p", "config_dir": "/d"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if v, _ := res.Get("FOO"); v != "bar" {
		t.Errorf("FOO = %q", v)
	}
	if v, _ := res.Get("BAZ"); v != "qux" {
		t.Errorf("BAZ = %q", v)
	}
}

func TestHookCallExit64IsUnsupported(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "hook")
	writeExec(t, bin, "#!/bin/sh\nexit 64\n")

	h := harness.NewHookForTest("x", bin, 0)
	_, err := h.Call("pre-launch", map[string]string{"harness": "x"})
	if !errors.Is(err, harness.ErrHookUnsupported) {
		t.Errorf("exit 64 should yield ErrHookUnsupported, got %v", err)
	}
}

func TestHookCallTimesOut(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "hook")
	writeExec(t, bin, "#!/bin/sh\nsleep 5\n")

	h := harness.NewHookForTest("x", bin, 150*time.Millisecond)
	start := time.Now()
	_, err := h.Call("env", map[string]string{"harness": "x"})
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("timeout was not enforced (took %s)", time.Since(start))
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error should mention the timeout: %v", err)
	}
}

func TestHookCallRejectsNonKeyValueOutput(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "hook")
	writeExec(t, bin, "#!/bin/sh\necho 'this is not key=value'\necho 'no-equals-here'\n")

	h := harness.NewHookForTest("x", bin, 0)
	_, err := h.Call("env", map[string]string{"harness": "x"})
	if err == nil {
		t.Fatal("expected an error for non key=value output")
	}
	if !strings.Contains(err.Error(), "key=value") {
		t.Errorf("error should mention key=value: %v", err)
	}
}

// --- RunEnvHook wiring (prefix check only) ---

func TestRunEnvHookIncludesVarsAndChecksPrefix(t *testing.T) {
	dir := t.TempDir()
	// Install the hook on PATH as aprofile-harness-fake.
	hookBin := filepath.Join(dir, "aprofile-harness-fake")
	writeExec(t, hookBin, "#!/bin/sh\ncat >/dev/null\necho 'FAKE_HOOK_VAR=hello'\necho 'REF_VAR=env:SOME_VAR'\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	def := &harness.Definition{Name: "fake", ExecHook: &harness.ExecHook{Operations: []string{"env"}}}
	vars, err := harness.RunEnvHook(def, harness.HookContext{Harness: "fake", Profile: "demo", ConfigDir: "/d"})
	if err != nil {
		t.Fatalf("RunEnvHook: %v", err)
	}
	got := map[string]string{}
	for _, v := range vars {
		got[v.Name] = v.Value
	}
	if got["FAKE_HOOK_VAR"] != "hello" {
		t.Errorf("FAKE_HOOK_VAR = %q", got["FAKE_HOOK_VAR"])
	}
	if got["REF_VAR"] != "env:SOME_VAR" {
		t.Errorf("reference should pass through: %q", got["REF_VAR"])
	}
}

func TestRunEnvHookRejectsSecretPrefix(t *testing.T) {
	dir := t.TempDir()
	hookBin := filepath.Join(dir, "aprofile-harness-fake")
	writeExec(t, hookBin, "#!/bin/sh\ncat >/dev/null\necho 'LEAK=sk-abc123'\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	def := &harness.Definition{Name: "fake", ExecHook: &harness.ExecHook{Operations: []string{"env"}}}
	_, err := harness.RunEnvHook(def, harness.HookContext{Harness: "fake", Profile: "demo", ConfigDir: "/d"})
	if err == nil {
		t.Fatal("env hook emitting a secret-prefixed value should be rejected")
	}
}

func TestRunEnvHookNoOpWhenNotDeclared(t *testing.T) {
	def := &harness.Definition{Name: "fake"} // no exec hook
	vars, err := harness.RunEnvHook(def, harness.HookContext{Harness: "fake"})
	if err != nil || vars != nil {
		t.Errorf("expected no-op, got vars=%v err=%v", vars, err)
	}
}
