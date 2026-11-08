package harness_test

import (
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
)

// TestBuiltinHistoryEntries pins the history entries of the built-in definitions
// (spec 003 data-model.md "Built-in values", research.md R1).
func TestBuiltinHistoryEntries(t *testing.T) {
	want := map[string]map[string]harness.MergeMode{
		"claude": {
			"projects/":     harness.MergePerFile,
			"history.jsonl": harness.MergeNone,
			"paste-cache/":  harness.MergePerFile,
			"file-history/": harness.MergePerFile,
			"todos/":        harness.MergePerFile,
			"plans/":        harness.MergePerFile,
			"tasks/":        harness.MergePerFile,
		},
		"codex": {
			"sessions/":                harness.MergePerFile,
			"archived_sessions/":       harness.MergePerFile,
			"session_index.jsonl":      harness.MergeNone,
			"session_index.jsonl.lock": harness.MergeNone,
			"history.jsonl":            harness.MergeNone,
		},
		"pi": {
			"agent/sessions/": harness.MergePerFile,
		},
		"opencode": {},
	}
	set, _ := harness.LoadBuiltins()
	if errs := set.Errors(); len(errs) != 0 {
		t.Fatalf("load errors: %v", errs)
	}
	for name, entries := range want {
		d, ok := set.Get(name)
		if !ok {
			t.Fatalf("no built-in %q", name)
		}
		got := map[string]harness.MergeMode{}
		for _, r := range d.HistoryRules() {
			if r.Layer != harness.LayerState {
				t.Errorf("%s: history rule %q has layer %q", name, r.Entry, r.Layer)
			}
			got[r.Entry] = r.EffectiveMerge()
		}
		if len(got) != len(entries) {
			t.Errorf("%s: history rules = %v, want %v", name, got, entries)
		}
		for e, m := range entries {
			if got[e] != m {
				t.Errorf("%s: %q merge = %q, want %q", name, e, got[e], m)
			}
		}
		if d.SupportsGroups() != (len(entries) > 0) {
			t.Errorf("%s: SupportsGroups() = %v", name, d.SupportsGroups())
		}
	}
	// Entries that stay private (research R1): per-process or third-party state.
	claude, _ := set.Get("claude")
	for _, e := range []string{"sessions/", "session-env/", "shell-snapshots/", "teams/", "transcripts/"} {
		if claude.HistoryRule(e) != nil {
			t.Errorf("claude %q must stay private state", e)
		}
	}
	pi, _ := set.Get("pi")
	if pi.HistoryNote == "" {
		t.Error("pi must carry a history_note about the session format migration")
	}
}
