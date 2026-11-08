package history

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/harness"
)

// Store is where one history group keeps its history entries (spec 003). The home
// store is the harness's default location; every other store lives under
// aprofile's data directory with the entries at their config-dir relative paths. It
// satisfies layer.HistoryStore.
type Store struct {
	def     *harness.Definition
	home    string // $HOME used for the default location (home store only)
	root    string
	name    string
	builtin bool
}

// EntryPath returns the store path of a profile-relative history entry. The home
// store resolves it like the default setup does (def.DefaultPathUnder, honoring
// default_locations); other stores join it to the store root.
func (s *Store) EntryPath(rel string) string {
	rel = strings.TrimSuffix(filepath.ToSlash(rel), "/")
	if s.builtin {
		return s.def.DefaultPathUnder(s.home, rel)
	}
	if rel == "" || rel == "." {
		return s.root
	}
	return filepath.Join(s.root, filepath.FromSlash(rel))
}

// IsHome reports whether this is the built-in home store (the default setup).
func (s *Store) IsHome() bool { return s.builtin }

// Name returns the group name.
func (s *Store) Name() string { return s.name }

// Root returns the store's root directory.
func (s *Store) Root() string { return s.root }

// homeStore builds the home store of a harness.
func homeStore(def *harness.Definition, home string) *Store {
	return &Store{def: def, home: home, root: def.DefaultPathUnder(home, ""), name: Home, builtin: true}
}

// dataStore builds the store of a non-home group.
func dataStore(def *harness.Definition, paths *config.Paths, group string) *Store {
	return &Store{def: def, root: paths.GroupStoreDir(def.Name, group), name: group}
}

// CheckHomeLocation refuses a join of the home group when the harness's default
// location cannot serve as a store (spec 003 Edge Cases): it does not exist, it is a
// symbolic link, it lies inside aprofile's data directory, or it is the config dir
// of a profile (profileDirs). The error is exit-2 class and names the location and
// the condition.
func CheckHomeLocation(def *harness.Definition, home string, paths *config.Paths, profileDirs []string) error {
	loc := filepath.Clean(def.DefaultPathUnder(home, ""))
	fi, err := os.Lstat(loc)
	switch {
	case os.IsNotExist(err):
		return invalidf(ErrUnsuitableHome, "cannot join %s/home: the default location %s does not exist (run %s once without a profile, or create it)", def.Name, loc, def.Binary)
	case err != nil:
		return invalidf(ErrUnsuitableHome, "cannot join %s/home: the default location %s cannot be read: %v", def.Name, loc, err)
	case fi.Mode()&os.ModeSymlink != 0:
		return invalidf(ErrUnsuitableHome, "cannot join %s/home: the default location %s is a symbolic link; aprofile does not use a linked default setup as a history store", def.Name, loc)
	case !fi.IsDir():
		return invalidf(ErrUnsuitableHome, "cannot join %s/home: the default location %s is not a directory", def.Name, loc)
	}
	if paths != nil && within(loc, paths.DataDir) {
		return invalidf(ErrUnsuitableHome, "cannot join %s/home: the default location %s lies inside aprofile's data directory %s", def.Name, loc, paths.DataDir)
	}
	for _, d := range profileDirs {
		if filepath.Clean(d) == loc {
			return invalidf(ErrUnsuitableHome, "cannot join %s/home: the default location %s is the config dir of a profile", def.Name, loc)
		}
	}
	return nil
}

// within reports whether path equals dir or lies below it.
func within(path, dir string) bool {
	if dir == "" {
		return false
	}
	path, dir = filepath.Clean(path), filepath.Clean(dir)
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

// invalidf builds an exit-2 class error wrapping kind.
func invalidf(kind error, format string, args ...any) error {
	return &invalidError{kind: kind, msg: fmt.Sprintf(format, args...)}
}

type invalidError struct {
	kind error
	msg  string
}

func (e *invalidError) Error() string { return e.msg }
func (e *invalidError) Unwrap() error { return e.kind }
