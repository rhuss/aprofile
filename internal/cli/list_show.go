package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/history"
	"github.com/cc-deck/aprofile/internal/hooksets"
	"github.com/cc-deck/aprofile/internal/launch"
	"github.com/cc-deck/aprofile/internal/layer"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/secret"
	"github.com/cc-deck/aprofile/internal/wrapper"
)

// schemaVersion is the --json schema version for list/show output (FR-041).
const schemaVersion = 1

type extraEnvJSON struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`                // reference | literal
	Reference string `json:"reference,omitempty"` // only for references (never a secret)
}

type profileJSON struct {
	Harness        string            `json:"harness"`
	Name           string            `json:"name"`
	Label          string            `json:"label"`
	Color          string            `json:"color"`
	Icon           string            `json:"icon"`
	Primary        bool              `json:"primary"`
	AuthKind       string            `json:"auth_kind"`
	AuthRefs       map[string]string `json:"auth_refs,omitempty"`
	Model          string            `json:"model,omitempty"`
	Endpoint       string            `json:"endpoint,omitempty"`
	ExtraEnv       []extraEnvJSON    `json:"extra_env,omitempty"`
	HerdrAgent     string            `json:"herdr_agent,omitempty"`
	HasCredentials bool              `json:"has_credentials"`
	WrapperStatus  string            `json:"wrapper_status"`
	ConfigDir      string            `json:"config_dir"`
	WrapperPath    string            `json:"wrapper_path"`
	// Feature 002.
	Share            string `json:"share"`
	ShareDescription string `json:"share_description"`
	ShareImplicit    bool   `json:"share_implicit,omitempty"`
	// Feature 003: the history group (empty when isolated).
	HistoryGroup string `json:"history_group"`
	// Feature 004: whether the profile folder's placed launch entries are current:
	// synced | stale | unknown (FR-020a).
	FolderSync string `json:"folder_sync"`
}

type listJSON struct {
	FormatVersion int           `json:"format_version"`
	Profiles      []profileJSON `json:"profiles"`
	// LeftoverWrappers names the aprofile wrappers in the bin dir that belong to no
	// profile (left by remove or rename until the next sync, FR-006b).
	LeftoverWrappers []string `json:"leftover_wrappers"`
}

func newListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List profiles grouped by harness",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runList(cmd, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

func runList(cmd *cobra.Command, asJSON bool) error {
	a, err := newApp(cmd)
	if err != nil {
		return err
	}
	profiles, failures, err := a.store.List()
	if err != nil {
		return err
	}

	leftovers := a.leftoverWrappers(profiles, failures)

	var reasons statusReasons
	if asJSON {
		out := listJSON{FormatVersion: schemaVersion, Profiles: []profileJSON{}, LeftoverWrappers: leftovers}
		for _, p := range profiles {
			pj, serr := a.profileToJSONErr(p)
			reasons.add(p.Harness, serr)
			out.Profiles = append(out.Profiles, pj)
		}
		reasons.print(a)
		return writeJSON(a.out, out)
	}

	if len(profiles) == 0 {
		fmt.Fprintln(a.out, "no profiles yet (create one with: aprofile add <harness> <name>)")
	}
	var lastHarness string
	for _, p := range profiles {
		if p.Harness != lastHarness {
			fmt.Fprintf(a.out, "%s:\n", p.Harness)
			lastHarness = p.Harness
		}
		def, _ := a.set.Get(p.Harness)
		status, serr := a.wrapperStatusErr(p, def)
		reasons.add(p.Harness, serr)
		if def != nil {
			if st, _ := a.folderStatus(p, def); st.Sync == launch.SyncStale {
				status += ", folder stale"
			}
		}
		primary := ""
		if p.Primary {
			primary = " (primary)"
		}
		group := p.HistoryGroup
		if group == "" {
			group = "-"
		}
		fmt.Fprintf(a.out, "  %s%s  [%s] share: %s  group: %s\n", styleLabel(a.out, p.DisplayLabel(), p.Color, p.Icon), primary, status, p.ShareLevel(), group)
	}
	switch len(leftovers) {
	case 0:
	case 1:
		fmt.Fprintf(a.out, "1 leftover wrapper from a removed or renamed profile: %s (run: aprofile sync)\n", leftovers[0])
	default:
		fmt.Fprintf(a.out, "%d leftover wrappers from removed or renamed profiles: %s (run: aprofile sync)\n", len(leftovers), strings.Join(leftovers, ", "))
	}
	reasons.print(a)
	for _, f := range failures {
		fmt.Fprintf(a.errOut, "aprofile: profile %s failed to load: %v\n", f.ID(), f.Err)
	}
	return nil
}

// leftoverWrappers lists the aprofile wrappers in the bin dir that belong to no
// profile (FR-006b), never nil. An unreadable bin dir is reported on stderr.
func (a *app) leftoverWrappers(profiles []*profile.Profile, failures []profile.LoadFailure) []string {
	keys := make([]harness.ProfileKey, 0, len(profiles)+len(failures))
	for _, p := range profiles {
		keys = append(keys, harness.ProfileKey{Harness: p.Harness, Name: p.Name})
	}
	for _, f := range failures {
		keys = append(keys, harness.ProfileKey{Harness: f.Harness, Name: f.Name})
	}
	names, err := wrapper.LeftoversFor(a.paths.BinDir, a.set, keys)
	if err != nil {
		fmt.Fprintf(a.errOut, "aprofile: cannot check the bin dir for leftover wrappers: %v\n", err)
	}
	if names == nil {
		names = []string{}
	}
	return names
}

func newShowCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "show <harness>/<name>",
		Short: "Show one profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runShow(cmd, args[0], asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

// showJSON is show's --json schema: format_version, the list schema's profile
// fields, plus the per-entry treatment (FR-015, FR-041, data-model.md Entry
// treatment).
type showJSON struct {
	FormatVersion int `json:"format_version"`
	profileJSON
	Credentials              credentialsJSON        `json:"credentials"`
	Entries                  []layer.EntryTreatment `json:"entries"`
	HookSets                 []hookSetJSON          `json:"hook_sets"`
	SettingsDiffer           bool                   `json:"settings_differ"`
	HomeEscapes              []harness.HomeEscape   `json:"home_escapes"`
	UnborrowedDefaultEntries []string               `json:"unborrowed_default_entries"`
	// History describes how the profile's history is shared (spec 003, FR-020).
	History string `json:"history"`
	// SetAside lists the profile's set-aside history (FR-020).
	SetAside []history.SetAsideRun `json:"set_aside"`
	// Folder reports whether the profile folder is self-contained (spec 004 FR-020).
	Folder *launch.FolderReport `json:"folder,omitempty"`
	// Features lists the profile's enabled features with their parameter values
	// (spec 006, FR-016), the same feature objects the features command reports.
	Features []featureJSON `json:"features"`
	// Problems lists where the profile does not fit its harness definition (FR-006):
	// an undeclared auth kind, or a model or endpoint the harness has no variable for.
	Problems []string `json:"problems"`
}

type hookSetJSON struct {
	Name   string `json:"name"`
	Reason string `json:"reason"` // named | every-profile | excluded | missing | failed-to-load
}

func runShow(cmd *cobra.Command, ref string, asJSON bool) error {
	a, err := newApp(cmd)
	if err != nil {
		return err
	}
	h, n, err := parseProfileRef(ref)
	if err != nil {
		return err
	}
	p, err := a.store.Load(h, n)
	if err != nil {
		return err
	}
	def, _ := a.set.Get(h)
	// The env hook (wrapper status) and the auth-status hook run concurrently, so a
	// hanging hook costs show one timeout, not two (FR-011).
	creds := make(chan credentialsJSON, 1)
	go func() { creds <- a.credentialStatus(p, def, true) }()
	pj, statusErr := a.profileToJSONErr(p)
	sj := showJSON{FormatVersion: schemaVersion, profileJSON: pj, HookSets: []hookSetJSON{}, HomeEscapes: []harness.HomeEscape{}, UnborrowedDefaultEntries: []string{},
		Problems: []string{}, Features: []featureJSON{}}
	sj.Credentials = <-creds
	sj.HasCredentials = sj.Credentials.Present
	fit := map[string]wrapper.FitIssue{}
	var folderSt launch.StatusReport
	if def != nil {
		for _, fi := range wrapper.FitIssues(p, def) {
			fit[fi.Field] = fi
			sj.Problems = append(sj.Problems, fi.Message+"; fix: "+fi.Fix)
		}
		var hs layer.HistoryStore
		if st, herr := a.groups().StoreFor(def, p); herr == nil && st != nil {
			hs = st
		} else if herr != nil {
			sj.Problems = append(sj.Problems, herr.Error()+"; fix: aprofile edit "+p.ID()+" --group <group>, or --no-group")
		}
		entries, err := layer.TreatmentsWithPlaced(def, p, "", sj.ConfigDir, hs, launch.LoadPlaced(a.paths.ManagedLaunchPath(p.Harness, p.Name)))
		if err != nil {
			return err
		}
		sj.Entries = entries
		sj.SettingsDiffer = len(layer.SettingsDiffer(entries)) > 0
		var ferr error
		folderSt, ferr = a.folderStatus(p, def)
		fr := folderSt.Folder()
		sj.Folder = &fr
		if ferr != nil {
			sj.Problems = append(sj.Problems, fmt.Sprintf("cannot tell whether the profile folder is current: %v; fix the file, then run: aprofile sync", ferr))
		}
		if def.HomeEscapes != nil {
			sj.HomeEscapes = def.HomeEscapes
		}
		if unb, err := layer.UnborrowedDefaultEntries(def); err == nil && unb != nil && p.ShareLevel() != profile.ShareNone {
			sj.UnborrowedDefaultEntries = unb
		}
		lib, _ := a.loadHookLibrary()
		for _, name := range sortedSetNames(lib) {
			if r := hooksets.Reason(lib[name], p); r != "" {
				sj.HookSets = append(sj.HookSets, hookSetJSON{Name: name, Reason: r})
			}
		}
		for _, name := range p.HookSets {
			switch s, ok := lib[name]; {
			case !ok:
				sj.HookSets = append(sj.HookSets, hookSetJSON{Name: name, Reason: "missing"})
			case s.LoadErr != nil:
				sj.HookSets = append(sj.HookSets, hookSetJSON{Name: name, Reason: "failed-to-load"})
			}
		}
		// Spec 006 FR-016: the features section, one entry per enabled feature
		// with its parameter values.
		for _, fs := range p.Features {
			fj := featureJSON{ID: fs.Feature, Enabled: true, Params: map[string]string{}}
			if resolved, err := def.ResolveFeatureParams(fs.Feature, fs.Params); err == nil {
				fj.Params = resolved
			} else {
				fj.Params = fs.Params
			}
			sj.Features = append(sj.Features, fj)
		}
	}
	if sj.Entries == nil {
		sj.Entries = []layer.EntryTreatment{}
	}
	sj.History = historyLine(p)
	sj.SetAside = []history.SetAsideRun{}
	if runs, err := history.ListSetAside(a.paths, p.Harness, p.Name); err == nil && runs != nil {
		sj.SetAside = runs
	}

	if statusErr != nil {
		fmt.Fprintf(a.errOut, "aprofile: wrapper status is unknown: %v\n", statusErr)
	}
	if asJSON {
		return writeJSON(a.out, sj)
	}
	fmt.Fprintf(a.out, "%s\n", styleLabel(a.out, p.DisplayLabel(), p.Color, p.Icon))
	fmt.Fprintf(a.out, "  harness:        %s\n", p.Harness)
	fmt.Fprintf(a.out, "  name:           %s\n", p.Name)
	fmt.Fprintf(a.out, "  share level:    %s: %s\n", p.ShareLevel(), levelDescription(p.ShareLevel()))
	if p.ShareImplicit {
		fmt.Fprintf(a.out, "                  (no level stored: profiles from before share levels are all; consider: aprofile edit %s --share content --dry-run)\n", p.ID())
	}
	if p.HistoryGroup != "" {
		fmt.Fprintf(a.out, "  history group:  %s\n", p.HistoryGroup)
	}
	fmt.Fprintf(a.out, "  history:        %s\n", sj.History)
	fmt.Fprintf(a.out, "  primary:        %v\n", p.Primary)
	authLine := p.AuthKind
	if fi, ok := fit["auth_kind"]; ok {
		authLine += " (" + fi.Detail + "; fix: " + fi.Fix + ")"
	}
	fmt.Fprintf(a.out, "  auth kind:      %s\n", authLine)
	printCredentials(a, sj.Credentials)
	for _, f := range []struct{ field, value string }{{"model", p.Model}, {"endpoint", p.Endpoint}} {
		if f.value == "" {
			continue
		}
		line := f.value
		if fi, ok := fit[f.field]; ok {
			line += " (" + fi.Detail + "; fix: " + fi.Fix + ")"
		}
		fmt.Fprintf(a.out, "  %-15s %s\n", f.field+":", line)
	}
	fmt.Fprintf(a.out, "  config dir:     %s\n", pj.ConfigDir)
	fmt.Fprintf(a.out, "  wrapper:        %s [%s]\n", pj.WrapperPath, pj.WrapperStatus)
	if sj.Folder != nil {
		printFolder(a, p, def, folderSt, *sj.Folder)
	}
	if len(sj.Features) > 0 {
		for i, f := range sj.Features {
			label := "  features:       "
			if i > 0 {
				label = "                  "
			}
			fmt.Fprintf(a.out, "%s%s\n", label, featureChangeLine(f))
		}
		// FR-016 / FR-015: drift between the recorded feature state and the
		// placed entries, and collisions, surface as show warnings.
		for _, w := range featureDriftWarnings(a, p, def) {
			fmt.Fprintf(a.out, "                  warning: %s\n", w)
		}
	}
	var sets []string
	for _, hs := range sj.HookSets {
		sets = append(sets, hs.Name+" ("+reasonText(hs.Reason)+")")
	}
	fmt.Fprintf(a.out, "  hook sets:      %s\n", joinOr(sets, "none"))
	if p.ShareLevel() == profile.ShareContent {
		if sj.SettingsDiffer {
			fmt.Fprintf(a.out, "  settings:       differ from the default setup (refresh with: aprofile settings pull %s --dry-run)\n", p.ID())
		} else {
			fmt.Fprintln(a.out, "  settings:       same as the default setup")
		}
	}
	printEntries(a, sj.Entries)
	if len(sj.SetAside) > 0 {
		fmt.Fprintln(a.out, "  set-aside history (from history group joins; never read by the harness):")
		for _, r := range sj.SetAside {
			fmt.Fprintf(a.out, "    %s  (%d items)\n", r.Dir, len(r.Items))
		}
	}
	if len(sj.UnborrowedDefaultEntries) > 0 {
		fmt.Fprintln(a.out, "  not borrowed (unclassified in the default setup):")
		for _, e := range sj.UnborrowedDefaultEntries {
			fmt.Fprintf(a.out, "    %s  (fix: aprofile harness classify %s %s --as content)\n", e, p.Harness, e)
		}
	}
	return nil
}

// treatmentGroups orders show's entry groups.
var treatmentGroups = []struct{ key, title string }{
	{layer.TreatmentPrivate, "private"},
	{layer.TreatmentLinked, "linked to the default setup"},
	{layer.TreatmentOwnCopy, "the profile's own"},
	{layer.TreatmentManaged, "managed by hook sets"},
	{layer.TreatmentUnclassified, "unclassified"},
	{layer.TreatmentHomeEscape, "$HOME escapes"},
}

// absentGroup holds the entries the level would link but the default setup does
// not have (status missing-link-target): nothing is linked, so show lists them
// apart from the linked entries instead of under them.
const absentGroup = "absent"

// historyGroupKey holds a member's history entries, linked to its group store
// (spec 003): listed apart from the entries borrowed from the default setup.
const historyGroupKey = "history"

// isHistoryLink reports whether an entry treatment is a member's history link.
func isHistoryLink(e layer.EntryTreatment) bool {
	return e.Treatment == layer.TreatmentLinked && strings.HasPrefix(e.Reason, "history group ")
}

// printEntries prints the entries grouped by treatment, each with its reason and
// any status other than ok.
func printEntries(a *app, entries []layer.EntryTreatment) {
	groups := append(append([]struct{ key, title string }(nil), treatmentGroups[:2]...),
		struct{ key, title string }{historyGroupKey, "history, linked to the history group store"},
		struct{ key, title string }{absentGroup, "not in the default setup (linked once it is)"})
	groups = append(groups, treatmentGroups[2:]...)
	for _, g := range groups {
		var group []layer.EntryTreatment
		for _, e := range entries {
			absent := e.Treatment == layer.TreatmentLinked && e.Status == layer.StatusMissingLinkTarget
			hist := isHistoryLink(e)
			if (g.key == historyGroupKey && hist) || (g.key == absentGroup && absent) || (g.key == e.Treatment && !absent && !hist) {
				e2 := e
				if absent {
					e2.Status = layer.StatusOK // the group title already says it
				}
				group = append(group, e2)
			}
		}
		if len(group) == 0 {
			continue
		}
		fmt.Fprintf(a.out, "  %s:\n", g.title)
		for _, e := range group {
			status := ""
			if e.Status != layer.StatusOK {
				status = " [" + e.Status + "]"
			}
			fmt.Fprintf(a.out, "    %-28s %s%s\n", e.Entry, e.Reason, status)
		}
	}
}

// profileToJSON builds the secret-free JSON representation of a profile.
func (a *app) profileToJSON(p *profile.Profile) profileJSON {
	pj, _ := a.profileToJSONErr(p)
	return pj
}

// profileToJSONErr is profileToJSON with the reason an unknown wrapper status has.
func (a *app) profileToJSONErr(p *profile.Profile) (profileJSON, error) {
	def, _ := a.set.Get(p.Harness)
	configDir := a.paths.ProfileConfigDir(p.Harness, p.Name)
	status, statusErr := a.wrapperStatusErr(p, def)
	pj := profileJSON{
		Harness:       p.Harness,
		Name:          p.Name,
		Label:         p.DisplayLabel(),
		Color:         p.Color,
		Icon:          p.Icon,
		Primary:       p.Primary,
		AuthKind:      p.AuthKind,
		Model:         p.Model,
		Endpoint:      p.Endpoint,
		HerdrAgent:    p.HerdrAgent,
		WrapperStatus: status,
		ConfigDir:     configDir,

		Share:            string(p.ShareLevel()),
		ShareDescription: levelDescription(p.ShareLevel()),
		ShareImplicit:    p.ShareImplicit,
		HistoryGroup:     p.HistoryGroup,
	}
	if def != nil {
		// list does not call the auth-status hook (one hook call per profile would slow
		// it down); show does, and may report present where list does not.
		pj.HasCredentials = a.credentialStatus(p, def, false).Present
		pj.WrapperPath = filepath.Join(a.paths.BinDir, def.WrapperName(p.Name))
		st, _ := a.folderStatus(p, def)
		pj.FolderSync = st.Sync
	}
	if len(p.AuthRefs) > 0 {
		pj.AuthRefs = map[string]string{}
		for v, ref := range p.AuthRefs {
			pj.AuthRefs[v] = ref.Raw // a reference, never a secret value
		}
	}
	for _, e := range p.ExtraEnv {
		ej := extraEnvJSON{Name: e.Name}
		if !e.Literal && secret.IsReference(e.Value) {
			ej.Kind = "reference"
			ej.Reference = e.Value
		} else {
			ej.Kind = "literal" // value withheld (FR-028: no secret values in --json)
		}
		pj.ExtraEnv = append(pj.ExtraEnv, ej)
	}
	return pj, statusErr
}

// printFolder prints show's folder lines (contracts/cli.md): self-containment,
// wrapper-only settings with their reasons, folder sync and unplaced entries.
func printFolder(a *app, p *profile.Profile, def *harness.Definition, st launch.StatusReport, f launch.FolderReport) {
	fmt.Fprintf(a.out, "  folder:         %s\n", f.StatusText())
	for _, w := range f.WrapperOnly {
		fmt.Fprintf(a.out, "                  wrapper-only: %s (%s)\n", w.SettingName(), launch.ReasonText(w.Reason, p, def))
	}
	sync := f.Sync
	if sync == launch.SyncStale {
		sync = "stale (run: aprofile sync)"
	}
	fmt.Fprintf(a.out, "  folder sync:    %s\n", sync)
	for i, e := range st.Unplaced {
		label := "  unplaced:       "
		if i > 0 {
			label = "                  "
		}
		_, _, short := st.UnplacedText(e)
		what := e.Location
		if name, ok := strings.CutPrefix(e.Location, "env."); ok {
			what = "env " + name
		}
		fmt.Fprintf(a.out, "%s%s: %s (%s)\n", label, e.File, what, short)
	}
}

// folderStatus reports the profile folder's launch settings (spec 004 FR-020 to
// FR-020b): carriers, whether sync would change the placed entries, and the launch
// entries aprofile did not place.
func (a *app) folderStatus(p *profile.Profile, def *harness.Definition) (launch.StatusReport, error) {
	return launch.Status(p, def, a.paths.ProfileConfigDir(p.Harness, p.Name), a.paths.ManagedLaunchPath(p.Harness, p.Name))
}

// wrapperStatus reports whether a profile's on-disk wrapper matches what sync
// would render (FR-006b): synced | stale | missing, or unknown when that cannot be
// told (no definition, or the exec hook's env operation failed).
func (a *app) wrapperStatus(p *profile.Profile, def *harness.Definition) string {
	status, _ := a.wrapperStatusErr(p, def)
	return status
}

// wrapperStatusErr is wrapperStatus with the reason an unknown status has (nil for
// a missing definition, which list reports on its own).
func (a *app) wrapperStatusErr(p *profile.Profile, def *harness.Definition) (string, error) {
	if def == nil {
		return "unknown", nil
	}
	existing, err := os.ReadFile(filepath.Join(a.paths.BinDir, def.WrapperName(p.Name)))
	if err != nil {
		return "missing", nil
	}
	want, err := wrapper.Expected(p, def, a.paths.ProfileConfigDir(p.Harness, p.Name))
	if err != nil {
		return "unknown", err
	}
	if string(existing) == want {
		return "synced", nil
	}
	return "stale", nil
}

// statusReasons collects why wrapper statuses are unknown and prints the first
// reason once per harness on stderr (M13: a failing exec hook would otherwise leave
// every profile of the harness at "unknown" without a word).
type statusReasons struct {
	order []string
	first map[string]error
}

func (r *statusReasons) add(harnessName string, err error) {
	if err == nil {
		return
	}
	if r.first == nil {
		r.first = map[string]error{}
	}
	if _, seen := r.first[harnessName]; !seen {
		r.first[harnessName] = err
		r.order = append(r.order, harnessName)
	}
}

func (r *statusReasons) print(a *app) {
	for _, h := range r.order {
		fmt.Fprintf(a.errOut, "aprofile: wrapper status of %s profiles is unknown: %v\n", h, r.first[h])
	}
}

// Credential sources show reports (FR-039).
const (
	credSourceFiles      = "files"       // a credentials-layer file or key in the config dir
	credSourceReferences = "references"  // every variable of the auth kind has a resolving reference
	credSourceAuthStatus = "auth-status" // the exec hook's auth-status answered ok (Keychain logins)
)

// credentialsJSON is show's credential report (FR-039, T066). It never carries a
// secret value: references are the reference text and whether it resolves now.
type credentialsJSON struct {
	Present    bool                `json:"present"`
	Sources    []string            `json:"sources"`
	References []credentialRefJSON `json:"references"`
	AuthStatus *authStatusJSON     `json:"auth_status,omitempty"`
}

type credentialRefJSON struct {
	Variable  string `json:"variable"`
	Reference string `json:"reference"`
	Resolves  bool   `json:"resolves"`
	Problem   string `json:"problem,omitempty"` // why it does not resolve (never a value)
}

// authStatusJSON is the hook's auth-status answer: ok | missing | unknown from the
// hook, or not-installed (the executable is not on PATH), unsupported (exit 64) or
// error (the call failed; see error).
type authStatusJSON struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	Error  string `json:"error,omitempty"`
}

// credentialStatus reports whether a profile has credentials, from three sources:
// credential files in the config dir, the auth kind's references (present when every
// variable the kind sets has a reference that resolves now), and, with callHook, the
// exec hook's auth-status operation, which covers logins kept in the macOS Keychain.
// A failing hook never fails the caller; it is reported in AuthStatus.
func (a *app) credentialStatus(p *profile.Profile, def *harness.Definition, callHook bool) credentialsJSON {
	cj := credentialsJSON{Sources: []string{}, References: []credentialRefJSON{}}
	if def == nil {
		return cj
	}
	configDir := a.paths.ProfileConfigDir(p.Harness, p.Name)
	if layer.HasCredentials(def, configDir) {
		cj.Sources = append(cj.Sources, credSourceFiles)
	}

	var vars []string
	if ak, ok := def.AuthKind(p.AuthKind); ok {
		vars = ak.VarNames()
	}
	for v := range p.AuthRefs {
		if !slices.Contains(vars, v) {
			vars = append(vars, v)
		}
	}
	sort.Strings(vars)
	allResolve := len(vars) > 0
	for _, v := range vars {
		rj := credentialRefJSON{Variable: v}
		ref, ok := p.AuthRefs[v]
		if !ok {
			rj.Problem = "no reference set"
		} else {
			rj.Reference = ref.Raw
			if _, err := secret.Resolve(ref); err != nil {
				rj.Problem = err.Error()
			} else {
				rj.Resolves = true
			}
		}
		allResolve = allResolve && rj.Resolves
		cj.References = append(cj.References, rj)
	}
	if allResolve {
		cj.Sources = append(cj.Sources, credSourceReferences)
	}

	if callHook && def.HookDeclares("auth-status") {
		st, err := harness.RunAuthStatusHook(def, harness.HookContext{
			Harness: p.Harness, Profile: p.Name, ConfigDir: configDir, AuthKind: p.AuthKind,
		})
		as := &authStatusJSON{Status: st.Status, Detail: st.Detail}
		switch {
		case err != nil:
			as.Status, as.Error = "error", err.Error()
		case st.NotInstalled:
			as.Status = "not-installed"
			as.Detail = harness.HookExecutable(def.Name) + " is not on PATH"
		case st.Status == "":
			as.Status = "unsupported"
		}
		cj.AuthStatus = as
		if as.Status == "ok" {
			cj.Sources = append(cj.Sources, credSourceAuthStatus)
		}
	}
	cj.Present = len(cj.Sources) > 0
	return cj
}

// printCredentials prints show's credential lines: present or none with the
// sources, each reference and whether it resolves, and the hook's auth-status.
func printCredentials(a *app, c credentialsJSON) {
	status := "none"
	if c.Present {
		status = "present (" + strings.Join(c.Sources, ", ") + ")"
	}
	fmt.Fprintf(a.out, "  credentials:    %s\n", status)
	for _, r := range c.References {
		switch {
		case r.Resolves:
			fmt.Fprintf(a.out, "                  %s: %s resolves\n", r.Variable, r.Reference)
		case r.Reference == "":
			fmt.Fprintf(a.out, "                  %s: %s\n", r.Variable, r.Problem)
		default:
			fmt.Fprintf(a.out, "                  %s: %s does not resolve (%s)\n", r.Variable, r.Reference, r.Problem)
		}
	}
	if as := c.AuthStatus; as != nil {
		line := "auth-status hook: " + as.Status
		switch {
		case as.Error != "":
			line += " (" + as.Error + ")"
		case as.Detail != "":
			line += " (" + as.Detail + ")"
		}
		fmt.Fprintf(a.out, "                  %s\n", line)
	}
}
