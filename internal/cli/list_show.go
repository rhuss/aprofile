package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/hooksets"
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
}

type listJSON struct {
	FormatVersion int           `json:"format_version"`
	Profiles      []profileJSON `json:"profiles"`
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

	if asJSON {
		out := listJSON{FormatVersion: schemaVersion}
		for _, p := range profiles {
			out.Profiles = append(out.Profiles, a.profileToJSON(p))
		}
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
		status := a.wrapperStatus(p, def)
		primary := ""
		if p.Primary {
			primary = " (primary)"
		}
		fmt.Fprintf(a.out, "  %s%s  [%s] share: %s\n", styleLabel(a.out, p.DisplayLabel(), p.Color, p.Icon), primary, status, p.ShareLevel())
	}
	for _, f := range failures {
		fmt.Fprintf(a.errOut, "aprofile: profile %s failed to load: %v\n", f.ID(), f.Err)
	}
	return nil
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

// showJSON is show's --json schema: the list schema plus the per-entry treatment
// (FR-015, data-model.md Entry treatment).
type showJSON struct {
	profileJSON
	Entries                  []layer.EntryTreatment `json:"entries"`
	HookSets                 []hookSetJSON          `json:"hook_sets"`
	SettingsDiffer           bool                   `json:"settings_differ"`
	HomeEscapes              []harness.HomeEscape   `json:"home_escapes"`
	UnborrowedDefaultEntries []string               `json:"unborrowed_default_entries"`
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
	sj := showJSON{profileJSON: a.profileToJSON(p), HookSets: []hookSetJSON{}, HomeEscapes: []harness.HomeEscape{}, UnborrowedDefaultEntries: []string{}}
	def, _ := a.set.Get(h)
	if def != nil {
		entries, err := layer.Treatments(def, p, "", sj.ConfigDir)
		if err != nil {
			return err
		}
		sj.Entries = entries
		sj.SettingsDiffer = len(layer.SettingsDiffer(entries)) > 0
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
	}
	if sj.Entries == nil {
		sj.Entries = []layer.EntryTreatment{}
	}

	if asJSON {
		return writeJSON(a.out, sj)
	}
	pj := sj.profileJSON
	fmt.Fprintf(a.out, "%s\n", styleLabel(a.out, p.DisplayLabel(), p.Color, p.Icon))
	fmt.Fprintf(a.out, "  harness:        %s\n", p.Harness)
	fmt.Fprintf(a.out, "  name:           %s\n", p.Name)
	fmt.Fprintf(a.out, "  share level:    %s: %s\n", p.ShareLevel(), levelDescription(p.ShareLevel()))
	if p.ShareImplicit {
		fmt.Fprintf(a.out, "                  (no level stored: profiles from before share levels are all; consider: aprofile edit %s --share content --dry-run)\n", p.ID())
	}
	fmt.Fprintf(a.out, "  primary:        %v\n", p.Primary)
	fmt.Fprintf(a.out, "  auth kind:      %s\n", p.AuthKind)
	fmt.Fprintf(a.out, "  credentials:    %s\n", presentStr(pj.HasCredentials))
	if p.Model != "" {
		fmt.Fprintf(a.out, "  model:          %s\n", p.Model)
	}
	if p.Endpoint != "" {
		fmt.Fprintf(a.out, "  endpoint:       %s\n", p.Endpoint)
	}
	fmt.Fprintf(a.out, "  config dir:     %s\n", pj.ConfigDir)
	fmt.Fprintf(a.out, "  wrapper:        %s [%s]\n", pj.WrapperPath, pj.WrapperStatus)
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

// printEntries prints the entries grouped by treatment, each with its reason and
// any status other than ok.
func printEntries(a *app, entries []layer.EntryTreatment) {
	groups := append(append([]struct{ key, title string }(nil), treatmentGroups[:2]...),
		struct{ key, title string }{absentGroup, "not in the default setup (linked once it is)"})
	groups = append(groups, treatmentGroups[2:]...)
	for _, g := range groups {
		var group []layer.EntryTreatment
		for _, e := range entries {
			absent := e.Treatment == layer.TreatmentLinked && e.Status == layer.StatusMissingLinkTarget
			if (g.key == absentGroup && absent) || (g.key == e.Treatment && !absent) {
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
	def, _ := a.set.Get(p.Harness)
	configDir := a.paths.ProfileConfigDir(p.Harness, p.Name)
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
		WrapperStatus: a.wrapperStatus(p, def),
		ConfigDir:     configDir,

		Share:            string(p.ShareLevel()),
		ShareDescription: levelDescription(p.ShareLevel()),
		ShareImplicit:    p.ShareImplicit,
	}
	if def != nil {
		pj.HasCredentials = layer.HasCredentials(def, configDir)
		pj.WrapperPath = filepath.Join(a.paths.BinDir, def.WrapperPrefix+"-"+p.Name)
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
	return pj
}

// wrapperStatus reports whether a profile's on-disk wrapper matches what sync
// would render (FR-006b): synced | stale | missing.
func (a *app) wrapperStatus(p *profile.Profile, def *harness.Definition) string {
	if def == nil {
		return "unknown"
	}
	path := filepath.Join(a.paths.BinDir, def.WrapperPrefix+"-"+p.Name)
	existing, err := os.ReadFile(path)
	if err != nil {
		return "missing"
	}
	configDir := a.paths.ProfileConfigDir(p.Harness, p.Name)
	hookEnv, err := harness.RunEnvHook(def, harness.HookContext{
		Harness:   p.Harness,
		Profile:   p.Name,
		ConfigDir: configDir,
		AuthKind:  p.AuthKind,
		Model:     p.Model,
		Endpoint:  p.Endpoint,
	})
	if err != nil {
		return "unknown"
	}
	want, err := wrapper.RenderWith(p, def, configDir, wrapper.RenderOptions{HookEnv: hookEnv})
	if err != nil {
		return "unknown"
	}
	if string(existing) == want {
		return "synced"
	}
	return "stale"
}

func presentStr(present bool) string {
	if present {
		return "present"
	}
	return "none"
}
