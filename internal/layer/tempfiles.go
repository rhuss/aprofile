package layer

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cc-deck/aprofile/internal/harness"
)

// Temp name markers of aprofile's own atomic writes: config.WriteFileAtomic and the
// importer's file copy create ".aprofile-tmp-*"; apply.go's directory copy and swap
// create ".<entry>.aprofile-tmp-*", ".<entry>.aprofile-new-*" and the swap's
// ".<entry>.aprofile-new-*-old".
var tempMarkers = []string{".aprofile-tmp-", ".aprofile-new-"}

// IsTempName reports whether a directory entry name is one of aprofile's own temp
// names. An interrupted write leaves such an entry behind; it is never a profile
// entry, so the config-dir walk skips it instead of reporting it as unclassified
// (T091, FR-018), and sync removes it once stale (RemoveStaleTemps).
func IsTempName(name string) bool {
	if !strings.HasPrefix(name, ".") {
		return false
	}
	for _, m := range tempMarkers {
		if strings.HasPrefix(name, m) {
			return true
		}
		// ".<entry>" + marker: the marker follows a non-empty entry name.
		if i := strings.Index(name[1:], m); i > 0 {
			return true
		}
	}
	return false
}

// StaleTempAge is how old a temp entry must be before sync removes it: a younger
// one may belong to a write still in progress (a concurrent aprofile run).
const StaleTempAge = time.Minute

// TempRestore is a swap temp renamed back to the entry it replaced.
type TempRestore struct {
	Temp  string // profile-relative name of the ".<entry>.aprofile-new-*-old" temp
	Entry string // profile-relative name of the restored entry
}

// TempCleanup is what RemoveStaleTemps did (or, in a dry run, would do).
type TempCleanup struct {
	// Removed lists the sorted profile-relative names of the stale temp entries
	// removed.
	Removed []string
	// Restored lists the interrupted swaps whose "-old" temp was renamed back to
	// its missing entry.
	Restored []TempRestore
	// Failed holds one error per temp entry that could not be removed or restored;
	// each names the path. Such an entry is left in place.
	Failed []error
}

// maxTempWalk bounds how many entries of a temp directory RemoveStaleTemps looks
// at to find its newest modification time. A larger directory is kept: it may be
// a long running copy, and keeping it costs only disk space.
const maxTempWalk = 100000

// swapOldSuffix ends the name replaceDirAtomic gives dst while it swaps the new
// copy in: ".<entry>.aprofile-new-<random>-old".
const swapOldSuffix = "-old"

// swapOldEntry returns the entry name an interrupted swap's "-old" temp belongs to
// (".<entry>.aprofile-new-<random>-old" -> "<entry>"), or "" when name is not one.
func swapOldEntry(name string) string {
	if !strings.HasPrefix(name, ".") || !strings.HasSuffix(name, swapOldSuffix) {
		return ""
	}
	i := strings.LastIndex(name, ".aprofile-new-")
	if i <= 1 {
		return ""
	}
	return name[1:i]
}

// RemoveStaleTemps cleans up aprofile's own temp entries (IsTempName) in the
// directories the config-dir walk reads (the config dir and its container
// directories; never through a link). A swap interrupted between its two renames
// leaves ".<entry>.aprofile-new-*-old" holding the only copy of <entry> while
// <entry> is missing: that temp is renamed back to <entry>, whatever its age (the
// rename kept the entry's old mtime). Every other temp entry last modified before
// cutoff is removed; a directory counts as modified when anything within it was
// (newestModTime), so a long running copy is never removed mid-copy. A dry run
// changes nothing and returns what it would do. A missing config dir yields
// nothing; the error is for a config dir that cannot be read.
func RemoveStaleTemps(def *harness.Definition, configDir string, cutoff time.Time, dryRun bool) (TempCleanup, error) {
	var res TempCleanup
	var temps []string
	if _, err := walkWithTemps(def, configDir, "", &temps); err != nil {
		return res, err
	}
	sort.Strings(temps)
	// Restores first: a "-old" temp is the only copy of its entry, and the other
	// temps of the same swap (the new copy) are then plain leftovers.
	pending := temps[:0:0]
	for _, rel := range temps {
		entry := swapOldEntry(path.Base(rel))
		if entry == "" {
			pending = append(pending, rel)
			continue
		}
		entryRel := path.Join(path.Dir(rel), entry)
		src := filepath.Join(configDir, filepath.FromSlash(rel))
		dst := filepath.Join(configDir, filepath.FromSlash(entryRel))
		if _, err := os.Lstat(dst); !os.IsNotExist(err) {
			pending = append(pending, rel) // the swap completed: a leftover
			continue
		}
		if !dryRun {
			if err := os.Rename(src, dst); err != nil {
				res.Failed = append(res.Failed, fmt.Errorf("restore %s from interrupted swap temp %s (kept; it holds the only copy, rename it back by hand): %w", dst, src, err))
				continue
			}
		}
		res.Restored = append(res.Restored, TempRestore{Temp: rel, Entry: entryRel})
	}
	for _, rel := range pending {
		p := filepath.Join(configDir, filepath.FromSlash(rel))
		newest, ok := newestModTime(p)
		if !ok || !newest.Before(cutoff) {
			continue
		}
		if !dryRun {
			if err := os.RemoveAll(p); err != nil {
				res.Failed = append(res.Failed, fmt.Errorf("remove stale temp entry %s: %w", p, err))
				continue
			}
		}
		res.Removed = append(res.Removed, rel)
	}
	return res, nil
}

// newestModTime returns the newest modification time of p and, for a directory,
// of everything within it (never following links). ok is false when p cannot be
// read or holds more than maxTempWalk entries.
func newestModTime(p string) (newest time.Time, ok bool) {
	fi, err := os.Lstat(p)
	if err != nil {
		return time.Time{}, false
	}
	newest = fi.ModTime()
	if !fi.IsDir() {
		return newest, true
	}
	seen := 0
	err = filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if seen++; seen > maxTempWalk {
			return errTooLarge
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	return newest, err == nil
}

var errTooLarge = errors.New("too many entries")
