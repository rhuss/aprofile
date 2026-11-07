// Package testutil provides hermetic test helpers. Every test that touches the
// filesystem MUST route through here so it runs against a temporary HOME and XDG
// root and never reads or writes the developer's real home (Constitution VII,
// SC-009).
package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// HermeticPATH is the PATH HermeticHome pins: system directories only, so no test
// finds a developer's real harness binary, hook or helper by accident. Tests that
// need a stub put its directory in front explicitly.
const HermeticPATH = "/usr/bin:/bin"

// Home describes a hermetic HOME/XDG root created for a single test.
type Home struct {
	Root       string // the temp HOME
	ConfigHome string // XDG_CONFIG_HOME
	DataHome   string // XDG_DATA_HOME
	StateHome  string // XDG_STATE_HOME
	CacheHome  string // XDG_CACHE_HOME
}

// HermeticHome creates a fresh temp directory, points HOME and every XDG base
// variable inside it, and registers the changes with t.Setenv so Go restores the
// previous environment when the test ends. It returns the resolved locations.
//
// It uses t.TempDir() + t.Setenv() exclusively, so no test can escape to the real
// home directory (Principle VII). It also neutralizes ZDOTDIR and SHELL: ZDOTDIR is
// pointed inside the temp root (otherwise a real ZDOTDIR would send sync's PATH
// block to the developer's real ~/.zshrc), and SHELL defaults to /bin/sh so the
// default hermetic home performs no shell-rc edit unless a test opts in by setting
// SHELL itself.
//
// PATH is pinned to HermeticPATH and every inherited APROFILE_* variable is
// unset (a test run from inside an aprofile wrapper inherits APROFILE_CONFIG_DIR,
// which changes how aprofile resolves its own dirs, and APROFILE_SET_VARS, which
// changes what login and the wrapper treat as already set).
func HermeticHome(t *testing.T) Home {
	t.Helper()
	root := t.TempDir()
	h := Home{
		Root:       root,
		ConfigHome: filepath.Join(root, ".config"),
		DataHome:   filepath.Join(root, ".local", "share"),
		StateHome:  filepath.Join(root, ".local", "state"),
		CacheHome:  filepath.Join(root, ".cache"),
	}
	for _, d := range []string{h.ConfigHome, h.DataHome, h.StateHome, h.CacheHome} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("testutil: create %s: %v", d, err)
		}
	}
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", h.ConfigHome)
	t.Setenv("XDG_DATA_HOME", h.DataHome)
	t.Setenv("XDG_STATE_HOME", h.StateHome)
	t.Setenv("XDG_CACHE_HOME", h.CacheHome)
	// Keep sync's PATH-block logic inside the temp root: a real ZDOTDIR would send
	// the zsh rc edit to the developer's home, and an inherited SHELL would pick a
	// real rc file. ZDOTDIR points inside root; SHELL defaults to /bin/sh (an
	// unsupported shell, so no rc is edited) unless a test sets SHELL itself.
	t.Setenv("ZDOTDIR", root)
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("PATH", HermeticPATH)
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); strings.HasPrefix(k, "APROFILE_") {
			t.Setenv(k, "") // registers the restore of the inherited value
			if err := os.Unsetenv(k); err != nil {
				t.Fatalf("testutil: unset %s: %v", k, err)
			}
		}
	}
	return h
}

// WriteFile writes a file under a hermetic root, creating parent directories. It
// fails the test on error.
func WriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("testutil: mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("testutil: write %s: %v", path, err)
	}
}
