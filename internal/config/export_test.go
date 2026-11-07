package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// SaveSettings writes the global settings file atomically. Only tests write it;
// aprofile itself never edits settings.yaml.
func SaveSettings(configDir string, s *Settings) error {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("create config dir %s: %w", configDir, err)
	}
	data, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	return WriteFileAtomic(filepath.Join(configDir, settingsFile), data, 0o644)
}
