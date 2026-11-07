package integration

import (
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// overheadBudget is SC-011's limit on what a wrapper without an exec hook may add
// to the harness's start time.
const overheadBudget = 50 * time.Millisecond

// TestScenario9WrapperOverhead covers SC-011: a wrapper with no exec hook adds at most
// 50 ms to the harness's start time.
//
// The measurement is built to resist machine load changing during the test: stub and
// wrapper runs are interleaved in pairs (alternating which goes first), the overhead
// of each pair is the wrapper time minus the stub time of that same pair, and the
// result is the 25th percentile of those per-pair differences, so a burst of load
// that slows a few pairs does not count against the wrapper. A breach is measured a
// second time before it fails the test, and a baseline so slow that a 50 ms budget
// cannot be measured skips instead of failing.
func TestScenario9WrapperOverhead(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timing test in -short mode")
	}
	e := newEnv(t)
	// An interactive auth kind needs no reference, so the wrapper does no secret
	// resolution and the measurement reflects pure wrapper overhead (SC-011: no hook).
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login")
	e.run(nil, "sync")

	wrapper := filepath.Join(e.root, ".local", "share", "aprofile", "bin", "claude-work")
	stub := filepath.Join(e.stubDir, "claude")

	// Warm up the OS caches for both binaries so the first slow runs do not count.
	for i := 0; i < 3; i++ {
		_ = timeOnce(t, e, stub)
		_ = timeOnce(t, e, wrapper)
	}

	const pairs = 20
	var overhead time.Duration
	for attempt := 1; attempt <= 2; attempt++ {
		m := measureOverhead(t, e, stub, wrapper, pairs)
		t.Logf("attempt %d: stub median=%v overhead p25=%v min=%v", attempt, m.stubMedian, m.p25, m.min)
		// Noise guard: spawning the stub should be fast; an implausibly slow baseline
		// means the machine is too loaded to measure a 50 ms budget meaningfully.
		if m.stubMedian > 200*time.Millisecond {
			t.Skipf("baseline too noisy to measure overhead (stub median %v); skipping", m.stubMedian)
		}
		overhead = m.p25
		if overhead <= overheadBudget {
			return
		}
	}
	t.Errorf("wrapper overhead %v exceeds the %v budget (SC-011) in two measurements", overhead, overheadBudget)
}

// overheadSample summarizes one interleaved measurement.
type overheadSample struct {
	stubMedian time.Duration // median direct stub run, for the noise guard
	p25        time.Duration // 25th percentile of per-pair wrapper-minus-stub differences
	min        time.Duration // smallest per-pair difference (logged for diagnosis)
}

// measureOverhead runs n stub/wrapper pairs back to back, alternating the order
// within a pair so neither binary systematically runs first, and returns the
// per-pair overhead statistics. Negative differences (noise) count as zero.
func measureOverhead(t *testing.T, e *env, stub, wrapper string, n int) overheadSample {
	t.Helper()
	stubs := make([]time.Duration, n)
	diffs := make([]time.Duration, n)
	for i := 0; i < n; i++ {
		var s, w time.Duration
		if i%2 == 0 {
			s = timeOnce(t, e, stub)
			w = timeOnce(t, e, wrapper)
		} else {
			w = timeOnce(t, e, wrapper)
			s = timeOnce(t, e, stub)
		}
		stubs[i] = s
		d := w - s
		if d < 0 {
			d = 0
		}
		diffs[i] = d
	}
	sortDurations(stubs)
	sortDurations(diffs)
	return overheadSample{stubMedian: stubs[n/2], p25: diffs[n/4], min: diffs[0]}
}

func sortDurations(d []time.Duration) {
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
}

// timeOnce runs a binary once (output discarded) and returns the wall-clock duration.
func timeOnce(t *testing.T, e *env, bin string) time.Duration {
	t.Helper()
	cmd := exec.Command(bin, "--version")
	cmd.Env = e.vars
	start := time.Now()
	if err := cmd.Run(); err != nil {
		t.Fatalf("run %s: %v", bin, err)
	}
	return time.Since(start)
}
