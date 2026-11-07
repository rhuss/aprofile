package harness

import (
	"testing"
	"time"
)

// SetHookTimeoutForTest shortens the timeout of every hook call made through the
// Run*Hook functions for the duration of the test (FR-011's 10 s default is restored
// on cleanup).
func SetHookTimeoutForTest(t testing.TB, d time.Duration) {
	t.Helper()
	orig := defaultHookTimeout
	defaultHookTimeout = d
	t.Cleanup(func() { defaultHookTimeout = orig })
}

// SetHookClockForTest replaces the clock the hook timeout memo reads (its TTL) for
// the duration of the test.
func SetHookClockForTest(t testing.TB, now func() time.Time) {
	t.Helper()
	orig := hookNow
	hookNow = now
	t.Cleanup(func() { hookNow = orig })
}
