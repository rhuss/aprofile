package sync

import "runtime"

// isDarwin reports whether the host is macOS, which selects ~/.bash_profile over
// ~/.bashrc for bash (FR-024).
func isDarwin() bool { return runtime.GOOS == "darwin" }
