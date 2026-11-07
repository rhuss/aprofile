package integration

import (
	"fmt"
	"os"
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
