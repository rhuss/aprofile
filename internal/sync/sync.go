// Package sync orchestrates one aprofile sync: it prepares each profile's config
// dir by layer, renders and writes one wrapper per profile, removes only
// aprofile-marked stale wrappers, writes the registry, and manages the PATH block.
// It is idempotent: a second run with no changes writes 0 files and reports 0
// changes (FR-022..FR-025, SC-003).
package sync

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/hooksets"
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
	// Relink replaces a real entry where a link is expected with the link, after
	// Confirm and a Backup (FR-021). Without it such entries are only reported.
	Relink  bool
	Confirm func(profileID string, steps []layer.Step) bool
	Backup  func(harness, name string, paths []string) (string, error)
}

// Result is a structured report of what the sync did.
type Result struct {
	Changes          []string // human-readable list of mutating changes
	WrappersWritten  []string
	WrappersRemoved  []string
	ConfigDirChanges int
	RegistryChanged  bool
	RegistryPath     string
	RegistryRemoved  []string // profile ids whose registry entry is dropped
	Path             PathResult
	Problems         []string // reported, non-fatal issues (FR-022)
	Notes            []string // reported conditions that changed nothing (FR-018: unclassified/leaking links)
	HookChanges      int      // managed hook entries placed or removed
	// Profiles is the per-profile plan (dry run) or outcome, sorted by profile id;
	// it backs `sync --json` (FR-041).
	Profiles []ProfilePlan
}

// Per-profile outcome values (sync --json, FR-041).
const (
	StatusOK     = "ok"     // processed this run
	StatusFailed = "failed" // not processed; wrapper and registry entry left unchanged (FR-022)

	ActionWrite     = "write"     // wrapper (re)written, or would be under --dry-run
	ActionUnchanged = "unchanged" // already up to date
	ActionKept      = "kept"      // left as is because the profile failed
	ActionAdd       = "add"       // registry entry added
	ActionUpdate    = "update"    // registry entry changed
	ActionNone      = "none"      // nothing exists and nothing is written
)

// ProfilePlan is one profile's part of a sync.
type ProfilePlan struct {
	Profile   string        `json:"profile"`
	Status    string        `json:"status"`
	Wrapper   WrapperPlan   `json:"wrapper"`
	ConfigDir ConfigDirPlan `json:"config_dir"`
	Hooks     []StepPlan    `json:"hooks"`
	Registry  string        `json:"registry"`
	Problems  []string      `json:"problems"`
	Notes     []string      `json:"notes"`
}

// WrapperPlan names a profile's wrapper and what sync does with it.
type WrapperPlan struct {
	Name   string `json:"name,omitempty"`
	Path   string `json:"path,omitempty"`
	Action string `json:"action"`
}

// ConfigDirPlan lists the config-dir steps of one profile.
type ConfigDirPlan struct {
	Path  string     `json:"path,omitempty"`
	Steps []StepPlan `json:"steps"`
}

// StepPlan is one config-dir or hook step: action is a change-plan kind (link,
// mkdir, copy-once, ...), "report" for a reported condition, or "needs-confirm" for
// a step sync skips because it replaces profile data.
type StepPlan struct {
	Action   string `json:"action"`
	Path     string `json:"path"`
	Note     string `json:"note,omitempty"`
	Mutating bool   `json:"mutating"`
}

func newProfilePlan(id string) *ProfilePlan {
	return &ProfilePlan{Profile: id, Status: StatusOK, Hooks: []StepPlan{}, ConfigDir: ConfigDirPlan{Steps: []StepPlan{}},
		Problems: []string{}, Notes: []string{}}
}

// Changed reports whether the sync changed anything.
func (r *Result) Changed() bool {
	return len(r.WrappersWritten) > 0 || len(r.WrappersRemoved) > 0 ||
		r.ConfigDirChanges > 0 || r.RegistryChanged || r.Path.Changed || r.HookChanges > 0
}

// HasProblems reports whether any profile/definition failed to load or clashed.
func (r *Result) HasProblems() bool { return len(r.Problems) > 0 }

// Run performs the sync.
func Run(paths *config.Paths, set *harness.Set, store *profile.Store, settings *config.Settings, opts Options) (*Result, error) {
	res := &Result{}

	res.RegistryPath = paths.RegistryPath()
	plans := map[string]*ProfilePlan{}
	planFor := func(id string) *ProfilePlan {
		if pp, ok := plans[id]; ok {
			return pp
		}
		pp := newProfilePlan(id)
		plans[id] = pp
		return pp
	}
	// problem records a profile's problem both in the global list and in its plan;
	// any problem means the profile did not process cleanly.
	problem := func(id, msg string) {
		res.Problems = append(res.Problems, msg)
		pp := planFor(id)
		pp.Problems = append(pp.Problems, msg)
	}
	note := func(id, msg string) {
		res.Notes = append(res.Notes, msg)
		pp := planFor(id)
		pp.Notes = append(pp.Notes, msg)
	}
	// A condition of a harness rather than of one profile (a declared exec hook that is
	// not installed) goes into every affected profile's plan but into the run's list
	// only once per key (Prod8).
	reported := map[string]bool{}
	first := func(key string) bool {
		if reported[key] {
			return false
		}
		reported[key] = true
		return true
	}
	noteOnce := func(key, id, msg string) {
		pp := planFor(id)
		pp.Notes = append(pp.Notes, msg)
		if first(key) {
			res.Notes = append(res.Notes, msg)
		}
	}
	problemOnce := func(key, id, msg string) {
		pp := planFor(id)
		pp.Problems = append(pp.Problems, msg)
		if first(key) {
			res.Problems = append(res.Problems, msg)
		}
	}

	profiles, failures, err := store.List()
	if err != nil {
		return nil, err
	}
	for _, f := range failures {
		problem(f.ID(), fmt.Sprintf("profile %s failed to load: %v", f.ID(), f.Err))
	}
	for _, e := range set.Errors() {
		res.Problems = append(res.Problems, "harness definition error: "+e.Error())
	}

	// Detect wrapper-name clashes (FR-002): skip both sides, report, but keep their
	// existing files (do not treat as stale). A profile that fails to load still claims
	// its wrapper name.
	keys := make([]harness.ProfileKey, 0, len(profiles)+len(failures))
	for _, p := range profiles {
		keys = append(keys, harness.ProfileKey{Harness: p.Harness, Name: p.Name})
	}
	for _, f := range failures {
		keys = append(keys, harness.ProfileKey{Harness: f.Harness, Name: f.Name})
	}

	// The hook-set library (FR-023); a bad file is a problem, the others still apply.
	lib, libErrs := hooksets.LoadLibrary(paths.HooksDir())
	for _, e := range libErrs {
		res.Problems = append(res.Problems, "hook set library: "+e.Error())
	}

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
		pp := planFor(p.ID())
		def, ok := set.Get(p.Harness)
		if !ok {
			problem(p.ID(), fmt.Sprintf("profile %s: harness %q has no usable definition (missing, or it failed to load); wrapper and registry entry left unchanged", p.ID(), p.Harness))
			continue
		}
		wrapperName := def.WrapperName(p.Name)
		known[wrapperName] = true
		pp.Wrapper.Name = wrapperName
		pp.Wrapper.Path = filepath.Join(paths.BinDir, wrapperName)

		if other, clash := set.WrapperClash(p.Harness, p.Name, keys, ""); clash {
			problem(p.ID(), fmt.Sprintf("profile %s: wrapper name %q is also claimed by profile %s; skipping", p.ID(), wrapperName, other.ID()))
			continue
		}

		// FR-006: the profile must fit its definition. An undeclared auth kind fails the
		// profile before anything is written (its wrapper would clear every auth
		// variable); a model or endpoint the harness has no variable for is a note.
		fatal := false
		for _, fi := range wrapper.FitIssues(p, def) {
			if fi.Fatal {
				fatal = true
				problem(p.ID(), fmt.Sprintf("profile %s: %s; fix: %s; wrapper and registry entry left unchanged", p.ID(), fi.Message, fi.Fix))
				continue
			}
			note(p.ID(), fmt.Sprintf("profile %s: %s; fix: %s", p.ID(), fi.Message, fi.Fix))
		}
		if fatal {
			continue
		}

		configDir := paths.ProfileConfigDir(p.Harness, p.Name)
		pp.ConfigDir.Path = configDir
		// T091 (FR-018): an interrupted atomic write leaves aprofile's own temp
		// entries behind. They are never profile entries: restore an entry an
		// interrupted swap left only in its "-old" temp, and remove the stale temps
		// (a younger one may belong to a write in progress). Each is a change of the
		// run (a planned one under --dry-run); one that fails is a problem.
		temps, terr := layer.RemoveStaleTemps(def, configDir, time.Now().Add(-layer.StaleTempAge), opts.DryRun)
		tempChange := func(action, path, why string) {
			pp.ConfigDir.Steps = append(pp.ConfigDir.Steps, StepPlan{Action: action, Path: path, Note: why, Mutating: true})
			res.Changes = append(res.Changes, fmt.Sprintf("config-dir %s: %s %s (%s)", p.ID(), action, path, why))
			res.ConfigDirChanges++
		}
		renamed := "renamed"
		if opts.DryRun {
			renamed = "would rename"
		}
		for _, r := range temps.Restored {
			tempChange(ActionRestoreEntry, r.Entry, "from "+r.Temp)
			note(p.ID(), fmt.Sprintf("config-dir %s: an interrupted aprofile swap left %s only in %s; %s it back", p.ID(), r.Entry, r.Temp, renamed))
		}
		for _, rel := range temps.Removed {
			tempChange(ActionRemoveTemp, rel, "stale temp entry left by an interrupted aprofile write")
		}
		for _, ferr := range temps.Failed {
			problem(p.ID(), fmt.Sprintf("config-dir %s: %v", p.ID(), ferr))
		}
		if terr != nil {
			problem(p.ID(), fmt.Sprintf("config-dir %s: look for stale temp entries: %v", p.ID(), terr))
		}
		prep := layer.PrepareOptions{DryRun: opts.DryRun, FixLinks: opts.FixLinks, Relink: opts.Relink}
		if opts.Relink {
			id := p.ID()
			h, n := p.Harness, p.Name
			prep.Confirm = func(steps []layer.Step) bool { return opts.Confirm != nil && opts.Confirm(id, steps) }
			prep.Backup = func(paths []string) (string, error) {
				if opts.Backup == nil {
					return "", fmt.Errorf("no backup location")
				}
				return opts.Backup(h, n, paths)
			}
		}
		changes, err := layer.PrepareConfigDirWith(def, p, configDir, prep)
		for _, c := range changes {
			pp.ConfigDir.Steps = append(pp.ConfigDir.Steps, StepPlan{Action: c.Action, Path: stepPath(c.Step, c.Path), Note: c.Note, Mutating: c.Mutating()})
			switch {
			case c.Mutating():
				res.Changes = append(res.Changes, "config-dir "+p.ID()+": "+c.String())
				res.ConfigDirChanges++
			case c.Action == "needs-confirm":
				// FR-017a: sync never replaces profile data; name the command that does.
				note(p.ID(), fmt.Sprintf("config-dir %s: %s skipped: it replaces profile data; to complete it run: aprofile sync --relink, or aprofile edit %s --share %s",
					p.ID(), c.Step.String(), p.ID(), p.ShareLevel()))
			default:
				note(p.ID(), "config-dir "+p.ID()+": "+c.String())
			}
		}
		if err != nil {
			problem(p.ID(), fmt.Sprintf("profile %s: prepare config dir: %v", p.ID(), err))
			continue
		}

		// Config patches (FR-019, T071): merge-only writes of the keys the definition
		// patches; a failing patch is a problem but does not hold back the wrapper.
		syncPatches(def, p, configDir, opts, res, pp, problem, note)

		// Hook sets (FR-028): place, update and remove only the managed entries.
		if def.Hooks != nil {
			if err := syncHooks(paths, def, p, lib, configDir, opts, res, pp, problem, note); err != nil {
				problem(p.ID(), fmt.Sprintf("profile %s: hook sets: %v", p.ID(), err))
			}
		}

		// The exec hook's write-config operation runs after the config dir is prepared
		// (FR-011, T066).
		syncWriteConfig(def, p, wrapper.HookContext(p, configDir), opts, res, pp, problem, noteOnce)
		if def.HookDeclares("pre-launch") {
			if _, lerr := exec.LookPath(harness.HookExecutable(def.Name)); lerr != nil {
				noteOnce(def.Name+" pre-launch", p.ID(), fmt.Sprintf("harness %s: exec hook %s is not on PATH; the wrappers of its profiles refuse to start until it is installed (the definition declares pre-launch)",
					def.Name, harness.HookExecutable(def.Name)))
			}
		}

		content, err := wrapper.Expected(p, def, configDir)
		switch {
		case errors.Is(err, harness.ErrHookNotFound):
			problemOnce(def.Name+" env", p.ID(), fmt.Sprintf("harness %s: %v; the wrappers and registry entries of its profiles are left unchanged", def.Name, err))
			continue
		case err != nil:
			problem(p.ID(), fmt.Sprintf("profile %s: %v", p.ID(), err))
			continue
		}
		wrapperPath := filepath.Join(paths.BinDir, wrapperName)
		written, err := writeWrapper(wrapperPath, content, opts.DryRun)
		if err != nil {
			problem(p.ID(), fmt.Sprintf("profile %s: write wrapper: %v", p.ID(), err))
			continue
		}
		// FR-044: a different executable earlier on PATH wins over the wrapper; name it
		// (the same check doctor runs).
		if shadow, ok := wrapper.ShadowedBy(paths.BinDir, wrapperName); ok {
			note(p.ID(), fmt.Sprintf("profile %s: %s; %s", p.ID(), wrapper.ShadowMessage(wrapperName, shadow), wrapper.ShadowFix))
		}
		pp.Wrapper.Action = ActionUnchanged
		if written {
			pp.Wrapper.Action = ActionWrite
			res.WrappersWritten = append(res.WrappersWritten, wrapperName)
			res.Changes = append(res.Changes, "wrapper "+wrapperName+": written")
		}

		entry := registry.Entry{
			Harness:     p.Harness,
			Name:        p.Name,
			Label:       p.DisplayLabel(),
			Color:       p.Color,
			Icon:        p.Icon,
			AuthKind:    p.AuthKind,
			Primary:     p.Primary,
			ConfigDir:   configDir,
			WrapperPath: wrapperPath,
			Share:       string(p.ShareLevel()),
		}
		regEntries = append(regEntries, entry)
		switch old, ok := oldByID[p.ID()]; {
		case !ok:
			pp.Registry = ActionAdd
		case old != entry:
			pp.Registry = ActionUpdate
		default:
			pp.Registry = ActionUnchanged
		}
		processed[p.ID()] = true
	}

	// FR-022: a profile that did not process cleanly (failed to load, missing
	// definition, wrapper-name clash, or a prepare/hook/render/write failure, all
	// reported in Problems) keeps its existing wrapper and registry entry unchanged. Carry
	// the old entry over and mark its wrapper known so stale removal never deletes a
	// working profile's wrapper because of a transient or unrelated failure.
	existing := map[string]bool{} // every profile id with a profile file
	preserve := func(id string) {
		existing[id] = true
		if processed[id] {
			return
		}
		pp := planFor(id)
		pp.Status = StatusFailed
		pp.Wrapper.Action = ActionKept
		pp.Registry = ActionNone
		if e, ok := oldByID[id]; ok {
			regEntries = append(regEntries, e)
			pp.Registry = ActionKept
			if e.WrapperPath != "" {
				known[filepath.Base(e.WrapperPath)] = true
				if pp.Wrapper.Name == "" {
					pp.Wrapper.Name = filepath.Base(e.WrapperPath)
					pp.Wrapper.Path = e.WrapperPath
				}
			}
		}
	}
	for _, p := range profiles {
		preserve(p.ID())
	}
	for _, f := range failures {
		preserve(f.ID())
	}
	for id := range oldByID {
		if !existing[id] {
			res.RegistryRemoved = append(res.RegistryRemoved, id)
		}
	}
	sort.Strings(res.RegistryRemoved)

	// Remove stale aprofile-marked wrappers. A wrapper whose harness has no usable
	// definition (it failed to load) while a profile (loaded or not) still names that
	// harness is not stale: sync cannot tell which of that harness's wrappers belong to
	// a profile, so it keeps them all (FR-022). Without any such profile the wrappers
	// are stale like any other.
	named := map[string]bool{}
	for _, k := range keys {
		named[k.Harness] = true
	}
	keepHarness := func(h string) bool { _, ok := set.Get(h); return !ok && named[h] }
	removed, err := removeStaleWrappers(paths.BinDir, known, keepHarness, opts.DryRun)
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

	ids := make([]string, 0, len(plans))
	for id := range plans {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		res.Profiles = append(res.Profiles, *plans[id])
	}
	return res, nil
}

// stepPath is the profile-relative path of a step, or the given fallback.
func stepPath(s layer.Step, fallback string) string {
	if s.Entry != "" {
		return s.Entry
	}
	return fallback
}

// Step actions sync adds to the change-plan kinds of the config-dir steps.
const (
	ActionPatch        = "patch"        // a config patch set keys of a file (FR-019)
	ActionWriteConfig  = "write-config" // the exec hook's write-config operation (FR-011)
	ActionReport       = "report"       // a reported condition that changed nothing
	ActionRemoveTemp   = "remove-temp"  // a stale aprofile temp entry removed (T091)
	ActionRestoreEntry = "restore"      // an entry renamed back from an interrupted swap's temp
)

// syncPatches applies the definition's config patches to one profile and records
// each applied patch as a mutating "patch" step naming the keys it set. A skipped
// patch (absent or linked file) is a note; a patch that cannot be applied (the file
// is not valid JSON/TOML, or cannot be written) is a problem and leaves the file
// unchanged.
func syncPatches(def *harness.Definition, p *profile.Profile, configDir string, opts Options, res *Result, pp *ProfilePlan,
	problem, note func(id, msg string)) {
	for _, o := range layer.ApplyConfigPatches(def, configDir, opts.DryRun) {
		switch {
		case o.Err != nil:
			problem(p.ID(), fmt.Sprintf("profile %s: %v", p.ID(), o.Err))
		case o.Skipped != "":
			msg := fmt.Sprintf("config patch %s not applied: %s", o.File, o.Skipped)
			pp.ConfigDir.Steps = append(pp.ConfigDir.Steps, StepPlan{Action: ActionReport, Path: o.File, Note: msg})
			note(p.ID(), fmt.Sprintf("config-dir %s: %s", p.ID(), msg))
		case o.Changed:
			n := "config patch: set " + strings.Join(o.Keys, ", ")
			pp.ConfigDir.Steps = append(pp.ConfigDir.Steps, StepPlan{Action: ActionPatch, Path: o.File, Note: n, Mutating: true})
			res.Changes = append(res.Changes, fmt.Sprintf("config-dir %s: patch %s (set %s)", p.ID(), o.File, strings.Join(o.Keys, ", ")))
			res.ConfigDirChanges++
		}
	}
}

// syncWriteConfig calls the exec hook's write-config operation (FR-011). It is not
// called under --dry-run, because the hook writes; the plan lists it as a
// non-mutating step instead. changed=true counts as a config-dir change, so an
// unchanged second sync still reports 0 changes. A missing hook executable is a note,
// once per harness (the config the hook would write is missing, but the wrapper still
// launches the harness); any other hook failure is a problem.
func syncWriteConfig(def *harness.Definition, p *profile.Profile, hctx harness.HookContext, opts Options, res *Result, pp *ProfilePlan,
	problem func(id, msg string), noteOnce func(key, id, msg string)) {
	if !def.HookDeclares("write-config") {
		return
	}
	if opts.DryRun {
		pp.ConfigDir.Steps = append(pp.ConfigDir.Steps, StepPlan{Action: ActionWriteConfig, Path: ".",
			Note: "exec hook write-config runs here; not called under --dry-run"})
		return
	}
	changed, err := harness.RunWriteConfigHook(def, hctx)
	switch {
	case errors.Is(err, harness.ErrHookNotFound):
		noteOnce(def.Name+" write-config", p.ID(), fmt.Sprintf("harness %s: %v", def.Name, err))
	case err != nil:
		problem(p.ID(), fmt.Sprintf("profile %s: %v", p.ID(), err))
	case changed:
		pp.ConfigDir.Steps = append(pp.ConfigDir.Steps, StepPlan{Action: ActionWriteConfig, Path: ".", Note: "exec hook reported changed=true", Mutating: true})
		res.Changes = append(res.Changes, fmt.Sprintf("config-dir %s: exec hook write-config changed the config", p.ID()))
		res.ConfigDirChanges++
	default:
		pp.ConfigDir.Steps = append(pp.ConfigDir.Steps, StepPlan{Action: ActionWriteConfig, Path: ".", Note: "exec hook reported changed=false"})
	}
}

// syncHooks places the managed hook entries of one profile and saves its record.
// Diverged entries are reported and left alone; a referenced set missing from the
// library, or a set whose body does not fit the harness, is a problem (sync exits
// non-zero) and its entries stay unchanged.
func syncHooks(paths *config.Paths, def *harness.Definition, p *profile.Profile, lib map[string]*hooksets.Set, configDir string, opts Options, res *Result,
	pp *ProfilePlan, problem, note func(id, msg string)) error {
	hp, applied, err := hooksets.SyncProfile(def, p, lib, paths.ManagedHooksPath(p.Harness, p.Name), configDir, opts.DryRun)
	for _, s := range applied {
		res.Changes = append(res.Changes, "hooks "+p.ID()+": "+s.String())
		res.HookChanges++
		pp.Hooks = append(pp.Hooks, StepPlan{Action: string(s.Kind), Path: stepPath(s, s.Path), Note: s.Note, Mutating: s.Mutating()})
	}
	if hp != nil {
		for _, m := range hp.MissingSets {
			problem(p.ID(), fmt.Sprintf("profile %s: hook set %q is referenced but missing from %s; its managed entries were left unchanged", p.ID(), m, paths.HooksDir()))
		}
		for _, m := range hp.InvalidSets {
			problem(p.ID(), fmt.Sprintf("profile %s: %s; the set was not applied", p.ID(), m))
		}
		for _, m := range hp.MissingScripts {
			note(p.ID(), fmt.Sprintf("hooks %s: %s", p.ID(), m))
		}
		for _, d := range hp.Divergences {
			note(p.ID(), fmt.Sprintf("hooks %s: %s", p.ID(), d.String()))
		}
	}
	return err
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
// in the known set. It never touches files without the aprofile marker (FR-023),
// nor a wrapper whose marker names a harness keepHarness reports (FR-022).
func removeStaleWrappers(binDir string, known map[string]bool, keepHarness func(string) bool, dryRun bool) ([]string, error) {
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
		if h := markerHarness(data); h != "" && keepHarness != nil && keepHarness(h) {
			continue // its harness failed to load: leave it unchanged (FR-022)
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

// markerHarness returns the harness named on a wrapper's marker line
// ("# aprofile:wrapper harness=<h> profile=<n>"), or "".
func markerHarness(content []byte) string {
	h, _, _ := wrapper.MarkerIdentity(content)
	return h
}
