package sync

import (
	"runtime"

	"golang.org/x/sys/unix"
)

// isDarwin reports whether the host is macOS, which selects ~/.bash_profile over
// ~/.bashrc for bash (FR-024).
func isDarwin() bool { return runtime.GOOS == "darwin" }

// writable reports whether the current user may write to path.
func writable(path string) bool { return unix.Access(path, unix.W_OK) == nil }
