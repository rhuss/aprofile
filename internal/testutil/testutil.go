// Package testutil provides hermetic test helpers. Every test that touches the
// filesystem MUST route through here so it runs against a temporary HOME and XDG
// root and never reads or writes the developer's real home (Constitution VII,
// SC-009).
package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

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
