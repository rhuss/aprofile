//go:build !aprofile_testseam

package profile_test

import (
	"path/filepath"
	"strconv"
	"testing"

	"github.com/cc-deck/aprofile/internal/profile"
)

// T092: without the aprofile_testseam build tag (every production build),
// APROFILE_TEST_RUNNING has no effect: the answer comes from the process scan only.
func TestRunningIgnoresTestSeamVariable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "profiles", "claude", "nobody")
	t.Setenv("APROFILE_TEST_RUNNING", strconv.Itoa(4242))
	proc, running, _ := profile.RunningProcess(dir)
	if running || proc.PID == 4242 {
		t.Errorf("a production build must ignore APROFILE_TEST_RUNNING: running=%v pid=%d", running, proc.PID)
	}
	t.Setenv("APROFILE_TEST_RUNNING", "unknown")
	_, _, knownWith := profile.RunningProcess(dir)
	t.Setenv("APROFILE_TEST_RUNNING", "")
	_, _, knownWithout := profile.RunningProcess(dir)
	if knownWith != knownWithout {
		t.Error("APROFILE_TEST_RUNNING=unknown must not change the answer of a production build")
	}
}
