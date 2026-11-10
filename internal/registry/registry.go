// Package registry writes the machine-readable registry other tools read to learn
// which profile a pane runs (FR-030). It is a public interface (Constitution V):
// breaking changes require a bumped FormatVersion. The registry carries only
// display and location fields, never secrets (FR-014, FR-028).
package registry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/cc-deck/aprofile/internal/config"
)

// FormatVersion is the registry schema version.
const FormatVersion = 1

// Registry is the top-level registry document.
type Registry struct {
	FormatVersion int     `json:"format_version"`
	Profiles      []Entry `json:"profiles"`
}

// Entry is one profile's registry record. No credentials, no references (FR-014).
type Entry struct {
	Harness  string `json:"harness"`
	Name     string `json:"name"`
	Label    string `json:"label"`
	Color    string `json:"color"`
	Icon     string `json:"icon"`
	AuthKind string `json:"auth_kind"`
	// Model is the profile's configured model from the profile store, "" when unset
	// (feature 004, FR-018). Never read from a harness file. Added additively at
	// format_version 1.
	Model       string `json:"model"`
	Primary     bool   `json:"primary"`
	ConfigDir   string `json:"config_dir"`
	WrapperPath string `json:"wrapper_path"`
	// Share is the profile's share level (feature 002, FR-019): all, content or
	// none. Added additively at format_version 1.
	Share string `json:"share"`
	// HistoryGroup is the profile's history group (feature 003, FR-024), empty for
	// an isolated profile. Added additively at format_version 1.
	HistoryGroup string `json:"history_group"`
}

// Marshal renders the registry JSON deterministically: entries sorted by
// (harness, name) and no timestamps or volatile fields, so an unchanged registry
// serializes byte-for-byte identically (FR-030).
func Marshal(entries []Entry) ([]byte, error) {
	sorted := append([]Entry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Harness != sorted[j].Harness {
			return sorted[i].Harness < sorted[j].Harness
		}
		return sorted[i].Name < sorted[j].Name
	})
	if sorted == nil {
		sorted = []Entry{}
	}
	reg := Registry{FormatVersion: FormatVersion, Profiles: sorted}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(reg); err != nil {
		return nil, fmt.Errorf("encode registry: %w", err)
	}
	return buf.Bytes(), nil
}

// Write writes the registry atomically to path, but only when its content differs
// from what is already there. It reports whether it changed anything, so an
// unchanged sync writes 0 files (SC-003, FR-030).
func Write(path string, entries []Entry) (changed bool, err error) {
	data, err := Marshal(entries)
	if err != nil {
		return false, err
	}
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
		return false, nil
	}
	if err := config.WriteFileAtomic(path, data, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// Read parses a registry file. A missing file yields an empty registry.
func Read(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Registry{FormatVersion: FormatVersion, Profiles: []Entry{}}, nil
		}
		return nil, fmt.Errorf("read registry %s: %w", path, err)
	}
	var reg Registry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("parse registry %s: %w", path, err)
	}
	return &reg, nil
}
