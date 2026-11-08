package history

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/layer"
)

// Membership changes as change plans (spec 003, data-model.md "Membership state
// transitions", plan.md Design Decisions 2 and 4). The plans are layer.Step lists,
// so the dry run, the confirmation and the apply share one list (SC-005).

// PlanJoin plans joining the store st with the history entries of the profile
// config dir configDir; moving a member to another group is the same plan. For each
// history entry, in definition order:
//   - missing or a link: the steps sync plans for a member (layer.PlanHistoryEntry):
//     the store side is created where missing (for home a write into the default
//     setup, allowed on this explicit join), a link elsewhere is relinked, a
//     missing entry is linked, a correct link needs nothing;
//   - a real directory with merge per-file: each regular file whose path relative
//     to the entry root is absent in the store is moved into it (never
//     overwriting), every other file is set aside; the emptied tree is removed,
//     then the entry is linked;
//   - any other real entry: set aside whole (not-mergeable), then linked.
//
// setAside is the set-aside directory of this join. The plan writes nothing;
// joining the current group again plans only what is missing (FR-022b).
func PlanJoin(def *harness.Definition, st layer.HistoryStore, configDir, setAside string) ([]layer.Step, error) {
	var steps []layer.Step
	opts := layer.PlanOptions{HistoryStore: st, ExplicitJoin: true}
	for _, rule := range def.HistoryRules() {
		rule := rule
		rel := rule.Path()
		path := filepath.Join(configDir, filepath.FromSlash(rel))
		if linked, handled := layer.PlanHistoryEntry(def, &rule, configDir, opts); handled {
			steps = append(steps, linked...)
			continue
		}
		fi, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		// A store entry that resolves into this profile's config dir (for example
		// a default setup whose projects/ was linked to a profile by hand) is the
		// profile's own data: merging or setting it aside would delete it or link
		// the entry to itself.
		if err := checkNotSelf(st.EntryPath(rel), path, configDir); err != nil {
			return nil, err
		}
		// An explicit join may always create the store side (ok is always true).
		mkdir, _ := layer.HistoryStoreSteps(def, &rule, configDir, opts)
		link := layer.HistoryLinkStep(rel, path, st)
		if rule.EffectiveMerge() == harness.MergePerFile && fi.IsDir() {
			steps = append(steps, mkdir...)
			merged, err := planMerge(path, rel, st.EntryPath(rel), setAside)
			if err != nil {
				return nil, err
			}
			steps = append(steps, merged...)
			steps = append(steps, layer.Step{Kind: layer.StepRemoveEmptyDir, Path: path, Entry: rel, IsDir: true,
				Note: "emptied by the join"})
			steps = append(steps, link)
			continue
		}
		steps = append(steps, layer.Step{Kind: layer.StepSetAside, Path: path, Entry: rel, IsDir: fi.IsDir(),
			Dest: filepath.Join(setAside, filepath.FromSlash(rel)), Reason: layer.ReasonNotMergeable,
			Note: "not mergeable"})
		steps = append(steps, mkdir...)
		steps = append(steps, link)
	}
	return steps, nil
}

// planMerge plans the per-file merge of one real directory entry, recursively and
// keyed by the path relative to the entry root (FR-014). Files of one session are
// not kept together: a partial move is allowed and listed item by item. Only
// regular files move: a symbolic link would give every member a path to whatever
// it points at (a private file of this profile, a place outside the store), so it
// is set aside, as is a file whose store path runs through a symbolic link.
func planMerge(dir, rel, store, setAside string) ([]layer.Step, error) {
	var steps []layer.Step
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		sub, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		entry := rel + "/" + filepath.ToSlash(sub)
		aside := filepath.Join(setAside, filepath.FromSlash(entry))
		dst := filepath.Join(store, sub)
		notMergeable := layer.Step{Kind: layer.StepSetAside, Path: p, Entry: entry, Dest: aside,
			Reason: layer.ReasonNotMergeable, Note: "not mergeable"}
		if !d.Type().IsRegular() {
			steps = append(steps, notMergeable)
			return nil
		}
		if linkedComponent(store, sub) {
			notMergeable.Note = "not mergeable: its store path runs through a symbolic link"
			steps = append(steps, notMergeable)
			return nil
		}
		switch fi, serr := os.Lstat(dst); {
		case serr == nil && sameFile(p, dst, fi):
			// An interrupted earlier move: the store already holds this very file
			// (a hard link); the move completes by removing the profile's name.
			steps = append(steps, layer.Step{Kind: layer.StepMoveToStore, Path: p, Entry: entry, Dest: dst, Fallback: aside,
				Note: "completes an interrupted move: the store already holds this file"})
		case serr == nil || !os.IsNotExist(serr):
			// Present in the store, or blocked there (a path component is a file):
			// the store is never changed, so the file is set aside.
			steps = append(steps, layer.Step{Kind: layer.StepSetAside, Path: p, Entry: entry, Dest: aside,
				Reason: layer.ReasonExistsInStore, Note: "exists in store"})
		default:
			steps = append(steps, layer.Step{Kind: layer.StepMoveToStore, Path: p, Entry: entry, Dest: dst, Fallback: aside})
		}
		return nil
	})
	return steps, err
}

// linkedComponent reports whether an existing directory component of store/sub
// (below store, the file itself excluded) is a symbolic link.
func linkedComponent(store, sub string) bool {
	parts := strings.Split(filepath.ToSlash(sub), "/")
	cur := store
	for _, part := range parts[:len(parts)-1] {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if err != nil {
			return false // missing: created as a real directory by the move
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return true
		}
	}
	return false
}

// sameFile reports whether path and the store path dst are two names (hard links)
// of one file in two different directories: an interrupted move. One directory
// entry reached through a link is not.
func sameFile(path, dst string, fi os.FileInfo) bool {
	pfi, err := os.Lstat(path)
	if err != nil || !os.SameFile(pfi, fi) {
		return false
	}
	return layer.DistinctDirs(path, dst)
}

// checkNotSelf refuses a join whose store entry is the profile's own entry or
// lies inside its config dir, however the path is spelled (links, letter case,
// firmlinks): it compares file identity, never path strings (exit-2 class;
// nothing has been changed then).
func checkNotSelf(store, path, configDir string) error {
	sfi, err := os.Stat(store)
	if err != nil {
		return nil // missing: created by the join
	}
	refuse := func() error {
		return invalidf(ErrStoreIsProfile, "cannot join: the store entry %s is (or lies inside) this profile's config dir %s; remove that link (or move the data back into the store by hand) first; nothing changed", store, configDir)
	}
	if pfi, err := os.Stat(path); err == nil && os.SameFile(sfi, pfi) {
		return refuse()
	}
	cfi, err := os.Stat(configDir)
	if err != nil {
		return nil
	}
	cur, err := filepath.EvalSymlinks(store)
	if err != nil {
		return nil
	}
	for {
		if fi, err := os.Stat(cur); err == nil && os.SameFile(fi, cfi) {
			return refuse()
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return nil
		}
		cur = parent
	}
}

// PlanDetach plans removing the links of entries that are no longer declared as
// history (FR-022a) into old, the store of the group a profile leaves or moves
// away from (nil: nothing): each link is removed (never its target); a state or
// credentials entry gets a fresh empty private entry, any other category gets its
// treatment from the next sync. For the home store only state entries are
// considered (see layer planRule).
func PlanDetach(def *harness.Definition, configDir string, old layer.HistoryStore) []layer.Step {
	if old == nil {
		return nil
	}
	var steps []layer.Step
	for _, rule := range def.Classification {
		if rule.History || rule.IsGlob() {
			continue
		}
		rel := rule.Path()
		path := filepath.Join(configDir, filepath.FromSlash(rel))
		target, err := os.Readlink(path)
		if err != nil || filepath.Clean(target) != filepath.Clean(old.EntryPath(rel)) {
			continue
		}
		private := rule.Layer == harness.LayerState || rule.Layer == harness.LayerCredentials
		if old.IsHome() && rule.Layer != harness.LayerState {
			// In the default setup only a state entry can have been history; any
			// other link there is the share level's own, or a leak for --fix-links.
			continue
		}
		steps = append(steps, layer.Step{Kind: layer.StepUnlink, Path: path, Entry: rel, Source: target,
			Note: "not history: leaves the history group's store; the store is unchanged"})
		switch {
		case private && rule.IsDir():
			steps = append(steps, layer.Step{Kind: layer.StepMkdir, Path: path, Entry: rel, IsDir: true, Note: string(rule.Layer) + ": private, empty"})
		case private:
			steps = append(steps, layer.Step{Kind: layer.StepCreate, Path: path, Entry: rel, Data: []byte{}, Mode: 0o600, Note: string(rule.Layer) + ": private, empty"})
		}
	}
	return steps
}

// PlanLeave plans leaving a group (FR-015): each history link is removed (never
// its target) and replaced by a fresh empty private entry; a missing history
// directory is created empty. Real entries are already private and stay. A link
// into the old store from an entry that is no longer declared as history (FR-022a)
// is removed too, so a profile that leaves never keeps writing into the group's
// history; old is the store of the group being left (nil when unknown). No step
// touches a store.
func PlanLeave(def *harness.Definition, configDir string, old layer.HistoryStore) ([]layer.Step, error) {
	steps := PlanDetach(def, configDir, old)
	for _, rule := range def.HistoryRules() {
		rel := rule.Path()
		path := filepath.Join(configDir, filepath.FromSlash(rel))
		fi, err := os.Lstat(path)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		isLink := err == nil && fi.Mode()&os.ModeSymlink != 0
		if isLink {
			target, _ := os.Readlink(path)
			steps = append(steps, layer.Step{Kind: layer.StepUnlink, Path: path, Entry: rel, Source: target,
				Note: "leaves the history group; the store is unchanged"})
		}
		if err == nil && !isLink {
			continue
		}
		if rule.IsDir() {
			steps = append(steps, layer.Step{Kind: layer.StepMkdir, Path: path, Entry: rel, IsDir: true, Note: "history: private, empty"})
		} else if isLink {
			steps = append(steps, layer.Step{Kind: layer.StepCreate, Path: path, Entry: rel, Data: []byte{}, Mode: 0o600, Note: "history: private, empty"})
		}
	}
	return steps, nil
}

// NeedsConfirmation reports whether a membership plan moves or sets aside data
// (FR-014): only then does the change ask once (or need --yes).
func NeedsConfirmation(steps []layer.Step) bool {
	for _, s := range steps {
		if s.Kind == layer.StepMoveToStore || s.Kind == layer.StepSetAside {
			return true
		}
	}
	return false
}

// SetAsideStamp formats the UTC timestamp of a set-aside directory.
func SetAsideStamp(now time.Time) string { return now.UTC().Format("20060102T150405Z") }

// SetAsideRunDir is the set-aside directory of one join:
// <data>/aprofile/set-aside/<harness>/<name>/<UTC timestamp>/. When a directory of
// that second exists already (a second join within the same second), a counter is
// appended (-2, -3, ...), so a join never mixes its items into an earlier run or
// rewrites that run's manifest.
func SetAsideRunDir(paths *config.Paths, harnessName, name string, now time.Time) string {
	base := filepath.Join(paths.SetAsideDir(harnessName, name), SetAsideStamp(now))
	dir := base
	for i := 2; ; i++ {
		if _, err := os.Lstat(dir); err != nil {
			// Free, or not inspectable (a permission error): any write there fails
			// and reports itself, so the search stops.
			return dir
		}
		dir = fmt.Sprintf("%s-%d", base, i)
	}
}

// manifest is the set-aside manifest.json (data-model.md "Set-aside history").
type manifest struct {
	Profile string               `json:"profile"`
	Group   string               `json:"group"`
	Created string               `json:"created"`
	Items   []layer.SetAsideItem `json:"items"`
}

// ManifestWriter returns the layer.ApplyOptions.SetAsideManifest callback of one
// join: each call appends the item and rewrites dir/manifest.json atomically, so
// the manifest is on disk before the item it names is moved.
func ManifestWriter(dir, profileID, group string, now time.Time) func(layer.SetAsideItem) error {
	m := manifest{Profile: profileID, Group: group, Created: now.UTC().Format(time.RFC3339), Items: []layer.SetAsideItem{}}
	var mu sync.Mutex
	return func(item layer.SetAsideItem) error {
		mu.Lock()
		defer mu.Unlock()
		m.Items = append(m.Items, item)
		data, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			return err
		}
		return config.WriteFileAtomic(filepath.Join(dir, "manifest.json"), append(data, '\n'), 0o644)
	}
}

// SetAsideRun is one set-aside directory of a profile with the items its manifest
// lists.
type SetAsideRun struct {
	Dir   string               `json:"dir"`
	Items []layer.SetAsideItem `json:"items"`
}

// ListSetAside lists the set-aside history of a profile, oldest first. A missing
// directory yields none.
func ListSetAside(paths *config.Paths, harnessName, name string) ([]SetAsideRun, error) {
	root := paths.SetAsideDir(harnessName, name)
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []SetAsideRun
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		run := SetAsideRun{Dir: filepath.Join(root, e.Name()), Items: []layer.SetAsideItem{}}
		if data, err := os.ReadFile(filepath.Join(run.Dir, "manifest.json")); err == nil {
			var m manifest
			if json.Unmarshal(data, &m) == nil && m.Items != nil {
				run.Items = m.Items
			}
		}
		out = append(out, run)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dir < out[j].Dir })
	return out, nil
}
