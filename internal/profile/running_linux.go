//go:build linux

package profile

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Running reports whether a harness process started for the given profile config
// dir is currently running. On Linux it scans /proc/<pid>/environ for a process
// whose APROFILE_CONFIG_DIR equals configDir (FR-006a). It is best-effort: any
// error reading a process is ignored.
func Running(configDir string) bool {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	want := "APROFILE_CONFIG_DIR=" + configDir
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue // not a pid directory
		}
		data, err := os.ReadFile(filepath.Join("/proc", e.Name(), "environ"))
		if err != nil {
			continue
		}
		for _, kv := range strings.Split(string(data), "\x00") {
			if kv == want {
				return true
			}
		}
	}
	return false
}
