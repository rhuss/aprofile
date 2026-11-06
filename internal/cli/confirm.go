package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/config"
)

// errDeclined reports that the user answered no to a confirmation; the command
// stops without changes and exits 0.
var errDeclined = errors.New("aborted")

// stdinIsTTY reports whether the command reads from a terminal. It is a variable so
// tests can simulate a terminal.
var stdinIsTTY = func(cmd *cobra.Command) bool {
	in := cmd.InOrStdin()
	if f, ok := in.(*os.File); ok {
		return isTTY(f)
	}
	return false
}

// confirmOrYes asks once before a step that replaces data. With yes it proceeds
// without asking; off a terminal without yes it returns ExitError{3} and nothing is
// changed (contracts/cli.md); a "no" answer returns errDeclined.
func confirmOrYes(cmd *cobra.Command, prompt string, yes bool) error {
	if yes {
		return nil
	}
	if !stdinIsTTY(cmd) {
		return exitErrorf(ExitNeedsConfirmation, "%s: confirmation needed; re-run with --yes (nothing changed)", prompt)
	}
	if !confirm(cmd, prompt) {
		return errDeclined
	}
	return nil
}

// backupTimestamp formats the UTC backup directory name, matching import's backups.
func backupTimestamp(now time.Time) string {
	return now.UTC().Format("20060102-150405.000000")
}

// backupPaths copies each existing path into one new backup directory,
// <data dir>/backups/<harness>/<name>/<UTC timestamp>/, before it is replaced. A
// file keeps its base name; a name that occurs twice gets a numeric suffix. A
// symlink is recorded as a symlink (its target is never copied or touched).
// Missing paths are skipped. It returns the backup directory, or "" when nothing
// needed a backup.
func backupPaths(p *config.Paths, harness, name string, paths []string, now time.Time) (string, error) {
	var existing []string
	for _, path := range paths {
		if _, err := os.Lstat(path); err == nil {
			existing = append(existing, path)
		}
	}
	if len(existing) == 0 {
		return "", nil
	}
	dir, err := newBackupDir(p.BackupDir(harness, name, backupTimestamp(now)))
	if err != nil {
		return "", err
	}
	used := map[string]int{}
	for _, path := range existing {
		base := filepath.Base(path)
		used[base]++
		if n := used[base]; n > 1 {
			base = fmt.Sprintf("%s.%d", base, n)
		}
		if err := backupOne(path, filepath.Join(dir, base)); err != nil {
			return "", fmt.Errorf("back up %s: %w", path, err)
		}
	}
	return dir, nil
}

// newBackupDir creates a new, empty backup directory at base, or at base-2, base-3
// and so on when base exists, so two backups never merge or overwrite each other.
func newBackupDir(base string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(base), 0o700); err != nil {
		return "", fmt.Errorf("create backup dir %s: %w", filepath.Dir(base), err)
	}
	dir := base
	for i := 2; ; i++ {
		err := os.Mkdir(dir, 0o700)
		if err == nil {
			return dir, nil
		}
		if !os.IsExist(err) || i > 1000 {
			return "", fmt.Errorf("create backup dir %s: %w", dir, err)
		}
		dir = fmt.Sprintf("%s-%d", base, i)
	}
}

func backupOne(src, dst string) error {
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
		if err := os.MkdirAll(dst, 0o700); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := backupOne(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return nil
	default:
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, fi.Mode().Perm())
	}
}
