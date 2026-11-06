package config_test

import (
	"path/filepath"
	"testing"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/testutil"
)

// T010: default share level and the hook-set paths.
func TestDefaultShareAndHookPaths(t *testing.T) {
	h := testutil.HermeticHome(t)
	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	s, _ := config.LoadSettings(p.ConfigDir)
	if got := s.EffectiveDefaultShare(); got != "content" {
		t.Errorf("built-in default share = %q, want content", got)
	}
	s.DefaultShare = "none"
	if err := config.SaveSettings(p.ConfigDir, s); err != nil {
		t.Fatal(err)
	}
	s2, _ := config.LoadSettings(p.ConfigDir)
	if s2.EffectiveDefaultShare() != "none" {
		t.Errorf("default_share not persisted: %+v", s2)
	}

	cfg := filepath.Join(h.ConfigHome, "aprofile")
	data := filepath.Join(h.DataHome, "aprofile")
	checks := map[string]string{
		p.HooksDir():                         filepath.Join(cfg, "hooks.d"),
		p.ManagedHooksPath("claude", "work"): filepath.Join(data, "managed-hooks", "claude", "work.json"),
	}
	for got, want := range checks {
		if got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
	}
}
