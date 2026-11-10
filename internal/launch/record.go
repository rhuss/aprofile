package launch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/layer"
)

// RecordFormatVersion is the managed-launch record schema version.
const RecordFormatVersion = 1

// Record lists the launch entries aprofile placed into one profile's harness files
// (feature 004, research R6, data-model.md "Launch record"). It lives at
// <data>/aprofile/managed-launch/<harness>/<name>.json, never inside the harness's
// files (FR-004). It holds locations and replaced keys only, never the value of a
// placed env entry (Principle IV).
type Record struct {
	FormatVersion int     `json:"format_version"`
	Entries       []Entry `json:"entries"`
}

// Entry is one placed entry: a profile-relative file and the location inside it
// (env.<NAME>, apiKeyHelper, model_providers.<p>.auth). Replaced holds, for a
// provider-auth block, the keys it replaced with their original raw TOML value
// (env_key = "\"OPENAI_API_KEY\""). The same type names unplaced entries in reports.
type Entry struct {
	File     string            `json:"file"`
	Location string            `json:"location"`
	Replaced map[string]string `json:"replaced,omitempty"`
}

// Has reports whether the record lists file#location.
func (r Record) Has(file, location string) bool {
	_, ok := r.find(file, location)
	return ok
}

// Get returns the entry for file#location.
func (r Record) Get(file, location string) (Entry, bool) {
	if i, ok := r.find(file, location); ok {
		return r.Entries[i], true
	}
	return Entry{}, false
}

func (r Record) find(file, location string) (int, bool) {
	for i, e := range r.Entries {
		if e.File == file && e.Location == location {
			return i, true
		}
	}
	return -1, false
}

// sorted returns a copy of the record with entries in a deterministic order.
func (r Record) sorted() Record {
	out := Record{FormatVersion: RecordFormatVersion, Entries: append([]Entry(nil), r.Entries...)}
	sort.SliceStable(out.Entries, func(i, j int) bool {
		if out.Entries[i].File != out.Entries[j].File {
			return out.Entries[i].File < out.Entries[j].File
		}
		return out.Entries[i].Location < out.Entries[j].Location
	})
	return out
}

// Load reads a record; a missing file is an empty record.
func Load(path string) (Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Record{FormatVersion: RecordFormatVersion}, nil
		}
		return Record{}, fmt.Errorf("read managed launch record %s: %w", path, err)
	}
	var r Record
	if err := json.Unmarshal(data, &r); err != nil {
		return Record{}, fmt.Errorf("parse managed launch record %s: %w", path, err)
	}
	if r.FormatVersion == 0 {
		r.FormatVersion = RecordFormatVersion
	}
	return r, nil
}

// Save writes the record atomically, or removes the file when the record is empty.
// It writes only when the content changed, so an unchanged sync writes nothing.
func (r Record) Save(path string) (changed bool, err error) {
	if len(r.Entries) == 0 {
		if _, err := os.Stat(path); err == nil {
			return true, os.Remove(path)
		}
		return false, nil
	}
	data, err := json.MarshalIndent(r.sorted(), "", "  ")
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

// Move renames a record (profile rename). A missing record is a no-op.
func Move(from, to string) error {
	if _, err := os.Stat(from); os.IsNotExist(err) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	return os.Rename(from, to)
}

// Delete removes a record (remove --purge, share level change to all). A missing
// record is a no-op.
func Delete(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Placed returns the record's entries as the layer package's PlacedKeys (file to
// locations), so share-level reports ignore them (feature 004, T024).
func (r Record) Placed() layer.PlacedKeys {
	out := layer.PlacedKeys{}
	for _, e := range r.Entries {
		out[e.File] = append(out[e.File], e.Location)
	}
	return out
}

// LoadPlaced is Load(path).Placed(), with an unreadable record giving none.
func LoadPlaced(path string) layer.PlacedKeys {
	rec, err := Load(path)
	if err != nil {
		return nil
	}
	return rec.Placed()
}
