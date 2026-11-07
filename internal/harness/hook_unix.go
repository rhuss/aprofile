//go:build unix

package harness

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// killProcessGroupOnCancel starts the hook in its own process group and makes a
// timeout kill the whole group (FR-011). exec.CommandContext alone kills only the
// direct child: a shell hook's children (for example a `sleep`, or a helper that
// still holds the stdout pipe) would otherwise outlive the timeout as orphans.
func killProcessGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// A negative pid signals every process of the group the hook leads.
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				return os.ErrProcessDone
			}
			return err
		}
		return nil
	}
}
