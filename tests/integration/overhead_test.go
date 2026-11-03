package integration

import (
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// TestScenario9WrapperOverhead covers SC-011: a wrapper with no exec hook adds at most
// 50 ms to the harness's start time, measured as the median of 20 wrapper runs against
// the no-op stub minus the median of 20 direct stub runs. It is robust to a noisy
// machine: if the measurement itself is implausibly slow it skips, but when it can
// measure cleanly it asserts the 50 ms budget.
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

	const samples = 20
	// Warm up the OS caches for both binaries so the first slow run does not skew either
	// median.
	_ = timeOnce(t, e, stub)
	_ = timeOnce(t, e, wrapper)

	stubMedian := medianRun(t, e, stub, samples)
	wrapperMedian := medianRun(t, e, wrapper, samples)

	// Noise guard: spawning the stub should be fast; if the baseline is implausibly slow
	// the machine is too loaded to measure a 50 ms budget meaningfully.
	if stubMedian > 200*time.Millisecond {
		t.Skipf("baseline too noisy to measure overhead (stub median %v); skipping", stubMedian)
	}

	overhead := wrapperMedian - stubMedian
	if overhead < 0 {
		overhead = 0 // measurement noise; the wrapper is not actually faster
	}
	t.Logf("stub median=%v wrapper median=%v overhead=%v", stubMedian, wrapperMedian, overhead)
	if overhead > 50*time.Millisecond {
		t.Errorf("wrapper overhead %v exceeds the 50 ms budget (SC-011)", overhead)
	}
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

// medianRun runs a binary n times and returns the median duration, which is far more
// stable than the mean under scheduler jitter.
func medianRun(t *testing.T, e *env, bin string, n int) time.Duration {
	t.Helper()
	durs := make([]time.Duration, n)
	for i := 0; i < n; i++ {
		durs[i] = timeOnce(t, e, bin)
	}
	sort.Slice(durs, func(i, j int) bool { return durs[i] < durs[j] })
	return durs[n/2]
}
