// Package sync orchestrates one aprofile sync: it prepares each profile's config
// dir by layer, renders and writes one wrapper per profile, removes only
// aprofile-marked stale wrappers, writes the registry, and manages the PATH block.
// It is idempotent: a second run with no changes writes 0 files and reports 0
// changes (FR-022..FR-025, SC-003).
package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/layer"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/registry"
	"github.com/cc-deck/aprofile/internal/wrapper"
)

// Options controls one sync run.
type Options struct {
	DryRun      bool
	RequirePath bool
	RemovePath  bool
	// FixLinks replaces a leaking state/owned/credentials symlink in a profile config
	// dir with an empty real entry, never deleting the link target (FR-018).
	FixLinks bool
}

// Result is a structured report of what the sync did.
type Result struct {
	Changes          []string // human-readable list of mutating changes
	WrappersWritten  []string
	WrappersRemoved  []string
	ConfigDirChanges int
	RegistryChanged  bool
	Path             PathResult
	Problems         []string // reported, non-fatal issues (FR-022)
	Notes            []string // reported conditions that changed nothing (FR-018: unclassified/leaking links)
}

// Changed reports whether the sync changed anything.
func (r *Result) Changed() bool {
	return len(r.WrappersWritten) > 0 || len(r.WrappersRemoved) > 0 ||
		r.ConfigDirChanges > 0 || r.RegistryChanged || r.Path.Changed
}

// HasProblems reports whether any profile/definition failed to load or clashed.
func (r *Result) HasProblems() bool { return len(r.Problems) > 0 }

// Run performs the sync.
func Run(paths *config.Paths, set *harness.Set, store *profile.Store, settings *config.Settings, opts Options) (*Result, error) {
	res := &Result{}

	profiles, failures, err := store.List()
	if err != nil {
		return nil, err
	}
	for _, f := range failures {
		res.Problems = append(res.Problems, fmt.Sprintf("profile %s failed to load: %v", f.ID(), f.Err))
	}
	for _, e := range set.Errors() {
		res.Problems = append(res.Problems, "harness definition error: "+e.Error())
	}

	// Detect wrapper-name clashes (FR-002): skip both sides, report, but keep their
	// existing files (do not treat as stale).
	clash := wrapperNameClashes(profiles, set)

	if !opts.DryRun {
		if err := os.MkdirAll(paths.BinDir, 0o755); err != nil {
			return nil, fmt.Errorf("create bin dir %s: %w", paths.BinDir, err)
		}
	}

	var regEntries []registry.Entry
	known := map[string]bool{}     // wrapper names that belong to a profile (not stale)
	processed := map[string]bool{} // profile IDs that got a fresh registry entry this run
	// The current registry lets us preserve a failed profile's existing wrapper and entry
	// unchanged (FR-022) instead of treating them as stale.
	oldByID := map[string]registry.Entry{}
	if oldReg, rerr := registry.Read(paths.RegistryPath()); rerr == nil && oldReg != nil {
		for _, e := range oldReg.Profiles {
			oldByID[e.Harness+"/"+e.Name] = e
		}
	}

	for _, p := range profiles {
		def, ok := set.Get(p.Harness)
		if !ok {
			res.Problems = append(res.Problems, fmt.Sprintf("profile %s: no definition for harness %q; wrapper left unchanged", p.ID(), p.Harness))
			continue
		}
		wrapperName := def.WrapperPrefix + "-" + p.Name
		known[wrapperName] = true

		if clash[wrapperName] {
			res.Problems = append(res.Problems, fmt.Sprintf("wrapper name %q is claimed by more than one profile; skipping", wrapperName))
			continue
		}

		configDir := paths.ProfileConfigDir(p.Harness, p.Name)
		changes, err := layer.PrepareConfigDirWith(def, configDir, layer.PrepareOptions{DryRun: opts.DryRun, FixLinks: opts.FixLinks})
		if err != nil {
			res.Problems = append(res.Problems, fmt.Sprintf("profile %s: prepare config dir: %v", p.ID(), err))
			continue
		}
		for _, c := range changes {
			if c.Mutating() {
				res.Changes = append(res.Changes, "config-dir "+p.ID()+": "+c.String())
				res.ConfigDirChanges++
			} else {
				res.Notes = append(res.Notes, "config-dir "+p.ID()+": "+c.String())
			}
		}

		hookEnv, herr := harness.RunEnvHook(def, harness.HookContext{
			Harness:   p.Harness,
			Profile:   p.Name,
			ConfigDir: configDir,
			AuthKind:  p.AuthKind,
			Model:     p.Model,
			Endpoint:  p.Endpoint,
		})
		if herr != nil {
			res.Problems = append(res.Problems, fmt.Sprintf("profile %s: exec hook env: %v", p.ID(), herr))
			continue
		}
		content, err := wrapper.RenderWith(p, def, configDir, wrapper.RenderOptions{HookEnv: hookEnv})
		if err != nil {
			res.Problems = append(res.Problems, fmt.Sprintf("profile %s: render wrapper: %v", p.ID(), err))
			continue
		}
		wrapperPath := filepath.Join(paths.BinDir, wrapperName)
		written, err := writeWrapper(wrapperPath, content, opts.DryRun)
		if err != nil {
			res.Problems = append(res.Problems, fmt.Sprintf("profile %s: write wrapper: %v", p.ID(), err))
			continue
		}
		if written {
			res.WrappersWritten = append(res.WrappersWritten, wrapperName)
			res.Changes = append(res.Changes, "wrapper "+wrapperName+": written")
		}

		regEntries = append(regEntries, registry.Entry{
			Harness:     p.Harness,
			Name:        p.Name,
			Label:       p.DisplayLabel(),
			Color:       p.Color,
			Icon:        p.Icon,
			AuthKind:    p.AuthKind,
			Primary:     p.Primary,
			ConfigDir:   configDir,
			WrapperPath: wrapperPath,
		})
		processed[p.ID()] = true
	}

	// FR-022: a profile that did not process cleanly (failed to load, missing
	// definition, wrapper-name clash, or a prepare/hook/render/write failure, all
	// reported in Problems) keeps its existing wrapper and registry entry unchanged. Carry
	// the old entry over and mark its wrapper known so stale removal never deletes a
	// working profile's wrapper because of a transient or unrelated failure.
	preserve := func(id string) {
		if processed[id] {
			return
		}
		if e, ok := oldByID[id]; ok {
			regEntries = append(regEntries, e)
			if e.WrapperPath != "" {
				known[filepath.Base(e.WrapperPath)] = true
			}
		}
	}
	for _, p := range profiles {
		preserve(p.ID())
	}
	for _, f := range failures {
		preserve(f.ID())
	}

	// Remove stale aprofile-marked wrappers.
	removed, err := removeStaleWrappers(paths.BinDir, known, opts.DryRun)
	if err != nil {
		return nil, err
	}
	for _, name := range removed {
		res.WrappersRemoved = append(res.WrappersRemoved, name)
		res.Changes = append(res.Changes, "wrapper "+name+": removed (stale)")
	}

	// Write the registry (only if changed).
	if opts.DryRun {
		data, _ := registry.Marshal(regEntries)
		existing, _ := os.ReadFile(paths.RegistryPath())
		if string(existing) != string(data) {
			res.RegistryChanged = true
			res.Changes = append(res.Changes, "registry: would update")
		}
	} else {
		changed, err := registry.Write(paths.RegistryPath(), regEntries)
		if err != nil {
			return nil, err
		}
		res.RegistryChanged = changed
		if changed {
			res.Changes = append(res.Changes, "registry: updated")
		}
	}

	// PATH management.
	res.Path = EnsurePath(paths.BinDir, settings, opts.RemovePath, opts.DryRun)

	return res, nil
}

// writeWrapper writes an executable wrapper only when its content differs, so an
// unchanged sync writes no files (SC-003).
func writeWrapper(path, content string, dryRun bool) (changed bool, err error) {
	if existing, err := os.ReadFile(path); err == nil && string(existing) == content {
		return false, nil
	}
	if dryRun {
		return true, nil
	}
	if err := config.WriteFileAtomic(path, []byte(content), 0o755); err != nil {
		return false, err
	}
	return true, nil
}

// removeStaleWrappers deletes aprofile-marked files in binDir whose names are not
// in the known set. It never touches files without the aprofile marker (FR-023).
func removeStaleWrappers(binDir string, known map[string]bool, dryRun bool) ([]string, error) {
	entries, err := os.ReadDir(binDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var removed []string
	for _, e := range entries {
		if e.IsDir() || known[e.Name()] {
			continue
		}
		path := filepath.Join(binDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if !wrapper.IsWrapper(data) {
			continue // not ours; never delete (FR-023)
		}
		if !dryRun {
			if err := os.Remove(path); err != nil {
				return nil, fmt.Errorf("remove stale wrapper %s: %w", path, err)
			}
		}
		removed = append(removed, e.Name())
	}
	sort.Strings(removed)
	return removed, nil
}

// wrapperNameClashes returns the set of wrapper names claimed by more than one
// profile (FR-002).
func wrapperNameClashes(profiles []*profile.Profile, set *harness.Set) map[string]bool {
	count := map[string]int{}
	for _, p := range profiles {
		def, ok := set.Get(p.Harness)
		if !ok {
			continue
		}
		count[def.WrapperPrefix+"-"+p.Name]++
	}
	clash := map[string]bool{}
	for name, n := range count {
		if n > 1 {
			clash[name] = true
		}
	}
	return clash
}
