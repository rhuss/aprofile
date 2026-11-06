package hooksets

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/layer"
	"github.com/cc-deck/aprofile/internal/profile"
)

// Divergence is a recorded managed entry that is no longer what aprofile placed:
// the user edited or removed it, or a real file sits where a link should be. It is
// left as is, never re-added, and reported (FR-020, FR-028).
type Divergence struct {
	Set    string `json:"set"`
	Entry  string `json:"entry"` // event name or profile-relative path
	Reason string `json:"reason"`
}

func (d Divergence) String() string {
	return fmt.Sprintf("managed hook entry %s of set %q diverged: %s", d.Entry, d.Set, d.Reason)
}

// HookPlan is the result of PlanHooks.
type HookPlan struct {
	Steps          []layer.Step
	Divergences    []Divergence
	MissingSets    []string // named by the profile but not in the library
	MissingScripts []string // referenced by a set body but not on disk
	InvalidSets    []string // applying sets whose body does not match the hook form
	Record         *Record  // the record after the steps are applied
}

// PlanOptions adjusts hook planning.
type PlanOptions struct {
	// Reapply re-places diverged entries (hook-sets reapply, FR-028): a replaced
	// file or a group edited in place is replaced after a backup (Confirm steps).
	Reapply bool
}

// PlanHooks computes the steps that make the profile's hook location hold exactly
// the managed entries of the sets that apply to it, next to every entry it did not
// place (FR-028, SC-008). rec is the profile's current record. At share level all
// it plans nothing: the hooks come from the default setup.
func PlanHooks(def *harness.Definition, p *profile.Profile, lib map[string]*Set, rec *Record, configDir string, opts PlanOptions) (*HookPlan, error) {
	hp := &HookPlan{Record: &Record{FormatVersion: RecordFormatVersion}}
	if def.Hooks == nil {
		return hp, nil
	}
	if rec == nil {
		rec = &Record{FormatVersion: RecordFormatVersion}
	}
	var applying []*Set
	if p.ShareLevel() == profile.ShareAll {
		// At all the hooks come from the default setup. Once the hook location is
		// the shared link again the record is simply dropped; while the profile
		// still has its own hook file, aprofile removes the entries it placed.
		if hookLocationLinked(def, configDir) || len(rec.Entries) == 0 {
			return hp, nil
		}
	} else {
		for _, name := range p.HookSets {
			if _, ok := lib[name]; !ok {
				hp.MissingSets = append(hp.MissingSets, name)
			}
		}
	}
	// Sets whose entries stay exactly as they are: named but missing, failed to
	// load, or with a body that does not fit the harness's hook form.
	missing := map[string]bool{}
	for _, m := range hp.MissingSets {
		missing[m] = true
	}
	for _, name := range SortedNames(lib) {
		s := lib[name]
		if s.LoadErr != nil {
			missing[name] = true
			continue
		}
		if p.ShareLevel() == profile.ShareAll || !Applies(s, p) {
			continue
		}
		// A set with a body of the wrong form is never applied (an every-profile
		// set is not validated when a profile is edited).
		if err := ValidateFor(s, def); err != nil {
			hp.InvalidSets = append(hp.InvalidSets, err.Error())
			missing[name] = true
			continue
		}
		applying = append(applying, s)
	}
	if p.ShareLevel() == profile.ShareAll {
		missing = map[string]bool{} // at all everything aprofile placed goes
	}
	for _, s := range applying {
		hp.MissingScripts = append(hp.MissingScripts, MissingScripts(s, def)...)
	}
	var err error
	switch def.Hooks.Form {
	case harness.HookFormEventGroups:
		err = planEventGroups(def, applying, missing, rec, configDir, opts, hp)
	case harness.HookFormFiles:
		err = planFiles(def, applying, missing, rec, configDir, opts, hp)
	}
	return hp, err
}

type desiredGroup struct {
	set   string
	event string
	raw   json.RawMessage
	fp    string
}

// planEventGroups plans the event-groups form: matcher groups at a key of a JSON
// file. The user's own groups come first, in their order, then the managed groups
// of the applying sets in set-name order (research R5).
func planEventGroups(def *harness.Definition, applying []*Set, missing map[string]bool, rec *Record, configDir string, opts PlanOptions, hp *HookPlan) error {
	file := def.Hooks.File()
	key := def.Hooks.Key()
	path := filepath.Join(configDir, filepath.FromSlash(file))
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		// Still linked to the default setup (a pending level change); leave it.
		hp.Record = rec
		return nil
	}
	doc := map[string]json.RawMessage{}
	exists := false
	if data, err := os.ReadFile(path); err == nil {
		exists = true
		if len(bytes.TrimSpace(data)) > 0 {
			if err := json.Unmarshal(data, &doc); err != nil {
				return fmt.Errorf("%s is not valid JSON: %w", path, err)
			}
		}
	}
	current := map[string][]json.RawMessage{}
	if raw, ok := doc[key]; ok && len(bytes.TrimSpace(raw)) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &current); err != nil {
			return fmt.Errorf("%s: %q is not an object of event groups: %w", path, key, err)
		}
	}

	var desired []desiredGroup
	for _, s := range applying {
		b := s.Bodies[def.Name]
		for _, ev := range b.EventNames() {
			for _, g := range b.Events[ev] {
				desired = append(desired, desiredGroup{set: s.Name, event: ev, raw: g, fp: Fingerprint(g)})
			}
		}
	}

	applyingSet := map[string]bool{}
	for _, s := range applying {
		applyingSet[s.Name] = true
	}
	// stillOurs reports whether a recorded entry's set is still the profile's: it
	// applies, or it is named but missing from the library (left unchanged).
	stillOurs := func(set string) bool { return applyingSet[set] || missing[set] }

	recordedByEvent := map[string][]RecordEntry{}
	for _, e := range rec.Entries {
		if e.Form == harness.HookFormEventGroups {
			recordedByEvent[e.Event] = append(recordedByEvent[e.Event], e)
		}
	}

	events := map[string]bool{}
	for ev := range current {
		events[ev] = true
	}
	for _, d := range desired {
		events[d.event] = true
	}
	for ev := range recordedByEvent {
		events[ev] = true
	}
	evNames := make([]string, 0, len(events))
	for ev := range events {
		evNames = append(evNames, ev)
	}
	sort.Strings(evNames)

	type managedGroup struct {
		set string
		raw json.RawMessage
		fp  string
		rec RecordEntry
	}
	newHooks := map[string][]json.RawMessage{}
	var newRec []RecordEntry
	var placedSets, removedSets []string
	reapplied := false
	var replacedNotes []string
	for _, ev := range evNames {
		// Claim recorded entries for the groups found unchanged, in file order.
		unclaimed := append([]RecordEntry(nil), recordedByEvent[ev]...)
		var present []managedGroup // recorded groups found unchanged, file order
		var user []json.RawMessage
		for _, g := range current[ev] {
			fp := Fingerprint(g)
			claimed := -1
			for i, e := range unclaimed {
				if e.Fingerprint == fp {
					claimed = i
					break
				}
			}
			if claimed < 0 {
				user = append(user, g)
				continue
			}
			present = append(present, managedGroup{set: unclaimed[claimed].Set, raw: g, fp: fp, rec: unclaimed[claimed]})
			unclaimed = append(unclaimed[:claimed], unclaimed[claimed+1:]...)
		}
		// Recorded entries not found unchanged have diverged, unless their set no
		// longer applies: then aprofile simply stops managing them.
		var diverged []RecordEntry
		for _, e := range unclaimed {
			if stillOurs(e.Set) {
				diverged = append(diverged, e)
			}
		}
		if opts.Reapply && len(diverged) > 0 {
			// Replace an edited copy only when exactly one of the user's groups has
			// the diverged entry's matcher; otherwise it cannot be told apart from
			// the user's own groups and stays diverged.
			var still []RecordEntry
			for _, e := range diverged {
				if missing[e.Set] || !hasDesired(desired, e.Set, ev) {
					// Nothing current to re-place for this set here: keep it diverged.
					still = append(still, e)
					hp.Divergences = append(hp.Divergences, Divergence{Set: e.Set, Entry: file + "#" + key + "." + ev,
						Reason: "the set has no current entry to re-place here; left as is"})
					continue
				}
				// The edited copy is a user group with the same matcher that still
				// runs one of the managed group's commands.
				var idx []int
				for i, g := range user {
					if matcherOf(g) == e.Matcher && sharesCommand(g, e.Commands) {
						idx = append(idx, i)
					}
				}
				switch len(idx) {
				case 0:
					reapplied = true // removed by hand: re-added below
				case 1:
					replacedNotes = append(replacedNotes, fmt.Sprintf("%s group with matcher %q", ev, e.Matcher))
					user = append(user[:idx[0]], user[idx[0]+1:]...)
					reapplied = true
				default:
					still = append(still, e)
					hp.Divergences = append(hp.Divergences, Divergence{Set: e.Set, Entry: file + "#" + key + "." + ev,
						Reason: fmt.Sprintf("%d groups with matcher %q could be the edited copy; not replaced (edit the file by hand)", len(idx), e.Matcher)})
				}
			}
			diverged = still
			for _, e := range diverged {
				newRec = append(newRec, e)
			}
		} else {
			for _, e := range diverged {
				hp.Divergences = append(hp.Divergences, Divergence{Set: e.Set, Entry: file + "#" + key + "." + ev,
					Reason: "edited or removed by hand; left as is (run: aprofile hook-sets reapply to re-place it)"})
				newRec = append(newRec, e)
			}
		}
		// A desired group that corresponds to a diverged entry (same set and
		// matcher) is not placed, even when the set's body changed since: the user
		// changed or removed that entry, and only reapply re-places it (FR-028).
		isDiverged := func(set string, raw json.RawMessage) bool {
			for _, e := range diverged {
				if e.Set == set && e.Matcher == matcherOf(raw) {
					return true
				}
			}
			return false
		}
		userHas := func(fp string) bool {
			for _, g := range user {
				if Fingerprint(g) == fp {
					return true
				}
			}
			return false
		}

		var managed []managedGroup
		for _, d := range desired {
			if d.event != ev || isDiverged(d.set, d.raw) {
				continue // never re-add an entry the user changed or removed
			}
			found := -1
			for i, m := range present {
				if m.set == d.set && m.fp == d.fp {
					found = i
					break
				}
			}
			if found >= 0 {
				present = append(present[:found], present[found+1:]...)
			} else if userHas(d.fp) {
				// An identical group the user or another tool added: it is theirs,
				// so it is neither duplicated nor taken over (FR-028).
				continue
			} else {
				placedSets = appendUnique(placedSets, d.set)
			}
			newRec = append(newRec, RecordEntry{Set: d.set, Form: harness.HookFormEventGroups, Event: ev, Fingerprint: d.fp, Matcher: matcherOf(d.raw), Commands: commandsOf(d.raw)})
			managed = append(managed, managedGroup{set: d.set, raw: d.raw, fp: d.fp})
		}
		// Entries of sets missing from the library stay unchanged, in file order.
		for _, m := range present {
			if missing[m.set] {
				managed = append(managed, m)
				newRec = append(newRec, m.rec)
				continue
			}
			removedSets = appendUnique(removedSets, m.set)
		}
		// Order: sets by name, file order within a set (stable).
		sort.SliceStable(managed, func(i, j int) bool { return managed[i].set < managed[j].set })
		groups := append([]json.RawMessage(nil), user...)
		for _, m := range managed {
			groups = append(groups, m.raw)
		}
		if len(groups) > 0 {
			newHooks[ev] = groups
		}
	}
	sortRecord(newRec)
	hp.Record = &Record{FormatVersion: RecordFormatVersion, Entries: append(otherForm(rec, harness.HookFormEventGroups), newRec...)}

	// Nothing placed, removed or re-placed: keep the file as it is, including the
	// order in which the user arranged their groups.
	if canonicalGroups(newHooks) == canonicalGroups(current) || (len(placedSets) == 0 && len(removedSets) == 0 && !reapplied) {
		return nil
	}
	if !exists && len(newHooks) == 0 {
		return nil
	}
	raw, err := layer.EncodeJSONCompact(newHooks)
	if err != nil {
		return err
	}
	doc[key] = raw
	data, err := layer.EncodeJSON(doc)
	if err != nil {
		return err
	}
	kind := layer.StepPlaceHook
	note := "managed hook entries of: " + strings.Join(placedSets, ", ")
	if len(placedSets) == 0 {
		kind = layer.StepRemoveHook
		note = "removed managed hook entries of: " + strings.Join(removedSets, ", ")
	} else if len(removedSets) > 0 {
		note += "; removed: " + strings.Join(removedSets, ", ")
	}
	note += "; entries aprofile did not place are kept"
	step := layer.Step{Kind: kind, Path: path, Entry: file + "#" + key, Data: data, Note: note}
	if reapplied {
		step.Confirm = true
		step.Note = "re-place diverged managed entries (backed up first); " + note
		if len(replacedNotes) > 0 {
			step.Note += "; replaces the edited copy: " + strings.Join(replacedNotes, ", ")
		}
	}
	hp.Steps = append(hp.Steps, step)
	return nil
}

// planFiles plans the files form: each set file becomes a link in the hook
// directory to the file in the library (FR-029). Real files and links that point
// elsewhere are never touched.
func planFiles(def *harness.Definition, applying []*Set, missing map[string]bool, rec *Record, configDir string, opts PlanOptions, hp *HookPlan) error {
	dir := def.Hooks.File()
	dirPath := filepath.Join(configDir, filepath.FromSlash(dir))
	if fi, err := os.Lstat(dirPath); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		hp.Record = rec
		return nil
	}
	type want struct{ set, rel, target string }
	var desired []want
	desiredByPath := map[string]want{}
	for _, s := range applying {
		for _, f := range s.Bodies[def.Name].Files {
			d := want{s.Name, dir + "/" + filepath.Base(f), filepath.Join(s.FilesDir, filepath.FromSlash(f))}
			if other, dup := desiredByPath[d.rel]; dup {
				hp.Divergences = append(hp.Divergences, Divergence{Set: d.set, Entry: d.rel,
					Reason: fmt.Sprintf("hook set %q also places a file named %s; this one was left out", other.set, filepath.Base(f))})
				continue
			}
			desiredByPath[d.rel] = d
			desired = append(desired, d)
		}
	}
	applyingSet := map[string]bool{}
	for _, s := range applying {
		applyingSet[s.Name] = true
	}
	var newRec []RecordEntry
	handled := map[string]bool{}
	for _, e := range rec.Entries {
		if e.Form != harness.HookFormFiles {
			continue
		}
		path := filepath.Join(configDir, filepath.FromSlash(e.Path))
		target, isLink := linkTarget(path)
		ours := isLink && target == e.Target
		d, stillWanted := desiredByPath[e.Path]
		switch {
		case ours && stillWanted && d.target == e.Target:
			newRec = append(newRec, e)
			handled[e.Path] = true
		case ours && missing[e.Set]:
			newRec = append(newRec, e) // the set was deleted from the library: unchanged
			handled[e.Path] = true
		case ours && stillWanted:
			// Our link, but the set's file moved: relink it in the same plan.
			hp.Steps = append(hp.Steps, layer.Step{Kind: layer.StepPlaceHook, Path: path, Entry: e.Path, Source: d.target,
				Note: "hook set " + d.set + " (file moved in the library)"})
			newRec = append(newRec, RecordEntry{Set: d.set, Form: harness.HookFormFiles, Path: d.rel, Target: d.target})
			handled[e.Path] = true
		case ours:
			hp.Steps = append(hp.Steps, layer.Step{Kind: layer.StepRemoveHook, Path: path, Entry: e.Path,
				Note: "hook set " + e.Set + " no longer applies"})
		case !applyingSet[e.Set] && !missing[e.Set]:
			// Diverged, but the set no longer applies: stop managing it silently.
		case missing[e.Set]:
			// The set is missing, failed to load or does not fit: its entries stay
			// as they are, and the divergence stays recorded.
			hp.Divergences = append(hp.Divergences, Divergence{Set: e.Set, Entry: e.Path, Reason: "the set is unavailable; left as is"})
			newRec = append(newRec, e)
			handled[e.Path] = true
		case opts.Reapply:
			// Placed again below, replacing what is there after a backup.
		default:
			reason := "removed by hand; not re-added"
			if _, err := os.Lstat(path); err == nil {
				reason = "replaced by a real file or another link; left as is"
			}
			hp.Divergences = append(hp.Divergences, Divergence{Set: e.Set, Entry: e.Path, Reason: reason + " (run: aprofile hook-sets reapply to re-place it)"})
			newRec = append(newRec, e)
			handled[e.Path] = true
		}
	}
	for _, d := range desired {
		if handled[d.rel] {
			continue
		}
		handled[d.rel] = true
		path := filepath.Join(configDir, filepath.FromSlash(d.rel))
		target, isLink := linkTarget(path)
		_, exists := os.Lstat(path)
		switch {
		case isLink && target == d.target:
			newRec = append(newRec, RecordEntry{Set: d.set, Form: harness.HookFormFiles, Path: d.rel, Target: d.target})
		case exists != nil:
			hp.Steps = append(hp.Steps, layer.Step{Kind: layer.StepPlaceHook, Path: path, Entry: d.rel, Source: d.target, Note: "hook set " + d.set})
			newRec = append(newRec, RecordEntry{Set: d.set, Form: harness.HookFormFiles, Path: d.rel, Target: d.target})
		case opts.Reapply && wasRecorded(rec, d.rel):
			hp.Steps = append(hp.Steps, layer.Step{Kind: layer.StepReplaceWithLink, Path: path, Entry: d.rel, Source: d.target, Confirm: true,
				Note: "re-place hook set " + d.set + " (the file there is backed up first)"})
			newRec = append(newRec, RecordEntry{Set: d.set, Form: harness.HookFormFiles, Path: d.rel, Target: d.target})
		default:
			hp.Divergences = append(hp.Divergences, Divergence{Set: d.set, Entry: d.rel, Reason: "a file aprofile did not place exists there; the set's file was not linked"})
		}
	}
	sortRecord(newRec)
	hp.Record = &Record{FormatVersion: RecordFormatVersion, Entries: append(otherForm(rec, harness.HookFormFiles), newRec...)}
	return nil
}

// sortRecord orders record entries deterministically so an unchanged sync writes
// the same bytes.
func sortRecord(entries []RecordEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Event != b.Event {
			return a.Event < b.Event
		}
		if a.Set != b.Set {
			return a.Set < b.Set
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Fingerprint < b.Fingerprint
	})
}

func hasDesired(desired []desiredGroup, set, event string) bool {
	for _, d := range desired {
		if d.set == set && d.event == event {
			return true
		}
	}
	return false
}

// sharesCommand reports whether a group runs one of the given commands' programs,
// which ties an edited copy to its managed group. The program is the first word
// that is not an interpreter or launcher and not a flag, so "bash ~/a.sh" and
// "bash ~/b.sh" are different programs.
func sharesCommand(group json.RawMessage, commands []string) bool {
	want := map[string]bool{}
	for _, c := range commands {
		if prog := programOf(c); prog != "" {
			want[prog] = true
		}
	}
	if len(want) == 0 {
		return false
	}
	for _, c := range commandsOf(group) {
		if want[programOf(c)] {
			return true
		}
	}
	return false
}

var launchers = map[string]bool{
	"bash": true, "sh": true, "zsh": true, "fish": true, "env": true, "node": true, "deno": true, "bun": true,
	"python": true, "python3": true, "ruby": true, "perl": true, "npx": true, "bunx": true, "uvx": true, "uv": true, "run": true, "exec": true,
}

func programOf(command string) string {
	for _, f := range strings.Fields(command) {
		base := filepath.Base(f)
		if strings.HasPrefix(f, "-") || strings.Contains(f, "=") || launchers[base] {
			continue
		}
		return f
	}
	return ""
}

func wasRecorded(rec *Record, rel string) bool {
	for _, e := range rec.Entries {
		if e.Form == harness.HookFormFiles && e.Path == rel {
			return true
		}
	}
	return false
}

func linkTarget(path string) (string, bool) {
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return "", false
	}
	t, _ := os.Readlink(path)
	return t, true
}

// otherForm keeps record entries of the other form (a definition change between
// forms leaves them for doctor to report rather than dropping them silently).
func otherForm(rec *Record, form string) []RecordEntry {
	var out []RecordEntry
	for _, e := range rec.Entries {
		if e.Form != form {
			out = append(out, e)
		}
	}
	return out
}

func canonicalGroups(m map[string][]json.RawMessage) string {
	norm := map[string][]any{}
	for ev, groups := range m {
		if len(groups) == 0 {
			continue
		}
		for _, g := range groups {
			var v any
			_ = json.Unmarshal(g, &v)
			norm[ev] = append(norm[ev], v)
		}
	}
	b, _ := json.Marshal(norm)
	return string(b)
}

func appendUnique(list []string, s string) []string {
	for _, v := range list {
		if v == s {
			return list
		}
	}
	return append(list, s)
}

// hookLocationLinked reports whether the hook location (or the file holding it) is
// a link, as it is at share level all.
func hookLocationLinked(def *harness.Definition, configDir string) bool {
	_, isLink := linkTarget(filepath.Join(configDir, filepath.FromSlash(def.Hooks.File())))
	return isLink
}

// SortedNames returns the library's set names, sorted.
func SortedNames(lib map[string]*Set) []string {
	names := make([]string, 0, len(lib))
	for n := range lib {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// MissingScripts returns the files a set body references that do not exist: the
// library files of the files form, and absolute (or ~) command paths of the
// event-groups form (spec Edge Cases, FR-020).
func MissingScripts(s *Set, def *harness.Definition) []string {
	b, ok := s.Bodies[def.Name]
	if !ok {
		return nil
	}
	var out []string
	for _, f := range b.Files {
		p := filepath.Join(s.FilesDir, filepath.FromSlash(f))
		if _, err := os.Stat(p); err != nil {
			out = append(out, fmt.Sprintf("hook set %q: file %s does not exist", s.Name, p))
		}
	}
	for _, ev := range b.EventNames() {
		for _, g := range b.Events[ev] {
			var grp struct {
				Hooks []struct {
					Command string `json:"command"`
				} `json:"hooks"`
			}
			if json.Unmarshal(g, &grp) != nil {
				continue
			}
			for _, h := range grp.Hooks {
				// The script is the first path-like word after any interpreter or
				// launcher and its flags, so "bash ~/x.sh" checks ~/x.sh.
				script := ""
				for _, f := range strings.Fields(h.Command) {
					if strings.HasPrefix(f, "-") || strings.Contains(f, "=") || launchers[filepath.Base(f)] {
						continue
					}
					script = f
					break
				}
				cmd := harness.ExpandHome(script)
				if script == "" || !filepath.IsAbs(cmd) {
					continue
				}
				if _, err := os.Stat(cmd); err != nil {
					out = append(out, fmt.Sprintf("hook set %q: script %s (%s) does not exist", s.Name, script, ev))
				}
			}
		}
	}
	return out
}

// PreviewAfter plans the profile's hook steps as they will be once the given
// config dir steps (a share level change) are applied, without touching the
// profile: it copies the hook location into a scratch dir, applies the config dir
// steps that touch it there, and runs PlanHooks against the copy. edit --share
// --dry-run prints these steps, and the applied change runs SyncProfile on the
// real dir afterwards, so both come from the same planner (FR-016, SC-006).
// Paths of the returned steps point into configDir.
func PreviewAfter(def *harness.Definition, p *profile.Profile, lib map[string]*Set, rec *Record, configDir string, levelSteps []layer.Step) (*HookPlan, error) {
	if def.Hooks == nil {
		return &HookPlan{Record: &Record{FormatVersion: RecordFormatVersion}}, nil
	}
	scratch, err := os.MkdirTemp("", "aprofile-hook-preview-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(scratch)

	rel := filepath.FromSlash(def.Hooks.File())
	real := filepath.Join(configDir, rel)
	if _, err := os.Lstat(real); err == nil {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(scratch, rel)), 0o755); err != nil {
			return nil, err
		}
		if err := layer.CopyTree(real, filepath.Join(scratch, rel)); err != nil {
			return nil, fmt.Errorf("preview hook location: %w", err)
		}
	}
	var sim []layer.Step
	for _, s := range levelSteps {
		if !touches(s.Path, real) {
			continue
		}
		r, err := filepath.Rel(configDir, s.Path)
		if err != nil {
			return nil, err
		}
		s.Path = filepath.Join(scratch, r)
		sim = append(sim, s)
	}
	if _, _, err := layer.Apply(sim, layer.ApplyOptions{Yes: true, Backup: func([]string) (string, error) { return "(preview)", nil }}); err != nil {
		return nil, fmt.Errorf("preview hook location: %w", err)
	}
	hp, err := PlanHooks(def, p, lib, rec, scratch, PlanOptions{})
	if err != nil {
		return nil, err
	}
	for i := range hp.Steps {
		if r, err := filepath.Rel(scratch, hp.Steps[i].Path); err == nil {
			hp.Steps[i].Path = filepath.Join(configDir, r)
		}
	}
	return hp, nil
}

// touches reports whether a step on path affects the entry at target: the same
// entry, an entry inside it, or a directory that contains it.
func touches(path, target string) bool {
	sep := string(filepath.Separator)
	return path == target || strings.HasPrefix(path, target+sep) || strings.HasPrefix(target, path+sep)
}

// SyncProfile places the managed hook entries of one profile and saves its record
// (FR-028): it loads the record, plans, applies the plan in safe mode and writes
// the record, unless dryRun. sync and edit --share both use it, so the rules for
// the record (including at share level all) live in PlanHooks only.
func SyncProfile(def *harness.Definition, p *profile.Profile, lib map[string]*Set, recPath, configDir string, dryRun bool) (*HookPlan, []layer.Step, error) {
	rec, err := LoadRecord(recPath)
	if err != nil {
		return nil, nil, err
	}
	hp, err := PlanHooks(def, p, lib, rec, configDir, PlanOptions{})
	if err != nil {
		return nil, nil, err
	}
	applied, _, err := layer.Apply(hp.Steps, layer.ApplyOptions{DryRun: dryRun, Safe: true})
	if err != nil {
		return hp, applied, err
	}
	if !dryRun {
		if _, err := hp.Record.Save(recPath); err != nil {
			return hp, applied, err
		}
	}
	return hp, applied, nil
}
