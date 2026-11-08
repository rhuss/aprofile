package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cc-deck/aprofile/internal/testutil"
)

// syncBudget is the plan's performance goal for one sync of 20 profiles.
const syncBudget = time.Second

// TestSyncTwentyProfilesUnderOneSecond checks the plan's performance goal: sync of
// 20 profiles across all levels and harnesses, on the fake default setups, takes
// under a second (first run and the idempotent second run).
//
// A single wall-clock sample is at the mercy of machine load, so a breach is
// measured once more, in a fresh setup, before it fails the test. The idempotent
// run is sampled twice per setup and its faster run counts.
func TestSyncTwentyProfilesUnderOneSecond(t *testing.T) {
	if testing.Short() || os.Getenv("APROFILE_PERF") == "0" {
		t.Skip("skipping timing test (-short or APROFILE_PERF=0)")
	}
	var first, second time.Duration
	for attempt := 1; attempt <= 2; attempt++ {
		first, second = timeTwentyProfileSyncs(t)
		t.Logf("attempt %d: first sync of 20 profiles %v, idempotent sync %v", attempt, first, second)
		if first <= syncBudget && second <= syncBudget {
			return
		}
	}
	if first > syncBudget {
		t.Errorf("first sync of 20 profiles took %v (budget %v) in two measurements", first, syncBudget)
	}
	if second > syncBudget {
		t.Errorf("idempotent sync of 20 profiles took %v (budget %v) in two measurements", second, syncBudget)
	}
}

// timeTwentyProfileSyncs builds a fresh environment with 20 profiles and returns
// the duration of the first sync and the faster of two idempotent syncs.
func timeTwentyProfileSyncs(t *testing.T) (first, second time.Duration) {
	t.Helper()
	e := newEnv(t)
	for _, h := range allHarnesses {
		testutil.SeedDefaultsAt(t, e.root, h)
	}
	for n := 0; n < 20; n++ {
		h := allHarnesses[n%len(allHarnesses)]
		lv := allLevels[n%len(allLevels)]
		e.run(nil, "add", h, fmt.Sprintf("p%02d", n), "--auth-kind", interactiveKind[h], "--share", lv)
	}
	timeSync := func() time.Duration {
		start := time.Now()
		e.run(nil, "sync")
		return time.Since(start)
	}
	first = timeSync()
	second = min(timeSync(), timeSync())
	return first, second
}

// joinBudget is the plan's goal for a join of 2,000 session files (spec 003).
const joinBudget = 5 * time.Second

// TestHistoryGroupJoinTwoThousandFilesUnderFiveSeconds: a join that moves 2,000
// session files into a group store completes within the budget (plan.md
// Performance Goals). A breach is measured once more before it fails.
func TestHistoryGroupJoinTwoThousandFilesUnderFiveSeconds(t *testing.T) {
	if testing.Short() || os.Getenv("APROFILE_PERF") == "0" {
		t.Skip("skipping timing test (-short or APROFILE_PERF=0)")
	}
	var took time.Duration
	for attempt := 1; attempt <= 2; attempt++ {
		e := newEnv(t)
		testutil.SeedDefaultsAt(t, e.root, "claude")
		e.run(nil, "group", "create", "claude/work")
		e.run(nil, "add", "claude", "big", "--auth-kind", "oauth-login")
		e.run(nil, "sync")
		dir := e.configDir("claude", "big")
		for i := 0; i < 2000; i++ {
			writeFile(t, filepath.Join(dir, "projects", fmt.Sprintf("p%02d", i%20), fmt.Sprintf("s%04d.jsonl", i)), "{}\n")
		}
		start := time.Now()
		e.run(nil, "edit", "claude/big", "--group", "work", "--yes")
		took = time.Since(start)
		t.Logf("attempt %d: join of 2,000 session files took %v", attempt, took)
		if n := countNamed([]string{e.groupStore("claude", "work")}, "s1999.jsonl"); n != 1 {
			t.Fatalf("the files were not moved into the store")
		}
		if took <= joinBudget {
			return
		}
	}
	t.Errorf("join of 2,000 session files took %v (budget %v) in two measurements", took, joinBudget)
}

// TestHistoryGroupSyncTwentyProfilesUnderOneSecond: sync of 20 profiles, half of
// them history group members, stays within the sync budget.
func TestHistoryGroupSyncTwentyProfilesUnderOneSecond(t *testing.T) {
	if testing.Short() || os.Getenv("APROFILE_PERF") == "0" {
		t.Skip("skipping timing test (-short or APROFILE_PERF=0)")
	}
	var first, second time.Duration
	for attempt := 1; attempt <= 2; attempt++ {
		e := newEnv(t)
		for _, h := range allHarnesses {
			testutil.SeedDefaultsAt(t, e.root, h)
		}
		for n := 0; n < 20; n++ {
			h := allHarnesses[n%len(allHarnesses)]
			args := []string{"add", h, fmt.Sprintf("p%02d", n), "--auth-kind", interactiveKind[h], "--share", allLevels[n%len(allLevels)]}
			if n%2 == 0 && h != "opencode" {
				args = append(args, "--group", "home")
			}
			e.run(nil, args...)
		}
		timeSync := func() time.Duration {
			start := time.Now()
			e.run(nil, "sync")
			return time.Since(start)
		}
		first = timeSync()
		second = min(timeSync(), timeSync())
		t.Logf("attempt %d: first sync %v, idempotent sync %v", attempt, first, second)
		if first <= syncBudget && second <= syncBudget {
			return
		}
	}
	t.Errorf("sync of 20 profiles with members took %v / %v (budget %v) in two measurements", first, second, syncBudget)
}

// TestHistoryGroupMemberWrapperOverhead: a member's wrapper (one more exported
// variable) stays within the SC-011 overhead budget.
func TestHistoryGroupMemberWrapperOverhead(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timing test in -short mode")
	}
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	e.run(nil, "add", "claude", "member", "--auth-kind", "oauth-login", "--group", "home")
	e.run(nil, "sync")
	wrapper := filepath.Join(e.root, ".local", "share", "aprofile", "bin", "claude-member")
	stub := filepath.Join(e.stubDir, "claude")
	for i := 0; i < 3; i++ {
		_ = timeOnce(t, e, stub)
		_ = timeOnce(t, e, wrapper)
	}
	var overhead time.Duration
	for attempt := 1; attempt <= 2; attempt++ {
		m := measureOverhead(t, e, stub, wrapper, 20)
		t.Logf("attempt %d: stub median=%v overhead p25=%v", attempt, m.stubMedian, m.p25)
		if m.stubMedian > 200*time.Millisecond {
			t.Skipf("baseline too noisy to measure overhead (stub median %v); skipping", m.stubMedian)
		}
		overhead = m.p25
		if overhead <= overheadBudget {
			return
		}
	}
	t.Errorf("member wrapper overhead %v exceeds the %v budget in two measurements", overhead, overheadBudget)
}
