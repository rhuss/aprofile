// Package config resolves aprofile's on-disk locations from the XDG base
// directories and loads the global settings file. It is a leaf package: it
// imports nothing else from aprofile, so every other package can depend on it.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// appName is the sub-directory used under every XDG base dir.
const appName = "aprofile"

// Paths holds every resolved location aprofile uses. All fields are absolute.
type Paths struct {
	Home       string // the user's home directory
	ConfigDir  string // $XDG_CONFIG_HOME/aprofile
	DataDir    string // $XDG_DATA_HOME/aprofile
	StateDir   string // $XDG_STATE_HOME/aprofile
	BinDir     string // where wrappers are written (configurable; default <DataDir>/bin)
	BackupsDir string // <DataDir>/backups
}

// Settings is the global, installation-wide configuration file (FR-005, FR-046).
// It carries no secrets.
type Settings struct {
	// BinDir overrides the default wrapper directory (<data dir>/bin).
	BinDir string `yaml:"bin_dir,omitempty"`
	// DisableShellEdit turns off all edits to shell startup files (FR-024).
	DisableShellEdit bool `yaml:"disable_shell_edit,omitempty"`
	// DefaultShare is the share level of new profiles (FR-007): all, content or
	// none. Empty means the built-in default, content.
	DefaultShare string `yaml:"default_share,omitempty"`
}

// BuiltinDefaultShare is the share level new profiles get when settings.yaml does
// not name one (FR-007).
const BuiltinDefaultShare = "content"

// EffectiveDefaultShare returns the configured default share level or the
// built-in one.
func (s *Settings) EffectiveDefaultShare() string {
	if s == nil || s.DefaultShare == "" {
		return BuiltinDefaultShare
	}
	return s.DefaultShare
}

// xdg returns the value of an XDG environment variable, or, when it is unset or
// not absolute (the XDG spec says relative values must be ignored), falls back
// to home/relFallback.
func xdg(envVar, home, relFallback string) string {
	if v := os.Getenv(envVar); filepath.IsAbs(v) {
		return v
	}
	return filepath.Join(home, relFallback)
}

// ResolvePaths computes every location from the current environment. It applies
// the standard XDG fallbacks (~/.config, ~/.local/share, ~/.local/state) on
// both Linux and macOS (FR-046), then applies the bin-dir override from the
// global settings file if one exists (FR-005).
func ResolvePaths() (*Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}

	configBase := xdg("XDG_CONFIG_HOME", home, filepath.Join(".config"))
	dataBase := xdg("XDG_DATA_HOME", home, filepath.Join(".local", "share"))
	stateBase := xdg("XDG_STATE_HOME", home, filepath.Join(".local", "state"))

	p := &Paths{
		Home:      home,
		ConfigDir: filepath.Join(configBase, appName),
		DataDir:   filepath.Join(dataBase, appName),
		StateDir:  filepath.Join(stateBase, appName),
	}
	p.BackupsDir = filepath.Join(p.DataDir, "backups")
	p.BinDir = filepath.Join(p.DataDir, "bin")

	s, err := LoadSettings(p.ConfigDir)
	if err != nil {
		return nil, err
	}
	if s.BinDir != "" {
		p.BinDir = s.BinDir
	}
	return p, nil
}

// SettingsPath is the global settings file location.
func (p *Paths) SettingsPath() string {
	return filepath.Join(p.ConfigDir, "settings.yaml")
}

// HarnessesDir is the directory users drop harness definition overrides into.
func (p *Paths) HarnessesDir() string {
	return filepath.Join(p.ConfigDir, "harnesses.d")
}

// RegistryPath is the fixed machine-readable registry location (FR-030),
// independent of the configurable bin directory.
func (p *Paths) RegistryPath() string {
	return filepath.Join(p.DataDir, "registry.json")
}

// OmarchyAccountsDir is the default Omarchy accounts directory (FR-031):
// `$XDG_STATE_HOME/omarchy/agents/accounts`, which lives beside aprofile's own state
// dir, not inside it. StateDir is `<state base>/aprofile`, so its parent is the state
// base. The CLI may override this with --output-dir.
func (p *Paths) OmarchyAccountsDir() string {
	return filepath.Join(filepath.Dir(p.StateDir), "omarchy", "agents", "accounts")
}

// ProfileStoreDir is the directory holding a harness's profile store files.
func (p *Paths) ProfileStoreDir(harness string) string {
	return filepath.Join(p.ConfigDir, "profiles", harness)
}

// ProfileStorePath is the human-editable store file for one profile (FR-006):
// <config dir>/profiles/<harness>/<name>.yaml. It carries a file extension so it
// can never collide with the profile config dir even if both XDG bases point at
// the same place.
func (p *Paths) ProfileStorePath(harness, name string) string {
	return filepath.Join(p.ProfileStoreDir(harness), name+".yaml")
}

// ProfileConfigDir is the per-profile directory the harness is pointed at
// (FR-005): <data dir>/profiles/<harness>/<name>.
func (p *Paths) ProfileConfigDir(harness, name string) string {
	return filepath.Join(p.DataDir, "profiles", harness, name)
}

// BackupDir is the backup location used by import (FR-045):
// <data dir>/backups/<harness>/<name>/<timestamp>/.
func (p *Paths) BackupDir(harness, name, timestamp string) string {
	return filepath.Join(p.BackupsDir, harness, name, timestamp)
}

// HooksDir is the hook-set library: one <name>.yaml per set (FR-023).
func (p *Paths) HooksDir() string {
	return filepath.Join(p.ConfigDir, "hooks.d")
}

// ManagedHooksPath is the record of hook entries aprofile placed into one profile
// (research R4). It lives in aprofile's data dir so the harness config dir gains no
// unknown entry.
func (p *Paths) ManagedHooksPath(harness, name string) string {
	return filepath.Join(p.DataDir, "managed-hooks", harness, name+".json")
}

// LoadSettings reads the global settings file from configDir. A missing file is
// not an error: it returns zero-valued Settings.
func LoadSettings(configDir string) (*Settings, error) {
	path := filepath.Join(configDir, "settings.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Settings{}, nil
		}
		return nil, fmt.Errorf("read settings %s: %w", path, err)
	}
	var s Settings
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse settings %s: %w", path, err)
	}
	switch s.DefaultShare {
	case "", "all", "content", "none":
	default:
		return nil, fmt.Errorf("settings %s: default_share %q must be all, content or none", path, s.DefaultShare)
	}
	return &s, nil
}

// SaveSettings writes the global settings file atomically.
func SaveSettings(configDir string, s *Settings) error {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("create config dir %s: %w", configDir, err)
	}
	data, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	return WriteFileAtomic(filepath.Join(configDir, "settings.yaml"), data, 0o644)
}
