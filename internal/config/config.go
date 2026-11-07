// Package config resolves aprofile's on-disk locations from the XDG base
// directories and loads the global settings file. It is a leaf package: it
// imports nothing else from aprofile, so every other package can depend on it.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// appName is the sub-directory used under every XDG base dir.
const appName = "aprofile"

// settingsFile is the global settings file's name inside the config dir.
const settingsFile = "settings.yaml"

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

// xdg returns the value of an XDG environment variable, or, when it is unset, not
// absolute (the XDG spec says relative values must be ignored) or redirected into the
// running profile's config dir (see redirectedIntoProfile), falls back to
// home/relFallback.
func xdg(envVar, home, relFallback string) string {
	if v := os.Getenv(envVar); filepath.IsAbs(v) && !redirectedIntoProfile(v) {
		return v
	}
	return filepath.Join(home, relFallback)
}

// XDGBase resolves one XDG base directory the way aprofile resolves its own dirs:
// the variable's value when it is absolute and not redirected into the running
// profile, else home/relFallback. Other packages that read an XDG variable for a
// user-level location (not the harness's) use it so they follow the same rule.
func XDGBase(envVar, home, relFallback string) string {
	return xdg(envVar, home, relFallback)
}

// redirectedIntoProfile reports whether an XDG value points into the profile config
// dir of the aprofile session it runs in (T076, FR-020). A wrapper that isolates its
// harness through the XDG base directories (config_env naming XDG_* variables, as
// OpenCode's does) exports those variables pointing inside the profile config dir,
// which it also exports as APROFILE_CONFIG_DIR. aprofile run inside such a session
// (by the user in the harness's shell, or by a hook) must still find its own config,
// data and state, so a value equal to or below APROFILE_CONFIG_DIR is treated as
// unset. The rule is generic: it looks at paths only, never at the harness name
// (Principle I). A value outside the profile dir is honored as usual.
func redirectedIntoProfile(v string) bool {
	profileDir := os.Getenv("APROFILE_CONFIG_DIR")
	if !filepath.IsAbs(profileDir) {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(profileDir), filepath.Clean(v))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// ResolvePaths computes every location from the current environment. It applies
// the standard XDG fallbacks (~/.config, ~/.local/share, ~/.local/state) on
// both Linux and macOS (FR-046), ignoring XDG values a profile wrapper redirected
// into its profile config dir (redirectedIntoProfile), then applies the bin-dir
// override from the global settings file if one exists (FR-005).
func ResolvePaths() (*Paths, error) {
	p, _, err := Resolve()
	return p, err
}

// Resolve is ResolvePaths that also returns the global settings it read, so a
// caller needing both reads settings.yaml once.
func Resolve() (*Paths, *Settings, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, fmt.Errorf("resolve home directory: %w", err)
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
		return nil, nil, err
	}
	if s.BinDir != "" {
		p.BinDir = s.BinDir
	}
	return p, s, nil
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

// CredentialsDir holds the credential files `import cc-deck` copies for one
// profile: <data dir>/credentials/<harness>/<name>/. It lies outside the profile
// config dir so the harness never sees an unclassified entry; rename moves it and
// remove --purge deletes it with the profile.
func (p *Paths) CredentialsDir(harness, name string) string {
	return filepath.Join(p.DataDir, "credentials", harness, name)
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
	path := filepath.Join(configDir, settingsFile)
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
	if err := validBinDir(s.BinDir); err != nil {
		return nil, fmt.Errorf("settings %s: %w", path, err)
	}
	return &s, nil
}

// validBinDir checks a configured bin_dir. The value is written into shell startup
// files and is the directory wrappers land in, so it must be one absolute path on a
// single line: a relative path would depend on the working directory, and a line
// break or other control character could smuggle extra lines into an rc file.
// Spaces and shell metacharacters are fine; writers quote the value.
func validBinDir(dir string) error {
	if dir == "" {
		return nil
	}
	if strings.IndexFunc(dir, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return fmt.Errorf("bin_dir %q must be a single line without control characters", dir)
	}
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("bin_dir %q must be an absolute path", dir)
	}
	return nil
}
