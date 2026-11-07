package layer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/profile"
)

// A mixed file carries keys of several layers in one structured document (FR-013).
// aprofile owns only its owned-layer keys; every other key belongs to the harness,
// the default setup (seeded once) or is unclassified, and must be preserved exactly.
// The helpers here are the single implementation of that rule, shared by seeding
// (SeedMixed, used by the planner and import), copy (MergeMixedOwned), import
// (MergeMixedKeys) and doctor (MixedSeededDrift). Built-in mixed files are JSON
// (Claude Code's .claude.json, OpenCode's opencode.json); a definition may declare a
// mixed file in another format (FR-013 allows TOML), but key-level handling is
// implemented for JSON only: merges refuse such a file and seeding starts it minimal,
// never copying the default setup's file verbatim.

// MergeMixedOwned merges only the owned-layer keys of a mixed file from src into dst
// and returns the re-encoded dst. Every key dst holds that aprofile does not own is
// left exactly as it was; owned keys that src carries overwrite (or are added to)
// dst. It is the mechanism copy (FR-033a) uses to carry a mixed file's owned keys
// into a target profile. A non-JSON ext is an error so a caller never silently drops
// keys it cannot represent.
func MergeMixedOwned(rule *harness.EntryRule, dst, src []byte, ext string) ([]byte, error) {
	out, _, err := MergeMixedKeys(rule, dst, src, ext, harness.LayerOwned)
	return out, err
}

// MergeMixedKeys copies from src into dst every key the rule declares in one of
// layers and returns the re-encoded dst with the sorted names of the keys it copied
// (keys src does not hold are left out). Every other key of dst, declared or not,
// stays exactly as it was. Both documents are parsed even when layers is empty, so
// a caller can validate them before writing anything. A non-JSON ext is an error.
func MergeMixedKeys(rule *harness.EntryRule, dst, src []byte, ext string, layers ...harness.Layer) ([]byte, []string, error) {
	if !IsJSONExt(ext) {
		return nil, nil, fmt.Errorf("mixed-file merge supports only JSON files, not %q", ext)
	}
	dstMap, err := decodeJSONObject(dst)
	if err != nil {
		return nil, nil, fmt.Errorf("decode target mixed file: %w", err)
	}
	srcMap, err := decodeJSONObject(src)
	if err != nil {
		return nil, nil, fmt.Errorf("decode source mixed file: %w", err)
	}
	want := map[harness.Layer]bool{}
	for _, l := range layers {
		want[l] = true
	}
	var copied []string
	for key, l := range rule.Keys {
		if !want[l] {
			continue
		}
		if v, ok := srcMap[key]; ok {
			dstMap[key] = v
			copied = append(copied, key)
		}
	}
	sort.Strings(copied)
	out, err := encodeJSONObject(dstMap)
	if err != nil {
		return nil, nil, err
	}
	return out, copied, nil
}

// MixedSeed is the content a profile's mixed file starts with when aprofile
// creates it.
type MixedSeed struct {
	Data   []byte // the document to write
	Source string // the default-setup file it was seeded from; "" when minimal
	Note   string // why, for sync's report
}

// SeedMixed returns the content a new profile's mixed file starts with at share
// level: at all and content the default setup's file without its credentials and
// state keys (and undeclared credential-like keys, see seedsKey); at none, or when
// the default setup has no such file, a minimal document (FR-011: nothing is seeded
// at none, not even owned keys). home is the directory ~ expands to ("" means
// $HOME). The planner seeds an absent file with it and import starts a new
// profile's file from it, so both give the same result.
func SeedMixed(def *harness.Definition, rule *harness.EntryRule, level profile.Share, home string) MixedSeed {
	rel := rule.Path()
	if level == profile.ShareNone {
		return MixedSeed{Data: minimalDoc(rel), Note: "minimal: nothing seeded at share level none"}
	}
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	src := def.DefaultPathUnder(home, rel)
	data, err := os.ReadFile(src)
	if err != nil {
		return MixedSeed{Data: minimalDoc(rel), Note: "minimal: the default setup has none"}
	}
	return MixedSeed{Data: stripNonSeedKeys(data, rule, rel), Source: src,
		Note: "seeded once from the default setup (credentials and state keys excluded)"}
}

// MixedSeededDrift returns the sorted names of the mixed file's seeded keys whose
// value in profileFile differs from the default setup's file. A seeded key is one
// seeding copies (seedsKey) that aprofile does not own: declared settings, content
// or shared keys and undeclared keys, but not owned, credentials or state keys, nor
// undeclared credential-like keys. doctor reports them as information (FR-013,
// FR-019, FR-044). A key missing from either side is not reported as drift. A
// non-JSON ext yields no findings (nothing to compare in this slice).
func MixedSeededDrift(rule *harness.EntryRule, profileFile, defaultFile []byte, ext string) ([]string, error) {
	if !IsJSONExt(ext) {
		return nil, nil
	}
	pm, err := decodeJSONObject(profileFile)
	if err != nil {
		return nil, err
	}
	dm, err := decodeJSONObject(defaultFile)
	if err != nil {
		return nil, err
	}
	var drift []string
	for key, pv := range pm {
		if !seedsKey(rule, key) {
			continue
		}
		if l, ok := ClassifyKey(rule, key); ok && l == harness.LayerOwned {
			continue
		}
		dv, dok := dm[key]
		if !dok {
			continue
		}
		if !jsonValueEqual(pv, dv) {
			drift = append(drift, key)
		}
	}
	sort.Strings(drift)
	return drift, nil
}

// decodeJSONObject parses a JSON object into a key->raw-value map, preserving each
// value's bytes verbatim. An empty document decodes to an empty map.
func decodeJSONObject(data []byte) (map[string]json.RawMessage, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]json.RawMessage{}
	}
	return m, nil
}

// encodeJSONObject renders a key->raw-value map deterministically (Go marshals map
// keys sorted), with a trailing newline, preserving the semantic content of every
// value.
func encodeJSONObject(m map[string]json.RawMessage) ([]byte, error) {
	out, err := EncodeJSON(m)
	if err != nil {
		return nil, fmt.Errorf("encode mixed file: %w", err)
	}
	return out, nil
}

// EncodeJSON renders a value as indented JSON with a trailing newline, without
// HTML escaping, so commands such as "a && b > c" in a user's settings file stay
// readable when aprofile rewrites it.
func EncodeJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// EncodeJSONCompact renders a value as compact JSON without HTML escaping.
func EncodeJSONCompact(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// jsonValueEqual compares two JSON values ignoring insignificant whitespace and
// object key order, so a re-serialized but semantically identical value is not
// flagged as drift.
func jsonValueEqual(a, b json.RawMessage) bool {
	na, err1 := normalizeJSON(a)
	nb, err2 := normalizeJSON(b)
	if err1 != nil || err2 != nil {
		return bytes.Equal(a, b)
	}
	return bytes.Equal(na, nb)
}

func normalizeJSON(raw json.RawMessage) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

// IsJSONExt reports whether ext names a JSON file (case-insensitive).
func IsJSONExt(ext string) bool { return strings.EqualFold(ext, ".json") }
