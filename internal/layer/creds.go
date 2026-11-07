package layer

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/cc-deck/aprofile/internal/harness"
)

// HasCredentials reports whether a profile config dir holds any credentials,
// without revealing them (FR-039). It checks concrete credential-layer files for
// non-empty content and mixed JSON files for a present, non-null credential key.
// Credentials kept only in the macOS Keychain (an interactive login) are not
// visible here; a definition's exec-hook auth-status covers that case (T045).
func HasCredentials(def *harness.Definition, configDir string) bool {
	for i := range def.Classification {
		rule := &def.Classification[i]
		if rule.IsGlob() {
			continue
		}
		path := filepath.Join(configDir, filepath.FromSlash(normalize(rule.Entry)))
		switch rule.Layer {
		case harness.LayerCredentials:
			if fi, err := os.Stat(path); err == nil && fi.Size() > 0 {
				return true
			}
		case harness.LayerMixed:
			if mixedHasCredentialKey(path, rule) {
				return true
			}
		}
	}
	return false
}

// mixedHasCredentialKey reports whether a mixed JSON file contains a present,
// non-null credential-layer key.
func mixedHasCredentialKey(path string, rule *harness.EntryRule) bool {
	if !IsJSONExt(filepath.Ext(path)) {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return false
	}
	for key, l := range rule.Keys {
		if l != harness.LayerCredentials {
			continue
		}
		raw, ok := obj[key]
		if ok && len(raw) > 0 && string(raw) != "null" {
			return true
		}
	}
	return false
}
