package layer

import (
	"fmt"
	"os"
	"path/filepath"

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
		if err := applyStep(s); err != nil {
			if s.Backup != "" {
				err = fmt.Errorf("%w (the data was backed up to %s)", err, s.Backup)
			}
			return applied, skipped, fmt.Errorf("%w; the steps before it were applied; re-run the command to finish (the plan is computed from the current state)", err)
		}
		applied = append(applied, s)
	}
	return applied, skipped, nil
}

func applyStep(s Step) error {
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
		if pathExists(s.Path) {
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
