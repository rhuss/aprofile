package layer

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/profile"
)

// Treatment values shown by `show` (data-model.md, Entry treatment).
const (
	TreatmentPrivate      = "private"
	TreatmentLinked       = "linked"
	TreatmentOwnCopy      = "own-copy"
	TreatmentManaged      = "managed-by-hook-set"
	TreatmentUnclassified = "unclassified"
	TreatmentHomeEscape   = "home-escape"
)

// Status values of an entry treatment.
const (
	StatusOK                 = "ok"
	StatusMissingLinkTarget  = "missing-link-target"
	StatusNotSynced          = "not-synced"
	StatusLinkReplacedByFile = "link-replaced-by-file"
	StatusLeakingLink        = "leaking-link"
	StatusDiffersFromDefault = "differs-from-default"
	// StatusKeptFromUpgrade: a profile from before share levels keeps its own
	// entry where share level all would link one (US6, FR-033).
	StatusKeptFromUpgrade = "kept-from-upgrade"
	// StatusWrongHistoryLink: a member's history entry links somewhere other than
	// its group store (spec 003, FR-021).
	StatusWrongHistoryLink = "wrong-history-link"
)

// EntryTreatment is the outcome for one entry of one profile (FR-015): its
// category, treatment, a plain-language reason and its current status.
type EntryTreatment struct {
	Entry     string `json:"entry"`
	Category  string `json:"category"`
	Treatment string `json:"treatment"`
	Reason    string `json:"reason"`
	Status    string `json:"status"`
}

// Treatments lists every entry of the profile config dir with its treatment and
// reason, plus the links the level expects that do not exist yet, the hook location
// and the definition's $HOME escapes (SC-005). It derives the treatment from the
// same definition data and policy the planner uses, so show, dry runs and sync agree.
func Treatments(def *harness.Definition, p *profile.Profile, home, configDir string) ([]EntryTreatment, error) {
	return TreatmentsWith(def, p, home, configDir, nil)
}

// TreatmentsWith is Treatments for a profile whose history group store is hs (nil
// for an isolated profile, which gives exactly Treatments' result). A member's
// history entries are reported as linked to the store with the reason "history
// group <g>", including the ones that do not exist yet (spec 003, FR-020).
func TreatmentsWith(def *harness.Definition, p *profile.Profile, home, configDir string, hs HistoryStore) ([]EntryTreatment, error) {
	return TreatmentsWithPlaced(def, p, home, configDir, hs, nil)
}

// PlacedKeys lists, per profile-relative file, the launch entries aprofile placed
// there (feature 004): "env.<NAME>" for an entry of the file's env object, or a
// top-level key such as "apiKeyHelper". The caller fills it from the launch record;
// layer never reads that record itself.
type PlacedKeys map[string][]string

// TreatmentsWithPlaced is TreatmentsWith for a profile whose settings files hold the
// placed launch entries in placed: they are ignored when comparing the profile's own
// settings with the default setup's, so a content profile does not read "differs
// from the default setup" because of them.
func TreatmentsWithPlaced(def *harness.Definition, p *profile.Profile, home, configDir string, hs HistoryStore, placed PlacedKeys) ([]EntryTreatment, error) {
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	level := string(p.ShareLevel())
	var out []EntryTreatment
	seen := map[string]bool{}

	found, err := walk(def, configDir, "")
	if err != nil {
		return nil, err
	}
	for _, we := range found {
		seen[we.Rel] = true
		path := filepath.Join(configDir, filepath.FromSlash(we.Rel))
		if we.Result.Unclassified {
			out = append(out, EntryTreatment{Entry: we.Rel, Category: "unclassified", Treatment: TreatmentUnclassified,
				Reason: "not classified by the " + def.Name + " definition: left in place, never linked; classify it with: aprofile harness classify " + def.Name + " " + we.Rel + " --as <category>",
				Status: StatusOK})
			continue
		}
		if hs != nil && we.Result.Rule != nil && we.Result.Rule.History {
			out = append(out, historyTreatment(we.Result.Rule, we.Rel, path, hs))
			continue
		}
		et := treatmentFor(def, level, we.Result.Rule, we.Rel, path, def.DefaultPathUnder(home, we.Rel), placed[we.Rel])
		if p.ShareImplicit && et.Treatment == TreatmentLinked && (et.Status == StatusLinkReplacedByFile || et.Status == StatusNotSynced) {
			if cur := inspect(path); cur.exists && !cur.link {
				// The planner keeps it too (PlanOptions.Upgrade).
				et.Treatment = TreatmentOwnCopy
				et.Status = StatusKeptFromUpgrade
				et.Reason = categoryNote(we.Result.Rule.Layer) + ": the profile's own from before share levels, kept; to share the default setup's instead, run: aprofile sync --relink"
			}
		}
		out = append(out, et)
	}
	// Links the level expects that are not there yet.
	rules := append([]harness.EntryRule(nil), def.Classification...)
	if hr := def.HookLocationRule(); hr != nil {
		rules = append(rules, *hr)
	}
	for i := range rules {
		r := &rules[i]
		if r.IsGlob() || seen[r.Path()] || !r.Layer.Borrowable() || def.Treatment(r.Layer, level) != harness.TreatLink {
			continue
		}
		et := treatmentFor(def, level, r, r.Path(), filepath.Join(configDir, filepath.FromSlash(r.Path())), def.DefaultPathUnder(home, r.Path()), placed[r.Path()])
		out = append(out, et)
	}
	// History entries of a member that do not exist yet.
	if hs != nil {
		for _, r := range def.HistoryRules() {
			if seen[r.Path()] {
				continue
			}
			seen[r.Path()] = true
			rr := r
			out = append(out, historyTreatment(&rr, r.Path(), filepath.Join(configDir, filepath.FromSlash(r.Path())), hs))
		}
	}
	// A hook location that is a key of a classified file.
	if hf := def.HookFileRule(); hf != nil {
		et := EntryTreatment{Entry: def.Hooks.Location, Category: string(harness.LayerHookLocation), Status: StatusOK}
		if def.Treatment(harness.LayerHookLocation, level) == harness.TreatLink && def.Treatment(harness.LayerSettings, level) == harness.TreatLink {
			et.Treatment = TreatmentLinked
			et.Reason = "hooks: the default setup's hooks, inside the linked " + hf.Path()
		} else {
			et.Treatment = TreatmentManaged
			et.Reason = "hooks: hook sets placed by aprofile into the profile's own " + hf.Path() + "; your own entries are never touched"
		}
		out = append(out, et)
	}
	for _, he := range def.HomeEscapes {
		out = append(out, EntryTreatment{Entry: he.Path, Category: TreatmentHomeEscape, Treatment: TreatmentHomeEscape,
			Reason: he.What + " read from $HOME: shared with every profile; no share level can separate this", Status: StatusOK})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Entry < out[j].Entry })
	return out, nil
}

// historyTreatment is the treatment of a member's history entry: linked to the
// group store, with a drift status when it is missing, a real entry, or a link to
// another place.
func historyTreatment(rule *harness.EntryRule, rel, path string, hs HistoryStore) EntryTreatment {
	cur := inspect(path)
	et := EntryTreatment{Entry: rel, Category: string(rule.Layer), Treatment: TreatmentLinked,
		Reason: "history group " + hs.Name(), Status: StatusOK}
	switch {
	case !cur.exists:
		et.Status = StatusNotSynced
	case !cur.link:
		et.Status = StatusLinkReplacedByFile
	case !samePath(cur.target, hs.EntryPath(rel)):
		et.Status = StatusWrongHistoryLink
	}
	return et
}

func treatmentFor(def *harness.Definition, level string, rule *harness.EntryRule, rel, path, src string, placed []string) EntryTreatment {
	cur := inspect(path)
	et := EntryTreatment{Entry: rel, Category: string(rule.Layer), Status: StatusOK}
	switch rule.Layer {
	case harness.LayerCredentials, harness.LayerState, harness.LayerOwned, harness.LayerMixed:
		et.Treatment = TreatmentPrivate
		switch rule.Layer {
		case harness.LayerCredentials:
			et.Reason = "credentials: always private"
		case harness.LayerState:
			et.Reason = "runtime state: always private"
			if rule.History {
				et.Reason = "history: private to this profile (it is in no history group)"
			}
		case harness.LayerOwned:
			et.Reason = "owned (MCP servers, model): private at every level, travels with copy"
		case harness.LayerMixed:
			et.Treatment = TreatmentOwnCopy
			et.Reason = "mixed file: the profile's own; owned keys private, other keys copied once (never linked)"
			if level == harness.LevelNone {
				et.Reason = "mixed file: the profile's own, started minimal at share level none"
			}
		}
		if rule.CompanionOf != "" {
			et.Reason += " (companion of " + rule.CompanionOf + ")"
		}
		if cur.link {
			et.Status = StatusLeakingLink
		}
		return et
	}
	treat := def.Treatment(rule.Layer, level)
	srcExists := PathExists(src)
	cat := categoryNote(rule.Layer)
	switch treat {
	case harness.TreatLink:
		et.Treatment = TreatmentLinked
		et.Reason = cat + ": linked to the default setup at share level " + level
		switch rule.Layer {
		case harness.LayerSettings:
			et.Reason = "settings: one live file shared with the default setup"
		case harness.LayerPluginStore:
			et.Reason = "plugin store: linked; installed plugins are shared"
			if level != harness.LevelAll {
				et.Reason += ", which plugins are enabled stays per profile"
			}
		case harness.LayerHookLocation:
			et.Reason = "hooks: the default setup's hook file is linked"
		}
		switch {
		case !cur.exists && !srcExists:
			// Nothing to link yet (lazy): sync links it once the default setup has it.
			et.Status = StatusMissingLinkTarget
			et.Reason = cat + ": the default setup has none yet; linked once it does"
		case !cur.exists, cur.link && cur.target != src:
			et.Status = StatusNotSynced
		case !cur.link && cur.dir && dirEmpty(path):
			et.Status = StatusNotSynced
		case !cur.link && !srcExists:
			// The default setup has no such entry, so there is nothing to link to;
			// the profile's own entry is kept (the planner does the same).
			et.Treatment = TreatmentOwnCopy
			et.Reason = cat + ": the profile's own; the default setup has none to link to"
		case !cur.link:
			et.Status = StatusLinkReplacedByFile
		}
	case harness.TreatCopyOnce, harness.TreatMinimal, harness.TreatOwn:
		et.Treatment = TreatmentOwnCopy
		switch treat {
		case harness.TreatCopyOnce:
			et.Reason = cat + ": the profile's own copy, taken from the default setup once"
			if !srcExists {
				// The planner started it minimal: there was nothing to copy.
				et.Reason = cat + ": the profile's own, started minimal; the default setup has none to copy"
			}
		case harness.TreatMinimal:
			et.Reason = cat + ": the profile's own, started minimal"
		default:
			et.Reason = cat + ": the profile's own at share level " + level
		}
		if cur.link {
			et.Status = StatusNotSynced
		} else if treat == harness.TreatCopyOnce && differsFromDefault(def, rel, path, src, placed) {
			et.Status = StatusDiffersFromDefault
		}
	case harness.TreatManaged:
		et.Treatment = TreatmentManaged
		et.Reason = "hooks: hook sets placed by aprofile; your own entries are never touched"
		if cur.link {
			et.Status = StatusNotSynced
		}
	}
	return et
}

// differsFromDefault reports whether a profile's own settings file differs from the
// default setup's, ignoring the hook location key (which hook sets manage) and the
// launch entries aprofile placed (feature 004).
func differsFromDefault(def *harness.Definition, rel, path, src string, placed []string) bool {
	pd, err1 := os.ReadFile(path)
	dd, err2 := os.ReadFile(src)
	if err1 != nil || err2 != nil {
		return false
	}
	if key := def.HookKeyIn(rel); key != "" {
		var e1, e2 error
		pd, _, e1 = withoutKey(pd, key, path)
		dd, _, e2 = withoutKey(dd, key, src)
		if e1 != nil || e2 != nil {
			return true
		}
	}
	if IsJSONExt(filepath.Ext(path)) {
		pa, e1 := decodeAnyObject(pd)
		da, e2 := decodeAnyObject(dd)
		if e1 == nil && e2 == nil {
			dropPlaced(pa, da, placed)
			return canonical(pa) != canonical(da)
		}
	}
	return !bytes.Equal(pd, dd)
}

// dropPlaced removes the placed launch entries from both documents before they are
// compared; an env object that only held placed entries in the profile, and that
// the default setup does not have, goes too.
func dropPlaced(profileDoc, defaultDoc map[string]any, placed []string) {
	for _, loc := range placed {
		if obj, name, ok := strings.Cut(loc, "."); ok {
			for _, doc := range []map[string]any{profileDoc, defaultDoc} {
				if m, isMap := doc[obj].(map[string]any); isMap {
					delete(m, name)
				}
			}
			if m, isMap := profileDoc[obj].(map[string]any); isMap && len(m) == 0 {
				if _, has := defaultDoc[obj]; !has {
					delete(profileDoc, obj)
				}
			}
			continue
		}
		delete(profileDoc, loc)
		delete(defaultDoc, loc)
	}
}

// SettingsDiffer reports the settings entries of a content profile whose own copy
// differs from the default setup (FR-015, FR-020).
func SettingsDiffer(entries []EntryTreatment) []string {
	var out []string
	for _, e := range entries {
		if e.Status == StatusDiffersFromDefault {
			out = append(out, e.Entry)
		}
	}
	return out
}
