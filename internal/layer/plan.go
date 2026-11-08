package layer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/profile"
)

// StepKind names one kind of config dir change (data-model.md, Change plan).
type StepKind string

const (
	StepLink            StepKind = "link"              // symlink to the default setup
	StepUnlink          StepKind = "unlink"            // remove a symlink (never its target)
	StepMkdir           StepKind = "mkdir"             // create an empty private directory
	StepCopyOnce        StepKind = "copy-once"         // copy settings from the default setup once
	StepCreate          StepKind = "create"            // create a minimal settings entry
	StepSeedMixed       StepKind = "seed-mixed"        // create a mixed file (seeded or minimal)
	StepReplaceWithLink StepKind = "replace-with-link" // replace profile data with a link (confirm)
	StepFixLink         StepKind = "fix-link"          // replace a leaking link with an empty entry
	StepMergeSettings   StepKind = "merge-settings"    // settings pull, merge (confirm)
	StepReplaceSettings StepKind = "replace-settings"  // settings pull --replace (confirm)
	StepPlaceHook       StepKind = "place-hook"        // place managed hook entries
	StepRemoveHook      StepKind = "remove-hook"       // remove managed hook entries
	StepReport          StepKind = "report"            // a condition reported, nothing changed
	// History groups (spec 003, data-model.md "Change plan steps").
	StepMoveToStore    StepKind = "move-to-store"    // move one file into the group store, never overwriting
	StepSetAside       StepKind = "set-aside"        // move a file or entry into the set-aside history
	StepLinkHistory    StepKind = "link-history"     // link a history entry to its store path
	StepRelinkHistory  StepKind = "relink-history"   // replace a history link pointing elsewhere
	StepRemoveEmptyDir StepKind = "remove-empty-dir" // remove a directory tree that holds no files any more
)

// HistoryStore is the store of the profile's history group as the planner sees it
// (spec 003, research R7). It is declared here so layer does not import the
// history package; history.Store satisfies it.
type HistoryStore interface {
	// EntryPath returns the store path of a profile-relative history entry.
	EntryPath(rel string) string
	// IsHome reports whether the store is the default setup (the home group).
	IsHome() bool
	// Name returns the group name.
	Name() string
}

// SetAsideItem is one entry of a set-aside manifest: the profile-relative path of
// an item that was set aside, and why (exists-in-store or not-mergeable).
type SetAsideItem struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Set-aside reasons.
const (
	ReasonExistsInStore = "exists-in-store"
	ReasonNotMergeable  = "not-mergeable"
)

// Step is one entry of a change plan. Path is absolute; Entry is the same path
// relative to the profile config dir ("" for the config dir itself). Data, when
// set, is the exact content the step writes, computed at plan time so a dry run and
// the applied change cannot differ and an unreadable input fails before anything is
// written.
type Step struct {
	Kind    StepKind `json:"kind"`
	Entry   string   `json:"path"`
	Path    string   `json:"-"`
	Source  string   `json:"source,omitempty"`
	Backup  string   `json:"backup,omitempty"`
	Note    string   `json:"note,omitempty"`
	Confirm bool     `json:"confirm"`
	IsDir   bool     `json:"-"`
	Data    []byte   `json:"-"`
	// Mode is the permission of a file the step creates (0 means 0o644); an
	// existing file keeps its own mode.
	Mode os.FileMode `json:"-"`
	// Reason is why a set-aside step sets the item aside (exists-in-store or
	// not-mergeable); it is recorded in the set-aside manifest.
	Reason string `json:"reason,omitempty"`
	// Dest is where a move-to-store or set-aside step moves Path to.
	Dest string `json:"dest,omitempty"`
	// Fallback is where a move-to-store step sets the file aside instead when the
	// store path appeared after planning (it is never overwritten).
	Fallback string `json:"-"`
}

// Mutating reports whether the step changes the filesystem when applied.
func (s Step) Mutating() bool { return s.Kind != StepReport }

// String renders a step for plans and dry runs: "<kind> <entry> (<note>)".
func (s Step) String() string {
	entry := s.Entry
	if entry == "" {
		entry = "."
	}
	out := string(s.Kind) + " " + entry
	if s.Source != "" && (s.Kind == StepLink || s.Kind == StepReplaceWithLink || s.Kind == StepLinkHistory || s.Kind == StepRelinkHistory) {
		out += " -> " + s.Source
	}
	if s.Dest != "" && (s.Kind == StepMoveToStore || s.Kind == StepSetAside) {
		out += " -> " + s.Dest
	}
	if s.Note != "" {
		out += " (" + s.Note + ")"
	}
	return out
}

// PlanOptions adjusts a plan.
type PlanOptions struct {
	// FixLinks replaces a leaking credentials/state/owned link with an empty
	// private entry (FR-034). Without it the link is reported.
	FixLinks bool
	// Relink turns a real entry where a link is expected into a confirmed
	// replace-with-link step (sync --relink, FR-021). Without it, content and plugin
	// store entries are kept and reported.
	Relink bool
	// Upgrade marks a profile from before share levels (no stored level, FR-033).
	// A real entry it already has where share level all would link one is kept
	// and reported, never replaced: slice one gave such profiles their own skills
	// (and pi its own agent/settings.json), and US6 keeps that behavior until the
	// user asks for the shared entry with sync --relink.
	Upgrade bool
	// HistoryStore is the store of the profile's history group (spec 003); nil for
	// an isolated profile, whose plan is unchanged by history flags (SC-002). With a
	// store, a history entry that links to its store path is correct, a link
	// elsewhere is relinked, a missing entry is linked, and a real entry is reported
	// (or, with Relink, replaced by the link after a backup and confirmation).
	HistoryStore HistoryStore
	// ExplicitJoin is set by add, edit and copy (never sync): a missing history
	// directory of the home store may then be created empty in the default
	// location (FR-016). Without it such an entry is reported.
	ExplicitJoin bool
	// ProfileID ("<harness>/<name>") names the profile in report notes.
	ProfileID string
	// SkipHistory leaves every history entry exactly as it is (no step at all): used
	// for a profile assigned to a history group that does not exist (FR-022c).
	SkipHistory bool
}

// Plan computes the change plan that brings configDir to the given share level
// (plan.md Interfaces). home is the directory ~ expands to in the definition's
// default locations ("" means $HOME). The plan depends only on the definition, the
// level and what currently exists, so sync, edit --share and dry runs share it
// (SC-006). It writes nothing.
func Plan(def *harness.Definition, level profile.Share, home, configDir string) ([]Step, error) {
	return PlanWith(def, level, home, configDir, PlanOptions{})
}

// PlanWith is Plan with options.
func PlanWith(def *harness.Definition, level profile.Share, home, configDir string, opts PlanOptions) ([]Step, error) {
	if !level.Valid() {
		return nil, fmt.Errorf("share level %q must be all, content or none", level)
	}
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	pl := &planner{def: def, level: string(level), home: home, configDir: configDir, opts: opts}
	if !DirExists(configDir) {
		pl.add(Step{Kind: StepMkdir, Path: configDir, IsDir: true, Note: "profile config dir"})
	}
	for i := range def.Classification {
		rule := &def.Classification[i]
		if rule.IsGlob() {
			continue
		}
		if err := pl.planRule(rule); err != nil {
			return nil, err
		}
	}
	if hr := def.HookLocationRule(); hr != nil {
		if err := pl.planRule(hr); err != nil {
			return nil, err
		}
	}
	// A user definition may restore a glob rule with a borrowable layer (a
	// catch-all). Built-ins cannot (loader). Default-setup entries it matches are
	// planned like named entries; doctor reports the rule (spec Edge Cases).
	if err := pl.planBorrowableGlobs(); err != nil {
		return nil, err
	}
	// Existing entries: glob-classified private entries that are links leak
	// (slice one's catch-all linked Codex databases); unclassified entries are
	// reported and left in place (FR-001).
	found, err := walk(def, configDir, "")
	if err != nil {
		return nil, err
	}
	for _, we := range found {
		switch {
		case we.Result.Unclassified:
			note := "unclassified: left in place, never linked or copied; classify it with: aprofile harness classify " + def.Name + " " + we.Rel + " --as <category>"
			if we.Link {
				note = "unclassified link -> " + we.Target + ": left in place; " + note[len("unclassified: left in place, "):]
			}
			pl.add(Step{Kind: StepReport, Path: filepath.Join(configDir, filepath.FromSlash(we.Rel)), Entry: we.Rel, Note: note})
		case we.Link && we.Result.Rule != nil && we.Result.Rule.IsGlob() && private(we.Result.Layer):
			pl.leakingLink(we.Result.Rule, we.Rel, we.Target, false)
		}
	}
	return pl.steps, nil
}

type planner struct {
	def       *harness.Definition
	level     string
	home      string
	configDir string
	opts      PlanOptions
	steps     []Step
}

func (pl *planner) add(s Step) {
	if s.Entry == "" && s.Path != pl.configDir {
		if rel, err := filepath.Rel(pl.configDir, s.Path); err == nil {
			s.Entry = filepath.ToSlash(rel)
		}
	}
	pl.steps = append(pl.steps, s)
}

func private(l harness.Layer) bool {
	return l == harness.LayerCredentials || l == harness.LayerState || l == harness.LayerOwned || l == harness.LayerMixed
}

// current describes what exists at a path.
type current struct {
	exists bool
	link   bool
	target string
	dir    bool
}

func inspect(path string) current {
	fi, err := os.Lstat(path)
	if err != nil {
		return current{}
	}
	c := current{exists: true, dir: fi.IsDir()}
	if fi.Mode()&os.ModeSymlink != 0 {
		c.link = true
		c.target, _ = os.Readlink(path)
	}
	return c
}

func (pl *planner) planRule(rule *harness.EntryRule) error {
	rel := rule.Path()
	path := filepath.Join(pl.configDir, filepath.FromSlash(rel))
	src := pl.def.DefaultPathUnder(pl.home, rel)
	cur := inspect(path)
	if pl.opts.SkipHistory && rule.History {
		return nil
	}
	if hs := pl.opts.HistoryStore; hs != nil {
		if rule.History {
			pl.planHistory(rule, rel, path, cur)
			return nil
		}
		// A member's link into its group store for an entry the definition does not
		// declare as history (any more, FR-022a), whatever its category now. The home
		// store is the default setup, where only a state entry can have been history:
		// a borrowable entry's link there is the share level's own, and any other
		// private link there is a leak that only --fix-links replaces (spec 002).
		if cur.link && samePath(cur.target, hs.EntryPath(rel)) && (!hs.IsHome() || rule.Layer == harness.LayerState) {
			if pl.opts.SkipHistory {
				// A membership change in the same command owns this link (it
				// detaches it), so it is listed once.
				return nil
			}
			const why = " links into the history group store but the definition does not declare it as history (no longer, or it was linked by hand)"
			if rule.Layer == harness.LayerState || rule.Layer == harness.LayerCredentials {
				// Replaced by an empty private entry; the link target is never touched.
				pl.add(Step{Kind: StepFixLink, Path: path, Entry: rel, IsDir: rule.IsDir(), Source: cur.target,
					Note: "not history: " + rel + why + " (" + hs.Name() + "); it becomes an empty private entry and the link target is unchanged"})
				return nil
			}
			// Any other category: the link is removed (never its target) and the entry
			// gets its category's treatment below, as if it were missing.
			pl.add(Step{Kind: StepUnlink, Path: path, Entry: rel, Source: cur.target,
				Note: "not history: " + rel + why + " (" + hs.Name() + "); it gets the " + string(rule.Layer) + " treatment and the link target is unchanged"})
			cur = current{}
		}
	}
	switch rule.Layer {
	case harness.LayerCredentials, harness.LayerState, harness.LayerOwned:
		if cur.link {
			pl.leakingLink(rule, rel, cur.target, true)
			return nil
		}
		if !cur.exists && rule.IsDir() && rule.Layer != harness.LayerCredentials {
			pl.add(Step{Kind: StepMkdir, Path: path, Entry: rel, IsDir: true, Note: string(rule.Layer) + ": private"})
		}
		if !cur.exists && rule.Layer == harness.LayerOwned && pl.holdsMCP(rel) {
			return pl.seedOwned(rel, path, src)
		}
	case harness.LayerMixed:
		if cur.link {
			pl.leakingLink(rule, rel, cur.target, true)
			return nil
		}
		if !cur.exists {
			return pl.seedMixed(rule, rel, path)
		}
	case harness.LayerContent, harness.LayerSettings, harness.LayerPluginStore, harness.LayerHookLocation:
		return pl.planBorrowable(rule, rel, path, src, cur)
	}
	return nil
}

// profileRef names the profile in report notes.
func (pl *planner) profileRef() string {
	if pl.opts.ProfileID != "" {
		return pl.opts.ProfileID
	}
	return pl.def.Name + "/<profile>"
}

// planHistory plans one history entry of a group member (spec 003, plan.md Design
// Decision 5). It never moves data: a real entry is reported (or replaced after a
// backup with Relink), and the default setup is only written on an explicit join.
func (pl *planner) planHistory(rule *harness.EntryRule, rel, path string, cur current) {
	if pl.historyLink(rule, rel, path, cur) {
		return
	}
	hs := pl.opts.HistoryStore
	group := hs.Name()
	if pl.opts.Relink {
		// The store side must exist before the real entry is replaced, or the backup
		// would leave a dangling link behind.
		if !pl.storeDir(rule, rel, path) {
			return
		}
		pl.add(Step{Kind: StepReplaceWithLink, Path: path, Entry: rel, Source: hs.EntryPath(rel), Confirm: true, IsDir: cur.dir,
			Note: "history group " + group + ": the real entry is replaced by the link to the store after a backup"})
		return
	}
	pl.add(Step{Kind: StepReport, Path: path, Entry: rel,
		Note: "history-not-linked: a real entry where a link to the history group " + group + " store is expected; to merge it into the store, run: aprofile edit " + pl.profileRef() + " --group " + group + " (or replace it after a backup with: aprofile sync --relink)"})
}

// historyLink plans a member's history entry that is missing or a link: the store
// side is created where allowed, a link elsewhere is relinked and a missing entry
// is linked. It returns false, adding nothing, for a real entry.
func (pl *planner) historyLink(rule *harness.EntryRule, rel, path string, cur current) bool {
	hs := pl.opts.HistoryStore
	store := hs.EntryPath(rel)
	switch {
	case cur.link && samePath(cur.target, store):
		// The link is correct; a store entry that was removed (a store deleted by
		// hand) is recreated empty (FR-022, Edge Cases).
		pl.storeDir(rule, rel, path)
	case cur.link:
		if pl.storeDir(rule, rel, path) {
			pl.add(Step{Kind: StepRelinkHistory, Path: path, Entry: rel, Source: store,
				Note: "history group " + hs.Name() + ": replaces a link to " + cur.target})
		}
	case !cur.exists:
		// A missing history file is linked too; it comes into existence when the
		// harness first writes it (FR-016).
		if pl.storeDir(rule, rel, path) {
			pl.add(HistoryLinkStep(rel, path, hs))
		}
	default:
		return false
	}
	return true
}

// storeDir makes sure the store side of a history entry exists before a link to it
// is kept or created: the directory of a directory entry, the parent directory of a
// file entry. A non-home store gets a mkdir step; the home store (the default setup)
// only on an explicit join, otherwise the entry is reported with the command that
// creates it, and storeDir returns false.
func (pl *planner) storeDir(rule *harness.EntryRule, rel, path string) bool {
	hs := pl.opts.HistoryStore
	group := hs.Name()
	need := hs.EntryPath(rel)
	if !rule.IsDir() {
		need = filepath.Dir(need)
	}
	if PathExists(need) {
		return true
	}
	switch {
	case !hs.IsHome():
		pl.add(Step{Kind: StepMkdir, Path: need, Entry: need, IsDir: true, Note: "history store " + group})
	case pl.opts.ExplicitJoin:
		pl.add(Step{Kind: StepMkdir, Path: need, Entry: need, IsDir: true, Note: "default setup"})
	case !rule.IsDir():
		// A missing history file of the default setup is linked anyway, even when
		// its directory is missing too: sync never writes into the default setup,
		// and the harness creates both when it first runs there (FR-016).
	default:
		pl.add(Step{Kind: StepReport, Path: path, Entry: rel,
			Note: "history: " + need + " does not exist in the default setup yet; to create it and link it, run: aprofile edit " + pl.profileRef() + " --group " + group})
		return false
	}
	return true
}

// HistoryLinkStep is the step that links a member's history entry to its store.
func HistoryLinkStep(rel, path string, hs HistoryStore) Step {
	return Step{Kind: StepLinkHistory, Path: path, Entry: rel, Source: hs.EntryPath(rel), Note: "history group " + hs.Name()}
}

// PlanHistoryEntry plans one history entry of a member of the group whose store is
// opts.HistoryStore when the entry is missing or a link (no data moves): the same
// steps sync and prepare plan. It returns handled=false and no steps for a real
// entry, which the caller merges or sets aside (a join) or reports (sync). The join
// planner of internal/history shares it, so a join and a sync never disagree about
// an entry that holds no data.
func PlanHistoryEntry(def *harness.Definition, rule *harness.EntryRule, configDir string, opts PlanOptions) (steps []Step, handled bool) {
	pl := &planner{def: def, configDir: configDir, opts: opts}
	rel := rule.Path()
	path := filepath.Join(configDir, filepath.FromSlash(rel))
	handled = pl.historyLink(rule, rel, path, inspect(path))
	return pl.steps, handled
}

// HistoryStoreSteps returns the steps that create the store side of a history entry
// (see storeDir); ok is false, with a report step, when it may not be created.
func HistoryStoreSteps(def *harness.Definition, rule *harness.EntryRule, configDir string, opts PlanOptions) (steps []Step, ok bool) {
	pl := &planner{def: def, configDir: configDir, opts: opts}
	rel := rule.Path()
	ok = pl.storeDir(rule, rel, filepath.Join(configDir, filepath.FromSlash(rel)))
	return pl.steps, ok
}

// samePath compares two paths after cleaning them.
func samePath(a, b string) bool { return filepath.Clean(a) == filepath.Clean(b) }

// leakingLink reports, or with FixLinks repairs, a private entry that is a link.
func (pl *planner) leakingLink(rule *harness.EntryRule, rel, target string, isRule bool) {
	path := filepath.Join(pl.configDir, filepath.FromSlash(rel))
	what := string(rule.Layer)
	if !pl.opts.FixLinks {
		note := fmt.Sprintf("leaking-link: %s entry is a link -> %s; run: aprofile sync --fix-links", what, target)
		pl.add(Step{Kind: StepReport, Path: path, Entry: rel, Note: note})
		return
	}
	isDir := rule.IsDir()
	if !isRule {
		if fi, err := os.Stat(path); err == nil {
			isDir = fi.IsDir()
		}
	}
	pl.add(Step{Kind: StepFixLink, Path: path, Entry: rel, IsDir: isDir, Source: target,
		Note: "replaced the leaking link with an empty private entry; the link target is unchanged and earlier data stays in the default setup"})
}

// seedMixed creates an absent mixed file with SeedMixed's content: seeded once from
// the default setup (credentials and state keys removed) at all and content,
// minimal at none (FR-011: nothing is seeded at none, not even owned keys).
func (pl *planner) seedMixed(rule *harness.EntryRule, rel, path string) error {
	// A mixed file declares credentials keys the harness fills in later, and the
	// seeded one keeps owned keys such as MCP servers, which can hold literal
	// tokens: either way it is private to the user (0600).
	s := SeedMixed(pl.def, rule, profile.Share(pl.level), pl.home)
	pl.add(Step{Kind: StepSeedMixed, Path: path, Entry: rel, Source: s.Source, Data: s.Data, Mode: 0o600, Note: s.Note})
	return nil
}

// holdsMCP reports whether the file at rel holds the harness's MCP servers.
func (pl *planner) holdsMCP(rel string) bool {
	return pl.def.MCP.File() != "" && pl.def.MCP.File() == rel
}

// seedOwned creates an absent owned file that holds the MCP servers as a copy of
// the default setup's at all and content, so MCP servers start as the default
// setup's on every harness (level table, FR-022). At none it stays absent and the
// harness starts empty. The copy is private to the user (0600), and every value
// that looks like a secret is reported by key (Constitution IV).
func (pl *planner) seedOwned(rel, path, src string) error {
	if pl.level == harness.LevelNone {
		return nil
	}
	fi, err := os.Stat(src)
	if err != nil || fi.IsDir() {
		return nil // the default setup has none: the harness starts empty
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read default setup %s: %w", src, err)
	}
	if IsJSONExt(filepath.Ext(path)) {
		if err := checkJSON(data, src); err != nil {
			return err
		}
	}
	pl.add(Step{Kind: StepCopyOnce, Path: path, Entry: rel, Source: src, Data: data, Mode: 0o600,
		Note: "owned: seeded once from the default setup (MCP servers start as the default setup's)"})
	for _, k := range secretWarningsIn(path, data) {
		pl.add(Step{Kind: StepReport, Path: path, Entry: rel, Note: "warning: " + k + " looks like a secret and is copied verbatim"})
	}
	return nil
}

// secretWarningsIn is SecretWarnings for a JSON or TOML file (Codex keeps its MCP
// servers in config.toml).
func secretWarningsIn(path string, data []byte) []string {
	if strings.EqualFold(filepath.Ext(path), ".toml") {
		var m map[string]any
		if err := toml.Unmarshal(data, &m); err != nil {
			return nil
		}
		js, err := json.Marshal(m)
		if err != nil {
			return nil
		}
		data = js
	}
	return SecretWarnings(data)
}

func (pl *planner) planBorrowable(rule *harness.EntryRule, rel, path, src string, cur current) error {
	treat := pl.def.Treatment(rule.Layer, pl.level)
	srcExists := PathExists(src)
	switch treat {
	case harness.TreatLink:
		if !srcExists {
			return nil // lazy: linked by a later sync once the default setup has it
		}
		switch {
		case !cur.exists:
			pl.add(Step{Kind: StepLink, Path: path, Entry: rel, Source: src, Note: categoryNote(rule.Layer) + ": linked"})
		case cur.link && cur.target == src:
			// already correct
		case cur.link:
			pl.add(Step{Kind: StepLink, Path: path, Entry: rel, Source: src, Note: "replaces a link to " + cur.target})
		case pl.opts.Upgrade && !pl.opts.Relink:
			pl.add(Step{Kind: StepReport, Path: path, Entry: rel,
				Note: StatusKeptFromUpgrade + ": the profile's own " + categoryNote(rule.Layer) + " from before share levels is kept; to share the default setup's instead, run: aprofile sync --relink"})
		case cur.dir && dirEmpty(path):
			// The profile's own directory is still empty (created at share level
			// none): replacing it with the link loses nothing.
			pl.add(Step{Kind: StepReplaceWithLink, Path: path, Entry: rel, Source: src, IsDir: true,
				Note: "the empty own " + categoryNote(rule.Layer) + " directory becomes the link"})
		case rule.Layer == harness.LayerSettings || rule.Layer == harness.LayerHookLocation || pl.opts.Relink:
			pl.add(Step{Kind: StepReplaceWithLink, Path: path, Entry: rel, Source: src, Confirm: true, IsDir: cur.dir,
				Note: "the profile's own " + categoryNote(rule.Layer) + " is replaced by the shared one after a backup"})
		default:
			pl.add(Step{Kind: StepReport, Path: path, Entry: rel,
				Note: "link-replaced-by-file: a real entry is kept where a link is expected; run: aprofile sync --relink"})
		}
	case harness.TreatCopyOnce:
		if cur.link {
			pl.add(Step{Kind: StepUnlink, Path: path, Entry: rel, Note: "becomes the profile's own copy"})
		}
		if !cur.exists || cur.link {
			return pl.copyOnce(rule, rel, path, src, srcExists)
		}
	case harness.TreatMinimal:
		if cur.link {
			pl.add(Step{Kind: StepUnlink, Path: path, Entry: rel, Note: "becomes the profile's own"})
		}
		if !cur.exists || cur.link {
			pl.createMinimal(rule, rel, path, "minimal: nothing is copied at share level none")
		}
	case harness.TreatOwn:
		if cur.link {
			pl.add(Step{Kind: StepUnlink, Path: path, Entry: rel, Note: categoryNote(rule.Layer) + ": not shared at share level " + pl.level})
		}
		if (!cur.exists || cur.link) && rule.IsDir() && (rule.Layer == harness.LayerPluginStore || pl.isSkills(rel)) {
			pl.add(Step{Kind: StepMkdir, Path: path, Entry: rel, IsDir: true, Note: "the profile's own " + categoryNote(rule.Layer)})
		}
	case harness.TreatManaged:
		if cur.link {
			pl.add(Step{Kind: StepUnlink, Path: path, Entry: rel, Note: "hooks come from hook sets at share level " + pl.level})
			pl.reportDefaultHooks(rel, src)
		}
	}
	return nil
}

func (pl *planner) isSkills(rel string) bool {
	return pl.def.SkillsLocation != nil && strings.TrimSuffix(pl.def.SkillsLocation.Path, "/") == rel
}

// copyOnce copies a settings entry from the default setup, leaving out the hook
// location key when the hooks live inside this file (FR-010).
func (pl *planner) copyOnce(rule *harness.EntryRule, rel, path, src string, srcExists bool) error {
	if !srcExists {
		pl.createMinimal(rule, rel, path, "minimal")
		pl.add(Step{Kind: StepReport, Path: path, Entry: rel, Note: "nothing to copy: the default setup has no " + rel})
		return nil
	}
	fi, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("read default setup %s: %w", src, err)
	}
	if fi.IsDir() {
		pl.add(Step{Kind: StepCopyOnce, Path: path, Entry: rel, Source: src, IsDir: true, Note: "copied once from the default setup"})
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read default setup %s: %w", src, err)
	}
	note := "copied once from the default setup"
	if key := pl.def.HookKeyIn(rel); key != "" {
		out, removed, err := withoutKey(data, key, src)
		if err != nil {
			return err
		}
		data = out
		note += " (hooks excluded)"
		if removed != "" {
			pl.add(Step{Kind: StepReport, Path: path, Entry: rel, Note: "default setup hooks not copied: " + removed +
				"; to keep them, run: aprofile hook-sets import " + pl.def.Name + " --name <set>"})
		}
	} else if IsJSONExt(filepath.Ext(path)) {
		if err := checkJSON(data, src); err != nil {
			return err
		}
	}
	pl.add(Step{Kind: StepCopyOnce, Path: path, Entry: rel, Source: src, Data: data, Mode: fi.Mode().Perm(), Note: note})
	// Constitution IV: values are copied verbatim, with a warning per value that
	// looks like a secret, naming the key, never the value.
	for _, k := range SecretWarnings(data) {
		pl.add(Step{Kind: StepReport, Path: path, Entry: rel, Note: "warning: " + k + " looks like a secret and is copied verbatim"})
	}
	return nil
}

// dirEmpty reports whether path is a directory without entries.
func dirEmpty(path string) bool {
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) == 0
}

// reportDefaultHooks lists the hooks a separate hooks entry held in the default
// setup when the profile stops linking it.
func (pl *planner) reportDefaultHooks(rel, src string) {
	h := pl.def.Hooks
	if h == nil || h.Form != harness.HookFormEventGroups {
		return
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return
	}
	if _, removed, err := withoutKey(data, h.Key(), src); err == nil && removed != "" {
		pl.add(Step{Kind: StepReport, Path: filepath.Join(pl.configDir, filepath.FromSlash(rel)), Entry: rel,
			Note: "default setup hooks no longer used: " + removed + "; to keep them, run: aprofile hook-sets import " + pl.def.Name + " --name <set>"})
	}
}

func (pl *planner) createMinimal(rule *harness.EntryRule, rel, path, note string) {
	if rule.IsDir() {
		pl.add(Step{Kind: StepMkdir, Path: path, Entry: rel, IsDir: true, Note: note})
		return
	}
	pl.add(Step{Kind: StepCreate, Path: path, Entry: rel, Data: minimalDoc(path), Note: note})
}

func categoryNote(l harness.Layer) string {
	switch l {
	case harness.LayerPluginStore:
		return "plugin store"
	case harness.LayerHookLocation:
		return "hook location"
	default:
		return string(l)
	}
}

// withoutKey removes a top-level key from a JSON document and summarizes what it
// held (event names and group counts for a hooks object). An invalid document is
// an error naming the file.
func withoutKey(data []byte, key, file string) ([]byte, string, error) {
	m, err := decodeJSONObject(data)
	if err != nil {
		return nil, "", fmt.Errorf("%s is not valid JSON: %w", file, err)
	}
	raw, ok := m[key]
	if !ok {
		return data, "", nil
	}
	delete(m, key)
	out, err := encodeJSONObject(m)
	if err != nil {
		return nil, "", err
	}
	return out, summarizeHooks(raw), nil
}

// summarizeHooks renders "Event (n groups), ..." for an event-groups object.
func summarizeHooks(raw json.RawMessage) string {
	var events map[string][]json.RawMessage
	if err := json.Unmarshal(raw, &events); err != nil || len(events) == 0 {
		return ""
	}
	var names []string
	for e := range events {
		names = append(names, e)
	}
	sort.Strings(names)
	var parts []string
	for _, e := range names {
		n := len(events[e])
		unit := "groups"
		if n == 1 {
			unit = "group"
		}
		parts = append(parts, fmt.Sprintf("%s (%d %s)", e, n, unit))
	}
	return strings.Join(parts, ", ")
}

func checkJSON(data []byte, file string) error {
	if _, err := decodeJSONObject(data); err != nil {
		return fmt.Errorf("%s is not valid JSON: %w", file, err)
	}
	return nil
}

// planBorrowableGlobs plans the default-setup entries that a borrowable glob rule
// (only possible in a user definition) classifies.
func (pl *planner) planBorrowableGlobs() error {
	hasGlob := false
	for _, r := range pl.def.Classification {
		if r.IsGlob() && r.Layer.Borrowable() {
			hasGlob = true
		}
	}
	if !hasGlob {
		return nil
	}
	for _, root := range pl.def.DefaultRoots() {
		dir := root.Dir
		if pl.home != "" {
			dir = pl.def.DefaultPathUnder(pl.home, root.Prefix)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			rel := e.Name()
			if root.Prefix != "" {
				rel = root.Prefix + "/" + e.Name()
			}
			res := Classify(pl.def, rel)
			if res.Rule == nil || !res.Rule.IsGlob() || !res.Layer.Borrowable() {
				continue
			}
			rule := harness.EntryRule{Entry: rel, Layer: res.Layer, Origin: res.Rule.Origin}
			if e.IsDir() {
				rule.Entry += "/"
			}
			if err := pl.planRule(&rule); err != nil {
				return err
			}
		}
	}
	return nil
}
