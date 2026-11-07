package layer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/cc-deck/aprofile/internal/secret"
)

// This file holds the settings copy rules of FR-018 (settings pull) and the
// per-key secret warnings of research R6. Settings are JSON only; other formats
// are handled as whole entries by the callers.

// MergeSettings merges the default setup's settings into a profile's own (FR-018):
// objects merge by key, lists gain the default's entries that are missing (profile
// entries stay, order kept), scalars take the default's value, and keys or entries
// only the profile has are kept. keys limits the pull to these top-level keys (all
// of the default's keys when empty); exclude names keys never touched (the hook
// location and owned keys). It returns the new document and the key paths removed
// (always none for a merge) and changed.
func MergeSettings(profileDoc, defaultDoc []byte, keys []string, exclude []string) (out []byte, removed, changed []string, err error) {
	pm, dm, err := decodePair(profileDoc, defaultDoc)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, k := range pullKeys(dm, keys, exclude) {
		dv, ok := dm[k]
		if !ok {
			continue
		}
		pv, had := pm[k]
		if !had {
			pm[k] = dv
			changed = append(changed, k)
			continue
		}
		merged := mergeValue(k, pv, dv, &changed)
		pm[k] = merged
	}
	out, err = encodeValue(pm)
	sort.Strings(changed)
	return out, nil, changed, err
}

// ReplaceSettings makes the pulled keys (or, without keys, the whole file) an exact
// copy of the default setup's, still excluding the hook location and owned keys
// (FR-018 --replace). It returns the key paths removed and changed, so a dry run can
// list for example a permission granted only in the profile (US3 AS6).
func ReplaceSettings(profileDoc, defaultDoc []byte, keys []string, exclude []string) (out []byte, removed, changed []string, err error) {
	pm, dm, err := decodePair(profileDoc, defaultDoc)
	if err != nil {
		return nil, nil, nil, err
	}
	targets := pullKeys(dm, keys, exclude)
	if len(keys) == 0 {
		// The whole file: profile-only keys go too, excluded keys stay.
		for k := range pm {
			if _, inDefault := dm[k]; !inDefault && !containsString(exclude, k) {
				targets = append(targets, k)
			}
		}
	}
	sort.Strings(targets)
	seen := map[string]bool{}
	for _, k := range targets {
		if seen[k] {
			continue
		}
		seen[k] = true
		pv, had := pm[k]
		dv, inDefault := dm[k]
		switch {
		case !inDefault && had:
			delete(pm, k)
			removed = append(removed, k)
		case !inDefault:
		case !had:
			pm[k] = dv
			changed = append(changed, k)
		default:
			diffReplace(k, pv, dv, &removed, &changed)
			pm[k] = dv
		}
	}
	out, err = encodeValue(pm)
	sort.Strings(removed)
	sort.Strings(changed)
	return out, removed, changed, err
}

// WithoutKey returns a JSON document without one top-level key (the hook location
// of a settings copy, FR-010). An invalid document is an error naming the file.
func WithoutKey(data []byte, key, file string) ([]byte, error) {
	out, _, err := withoutKey(data, key, file)
	return out, err
}

// settingsSecretOptions is how a settings-layer value is judged: the prefix check
// only (settings routinely hold long values without spaces, such as MCP tool
// permission rules and model ids), with references treated as not literal.
var settingsSecretOptions = secret.Options{PrefixOnly: true, SkipReferences: true}

// SecretWarnings returns the key paths of string values that look like secrets: a
// value with a known secret prefix (secret.LooksLikeLiteralSecret with
// settingsSecretOptions), or a literal value under a credential-like key name
// (secret.SecretUnderCredentialKey: references and environment variable names such
// as Codex's env_key = "OPENAI_API_KEY" are not literals). Values are never
// included (Principle IV, research R6).
func SecretWarnings(doc []byte) []string {
	var v any
	if err := json.Unmarshal(doc, &v); err != nil {
		return nil
	}
	var out []string
	var walk func(path, key string, v any)
	walk = func(path, key string, v any) {
		switch t := v.(type) {
		case map[string]any:
			for k, c := range t {
				walk(joinPath(path, k), k, c)
			}
		case []any:
			for i, c := range t {
				walk(fmt.Sprintf("%s[%d]", path, i), key, c)
			}
		case string:
			if t == "" {
				return
			}
			if secret.LooksLikeLiteralSecret(t, settingsSecretOptions) || secret.SecretUnderCredentialKey(key, t) {
				out = append(out, path)
			}
		}
	}
	walk("", "", v)
	sort.Strings(out)
	return out
}

func joinPath(path, k string) string {
	if path == "" {
		return k
	}
	return path + "." + k
}

func decodePair(profileDoc, defaultDoc []byte) (map[string]any, map[string]any, error) {
	pm, err := decodeAnyObject(profileDoc)
	if err != nil {
		return nil, nil, fmt.Errorf("profile settings are not a valid JSON object: %w", err)
	}
	dm, err := decodeAnyObject(defaultDoc)
	if err != nil {
		return nil, nil, fmt.Errorf("default setup settings are not a valid JSON object: %w", err)
	}
	return pm, dm, nil
}

func decodeAnyObject(data []byte) (map[string]any, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{}, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("top level is not an object")
	}
	return m, nil
}

func encodeValue(v any) ([]byte, error) { return EncodeJSON(v) }

// pullKeys returns the top-level keys to pull: the named ones, or all of the
// default's, minus excluded keys.
func pullKeys(dm map[string]any, keys, exclude []string) []string {
	var out []string
	if len(keys) == 0 {
		for k := range dm {
			out = append(out, k)
		}
	} else {
		out = append(out, keys...)
	}
	var kept []string
	for _, k := range out {
		if !containsString(exclude, k) {
			kept = append(kept, k)
		}
	}
	sort.Strings(kept)
	return kept
}

func mergeValue(path string, pv, dv any, changed *[]string) any {
	switch d := dv.(type) {
	case map[string]any:
		p, ok := pv.(map[string]any)
		if !ok {
			*changed = append(*changed, path)
			return dv
		}
		keys := make([]string, 0, len(d))
		for k := range d {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if cur, had := p[k]; had {
				p[k] = mergeValue(joinPath(path, k), cur, d[k], changed)
			} else {
				p[k] = d[k]
				*changed = append(*changed, joinPath(path, k))
			}
		}
		return p
	case []any:
		p, ok := pv.([]any)
		if !ok {
			*changed = append(*changed, path)
			return dv
		}
		out := append([]any(nil), p...)
		for _, item := range d {
			if !containsJSON(out, item) {
				out = append(out, item)
				*changed = append(*changed, path+"["+itemLabel(item)+"]")
			}
		}
		return out
	default:
		if canonical(pv) != canonical(dv) {
			*changed = append(*changed, path)
		}
		return dv
	}
}

// diffReplace records what replacing pv by dv removes or changes.
func diffReplace(path string, pv, dv any, removed, changed *[]string) {
	switch p := pv.(type) {
	case map[string]any:
		d, ok := dv.(map[string]any)
		if !ok {
			*changed = append(*changed, path)
			return
		}
		for k, cur := range p {
			if nv, had := d[k]; had {
				diffReplace(joinPath(path, k), cur, nv, removed, changed)
			} else {
				*removed = append(*removed, joinPath(path, k))
			}
		}
		for k := range d {
			if _, had := p[k]; !had {
				*changed = append(*changed, joinPath(path, k))
			}
		}
	case []any:
		d, ok := dv.([]any)
		if !ok {
			*changed = append(*changed, path)
			return
		}
		for _, item := range p {
			if !containsJSON(d, item) {
				*removed = append(*removed, path+"["+itemLabel(item)+"]")
			}
		}
		for _, item := range d {
			if !containsJSON(p, item) {
				*changed = append(*changed, path+"["+itemLabel(item)+"]")
			}
		}
	default:
		if canonical(pv) != canonical(dv) {
			*changed = append(*changed, path)
		}
	}
}

func containsJSON(list []any, item any) bool {
	c := canonical(item)
	for _, v := range list {
		if canonical(v) == c {
			return true
		}
	}
	return false
}

// itemLabel renders a list item for a change list: its JSON, unless a string in it
// looks like a secret (for example a permission recorded with a token in its
// command), then a placeholder; values that look like secrets are never printed.
func itemLabel(item any) string {
	if len(SecretWarnings(mustJSON(map[string]any{"item": item}))) > 0 || containsSecretPrefix(item) {
		return "<value hidden: looks like a secret>"
	}
	return canonical(item)
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// containsSecretPrefix reports whether any string in v contains a known secret
// prefix anywhere (not only at its start).
func containsSecretPrefix(v any) bool {
	switch t := v.(type) {
	case string:
		for _, field := range strings.FieldsFunc(t, func(r rune) bool { return r == ' ' || r == '=' || r == '(' || r == ')' || r == '"' || r == '\'' }) {
			if secret.CheckPrefix(field) != nil {
				return true
			}
		}
	case []any:
		for _, c := range t {
			if containsSecretPrefix(c) {
				return true
			}
		}
	case map[string]any:
		for _, c := range t {
			if containsSecretPrefix(c) {
				return true
			}
		}
	}
	return false
}

// canonical renders a value as key-sorted JSON for comparison.
func canonical(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
