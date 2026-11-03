//go:build !linux

package profile

// Running reports whether a harness process started for the given profile config
// dir is currently running. Reliable per-process environment inspection is not
// portable (on macOS another process's environment is not readable), so on
// non-Linux platforms aprofile cannot detect a running harness and returns false;
// `rename` proceeds and the documented limitation applies (FR-006a).
func Running(configDir string) bool { return false }
