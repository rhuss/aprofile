package launch

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// privateRecordDir creates recovery directories privately and repairs the owned
// managed-launch root and harness directory. Existing XDG/data ancestors belong
// to the caller and must retain their permissions.
func privateRecordDir(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if filepath.Base(dir) == "managed-launch" {
		return privateMode(dir, 0o700)
	}
	if parent := filepath.Dir(dir); filepath.Base(parent) == "managed-launch" {
		if err := privateMode(parent, 0o700); err != nil {
			return err
		}
		return privateMode(dir, 0o700)
	}
	return nil
}

// privateMode avoids changing even metadata when the permissions are correct.
func privateMode(path string, mode os.FileMode) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if fi.Mode().Perm() == mode {
		return nil
	}
	return os.Chmod(path, mode)
}

// lockRecord serializes a profile's complete record/settings transaction across
// processes on the supported Linux/macOS platforms. The lock file must persist:
// unlinking it would let a new caller lock a different inode while waiters remain
// on the old one. Closing releases the lock, including after process termination.
func lockRecord(path string) (*os.File, error) {
	if err := privateRecordDir(path); err != nil {
		return nil, fmt.Errorf("create launch lock directory: %w", err)
	}
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open launch lock: %w", err)
	}
	if err = privateMode(path+".lock", 0o600); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("secure launch lock: %w", err)
	}
	for {
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX)
		if err != unix.EINTR {
			break
		}
	}
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("lock launch record: %w", err)
	}
	return f, nil
}
