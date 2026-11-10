package launch

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/profile"
)

// beforeWrite is a test hook called between Apply's first plan and the fresh read
// it writes from.
var beforeWrite func()

// Sync states of a profile folder (FR-020a).
const (
	SyncSynced  = "synced"
	SyncStale   = "stale"
	SyncUnknown = "unknown"
)

// StatusReport is what list, show and doctor report about a profile folder.
type StatusReport struct {
	Settings []Setting
	Sync     string // synced | stale | unknown
	Unplaced []Entry
	Err      error
	// Linked names the folder_launch files that are links (into the default setup).
	Linked map[string]bool
}

// HookEnv returns the exec hook's env output for a profile when the definition
// declares the env operation and folder launch locations; otherwise nil. It is the
// same call the wrapper makes, so the folder and the wrapper take the same carrier
// decision: hook variables are listed in APROFILE_SET_VARS, and a key read from a
// hook variable stays with the wrapper (cleared source).
func HookEnv(p *profile.Profile, def *harness.Definition, configDir string) ([]harness.EnvVar, error) {
	if def.FolderLaunch == nil || !def.HookDeclares("env") {
		return nil, nil
	}
	return harness.RunEnvHook(def, harness.HookContext{
		Harness: p.Harness, Profile: p.Name, ConfigDir: configDir,
		AuthKind: p.AuthKind, Model: p.Model, Endpoint: p.Endpoint,
	})
}

// Apply places the profile's launch entries: it plans, then re-reads the files and
// re-plans immediately before writing (the harness writes the same files at
// runtime), writes each changed file atomically keeping its mode, and saves the
// record (FR-003 to FR-005, FR-009a). Under dryRun it only plans. A linked file is
// never written. The files the record lists are read as well, so entries at a
// location the definition no longer names are removed (FR-005).
//
// The record is written ahead (writeAhead): before any file changes, it is saved
// with the old entries (updated from the plan) plus the new entries that replace a
// setting, so a write that fails part way never loses a replaced setting and never
// claims a user's own entry; a new entry left unrecorded is found equal and
// recorded by the next sync. A planning error leaves every file and the record
// unchanged.
func Apply(p *profile.Profile, def *harness.Definition, configDir, recPath string, dryRun bool) (Result, error) {
	rec, err := Load(recPath)
	if err != nil {
		return Result{}, err
	}
	hookEnv, err := HookEnv(p, def, configDir)
	if err != nil {
		return Result{}, err
	}
	res, err := PlanWith(p, def, ReadDocFor(def, configDir, rec), rec, hookEnv)
	if err != nil || dryRun {
		return res, err
	}
	if len(res.writes) == 0 && recordEqual(res.NewRecord, rec) {
		return res, nil
	}
	if beforeWrite != nil {
		beforeWrite()
	}
	doc := ReadDocFor(def, configDir, rec)
	res, err = PlanWith(p, def, doc, rec, hookEnv)
	if err != nil {
		return res, err
	}
	files := make([]string, 0, len(res.writes))
	for f := range res.writes {
		if doc.Files[f].Linked {
			return res, fmt.Errorf("%s: is a link; launch settings are never written through it", f)
		}
		files = append(files, f)
	}
	sort.Strings(files)
	if len(files) > 0 {
		if _, err := writeAhead(res.NewRecord, rec).Save(recPath); err != nil {
			return res, fmt.Errorf("save launch record: %w", err)
		}
	}
	for _, f := range files {
		mode := doc.Files[f].Mode
		if mode == 0 {
			mode = 0o644
		}
		path := filepath.Join(configDir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return res, err
		}
		if err := config.WriteFileAtomic(path, res.writes[f], mode); err != nil {
			return res, fmt.Errorf("%s: write: %w", f, err)
		}
	}
	if _, err := res.NewRecord.Save(recPath); err != nil {
		return res, fmt.Errorf("save launch record: %w", err)
	}
	return res, nil
}

// writeAhead returns the record Apply saves before it writes any file: every entry
// of prev (updated from next, whose entries carry the replaced settings prev
// recorded for them) plus the new entries of next that replace a setting (a Codex
// auth block replacing env_key), so a failed write never loses the replaced value.
// Other new entries are recorded only after the writes: recording them early would
// let a failed write claim a user's own entry as placed, and a later sync would
// remove it. A new entry whose file was written but whose record was not saved is
// found equal and unrecorded by the next sync and recorded then.
func writeAhead(next, prev Record) Record {
	out := Record{FormatVersion: RecordFormatVersion, Entries: append([]Entry(nil), prev.Entries...)}
	for _, e := range next.Entries {
		if i, ok := out.find(e.File, e.Location); ok {
			out.Entries[i] = e
			continue
		}
		if len(e.Replaced) > 0 {
			out.Entries = append(out.Entries, e)
		}
	}
	return out.sorted()
}

// recordEqual compares two records by their entries.
func recordEqual(a, b Record) bool {
	ea, eb := a.sorted().Entries, b.sorted().Entries
	if len(ea) == 0 && len(eb) == 0 {
		return true
	}
	return reflect.DeepEqual(ea, eb)
}

// Status reports the profile folder's settings, whether it needs sync, and the
// launch entries aprofile did not place (FR-020, FR-020a, FR-020b). A file that
// cannot be planned gives Sync "unknown" with Err set.
func Status(p *profile.Profile, def *harness.Definition, configDir, recPath string) (StatusReport, error) {
	rep := StatusReport{Sync: SyncUnknown, Linked: map[string]bool{}}
	rec, err := Load(recPath)
	if err != nil {
		rep.Settings = Decide(p, def, ReadDoc(def, configDir))
		rep.Err = err
		return rep, err
	}
	doc := ReadDocFor(def, configDir, rec)
	for f, fd := range doc.Files {
		if fd.Linked {
			rep.Linked[f] = true
		}
	}
	hookEnv, err := HookEnv(p, def, configDir)
	if err != nil {
		// The exec hook's failure is reported where the wrapper status is; the
		// folder's state cannot be told without its output.
		rep.Settings = Decide(p, def, doc)
		rep.Err = err
		return rep, nil
	}
	rep.Settings = DecideWith(p, def, doc, hookEnv)
	res, err := PlanWith(p, def, doc, rec, hookEnv)
	if err != nil {
		rep.Err = err
		return rep, err
	}
	rep.Unplaced = res.Unplaced
	rep.Sync = SyncSynced
	if res.Stale() {
		rep.Sync = SyncStale
	}
	return rep, nil
}
