//go:build linux

package profile

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// scanProcesses scans /proc/<pid>/environ for a process whose
// APROFILE_CONFIG_DIR equals configDir (FR-006a). It is best-effort: a process
// that cannot be read is skipped; known is false only when /proc is unreadable.
func scanProcesses(configDir string) (Process, bool, bool) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return Process{}, false, false
	}
	want := "APROFILE_CONFIG_DIR=" + configDir
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue // not a pid directory
		}
		data, err := os.ReadFile(filepath.Join("/proc", e.Name(), "environ"))
		if err != nil {
			continue
		}
		for _, kv := range strings.Split(string(data), "\x00") {
			if kv == want {
				comm, _ := os.ReadFile(filepath.Join("/proc", e.Name(), "comm"))
				return Process{PID: pid, Name: strings.TrimSpace(string(comm))}, true, true
			}
		}
	}
	return Process{}, false, true
}
