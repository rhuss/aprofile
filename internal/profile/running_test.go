//go:build linux || darwin

package profile_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/cc-deck/aprofile/internal/profile"
)

// T017/R11: a child process started with APROFILE_CONFIG_DIR set is detected; a
// config dir no process uses is not.
func TestRunningDetectsChildProcess(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "profiles", "claude", "work")
	// Re-run this test binary as the child: macOS hides the environment of Apple
	// platform binaries such as /bin/sleep, but not of other programs (harnesses).
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperSleep$")
	cmd.Env = append(os.Environ(), "APROFILE_CONFIG_DIR="+dir, "APROFILE_TEST_HELPER_SLEEP=1")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start child process: %v", err)
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()

	deadline := time.Now().Add(3 * time.Second)
	for {
		_, running, known := profile.RunningProcess(dir)
		if !known {
			t.Skip("process list not readable on this machine")
		}
		if running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("running child process not detected")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, running, _ := profile.RunningProcess(dir + "-other"); running {
		t.Error("a config dir no process uses must not be reported as running")
	}
}

// TestHelperSleep is the child process of TestRunningDetectsChildProcess.
func TestHelperSleep(t *testing.T) {
	if os.Getenv("APROFILE_TEST_HELPER_SLEEP") != "1" {
		t.Skip("helper process only")
	}
	time.Sleep(30 * time.Second)
}
