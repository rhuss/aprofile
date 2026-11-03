package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/testutil"
)

// hdEnv is a hermetic environment for the harness-definition (US5) integration
// test: a temp HOME/XDG root, the built aprofile binary, a fake harness binary and
// an exec hook on PATH, and a populated harnesses.d.
type hdEnv struct {
	t          *testing.T
	root       string
	aprofile   string
	configHome string
	dataHome   string
	vars       []string
}

func newHDEnv(t *testing.T) *hdEnv {
	t.Helper()
	root := t.TempDir()
	configHome := filepath.Join(root, ".config")
	dataHome := filepath.Join(root, ".local", "share")
	stateHome := filepath.Join(root, ".local", "state")
	scripts := filepath.Join(root, "scripts")
	for _, d := range []string{configHome, dataHome, stateHome, scripts} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	aprofileBin := testutil.BuildBinary(t, "github.com/cc-deck/aprofile/cmd/aprofile", "aprofile")

	// The fake harness binary: prints a marker and its environment so the test can
	// assert the wrapper exec'd it with the hook-provided variable.
	writeExecFile(t, filepath.Join(scripts, "fakebin"),
		"#!/bin/sh\necho FAKEBIN_RAN\nenv\n")
	// The exec hook: answers `env` with a variable, returns 64 (unsupported) for
	// every other operation (so pre-launch is a no-op).
	writeExecFile(t, filepath.Join(scripts, "aprofile-harness-fake"),
		"#!/bin/sh\ncase \"$1\" in\n  env) cat >/dev/null; echo 'FAKE_HOOK_VAR=hello' ;;\n  *) exit 64 ;;\nesac\n")

	binDir := filepath.Join(dataHome, "aprofile", "bin")
	path := strings.Join([]string{scripts, binDir, "/usr/bin", "/bin"}, string(os.PathListSeparator))
	vars := []string{
		"HOME=" + root,
		"XDG_CONFIG_HOME=" + configHome,
		"XDG_DATA_HOME=" + dataHome,
		"XDG_STATE_HOME=" + stateHome,
		"PATH=" + path,
		"SHELL=/bin/sh",
		"ZDOTDIR=" + root,
	}
	return &hdEnv{t: t, root: root, aprofile: aprofileBin, configHome: configHome, dataHome: dataHome, vars: vars}
}

func (e *hdEnv) harnessesDir() string {
	return filepath.Join(e.configHome, "aprofile", "harnesses.d")
}

func (e *hdEnv) run(args ...string) (string, error) {
	e.t.Helper()
	cmd := exec.Command(e.aprofile, args...)
	cmd.Env = e.vars
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (e *hdEnv) mustRun(args ...string) string {
	e.t.Helper()
	out, err := e.run(args...)
	if err != nil {
		e.t.Fatalf("aprofile %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func (e *hdEnv) runWrapper(name string, args ...string) (string, error) {
	e.t.Helper()
	binDir := filepath.Join(e.dataHome, "aprofile", "bin")
	cmd := exec.Command(filepath.Join(binDir, name), args...)
	cmd.Env = e.vars
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func writeExecFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestHarnessDefinitionScenario7(t *testing.T) {
	e := newHDEnv(t)

	// A data-only definition for a brand-new harness (SC-004: no core change).
	testutil.WriteFile(t, filepath.Join(e.harnessesDir(), "fake.yaml"),
		"name: fake\nbinary: fakebin\nwrapper_prefix: fake\nconfig_env:\n  FAKE_CONFIG_DIR: \".\"\nexec_hook:\n  operations: [env, pre-launch]\n")
	// A one-field override of a built-in.
	testutil.WriteFile(t, filepath.Join(e.harnessesDir(), "claude-override.yaml"),
		"name: claude\ndefault_config_location: \"/custom/location\"\n")
	// An invalid file: isolated, must not stop the others (FR-012).
	testutil.WriteFile(t, filepath.Join(e.harnessesDir(), "broken.yaml"),
		"name: broken\nbinary: broken\n")

	// Add a profile for the fake harness and sync it. sync reports the invalid
	// definition and exits non-zero (FR-022), but the other harnesses keep working:
	// the fake-demo wrapper is still generated (FR-012 isolation).
	e.mustRun("add", "fake", "demo")
	syncOut, _ := e.run("sync")
	if !strings.Contains(syncOut, "broken.yaml") || !strings.Contains(syncOut, "wrapper_prefix") {
		t.Errorf("sync should name the invalid file and field:\n%s", syncOut)
	}

	// The wrapper exists and includes the exec-hook `env` output.
	wrapperPath := filepath.Join(e.dataHome, "aprofile", "bin", "fake-demo")
	data, err := os.ReadFile(wrapperPath)
	if err != nil {
		t.Fatalf("wrapper not generated: %v", err)
	}
	if !strings.Contains(string(data), "FAKE_HOOK_VAR") {
		t.Errorf("exec-hook env output not baked into wrapper:\n%s", data)
	}

	// Running the wrapper execs the fake binary (SC-004) with the hook variable set,
	// and the exit-64 pre-launch hook is treated as a no-op (the binary still runs).
	out, err := e.runWrapper("fake-demo")
	if err != nil {
		t.Fatalf("wrapper run failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "FAKEBIN_RAN") {
		t.Errorf("fake binary did not run (pre-launch exit 64 should be a no-op):\n%s", out)
	}
	if !strings.Contains(out, "FAKE_HOOK_VAR=hello") {
		t.Errorf("hook variable not present in the launched environment:\n%s", out)
	}
	if !strings.Contains(out, "APROFILE_NAME=demo") {
		t.Errorf("APROFILE contract missing in the launched environment:\n%s", out)
	}

	// harness show reflects the override and marks each field's source.
	showOut := e.mustRun("harness", "show", "claude", "--json")
	var show struct {
		Name                  string            `json:"name"`
		DefaultConfigLocation string            `json:"default_config_location"`
		ModelVar              string            `json:"model_var"`
		Source                map[string]string `json:"source"`
	}
	if err := json.Unmarshal([]byte(showOut), &show); err != nil {
		t.Fatalf("harness show --json not parseable: %v\n%s", err, showOut)
	}
	if show.DefaultConfigLocation != "/custom/location" {
		t.Errorf("override not merged: %q", show.DefaultConfigLocation)
	}
	if show.ModelVar != "ANTHROPIC_MODEL" {
		t.Errorf("built-in field lost after merge: %q", show.ModelVar)
	}
	if !strings.Contains(show.Source["default_config_location"], "claude-override.yaml") {
		t.Errorf("overridden field not marked from file: %q", show.Source["default_config_location"])
	}
	if show.Source["model_var"] != "builtin" {
		t.Errorf("untouched field not marked builtin: %q", show.Source["model_var"])
	}

	// harness list shows every harness and surfaces the invalid-file error naming
	// the file and field, while the others keep working.
	listOut := e.mustRun("harness", "list")
	for _, name := range []string{"claude", "codex", "opencode", "pi", "fake"} {
		if !strings.Contains(listOut, name) {
			t.Errorf("harness list missing %q:\n%s", name, listOut)
		}
	}
	if !strings.Contains(listOut, "broken.yaml") {
		t.Errorf("invalid file not reported by harness list:\n%s", listOut)
	}
}
