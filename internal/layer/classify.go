// Package layer classifies the entries of a profile config dir into categories
// (credentials, state, owned, mixed, and the borrowable content, settings, plugin
// store and hook location) or reports them as unclassified, and plans and applies
// the config dir layout for a profile's share level. It contains no harness-name
// branching: every decision comes from the harness definition (Principle I).
package layer

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/secret"
)

// Result is the classification of a single config-dir entry.
type Result struct {
	Entry        string             // the entry name as given
	Layer        harness.Layer      // the entry's category (credentials ... hook-location)
	Unclassified bool               // true when no rule matched
	Rule         *harness.EntryRule // the matching rule (nil when unclassified)
}

// Classify maps one config-dir entry to its layer using the definition's rules in
// order; the first matching rule wins (FR-013). An entry no rule matches is
// returned Unclassified (FR-018). A trailing slash on either side is ignored so a
// directory entry "projects" matches a rule written "projects/".
func Classify(def *harness.Definition, entry string) Result {
	norm := normalize(filepath.ToSlash(entry))
	// The hook location is a category of its own when it is a separate entry (a
	// hooks file or a directory of hook files). It is a named entry, so it wins over
	// glob rules: a user catch-all must not link it and block hook sets.
	hr := def.HookLocationRule()
	isHook := hr != nil && hr.Path() == norm
	for i := range def.Classification {
		rule := &def.Classification[i]
		if isHook && rule.IsGlob() {
			break
		}
		if matchEntry(rule.Entry, norm) {
			return Result{Entry: entry, Layer: rule.Layer, Rule: rule}
		}
	}
	if isHook {
		return Result{Entry: entry, Layer: harness.LayerHookLocation, Rule: hr}
	}
	return Result{Entry: entry, Unclassified: true}
}

// isContainer reports whether rel is a parent directory of some classified entry
// (OpenCode's "config" and "config/opencode", pi's "agent"), so its children are
// classified one by one rather than the directory itself being unclassified.
func isContainer(def *harness.Definition, rel string) bool {
	prefix := normalize(filepath.ToSlash(rel)) + "/"
	for _, r := range def.Classification {
		if strings.HasPrefix(filepath.ToSlash(r.Entry), prefix) {
			return true
		}
	}
	if hr := def.HookLocationRule(); hr != nil && strings.HasPrefix(hr.Path(), prefix) {
		return true
	}
	return false
}

// walkedEntry is one existing entry found by walk.
type walkedEntry struct {
	Rel    string
	Result Result
	Link   bool
	Target string
}

// walk lists the entries under root (profile-relative names prefixed with
// prefix), descending only into real container directories. Classified entries
// are not descended into: their contents belong to them. aprofile's own temp names
// (IsTempName, left by an interrupted atomic write) are skipped: they are never
// entries of the profile (T091).
func walk(def *harness.Definition, root, prefix string) ([]walkedEntry, error) {
	return walkWithTemps(def, root, prefix, nil)
}

// walkWithTemps is walk that also collects the profile-relative names of the temp
// entries it skips into temps (when not nil), from the same directories walk reads.
func walkWithTemps(def *harness.Definition, root, prefix string, temps *[]string) ([]walkedEntry, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", root, err)
	}
	var out []walkedEntry
	for _, e := range entries {
		rel := e.Name()
		if prefix != "" {
			rel = prefix + "/" + e.Name()
		}
		if IsTempName(e.Name()) {
			if temps != nil {
				*temps = append(*temps, rel)
			}
			continue
		}
		path := filepath.Join(root, e.Name())
		we := walkedEntry{Rel: rel, Result: Classify(def, rel)}
		if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			we.Link = true
			we.Target, _ = os.Readlink(path)
		}
		if we.Result.Unclassified && !we.Link && e.IsDir() && isContainer(def, rel) {
			sub, err := walkWithTemps(def, path, rel, temps)
			if err != nil {
				return nil, err
			}
			out = append(out, sub...)
			continue
		}
		out = append(out, we)
	}
	return out, nil
}

// UnborrowedDefaultEntries lists the default-setup entries no classification rule
// covers (FR-001a). They are never borrowed by any profile; show lists them with
// `aprofile harness classify` as the fix and doctor reports them once per harness.
func UnborrowedDefaultEntries(def *harness.Definition) ([]string, error) {
	var out []string
	for _, root := range def.DefaultRoots() {
		if root.Prefix != "" && !Classify(def, root.Prefix).Unclassified {
			continue // the whole root is one classified entry (for example state)
		}
		found, err := walk(def, root.Dir, root.Prefix)
		if err != nil {
			return nil, err
		}
		for _, we := range found {
			if we.Result.Unclassified {
				out = append(out, we.Rel)
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// ClassifyKey resolves the layer of a top-level key within a mixed file. A key the
// mixed rule does not declare returns false: it is seeded like a settings key unless
// its name looks like a credential (seedsKey), and doctor reports it once per
// harness (UndeclaredDefaultKeys, FR-013).
func ClassifyKey(rule *harness.EntryRule, key string) (harness.Layer, bool) {
	l, ok := rule.Keys[key]
	return l, ok
}

// LooksLikeCredentialKey reports whether a mixed-file key name suggests it holds a
// credential (secret.LooksLikeSecretKey: a token, secret, password, API key, account
// or auth data, or any name ending in "key"). An undeclared key with such a name is
// never seeded, so a key a harness adds in a later version cannot carry a secret
// into a new profile (Principle IV). False positives only cost a seeded preference.
func LooksLikeCredentialKey(key string) bool {
	return secret.LooksLikeSecretKey(key)
}

// seedsKey reports whether a mixed file's key is copied from the default setup when
// the file is seeded (at all and content): credentials and state keys never are,
// nor is an undeclared key that looks like a credential. Owned, settings, content
// and shared keys and the other undeclared keys are.
func seedsKey(rule *harness.EntryRule, key string) bool {
	l, declared := ClassifyKey(rule, key)
	if !declared {
		return !LooksLikeCredentialKey(key)
	}
	return l != harness.LayerCredentials && l != harness.LayerState
}

// UndeclaredKeys lists the top-level keys of one mixed file in the default setup
// that the definition does not declare: Seeded ones are copied into new profiles,
// NotSeeded ones are left out because their name looks like a credential.
type UndeclaredKeys struct {
	Entry     string   `json:"entry"`
	Seeded    []string `json:"seeded,omitempty"`
	NotSeeded []string `json:"not_seeded,omitempty"`
}

// UndeclaredDefaultKeys returns, per mixed JSON file of the definition, the
// undeclared top-level keys of the default setup's file (FR-013). doctor reports
// them once per harness. A missing or unparsable default file is skipped.
func UndeclaredDefaultKeys(def *harness.Definition) ([]UndeclaredKeys, error) {
	var out []UndeclaredKeys
	for i := range def.Classification {
		rule := &def.Classification[i]
		if rule.Layer != harness.LayerMixed || rule.IsGlob() || !IsJSONExt(filepath.Ext(rule.Path())) {
			continue
		}
		data, err := os.ReadFile(def.DefaultPath(rule.Path()))
		if err != nil {
			continue
		}
		m, err := decodeJSONObject(data)
		if err != nil {
			continue
		}
		u := UndeclaredKeys{Entry: rule.Path()}
		for k := range m {
			if _, ok := ClassifyKey(rule, k); ok {
				continue
			}
			if seedsKey(rule, k) {
				u.Seeded = append(u.Seeded, k)
			} else {
				u.NotSeeded = append(u.NotSeeded, k)
			}
		}
		if len(u.Seeded)+len(u.NotSeeded) == 0 {
			continue
		}
		sort.Strings(u.Seeded)
		sort.Strings(u.NotSeeded)
		out = append(out, u)
	}
	return out, nil
}

// normalize strips a trailing slash so directory and file spellings compare equal.
func normalize(entry string) string {
	return strings.TrimSuffix(entry, "/")
}

// matchEntry reports whether ruleEntry matches the normalized entry. A rule with
// glob metacharacters is matched with path.Match; otherwise it is an exact match.
func matchEntry(ruleEntry, entry string) bool {
	re := normalize(ruleEntry)
	if strings.ContainsAny(re, "*?[") {
		ok, err := filepath.Match(re, entry)
		return err == nil && ok
	}
	return re == entry
}
