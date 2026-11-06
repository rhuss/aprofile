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
// are not descended into: their contents belong to them.
func walk(def *harness.Definition, root, prefix string) ([]walkedEntry, error) {
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
		path := filepath.Join(root, e.Name())
		we := walkedEntry{Rel: rel, Result: Classify(def, rel)}
		if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			we.Link = true
			we.Target, _ = os.Readlink(path)
		}
		if we.Result.Unclassified && !we.Link && e.IsDir() && isContainer(def, rel) {
			sub, err := walk(def, path, rel)
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
// mixed rule does not cover is reported unclassified (FR-013, FR-018).
func ClassifyKey(rule *harness.EntryRule, key string) (harness.Layer, bool) {
	l, ok := rule.Keys[key]
	return l, ok
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
