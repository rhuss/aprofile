package layer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/harness"
)

// Config patches (FR-019, T071) are the definition's merge-only writes into
// structured files of a profile config dir. A patch sets the keys it names, merging
// nested maps key by key, and leaves every other key of the file as it was. It never
// removes a key, never creates an absent file, never writes through a link (a linked
// entry belongs to the default setup, which sync never modifies, Principle II), and
// writes a file only when its content changes, keeping the file's mode. JSON files
// keep untouched values byte for byte (key order and indentation are normalized when
// the file is rewritten); TOML files are re-encoded, so comments are lost on the
// first write that changes the file.

// PatchOutcome is the result of one config patch.
type PatchOutcome struct {
	// File is the profile-relative path the patch targets.
	File string
	// Keys lists the dotted key paths whose value the patch set; empty when the
	// file already held every patched value.
	Keys []string
	// Changed is true when the file was written (or, in a dry run, would be).
	Changed bool
	// Skipped says why the patch was not applied: the file is absent or a link.
	Skipped string
	// Err is set when the file could not be read, parsed or written; the file is
	// then left as it was.
	Err error
}

// patchDoc is one target file, loaded once and shared by every patch naming it.
type patchDoc struct {
	path   string
	toml   bool
	mode   os.FileMode
	orig   []byte
	json   map[string]json.RawMessage
	tomlM  map[string]any
	err    error
	skip   string
	result []int // indexes of the outcomes for this file
}

// ApplyConfigPatches applies the definition's config_patches to the profile config
// dir in declaration order and returns one outcome per patch. With dryRun nothing is
// written; the outcomes say what would change. Patches of the same file are merged
// in memory and the file is written at most once; a set of patches whose net effect
// leaves the file as it was reports no change.
func ApplyConfigPatches(def *harness.Definition, configDir string, dryRun bool) []PatchOutcome {
	if len(def.ConfigPatches) == 0 {
		return nil
	}
	out := make([]PatchOutcome, len(def.ConfigPatches))
	docs := map[string]*patchDoc{}
	var order []string
	for i, cp := range def.ConfigPatches {
		out[i].File = cp.File
		doc, ok := docs[cp.File]
		if !ok {
			doc = loadPatchDoc(configDir, cp.File)
			docs[cp.File] = doc
			order = append(order, cp.File)
		}
		doc.result = append(doc.result, i)
		if doc.skip != "" || doc.err != nil {
			continue
		}
		var keys []string
		var err error
		if doc.toml {
			keys, err = mergeTOML(doc.tomlM, cp.Merge, "")
		} else {
			keys, err = mergeJSONObject(doc.json, cp.Merge, "")
		}
		if err != nil {
			out[i].Err = fmt.Errorf("config patch %s: %w", cp.File, err)
			continue
		}
		sort.Strings(keys)
		out[i].Keys = keys
	}

	for _, file := range order {
		doc := docs[file]
		if doc.skip != "" || doc.err != nil {
			for _, i := range doc.result {
				out[i].Skipped, out[i].Err = doc.skip, doc.err
			}
			continue
		}
		data, changed, err := doc.encode()
		if err == nil && changed && !dryRun {
			err = config.WriteFileAtomic(doc.path, data, doc.mode)
		}
		for _, i := range doc.result {
			switch {
			case err != nil:
				out[i].Keys, out[i].Err = nil, fmt.Errorf("config patch %s: %w", file, err)
			case !changed:
				out[i].Keys = nil
			default:
				out[i].Changed = len(out[i].Keys) > 0
			}
		}
	}
	return out
}

// loadPatchDoc reads and parses a patch target, or records why it is skipped.
func loadPatchDoc(configDir, file string) *patchDoc {
	doc := &patchDoc{path: filepath.Join(configDir, filepath.FromSlash(file)),
		toml: strings.EqualFold(filepath.Ext(file), ".toml")}
	// Walk the path below the config dir: a link anywhere on it points outside the
	// profile (a shared entry), so the patch is skipped rather than written through.
	cur := configDir
	for _, seg := range strings.Split(filepath.ToSlash(file), "/") {
		cur = filepath.Join(cur, seg)
		fi, err := os.Lstat(cur)
		switch {
		case errors.Is(err, os.ErrNotExist):
			doc.skip = "the file does not exist (patches merge into existing files only)"
			return doc
		case err != nil:
			doc.err = fmt.Errorf("config patch %s: %w", file, err)
			return doc
		case fi.Mode()&os.ModeSymlink != 0:
			doc.skip = "it is a link into the default setup, which sync never modifies"
			return doc
		}
	}
	fi, err := os.Stat(doc.path)
	if err == nil && !fi.Mode().IsRegular() {
		err = fmt.Errorf("not a regular file")
	}
	if err != nil {
		doc.err = fmt.Errorf("config patch %s: %w", file, err)
		return doc
	}
	doc.mode = fi.Mode().Perm()
	if doc.orig, err = os.ReadFile(doc.path); err != nil {
		doc.err = fmt.Errorf("config patch %s: %w", file, err)
		return doc
	}
	if doc.toml {
		doc.tomlM = map[string]any{}
		if len(bytes.TrimSpace(doc.orig)) > 0 {
			if err := toml.Unmarshal(doc.orig, &doc.tomlM); err != nil {
				doc.err = fmt.Errorf("config patch %s: not valid TOML, left unchanged: %w", file, err)
			}
		}
		return doc
	}
	if doc.json, err = decodeJSONObject(doc.orig); err != nil {
		doc.err = fmt.Errorf("config patch %s: not a valid JSON object, left unchanged: %w", file, err)
	}
	return doc
}

// encode renders the merged document and reports whether it differs semantically
// from the file's original content.
func (d *patchDoc) encode() ([]byte, bool, error) {
	if d.toml {
		var orig map[string]any
		if len(bytes.TrimSpace(d.orig)) > 0 {
			if err := toml.Unmarshal(d.orig, &orig); err != nil {
				return nil, false, err
			}
		}
		if tomlEqual(orig, d.tomlM) {
			return nil, false, nil
		}
		data, err := toml.Marshal(d.tomlM)
		return data, true, err
	}
	data, err := EncodeJSON(d.json)
	if err != nil {
		return nil, false, err
	}
	if len(bytes.TrimSpace(d.orig)) > 0 && canonicalJSONEqual(d.orig, data) {
		return nil, false, nil
	}
	return data, true, nil
}

// mergeJSONObject merges patch into dst in place and returns the dotted paths of
// the values it set. A patch map merges into an existing JSON object key by key;
// any other value replaces the key's value.
func mergeJSONObject(dst map[string]json.RawMessage, patch map[string]any, prefix string) ([]string, error) {
	var changed []string
	for _, k := range sortedPatchKeys(patch) {
		path := joinKey(prefix, k)
		pv := patch[k]
		if pm, ok := pv.(map[string]any); ok {
			if raw, exists := dst[k]; exists && isJSONObject(raw) {
				sub, err := decodeJSONObject(raw)
				if err != nil {
					return nil, err
				}
				subChanged, err := mergeJSONObject(sub, pm, path)
				if err != nil {
					return nil, err
				}
				if len(subChanged) > 0 {
					enc, err := EncodeJSONCompact(sub)
					if err != nil {
						return nil, err
					}
					dst[k] = enc
					changed = append(changed, subChanged...)
				}
				continue
			}
		}
		enc, err := EncodeJSONCompact(pv)
		if err != nil {
			return nil, fmt.Errorf("key %s: %w", path, err)
		}
		if raw, exists := dst[k]; exists && canonicalJSONEqual(raw, enc) {
			continue
		}
		dst[k] = enc
		changed = append(changed, path)
	}
	return changed, nil
}

// mergeTOML is mergeJSONObject for a decoded TOML document.
func mergeTOML(dst map[string]any, patch map[string]any, prefix string) ([]string, error) {
	var changed []string
	for _, k := range sortedPatchKeys(patch) {
		path := joinKey(prefix, k)
		pv := patch[k]
		if pm, ok := pv.(map[string]any); ok {
			if sub, ok := dst[k].(map[string]any); ok {
				subChanged, err := mergeTOML(sub, pm, path)
				if err != nil {
					return nil, err
				}
				changed = append(changed, subChanged...)
				continue
			}
		}
		if cur, exists := dst[k]; exists && tomlEqual(cur, pv) {
			continue
		}
		dst[k] = deepCopyPatch(pv) // never alias the definition's data
		changed = append(changed, path)
	}
	return changed, nil
}

// deepCopyPatch copies the maps and lists of a patch value.
func deepCopyPatch(v any) any {
	switch t := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(t))
		for k, e := range t {
			m[k] = deepCopyPatch(e)
		}
		return m
	case []any:
		l := make([]any, len(t))
		for i, e := range t {
			l[i] = deepCopyPatch(e)
		}
		return l
	}
	return v
}

func isJSONObject(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) > 0 && t[0] == '{'
}

// canonicalJSONEqual compares two JSON documents ignoring whitespace and key order,
// keeping numbers exact (no float conversion).
func canonicalJSONEqual(a, b []byte) bool {
	ca, err1 := canonicalJSON(a)
	cb, err2 := canonicalJSON(b)
	if err1 != nil || err2 != nil {
		return bytes.Equal(a, b)
	}
	return bytes.Equal(ca, cb)
}

func canonicalJSON(data []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

// tomlEqual compares two TOML values by their encoding, so an int from the
// definition equals the int64 a decoded file holds.
func tomlEqual(a, b any) bool {
	ea, err1 := toml.Marshal(map[string]any{"v": a})
	eb, err2 := toml.Marshal(map[string]any{"v": b})
	return err1 == nil && err2 == nil && bytes.Equal(ea, eb)
}

func sortedPatchKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func joinKey(prefix, k string) string {
	if prefix == "" {
		return k
	}
	return prefix + "." + k
}
