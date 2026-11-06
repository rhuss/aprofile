package hooksets

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cc-deck/aprofile/internal/config"
)

// RecordFormatVersion is the managed-hooks record schema version.
const RecordFormatVersion = 1

// Record lists the hook entries aprofile placed into one profile (research R4). It
// lives at <data>/aprofile/managed-hooks/<harness>/<name>.json, outside the harness
// config dir, so the harness never sees an unknown entry.
type Record struct {
	FormatVersion int           `json:"format_version"`
	Entries       []RecordEntry `json:"entries"`
}

// RecordEntry is one managed entry: a matcher group (event-groups form, identified
// by its fingerprint) or a link (files form, identified by path and target).
type RecordEntry struct {
	Set         string   `json:"set"`
	Form        string   `json:"form"`
	Event       string   `json:"event,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty"`
	Matcher     string   `json:"matcher,omitempty"`
	Commands    []string `json:"commands,omitempty"` // the group's hook commands, to recognize an edited copy
	Path        string   `json:"path,omitempty"`
	Target      string   `json:"target,omitempty"`
}

// LoadRecord reads a record; a missing file is an empty record.
func LoadRecord(path string) (*Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Record{FormatVersion: RecordFormatVersion}, nil
		}
		return nil, fmt.Errorf("read managed hooks record %s: %w", path, err)
	}
	var r Record
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse managed hooks record %s: %w", path, err)
	}
	if r.FormatVersion == 0 {
		r.FormatVersion = RecordFormatVersion
	}
	return &r, nil
}

// Save writes the record atomically, or removes the file when the record is empty.
// It writes only when the content changed, so an unchanged sync writes nothing.
func (r *Record) Save(path string) (changed bool, err error) {
	if len(r.Entries) == 0 {
		if _, err := os.Stat(path); err == nil {
			return true, os.Remove(path)
		}
		return false, nil
	}
	r.FormatVersion = RecordFormatVersion
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return false, err
	}
	data = append(data, '\n')
	if existing, err := os.ReadFile(path); err == nil && string(existing) == string(data) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, config.WriteFileAtomic(path, data, 0o644)
}

// Fingerprint is the SHA-256 of a matcher group's canonical JSON (keys sorted), so
// whitespace or key order differences do not count as an edit.
func Fingerprint(group json.RawMessage) string {
	var v any
	if err := json.Unmarshal(group, &v); err != nil {
		sum := sha256.Sum256(group)
		return "sha256:" + hex.EncodeToString(sum[:])
	}
	canon, _ := json.Marshal(v)
	sum := sha256.Sum256(canon)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// commandsOf returns the commands of a group's hooks.
func commandsOf(group json.RawMessage) []string {
	var g struct {
		Hooks []struct {
			Command string `json:"command"`
		} `json:"hooks"`
	}
	_ = json.Unmarshal(group, &g)
	var out []string
	for _, h := range g.Hooks {
		if h.Command != "" {
			out = append(out, h.Command)
		}
	}
	return out
}

// matcherOf returns a group's "matcher" string, if any.
func matcherOf(group json.RawMessage) string {
	var g struct {
		Matcher string `json:"matcher"`
	}
	_ = json.Unmarshal(group, &g)
	return g.Matcher
}
