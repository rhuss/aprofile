//go:build !unix

package harness

import "os/exec"

// killProcessGroupOnCancel keeps exec.CommandContext's default on platforms without
// POSIX process groups: a timeout kills the direct child, and WaitDelay bounds the
// wait for any child that still holds the output pipes.
func killProcessGroupOnCancel(*exec.Cmd) {}
