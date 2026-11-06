//go:build !linux && !darwin

package profile

// scanProcesses cannot inspect other processes' environments on this platform:
// the answer is unknown, so callers ask the user before a share level change
// (FR-017) and rename proceeds (FR-006a).
func scanProcesses(configDir string) (Process, bool, bool) { return Process{}, false, false }
