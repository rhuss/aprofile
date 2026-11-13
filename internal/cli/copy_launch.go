package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/launch"
	"github.com/cc-deck/aprofile/internal/profile"
)

// copyLaunchSource presents source settings before aprofile's placements, without
// mutating the source. replaced tracks exactly what the copy wrote in the target:
// nil keys mean a whole file; nonempty keys mean only those mixed-file subtrees.
type copyLaunchSource struct {
	restored map[string][]byte
	replaced map[string][]string
}

func newCopyLaunchSource(srcDir, recordPath string) (*copyLaunchSource, error) {
	rec, err := launch.Load(recordPath)
	if err != nil {
		return nil, err
	}
	def := &harness.Definition{}
	doc := launch.ReadDocFor(def, srcDir, rec)
	result, err := launch.Plan(&profile.Profile{}, def, doc, rec)
	if err != nil {
		return nil, fmt.Errorf("restore source settings for copy: %w", err)
	}
	return &copyLaunchSource{restored: result.Writes()}, nil
}

func (s *copyLaunchSource) read(srcDir, rel string) ([]byte, error) {
	if data, ok := s.restored[filepath.ToSlash(rel)]; ok {
		return data, nil
	}
	return os.ReadFile(filepath.Join(srcDir, rel))
}

func (s *copyLaunchSource) markFile(rel string) {
	if s.replaced == nil {
		s.replaced = map[string][]string{}
	}
	s.replaced[filepath.ToSlash(rel)] = nil
}

func (s *copyLaunchSource) markKeys(rel string, keys []string) {
	if len(keys) == 0 {
		return
	}
	if s.replaced == nil {
		s.replaced = map[string][]string{}
	}
	rel = filepath.ToSlash(rel)
	if old, ok := s.replaced[rel]; ok && old == nil {
		return
	}
	s.replaced[rel] = append(s.replaced[rel], keys...)
}

// copyTree follows the same no-symlinks rule as copyTree, substituting restored
// bytes only for regular source files that actually exist and are copied.
func (s *copyLaunchSource) copyTree(srcDir, dstDir, rel string) error {
	src := filepath.Join(srcDir, rel)
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	if fi.IsDir() {
		if err := os.MkdirAll(filepath.Join(dstDir, rel), 0o755); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := s.copyTree(srcDir, dstDir, filepath.Join(rel, e.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	data, err := s.read(srcDir, rel)
	if err != nil {
		return err
	}
	if err := config.WriteFileAtomic(filepath.Join(dstDir, rel), data, fi.Mode().Perm()); err != nil {
		return err
	}
	s.markFile(rel)
	return nil
}

// A copied file has fresh originals. Retaining its old target recovery metadata
// would restore unrelated target values when the copied feature is disabled.
// Untouched files and mixed-file keys retain their existing provenance.
func (s *copyLaunchSource) forgetReplaced(recordPath string) error {
	if len(s.replaced) == 0 {
		return nil
	}
	return launch.UpdateRecord(recordPath, func(rec *launch.Record) error {
		kept := make([]launch.Entry, 0, len(rec.Entries))
		for _, e := range rec.Entries {
			keys, replaced := s.replaced[e.File]
			if replaced && keys != nil {
				replaced = false
				for _, key := range keys {
					if e.Location == key || strings.HasPrefix(e.Location, key+".") {
						replaced = true
						break
					}
				}
			}
			if !replaced {
				kept = append(kept, e)
			}
		}
		rec.Entries = kept
		return nil
	})
}
