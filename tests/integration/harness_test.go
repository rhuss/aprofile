// Package integration drives the built aprofile binary against a no-op stub
// harness in a hermetic HOME/XDG root (Constitution VII, SC-009). Nothing here
// touches the developer's real home.
package integration

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/testutil"
)

// env is a hermetic environment for one integration test: a temp HOME/XDG root,
// the built aprofile binary, and a stub harness installed as "claude" and "codex"
// on PATH.
type env struct {
	t        *testing.T
	root     string
	aprofile string
	stubDir  string
	vars     []string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	root := t.TempDir()
	configHome := filepath.Join(root, ".config")
	dataHome := filepath.Join(root, ".local", "share")
	stateHome := filepath.Join(root, ".local", "state")
	for _, d := range []string{configHome, dataHome, stateHome} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	aprofileBin := testutil.BuildBinary(t, "github.com/cc-deck/aprofile/cmd/aprofile", "aprofile")
	// Build the stub and install it as "claude" so the wrapper's `exec claude`
	// resolves to it.
	stub := testutil.BuildBinary(t, "github.com/cc-deck/aprofile/tests/fixtures/stub", "claude")
	stubDir := filepath.Dir(stub)
	// The same stub is also installed as "codex" (feature 004), so Codex wrappers
	// can be run to observe their environment and arguments.
	codexStubDir := filepath.Dir(testutil.BuildBinary(t, "github.com/cc-deck/aprofile/tests/fixtures/stub", "codex"))
	binDir := filepath.Join(dataHome, "aprofile", "bin")

	path := strings.Join([]string{stubDir, codexStubDir, binDir, "/usr/bin", "/bin"}, string(os.PathListSeparator))
	vars := []string{
		"HOME=" + root,
		"XDG_CONFIG_HOME=" + configHome,
		"XDG_DATA_HOME=" + dataHome,
		"XDG_STATE_HOME=" + stateHome,
		"PATH=" + path,
		"SHELL=/bin/zsh",
		"ZDOTDIR=" + root,
	}
	return &env{t: t, root: root, aprofile: aprofileBin, stubDir: stubDir, vars: vars}
}

// run executes the aprofile binary with extra env vars (key=value) and returns
// combined output. It fails the test if the command errors.
func (e *env) run(extra []string, args ...string) string {
	e.t.Helper()
	out, err := e.tryRun(extra, args...)
	if err != nil {
		e.t.Fatalf("aprofile %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func (e *env) tryRun(extra []string, args ...string) (string, error) {
	e.t.Helper()
	cmd := exec.Command(e.aprofile, args...)
	cmd.Env = append(append([]string{}, e.vars...), extra...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// tryRunSplit runs the binary capturing stdout and stderr separately, so a test can
// parse clean JSON from stdout even when the command also writes a summary to stderr.
func (e *env) tryRunSplit(extra []string, args ...string) (stdout, stderr string, err error) {
	e.t.Helper()
	cmd := exec.Command(e.aprofile, args...)
	cmd.Env = append(append([]string{}, e.vars...), extra...)
	var so, se bytes.Buffer
	cmd.Stdout = &so
	cmd.Stderr = &se
	err = cmd.Run()
	return so.String(), se.String(), err
}

// runWrapper executes a generated wrapper by name from the bin dir.
func (e *env) runWrapper(name string, extra []string, args ...string) (string, error) {
	e.t.Helper()
	binDir := filepath.Join(e.root, ".local", "share", "aprofile", "bin")
	cmd := exec.Command(filepath.Join(binDir, name), args...)
	cmd.Env = append(append([]string{}, e.vars...), extra...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (e *env) configDir(harness, name string) string {
	return filepath.Join(e.root, ".local", "share", "aprofile", "profiles", harness, name)
}
