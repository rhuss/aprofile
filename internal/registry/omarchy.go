package registry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cc-deck/aprofile/internal/config"
)

// OmarchyProfile is the minimal per-profile information the Omarchy export needs
// (FR-031). Provider groups profiles into per-provider accounts files.
type OmarchyProfile struct {
	Harness  string
	Name     string
	Provider string // from the harness definition's omarchy_provider
	Label    string
	Home     string // the profile config dir
	Primary  bool
}

// OmarchyAccount is the part of an Omarchy accounts entry aprofile writes. Other
// fields of an entry (and other top-level keys of the file) are kept as they are.
type OmarchyAccount struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Home    string `json:"home"`
	Primary bool   `json:"primary"`
}

// omarchyIDPrefix marks accounts aprofile manages. Entries without it are preserved
// unchanged; aprofile entries for profiles that no longer exist are removed (FR-031).
const omarchyIDPrefix = "aprofile:"

// OmarchyID is the stable account id aprofile writes for a profile: aprofile:<h>/<n>.
func OmarchyID(harness, name string) string {
	return omarchyIDPrefix + harness + "/" + name
}

// ExportOmarchy writes one accounts file per provider under outputDir (FR-031).
//
// Every existing <provider>.json in outputDir is visited, not only the providers
// that currently have profiles, so the stale aprofile entries of a provider whose
// last profile was removed are dropped too. In each file:
//   - entries whose id lacks the "aprofile:" prefix are kept verbatim (raw JSON);
//   - an aprofile entry of a current profile of that provider is updated in place
//     (id, label, home, primary), keeping any other field another tool added;
//   - an aprofile entry whose profile id is in keep (a profile that exists but could
//     not be exported, e.g. it failed to load) is kept verbatim;
//   - every other aprofile entry is removed;
//   - aprofile entries not yet present are appended, sorted by id;
//   - other top-level keys are kept, in their order.
//
// A file is never deleted: one left without accounts keeps an empty list. A file
// with nothing to change is not rewritten, so aprofile never reformats a file it has
// no entries in. All files are parsed before any is written: an unparseable file
// aborts the export with nothing written. It returns the paths written, sorted.
// Profiles with an empty provider are ignored (the harness opts out of Omarchy).
func ExportOmarchy(outputDir string, profiles []OmarchyProfile, keep []string) ([]string, error) {
	desired := map[string]map[string]OmarchyAccount{} // provider -> id -> account
	for _, p := range profiles {
		if p.Provider == "" {
			continue
		}
		if desired[p.Provider] == nil {
			desired[p.Provider] = map[string]OmarchyAccount{}
		}
		id := OmarchyID(p.Harness, p.Name)
		desired[p.Provider][id] = OmarchyAccount{ID: id, Label: p.Label, Home: p.Home, Primary: p.Primary}
	}
	keepIDs := map[string]bool{}
	for _, id := range keep {
		keepIDs[omarchyIDPrefix+id] = true
	}

	providers := map[string]bool{}
	for prov := range desired {
		providers[prov] = true
	}
	existing, err := filepath.Glob(filepath.Join(outputDir, "*.json"))
	if err != nil {
		return nil, err
	}
	for _, f := range existing {
		providers[strings.TrimSuffix(filepath.Base(f), ".json")] = true
	}
	names := make([]string, 0, len(providers))
	for prov := range providers {
		names = append(names, prov)
	}
	sort.Strings(names)

	type pending struct {
		path string
		data []byte
		mode os.FileMode
	}
	var writes []pending
	for _, prov := range names {
		path := filepath.Join(outputDir, prov+".json")
		data, mode, changed, err := mergeOmarchyFile(path, desired[prov], keepIDs)
		if err != nil {
			return nil, err
		}
		if changed {
			writes = append(writes, pending{path, data, mode})
		}
	}

	var written []string
	for _, w := range writes {
		if err := config.WriteFileAtomic(w.path, w.data, w.mode); err != nil {
			return written, err
		}
		written = append(written, w.path)
	}
	return written, nil
}

// mergeOmarchyFile computes the new content of one provider file. It reports
// changed=false when the file needs no write.
func mergeOmarchyFile(path string, want map[string]OmarchyAccount, keep map[string]bool) (data []byte, mode os.FileMode, changed bool, err error) {
	mode = 0o644
	top := &rawObject{vals: map[string]json.RawMessage{}}
	raw, rerr := os.ReadFile(path)
	exists := rerr == nil
	switch {
	case exists:
		if fi, err := os.Stat(path); err == nil {
			mode = fi.Mode().Perm()
		}
		top, err = parseRawObject(raw)
		if err != nil {
			return nil, 0, false, fmt.Errorf("parse omarchy accounts %s: %w", path, err)
		}
	case !os.IsNotExist(rerr):
		return nil, 0, false, fmt.Errorf("read omarchy accounts %s: %w", path, rerr)
	}
	if !exists && len(want) == 0 {
		return nil, 0, false, nil
	}

	var entries []json.RawMessage
	if v, ok := top.vals["accounts"]; ok && !isJSONNull(v) {
		if err := json.Unmarshal(v, &entries); err != nil {
			return nil, 0, false, fmt.Errorf("parse omarchy accounts %s: \"accounts\" is not a list: %w", path, err)
		}
	}

	modified := !exists
	emitted := map[string]bool{}
	out := make([]json.RawMessage, 0, len(entries)+len(want))
	for _, e := range entries {
		var head struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(e, &head) != nil || !strings.HasPrefix(head.ID, omarchyIDPrefix) {
			out = append(out, e) // foreign (or not even an object): keep verbatim
			continue
		}
		acct, ok := want[head.ID]
		switch {
		case ok && !emitted[head.ID]:
			merged, err := mergeAccount(e, acct)
			if err != nil {
				return nil, 0, false, fmt.Errorf("omarchy accounts %s: %w", path, err)
			}
			if !jsonEqual(e, merged) {
				modified = true
			}
			out = append(out, merged)
			emitted[head.ID] = true
		case keep[head.ID] && !ok:
			out = append(out, e)
		default:
			modified = true // stale (or duplicate) aprofile entry: dropped
		}
	}
	ids := make([]string, 0, len(want))
	for id := range want {
		if !emitted[id] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		b, err := marshalNoEscape(want[id])
		if err != nil {
			return nil, 0, false, err
		}
		out = append(out, b)
		modified = true
	}
	if !modified {
		return nil, 0, false, nil
	}

	list, err := joinArray(out)
	if err != nil {
		return nil, 0, false, err
	}
	top.set("accounts", list)
	data, err = top.marshalIndent()
	if err != nil {
		return nil, 0, false, fmt.Errorf("encode omarchy accounts %s: %w", path, err)
	}
	if exists && bytes.Equal(raw, data) {
		return nil, 0, false, nil
	}
	return data, mode, true, nil
}

// mergeAccount sets aprofile's four fields on an existing entry, keeping its other
// fields and their order.
func mergeAccount(entry json.RawMessage, a OmarchyAccount) (json.RawMessage, error) {
	obj, err := parseRawObject(entry)
	if err != nil {
		return nil, err
	}
	for _, kv := range []struct {
		k string
		v any
	}{{"id", a.ID}, {"label", a.Label}, {"home", a.Home}, {"primary", a.Primary}} {
		b, err := marshalNoEscape(kv.v)
		if err != nil {
			return nil, err
		}
		obj.set(kv.k, b)
	}
	return obj.marshal()
}

// rawObject is a JSON object whose values stay raw and whose key order is kept,
// so a rewrite changes only the values aprofile sets.
type rawObject struct {
	keys []string
	vals map[string]json.RawMessage
}

func parseRawObject(data []byte) (*rawObject, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("not a JSON object")
	}
	o := &rawObject{vals: map[string]json.RawMessage{}}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("invalid object key %v", tok)
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		if _, dup := o.vals[key]; !dup {
			o.keys = append(o.keys, key)
		}
		o.vals[key] = v
	}
	if _, err := dec.Token(); err != nil { // closing '}'
		return nil, err
	}
	// Only whitespace may follow the object: anything else (another value, a stray
	// delimiter, garbage) is an error, so a rewrite never silently drops it.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("trailing data after the JSON object")
	}
	return o, nil
}

func (o *rawObject) set(key string, v json.RawMessage) {
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = v
}

func (o *rawObject) marshal() (json.RawMessage, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := marshalNoEscape(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(o.vals[k])
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// marshalIndent renders the object with two-space indentation and a trailing
// newline, the layout aprofile's other JSON files use.
func (o *rawObject) marshalIndent() ([]byte, error) {
	compact, err := o.marshal()
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, compact, "", "  "); err != nil {
		return nil, err
	}
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

func joinArray(items []json.RawMessage) (json.RawMessage, error) {
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, it := range items {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(it)
	}
	buf.WriteByte(']')
	return buf.Bytes(), nil
}

// marshalNoEscape is json.Marshal without HTML escaping (labels may contain &).
func marshalNoEscape(v any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func jsonEqual(a, b json.RawMessage) bool {
	var ca, cb bytes.Buffer
	if json.Compact(&ca, a) != nil || json.Compact(&cb, b) != nil {
		return false
	}
	return bytes.Equal(ca.Bytes(), cb.Bytes())
}

func isJSONNull(v json.RawMessage) bool { return string(bytes.TrimSpace(v)) == "null" }
