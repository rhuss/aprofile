//go:build aprofile_testseam

package profile_test

import (
	"path/filepath"
	"testing"

	"github.com/cc-deck/aprofile/internal/profile"
)

// T092: a binary built with the aprofile_testseam tag (the integration tests'
// aprofile) answers from APROFILE_TEST_RUNNING; unset means "none".
func TestRunningTestSeam(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "profiles", "claude", "x")
	for v, want := range map[string][2]bool{ // running, known
		"":        {false, true},
		"none":    {false, true},
		"unknown": {false, false},
		"4242":    {true, true},
	} {
		t.Setenv("APROFILE_TEST_RUNNING", v)
		proc, running, known := profile.RunningProcess(dir)
		if running != want[0] || known != want[1] {
			t.Errorf("APROFILE_TEST_RUNNING=%q: running=%v known=%v, want %v", v, running, known, want)
		}
		if v == "4242" && proc.PID != 4242 {
			t.Errorf("forced pid = %d", proc.PID)
		}
	}
}
