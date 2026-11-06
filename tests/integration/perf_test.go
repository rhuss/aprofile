package integration

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/cc-deck/aprofile/internal/testutil"
)

// TestSyncTwentyProfilesUnderOneSecond checks the plan's performance goal: sync of
// 20 profiles across all levels and harnesses, on the fake default setups, takes
// under a second (first run and the idempotent second run).
func TestSyncTwentyProfilesUnderOneSecond(t *testing.T) {
	if testing.Short() || os.Getenv("APROFILE_PERF") == "0" {
		t.Skip("skipping timing test (-short or APROFILE_PERF=0)")
	}
	e := newEnv(t)
	for _, h := range allHarnesses {
		testutil.SeedDefaultsAt(t, e.root, h)
	}
	n := 0
	for n < 20 {
		h := allHarnesses[n%len(allHarnesses)]
		lv := allLevels[n%len(allLevels)]
		e.run(nil, "add", h, fmt.Sprintf("p%02d", n), "--auth-kind", interactiveKind[h], "--share", lv)
		n++
	}
	for _, run := range []string{"first", "second"} {
		start := time.Now()
		e.run(nil, "sync")
		if d := time.Since(start); d > time.Second {
			t.Errorf("%s sync of 20 profiles took %v (budget 1 s)", run, d)
		} else {
			t.Logf("%s sync of 20 profiles: %v", run, d)
		}
	}
}
