// Package layer classifies the entries of a profile config dir into the four
// isolation layers (credentials, owned, state, shared) or reports them as
// unclassified, and prepares a config dir accordingly. It contains no
// harness-name branching: every decision comes from the harness definition's
// classification rules (Principle I).
package layer

import (
	"path/filepath"
	"strings"

	"github.com/cc-deck/aprofile/internal/harness"
)

// Result is the classification of a single config-dir entry.
type Result struct {
	Entry        string             // the entry name as given
	Layer        harness.Layer      // credentials/owned/state/shared, or mixed for a mixed file
	Unclassified bool               // true when no rule matched
	Rule         *harness.EntryRule // the matching rule (nil when unclassified)
}

// Classify maps one config-dir entry to its layer using the definition's rules in
// order; the first matching rule wins (FR-013). An entry no rule matches is
// returned Unclassified (FR-018). A trailing slash on either side is ignored so a
// directory entry "projects" matches a rule written "projects/".
func Classify(def *harness.Definition, entry string) Result {
	norm := normalize(entry)
	for i := range def.Classification {
		rule := &def.Classification[i]
		if matchEntry(rule.Entry, norm) {
			return Result{Entry: entry, Layer: rule.Layer, Rule: rule}
		}
	}
	return Result{Entry: entry, Unclassified: true}
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
