package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/testutil"
)

func TestResolvePaths_XDGSet(t *testing.T) {
	h := testutil.HermeticHome(t)

	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}

	want := map[string]string{
		"ConfigDir":  filepath.Join(h.ConfigHome, "aprofile"),
		"DataDir":    filepath.Join(h.DataHome, "aprofile"),
		"StateDir":   filepath.Join(h.StateHome, "aprofile"),
		"BinDir":     filepath.Join(h.DataHome, "aprofile", "bin"),
		"BackupsDir": filepath.Join(h.DataHome, "aprofile", "backups"),
	}
	got := map[string]string{
		"ConfigDir":  p.ConfigDir,
		"DataDir":    p.DataDir,
		"StateDir":   p.StateDir,
		"BinDir":     p.BinDir,
		"BackupsDir": p.BackupsDir,
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %q, want %q", k, got[k], w)
		}
	}
}

func TestResolvePaths_XDGFallback(t *testing.T) {
	// Simulate both platforms: unset the XDG vars so the ~/.config etc fallbacks
	// are exercised. HermeticHome sets HOME to a temp dir first.
	h := testutil.HermeticHome(t)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")

	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}
	if p.ConfigDir != filepath.Join(h.Root, ".config", "aprofile") {
		t.Errorf("ConfigDir fallback = %q", p.ConfigDir)
	}
	if p.DataDir != filepath.Join(h.Root, ".local", "share", "aprofile") {
		t.Errorf("DataDir fallback = %q", p.DataDir)
	}
	if p.StateDir != filepath.Join(h.Root, ".local", "state", "aprofile") {
		t.Errorf("StateDir fallback = %q", p.StateDir)
	}
}

func TestResolvePaths_RelativeXDGIgnored(t *testing.T) {
	// The XDG spec requires relative values to be ignored.
	h := testutil.HermeticHome(t)
	t.Setenv("XDG_CONFIG_HOME", "relative/path")

	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}
	if p.ConfigDir != filepath.Join(h.Root, ".config", "aprofile") {
		t.Errorf("relative XDG_CONFIG_HOME not ignored: %q", p.ConfigDir)
	}
}

func TestBinDirOverride(t *testing.T) {
	h := testutil.HermeticHome(t)
	custom := filepath.Join(h.Root, "custom-bin")
	if err := config.SaveSettings(filepath.Join(h.ConfigHome, "aprofile"), &config.Settings{BinDir: custom}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}
	if p.BinDir != custom {
		t.Errorf("BinDir = %q, want override %q", p.BinDir, custom)
	}
	// The registry path must remain under the data dir, independent of the bin dir.
	if p.RegistryPath() != filepath.Join(h.DataHome, "aprofile", "registry.json") {
		t.Errorf("RegistryPath = %q, should be independent of bin dir", p.RegistryPath())
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	h := testutil.HermeticHome(t)
	configDir := filepath.Join(h.ConfigHome, "aprofile")
	in := &config.Settings{BinDir: "/some/bin", DisableShellEdit: true}
	if err := config.SaveSettings(configDir, in); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	out, err := config.LoadSettings(configDir)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if out.BinDir != in.BinDir || out.DisableShellEdit != in.DisableShellEdit {
		t.Errorf("round-trip mismatch: got %+v, want %+v", out, in)
	}
}

func TestLoadSettings_MissingFileIsZero(t *testing.T) {
	h := testutil.HermeticHome(t)
	out, err := config.LoadSettings(filepath.Join(h.ConfigHome, "aprofile"))
	if err != nil {
		t.Fatalf("LoadSettings on missing file should not error: %v", err)
	}
	if out.BinDir != "" || out.DisableShellEdit {
		t.Errorf("missing settings should be zero-valued, got %+v", out)
	}
}

func TestProfilePathsNoCollision(t *testing.T) {
	// Even if both XDG bases point at the same dir, the store file (with .yaml)
	// must not collide with the config dir (no extension) (FR-006).
	h := testutil.HermeticHome(t)
	shared := filepath.Join(h.Root, "shared")
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", shared)
	t.Setenv("XDG_DATA_HOME", shared)

	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}
	store := p.ProfileStorePath("claude", "work")
	dir := p.ProfileConfigDir("claude", "work")
	if store == dir {
		t.Fatalf("store path and config dir collide: %q", store)
	}
}
