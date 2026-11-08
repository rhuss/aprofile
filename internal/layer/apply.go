package layer

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/cc-deck/aprofile/internal/config"
)

// ApplyOptions controls how a change plan is executed.
type ApplyOptions struct {
	// DryRun lists what would happen and writes nothing.
	DryRun bool
	// Safe skips every step that replaces profile data (Confirm steps); sync runs
	// in safe mode (FR-017a, research R9).
	Safe bool
	// Yes confirms the Confirm steps; without it they are skipped. Callers ask the
	// user (or require --yes) before setting it.
	Yes bool
	// Backup copies the given paths to a backup location and returns it. It is
	// required for every Confirm step that is applied: profile data is never
	// replaced without a backup (SC-007).
	Backup func(paths []string) (string, error)
	// SetAsideManifest records one set-aside item (spec 003). Apply calls it before
	// the item is moved, so the manifest never misses an item that was moved.
	SetAsideManifest func(item SetAsideItem) error
}

// Apply executes a plan in order. It returns the steps it applied (report steps
// included, as no-ops) and the Confirm steps it skipped. It never deletes a link
// target: links are removed with os.Remove on the link itself, and a real entry is
// only removed by a confirmed replace-with-link after its backup. Applying the same
// plan's result again yields no further changes (idempotent).
func Apply(steps []Step, opts ApplyOptions) (applied, skipped []Step, err error) {
	for _, s := range steps {
		if s.Confirm && (opts.Safe || !opts.Yes) {
			skipped = append(skipped, s)
			continue
		}
		if opts.DryRun || s.Kind == StepReport {
			applied = append(applied, s)
			continue
		}
		if s.Confirm {
			if opts.Backup == nil {
				return applied, skipped, fmt.Errorf("%s %s: refusing to replace profile data without a backup", s.Kind, s.Path)
			}
			dir, err := opts.Backup([]string{s.Path})
			if err != nil {
				return applied, skipped, fmt.Errorf("back up %s: %w", s.Path, err)
			}
			s.Backup = dir
		}
		if err := applyStep(&s, opts); err != nil {
			if s.Backup != "" {
				err = fmt.Errorf("%w (the data was backed up to %s)", err, s.Backup)
			}
			return applied, skipped, fmt.Errorf("%w; the steps before it were applied; re-run the command to finish (the plan is computed from the current state)", err)
		}
		applied = append(applied, s)
	}
	return applied, skipped, nil
}

func applyStep(s *Step, opts ApplyOptions) error {
	switch s.Kind {
	case StepMkdir:
		if err := os.MkdirAll(s.Path, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", s.Path, err)
		}
	case StepLink:
		if cur := inspect(s.Path); cur.exists {
			if !cur.link {
				return fmt.Errorf("link %s: a real entry exists; it is never replaced without confirmation", s.Path)
			}
			if err := os.Remove(s.Path); err != nil { // the stale link only
				return fmt.Errorf("remove stale link %s: %w", s.Path, err)
			}
		}
		return symlink(s.Source, s.Path)
	case StepUnlink, StepRemoveHook:
		if s.Kind == StepRemoveHook && s.Data != nil {
			return writeData(s.Path, s.Data, s.Mode)
		}
		cur := inspect(s.Path)
		if !cur.exists {
			return nil
		}
		if !cur.link {
			return fmt.Errorf("unlink %s: not a link; real entries are never removed here", s.Path)
		}
		if err := os.Remove(s.Path); err != nil {
			return fmt.Errorf("remove link %s: %w", s.Path, err)
		}
	case StepCopyOnce, StepCreate, StepSeedMixed:
		if PathExists(s.Path) {
			return fmt.Errorf("%s %s: the entry already exists", s.Kind, s.Path)
		}
		if s.Data == nil && s.IsDir && s.Source != "" {
			return copyDirAtomic(s.Source, s.Path)
		}
		return writeData(s.Path, s.Data, s.Mode)
	case StepReplaceWithLink:
		if cur := inspect(s.Path); cur.exists {
			var err error
			switch {
			case cur.link:
				err = os.Remove(s.Path)
			case !s.Confirm:
				err = os.Remove(s.Path) // an empty directory only; fails otherwise
			default:
				err = os.RemoveAll(s.Path) // backed up by Apply before this point
			}
			if err != nil {
				return fmt.Errorf("replace %s: %w", s.Path, err)
			}
		}
		return symlink(s.Source, s.Path)
	case StepFixLink:
		cur := inspect(s.Path)
		if !cur.link {
			return nil
		}
		if err := os.Remove(s.Path); err != nil { // removes the link, never its target
			return fmt.Errorf("remove leaking link %s: %w", s.Path, err)
		}
		if s.IsDir {
			return os.MkdirAll(s.Path, 0o755)
		}
		return os.WriteFile(s.Path, []byte{}, 0o600)
	case StepMergeSettings, StepReplaceSettings:
		if s.Data == nil && s.IsDir && s.Source != "" {
			return replaceDirAtomic(s.Source, s.Path) // backed up by Apply
		}
		return writeData(s.Path, s.Data, s.Mode)
	case StepPlaceHook:
		if s.Data != nil {
			return writeData(s.Path, s.Data, s.Mode)
		}
		if cur := inspect(s.Path); cur.exists {
			if !cur.link {
				return fmt.Errorf("place hook %s: a real file exists; it is never replaced", s.Path)
			}
			if err := os.Remove(s.Path); err != nil {
				return err
			}
		}
		return symlink(s.Source, s.Path)
	case StepMoveToStore:
		sfi, err := os.Lstat(s.Path)
		if os.IsNotExist(err) {
			return nil // already moved
		}
		if dfi, derr := os.Lstat(s.Dest); err == nil && derr == nil && os.SameFile(sfi, dfi) && DistinctDirs(s.Path, s.Dest) {
			// An interrupted earlier move left a hard link in the store: removing the
			// profile's name completes it.
			return os.Remove(s.Path)
		}
		err = moveNoOverwrite(s.Path, s.Dest)
		if err == nil {
			return nil
		}
		if !os.IsExist(err) || s.Fallback == "" {
			return fmt.Errorf("move %s into the store: %w", s.Path, err)
		}
		// The store file appeared after planning (a running member wrote it): it is
		// never overwritten; the profile's file is set aside instead (FR-014).
		if err := recordSetAside(opts, s.Entry, ReasonExistsInStore); err != nil {
			return err
		}
		if err := moveNoOverwrite(s.Path, s.Fallback); err != nil {
			return fmt.Errorf("set aside %s: %w", s.Path, err)
		}
		// Report what happened: the applied step is the set-aside.
		s.Kind, s.Dest, s.Reason = StepSetAside, s.Fallback, ReasonExistsInStore
		s.Note = "exists in store: the store file appeared after planning and is never overwritten"
	case StepSetAside:
		if _, err := os.Lstat(s.Path); os.IsNotExist(err) {
			return nil // already set aside
		}
		if err := recordSetAside(opts, s.Entry, s.Reason); err != nil {
			return err
		}
		if err := moveTree(s.Path, s.Dest); err != nil {
			return fmt.Errorf("set aside %s: %w", s.Path, err)
		}
	case StepRemoveEmptyDir:
		return removeEmptyTree(s.Path)
	case StepLinkHistory:
		// Only creates the link; store directories come from separate mkdir steps.
		if cur := inspect(s.Path); cur.exists {
			if cur.link && samePath(cur.target, s.Source) {
				return nil
			}
			return fmt.Errorf("link history %s: an entry exists; it is never replaced here", s.Path)
		}
		return symlink(s.Source, s.Path)
	case StepRelinkHistory:
		cur := inspect(s.Path)
		if cur.exists && !cur.link {
			return fmt.Errorf("relink history %s: not a link; real entries are never removed here", s.Path)
		}
		if cur.link {
			if samePath(cur.target, s.Source) {
				return nil
			}
			if err := os.Remove(s.Path); err != nil { // the link only, never its target
				return fmt.Errorf("remove link %s: %w", s.Path, err)
			}
		}
		return symlink(s.Source, s.Path)
	default:
		return fmt.Errorf("unknown step kind %q", s.Kind)
	}
	return nil
}

func symlink(target, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.Symlink(target, path); err != nil {
		return fmt.Errorf("symlink %s -> %s: %w", path, target, err)
	}
	return nil
}

// writeData writes a step's content atomically. An existing file keeps its mode;
// a new file gets the step's mode, or 0o644.
func writeData(path string, data []byte, want os.FileMode) error {
	mode := os.FileMode(0o644)
	if want != 0 {
		mode = want
	}
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if data == nil {
		data = []byte{}
	}
	return config.WriteFileAtomic(path, data, mode)
}

// copyDirAtomic copies a directory tree into a temporary sibling and renames it
// into place, so an interrupted copy never leaves a partial directory that a later
// plan would take for the profile's own complete copy.
func copyDirAtomic(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".aprofile-tmp-")
	if err != nil {
		return err
	}
	if err := os.Remove(tmp); err != nil { // copyDir creates it with the source mode
		return err
	}
	if err := copyDir(src, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	return nil
}

// replaceDirAtomic replaces dst with a copy of src: the copy is made next to dst
// first and swapped in with renames, so an interrupted run leaves either the old
// or the new directory, never a partial one.
func replaceDirAtomic(src, dst string) error {
	if _, err := os.Lstat(dst); os.IsNotExist(err) {
		return copyDirAtomic(src, dst)
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".aprofile-new-")
	if err != nil {
		return err
	}
	if err := os.Remove(tmp); err != nil {
		return err
	}
	if err := copyDir(src, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	old := tmp + "-old"
	if err := os.Rename(dst, old); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Rename(old, dst)
		_ = os.RemoveAll(tmp)
		return err
	}
	return os.RemoveAll(old)
}

// CopyTree copies a file, link or directory tree to dst, recreating links as links
// and keeping file modes. It is not atomic; callers use it for scratch copies.
func CopyTree(src, dst string) error { return copyDir(src, dst) }

// copyDir copies a directory tree. Symlinks inside are recreated as links; files
// and directories keep their mode.
func copyDir(src, dst string) error {
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	case fi.IsDir():
		// Created writable for the copy, then given the source mode.
		if err := os.MkdirAll(dst, 0o700); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyDir(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return os.Chmod(dst, fi.Mode().Perm())
	default:
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, fi.Mode().Perm())
	}
}

// DistinctDirs reports whether a and b lie in two different directories, compared
// by file identity (never by path strings, which miss links, letter case and
// firmlinks): two names of one file there are two hard links, while one directory
// reached by two paths holds a single name (never removed as a "completed move").
func DistinctDirs(a, b string) bool {
	da, err1 := os.Stat(filepath.Dir(a))
	db, err2 := os.Stat(filepath.Dir(b))
	return err1 == nil && err2 == nil && !os.SameFile(da, db)
}

// recordSetAside calls the manifest writer for one item, when one is set.
func recordSetAside(opts ApplyOptions, entry, reason string) error {
	if opts.SetAsideManifest == nil {
		return nil
	}
	if err := opts.SetAsideManifest(SetAsideItem{Path: entry, Reason: reason}); err != nil {
		return fmt.Errorf("record set-aside item %s: %w", entry, err)
	}
	return nil
}

// linkFile is os.Link; tests replace it to simulate a cross-device move.
var linkFile = os.Link

// moveNoOverwrite moves one file (or symbolic link) to dst and never replaces an
// existing dst (research R6): a hard link fails with EEXIST when dst exists, and
// only then is the source removed. On EXDEV the file is copied to an O_EXCL target
// with its mode and modification time, synced, and the source removed. os.Rename is
// not used because it silently replaces an existing file. An existing dst yields an
// error for which os.IsExist is true.
func moveNoOverwrite(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		if err := os.Symlink(target, dst); err != nil {
			return err
		}
		return os.Remove(src)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", src)
	}
	err = linkFile(src, dst)
	switch {
	case err == nil:
	case isCrossDevice(err):
		if err := copyExcl(src, dst, fi); err != nil {
			return err
		}
	default:
		return err
	}
	return os.Remove(src)
}

// copyExcl copies src to a new file dst (O_EXCL), keeping mode and mtime.
func copyExcl(src, dst string, fi os.FileInfo) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fi.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dst)
		return err
	}
	_ = os.Chmod(dst, fi.Mode().Perm())
	return os.Chtimes(dst, fi.ModTime(), fi.ModTime())
}

func isCrossDevice(err error) bool { return errors.Is(err, syscall.EXDEV) }

// moveTree moves a file or directory tree to dst, which must not exist. Within one
// file system it is a rename; across file systems the tree is copied and the source
// removed afterwards.
func moveTree(src, dst string) error {
	if _, err := os.Lstat(dst); err == nil {
		return &os.PathError{Op: "set aside", Path: dst, Err: os.ErrExist}
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	err := os.Rename(src, dst)
	if err == nil || !isCrossDevice(err) {
		return err
	}
	if err := copyDir(src, dst); err != nil {
		_ = os.RemoveAll(dst)
		return err
	}
	return os.RemoveAll(src)
}

// removeEmptyTree removes a directory tree that holds only directories. It fails,
// removing nothing more, when a file or link is left in it.
func removeEmptyTree(path string) error {
	fi, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return nil // already replaced by a link: nothing to remove, the link stays
	}
	if !fi.IsDir() {
		return fmt.Errorf("remove %s: not a directory", path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() || e.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("remove %s: %s is still there; nothing is deleted", path, filepath.Join(path, e.Name()))
		}
		if err := removeEmptyTree(filepath.Join(path, e.Name())); err != nil {
			return err
		}
	}
	return os.Remove(path)
}
