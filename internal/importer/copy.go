package importer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// copier copies a file or a directory tree verbatim. It follows symlinks (a cc-deck
// config dir links shared entries, and an imported entry must be a real per-profile
// entry), skips dangling links and symlink cycles with a warning, and writes every
// file atomically (temp file + rename), so an existing dst that is a symlink is
// replaced and never written through. With private set, files are written 0600 and
// directories 0700 (credentials and their backups); otherwise source permissions
// are kept. Warnings name paths only, never file contents.
type copier struct {
	private bool
	warn    func(string)
}

func (c copier) warnf(format string, args ...any) {
	if c.warn != nil {
		c.warn(fmt.Sprintf(format, args...))
	}
}

// copy replaces dst with a copy of src.
func (c copier) copy(src, dst string) error {
	fi, err := os.Stat(src)
	if err != nil {
		if isDanglingLink(src) {
			c.warnf("skipped dangling symlink %s", src)
			return nil
		}
		return err
	}
	if fi.IsDir() {
		if err := os.RemoveAll(dst); err != nil {
			return err
		}
		return c.tree(src, dst, map[string]bool{})
	}
	if !fi.Mode().IsRegular() {
		c.warnf("skipped %s: not a regular file or directory", src)
		return nil
	}
	return c.file(src, dst, fi.Mode())
}

// tree copies the directory src into dst, following symlinks. seen holds the real
// paths of the directories on the current descent, so a link back to an ancestor
// is reported instead of recursing forever.
func (c copier) tree(src, dst string, seen map[string]bool) error {
	real, err := filepath.EvalSymlinks(src)
	if err != nil {
		return err
	}
	if seen[real] {
		c.warnf("skipped symlink cycle at %s", src)
		return nil
	}
	seen[real] = true
	defer delete(seen, real)

	fi, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, c.dirMode(fi.Mode())); err != nil {
		return err
	}
	if err := os.Chmod(dst, c.dirMode(fi.Mode())); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		st, err := os.Stat(s)
		if err != nil {
			if isDanglingLink(s) {
				c.warnf("skipped dangling symlink %s", s)
				continue
			}
			return err
		}
		switch {
		case st.IsDir():
			if err := c.tree(s, d, seen); err != nil {
				return err
			}
		case st.Mode().IsRegular():
			if err := c.file(s, d, st.Mode()); err != nil {
				return err
			}
		default:
			c.warnf("skipped %s: not a regular file or directory", s)
		}
	}
	return nil
}

func (c copier) dirMode(src os.FileMode) os.FileMode {
	if c.private {
		return 0o700
	}
	return src.Perm() | 0o700
}

func (c copier) fileMode(src os.FileMode) os.FileMode {
	if c.private {
		return 0o600
	}
	return src.Perm()
}

// file copies one regular file atomically: it streams into a temp file in dst's
// directory (created 0600 by os.CreateTemp), sets the final mode and renames it
// into place.
func (c copier) file(src, dst string, mode os.FileMode) error {
	dir := filepath.Dir(dst)
	parentMode := os.FileMode(0o755)
	if c.private {
		parentMode = 0o700
	}
	if err := os.MkdirAll(dir, parentMode); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(dir, ".aprofile-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("copy %s: %w", src, err)
	}
	if err := tmp.Chmod(c.fileMode(mode)); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, dst)
}

func isDanglingLink(path string) bool {
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return false
	}
	_, err = os.Stat(path)
	return os.IsNotExist(err)
}
