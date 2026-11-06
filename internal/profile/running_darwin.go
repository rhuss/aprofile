//go:build darwin

package profile

import (
	"bytes"
	"os"

	"golang.org/x/sys/unix"
)

// scanProcesses scans the current user's processes for one whose environment holds
// APROFILE_CONFIG_DIR=<configDir> (research R11). On macOS another process's
// environment is readable through sysctl kern.procargs2 for processes of the same
// user. Apple platform binaries (for example /bin/sleep) do not expose their
// environment there; harness binaries do. known is false when the process list
// cannot be read; callers then ask the user instead of assuming the profile is
// stopped.
func scanProcesses(configDir string) (Process, bool, bool) {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.uid", os.Getuid())
	if err != nil {
		return Process{}, false, false
	}
	want := []byte("APROFILE_CONFIG_DIR=" + configDir)
	self := os.Getpid()
	for i := range procs {
		pid := int(procs[i].Proc.P_pid)
		if pid <= 0 || pid == self {
			continue
		}
		raw, err := unix.SysctlRaw("kern.procargs2", pid)
		if err != nil {
			continue // exited, a zombie, or not readable: skip this process only
		}
		if envContains(raw, want) {
			return Process{PID: pid, Name: execPath(raw)}, true, true
		}
	}
	return Process{}, false, true
}

// execPath returns the executable path that kern.procargs2 records after argc.
func execPath(raw []byte) string {
	if len(raw) <= 4 {
		return ""
	}
	if i := bytes.IndexByte(raw[4:], 0); i > 0 {
		return string(raw[4 : 4+i])
	}
	return ""
}

// envContains reports whether a kern.procargs2 buffer (argc, executable path,
// argv, then environment strings, all NUL-terminated) holds the exact entry.
func envContains(raw, want []byte) bool {
	if len(raw) < 4 {
		return false
	}
	for _, field := range bytes.Split(raw[4:], []byte{0}) {
		if bytes.Equal(field, want) {
			return true
		}
	}
	return false
}
