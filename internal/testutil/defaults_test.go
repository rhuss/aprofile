package testutil

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestSeedHistoryWritesResearchFixtures(t *testing.T) {
	cases := map[string][]string{
		"claude": {
			"projects/-tmp-demo/" + HistorySessionID + ".jsonl",
			"projects/-tmp-demo/memory/MEMORY.md",
			"history.jsonl",
			"paste-cache/h1.txt",
			"file-history/" + HistorySessionID + "/a@v1",
			"todos/" + HistorySessionID + "-agent-x.json",
			"plans/slug.md",
			"tasks/" + HistorySessionID + "/1.json",
		},
		"codex": {
			"sessions/2026/11/07/rollout-x-" + HistorySessionID + ".jsonl",
			"session_index.jsonl",
			"session_index.jsonl.lock",
			"history.jsonl",
		},
		"pi": {
			"agent/sessions/--tmp--/ts_" + HistorySessionID + ".jsonl",
		},
	}
	for h, want := range cases {
		t.Run(h, func(t *testing.T) {
			home := HermeticHome(t)
			dir := filepath.Join(home.Root, "cfg")
			got := SeedHistory(t, dir, h)
			sort.Strings(got)
			sort.Strings(want)
			if len(got) != len(want) {
				t.Fatalf("SeedHistory(%s) = %v, want %v", h, got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("SeedHistory(%s) = %v, want %v", h, got, want)
				}
				if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(want[i]))); err != nil {
					t.Errorf("%s not written: %v", want[i], err)
				}
			}
		})
	}
}

func TestSeedHistoryUnknownHarnessWritesNothing(t *testing.T) {
	home := HermeticHome(t)
	dir := filepath.Join(home.Root, "cfg")
	if got := SeedHistory(t, dir, "opencode"); len(got) != 0 {
		t.Fatalf("opencode has no history fixtures, got %v", got)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("SeedHistory created %s for a harness without history", dir)
	}
}
