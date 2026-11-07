package config_test

import (
	"path/filepath"
	"testing"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/testutil"
)

// TestResolvePaths_InsideProfileSession covers T076/FR-020: a wrapper that isolates
// its harness through the XDG base directories (OpenCode) exports XDG_* values that
// point inside the profile config dir it also exports as APROFILE_CONFIG_DIR. An
// aprofile run from inside that session must ignore those redirected values and
// resolve its own dirs from the $HOME defaults, not from inside the profile.
func TestResolvePaths_InsideProfileSession(t *testing.T) {
	h := testutil.HermeticHome(t)
	profileDir := filepath.Join(h.DataHome, "aprofile", "profiles", "opencode", "work")
	t.Setenv("APROFILE_CONFIG_DIR", profileDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(profileDir, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(profileDir, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(profileDir, "state"))

	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}
	want := map[string]string{
		"ConfigDir": filepath.Join(h.Root, ".config", "aprofile"),
		"DataDir":   filepath.Join(h.Root, ".local", "share", "aprofile"),
		"StateDir":  filepath.Join(h.Root, ".local", "state", "aprofile"),
	}
	got := map[string]string{"ConfigDir": p.ConfigDir, "DataDir": p.DataDir, "StateDir": p.StateDir}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %q, want %q (the profile's redirected XDG value must be ignored)", k, got[k], w)
		}
	}
}

// TestResolvePaths_ProfileSessionKeepsOutsideXDG: inside a profile session, an XDG
// value that does not point into the profile config dir (a harness that is not
// isolated through XDG, or a user's own custom base dir) is still honored. Only the
// redirected values are dropped, and a sibling dir sharing the profile dir's name as a
// prefix is not "inside" it.
func TestResolvePaths_ProfileSessionKeepsOutsideXDG(t *testing.T) {
	h := testutil.HermeticHome(t)
	profileDir := filepath.Join(h.DataHome, "aprofile", "profiles", "claude", "work")
	t.Setenv("APROFILE_CONFIG_DIR", profileDir)
	custom := filepath.Join(h.Root, "custom-config")
	sibling := profileDir + "-state" // shares the prefix but is not inside
	t.Setenv("XDG_CONFIG_HOME", custom)
	t.Setenv("XDG_STATE_HOME", sibling)
	// XDG_DATA_HOME equal to the profile dir itself also counts as redirected.
	t.Setenv("XDG_DATA_HOME", profileDir+string(filepath.Separator))

	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}
	if want := filepath.Join(custom, "aprofile"); p.ConfigDir != want {
		t.Errorf("ConfigDir = %q, want %q", p.ConfigDir, want)
	}
	if want := filepath.Join(sibling, "aprofile"); p.StateDir != want {
		t.Errorf("StateDir = %q, want %q", p.StateDir, want)
	}
	if want := filepath.Join(h.Root, ".local", "share", "aprofile"); p.DataDir != want {
		t.Errorf("DataDir = %q, want %q (XDG_DATA_HOME equal to the profile dir is redirected)", p.DataDir, want)
	}
}

// TestResolvePaths_RelativeProfileDirIgnored: a relative or empty APROFILE_CONFIG_DIR
// never drops an XDG value (only an absolute profile dir can contain one).
func TestResolvePaths_RelativeProfileDirIgnored(t *testing.T) {
	h := testutil.HermeticHome(t)
	t.Setenv("APROFILE_CONFIG_DIR", "relative/dir")
	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}
	if want := filepath.Join(h.ConfigHome, "aprofile"); p.ConfigDir != want {
		t.Errorf("ConfigDir = %q, want %q", p.ConfigDir, want)
	}
}
