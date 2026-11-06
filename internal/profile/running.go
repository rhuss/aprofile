package profile

// Process identifies a running process found by the running-profile scan.
type Process struct {
	PID  int
	Name string // executable path or command name, when readable
}

// RunningProcess reports whether a harness process started for the given profile
// config dir is running (FR-006a, FR-017) and returns the first such process (one
// whose environment holds APROFILE_CONFIG_DIR=<configDir>), so a refusal can name
// it. known is false when the process list cannot be read, so callers ask the user
// instead of assuming the profile is stopped.
func RunningProcess(configDir string) (proc Process, running, known bool) {
	return scanProcesses(configDir)
}
