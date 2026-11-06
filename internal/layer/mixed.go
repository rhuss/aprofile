package layer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/cc-deck/aprofile/internal/harness"
)

// A mixed file carries keys of several layers in one structured document (FR-013).
// aprofile owns only its owned-layer keys; every other key belongs to the harness,
// the default setup (seeded once) or is unclassified, and must be preserved exactly.
// The helpers here are the single implementation of that rule, shared by seeding
// (prepare.go), copy (US4) and doctor (T030). Only JSON mixed files are supported in
// this slice (Claude's .claude.json); TOML arrives with Codex.

// MergeMixedOwned merges only the owned-layer keys of a mixed file from src into dst
// and returns the re-encoded dst. Every key dst holds that aprofile does not own is
// left exactly as it was; owned keys that src carries overwrite (or are added to)
// dst. It is the mechanism copy (FR-033a) uses to carry a mixed file's owned keys
// into a target profile. A non-JSON ext is an error so a caller never silently drops
// keys it cannot represent.
func MergeMixedOwned(rule *harness.EntryRule, dst, src []byte, ext string) ([]byte, error) {
	if !isJSONExt(ext) {
		return nil, fmt.Errorf("mixed-file merge supports only JSON files, not %q", ext)
	}
	dstMap, err := decodeJSONObject(dst)
	if err != nil {
		return nil, fmt.Errorf("decode target mixed file: %w", err)
	}
	srcMap, err := decodeJSONObject(src)
	if err != nil {
		return nil, fmt.Errorf("decode source mixed file: %w", err)
	}
	for key, l := range rule.Keys {
		if l != harness.LayerOwned {
			continue
		}
		if v, ok := srcMap[key]; ok {
			dstMap[key] = v
		}
	}
	return encodeJSONObject(dstMap)
}

// MixedSharedDrift returns the sorted names of the mixed file's shared-layer keys
// whose value in profileFile differs from the default setup's file. doctor uses it
// to report seeded shared keys that have drifted from the default setup (FR-013,
// FR-044). A key missing from either side is not reported as drift. A non-JSON ext
// yields no findings (nothing to compare in this slice).
func MixedSharedDrift(rule *harness.EntryRule, profileFile, defaultFile []byte, ext string) ([]string, error) {
	if !isJSONExt(ext) {
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
	for key, l := range rule.Keys {
		if l != harness.LayerShared {
			continue
		}
		pv, pok := pm[key]
		dv, dok := dm[key]
		if !pok || !dok {
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

// isJSONExt reports whether ext names a JSON file (case-insensitive).
func isJSONExt(ext string) bool {
	return strings.EqualFold(ext, ".json")
}

// IsJSONExt reports whether ext names a JSON file (case-insensitive).
func IsJSONExt(ext string) bool { return isJSONExt(ext) }
