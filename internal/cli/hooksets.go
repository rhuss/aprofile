package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/hooksets"
	"github.com/cc-deck/aprofile/internal/layer"
	"github.com/cc-deck/aprofile/internal/profile"
)

// loadHookLibrary loads the hook-set library. Load errors of single files are
// returned so callers can report them; the valid sets are usable.
func (a *app) loadHookLibrary() (map[string]*hooksets.Set, []error) {
	return hooksets.LoadLibrary(a.paths.HooksDir())
}

// applyHookSetFlags sets the profile's named and excluded hook sets and validates
// them against the library and the profile's harness and level (FR-024, FR-025,
// FR-027). setNamed/setExcl tell whether the flags were given; unchanged lists are
// still validated when the share level changes.
func (a *app) applyHookSetFlags(p *profile.Profile, def *harness.Definition, named, excluded []string, setNamed, setExcl, levelChanged bool) error {
	if setNamed {
		p.HookSets = named
	}
	if setExcl {
		p.ExcludeHookSets = excluded
	}
	// Validate only what this edit touches, so an unrelated edit is not blocked by
	// a set deleted from the library after it was attached (spec Edge Cases).
	if !setNamed && !setExcl && !levelChanged {
		return nil
	}
	return a.validateHookSets(p, def)
}

// validateHookSets checks a profile's hook-set lists: every name must exist in the
// library and have a body for the harness, the harness must support hook sets, and
// an all profile names no sets (it uses the default setup's hooks).
func (a *app) validateHookSets(p *profile.Profile, def *harness.Definition) error {
	if len(p.HookSets) == 0 && len(p.ExcludeHookSets) == 0 {
		return nil
	}
	if len(p.HookSets) > 0 && p.ShareLevel() == profile.ShareAll {
		return exitErrorf(ExitInvalidForLevel,
			"profile %s uses share level all, which uses the hooks of the default setup; hook sets need --share content or --share none", p.ID())
	}
	if def.Hooks == nil {
		return fmt.Errorf("harness %q does not declare a hook location, so it does not support hook sets", def.Name)
	}
	lib, _ := a.loadHookLibrary()
	for _, name := range p.HookSets {
		s, ok := lib[name]
		if !ok {
			return fmt.Errorf("hook set %q does not exist in %s (see: aprofile hook-sets list)", name, a.paths.HooksDir())
		}
		if err := hooksets.ValidateFor(s, def); err != nil {
			return err
		}
	}
	for _, name := range p.ExcludeHookSets {
		if _, ok := lib[name]; !ok {
			return fmt.Errorf("excluded hook set %q does not exist in %s", name, a.paths.HooksDir())
		}
	}
	return nil
}

// hookSetSummary renders the sets that apply to a profile with their reasons.
func hookSetSummary(lib map[string]*hooksets.Set, p *profile.Profile) []string {
	var out []string
	for _, name := range sortedSetNames(lib) {
		if r := hooksets.Reason(lib[name], p); r != "" {
			out = append(out, name+" ("+reasonText(r)+")")
		}
	}
	return out
}

func reasonText(r string) string {
	switch r {
	case "named":
		return "named by the profile"
	case "every-profile":
		return "applies to every profile"
	case "excluded":
		return "excluded by the profile"
	case "missing":
		return "missing from the library; its entries are left unchanged"
	case "failed-to-load":
		return "failed to load; its entries are left unchanged"
	}
	return r
}

func sortedSetNames(lib map[string]*hooksets.Set) []string { return hooksets.SortedNames(lib) }

func joinOr(list []string, empty string) string {
	if len(list) == 0 {
		return empty
	}
	return strings.Join(list, ", ")
}

func newHookSetsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hook-sets",
		Short: "Manage the hook-set library and the hooks it places into profiles",
	}
	cmd.AddCommand(newHookSetsListCmd(), newHookSetsShowCmd(), newHookSetsImportCmd(), newHookSetsReapplyCmd())
	return cmd
}

type hookSetInfoJSON struct {
	Name         string                   `json:"name"`
	Description  string                   `json:"description,omitempty"`
	EveryProfile bool                     `json:"every_profile"`
	Harnesses    []string                 `json:"harnesses"`
	File         string                   `json:"file"`
	Profiles     []hookSetProfileJSON     `json:"profiles"`
	Bodies       map[string]hooksets.Body `json:"bodies,omitempty"`
}

type hookSetProfileJSON struct {
	Profile string `json:"profile"`
	Reason  string `json:"reason"`
}

func (a *app) hookSetInfo(s *hooksets.Set, withBodies bool) (hookSetInfoJSON, error) {
	info := hookSetInfoJSON{Name: s.Name, Description: s.Description, EveryProfile: s.EveryProfile, Harnesses: s.Harnesses(), File: s.File, Profiles: []hookSetProfileJSON{}}
	if withBodies {
		info.Bodies = s.Bodies
	}
	profiles, _, err := a.store.List()
	if err != nil {
		return info, err
	}
	for _, p := range profiles {
		if r := hooksets.Reason(s, p); r != "" {
			info.Profiles = append(info.Profiles, hookSetProfileJSON{Profile: p.ID(), Reason: r})
		}
	}
	return info, nil
}

func newHookSetsListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List hook sets with the harnesses they cover and the profiles using them",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := newApp(cmd)
			if err != nil {
				return err
			}
			lib, errs := a.loadHookLibrary()
			out := struct {
				FormatVersion int               `json:"format_version"`
				Sets          []hookSetInfoJSON `json:"sets"`
				Errors        []string          `json:"errors,omitempty"`
			}{FormatVersion: schemaVersion, Sets: []hookSetInfoJSON{}}
			for _, e := range errs {
				out.Errors = append(out.Errors, e.Error())
			}
			for _, name := range sortedSetNames(lib) {
				if lib[name].LoadErr != nil {
					continue // reported in errors
				}
				info, err := a.hookSetInfo(lib[name], false)
				if err != nil {
					return err
				}
				out.Sets = append(out.Sets, info)
			}
			if asJSON {
				return writeJSON(a.out, out)
			}
			if len(out.Sets) == 0 {
				fmt.Fprintf(a.out, "no hook sets in %s (create one with: aprofile hook-sets import <harness> --name <set>)\n", a.paths.HooksDir())
			}
			for _, s := range out.Sets {
				every := ""
				if s.EveryProfile {
					every = ", every profile"
				}
				var users []string
				for _, p := range s.Profiles {
					users = append(users, p.Profile+" ("+reasonText(p.Reason)+")")
				}
				fmt.Fprintf(a.out, "%s  [%s%s]  %s\n", s.Name, strings.Join(s.Harnesses, ", "), every, s.Description)
				fmt.Fprintf(a.out, "    used by: %s\n", joinOr(users, "no profile"))
			}
			for _, e := range out.Errors {
				fmt.Fprintf(a.errOut, "aprofile: %s\n", e)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

func newHookSetsShowCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Show one hook set's bodies and the profiles it applies to",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp(cmd)
			if err != nil {
				return err
			}
			lib, _ := a.loadHookLibrary()
			s, ok := lib[args[0]]
			if !ok {
				return fmt.Errorf("hook set %q does not exist in %s", args[0], a.paths.HooksDir())
			}
			if s.LoadErr != nil {
				return s.LoadErr
			}
			info, err := a.hookSetInfo(s, true)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(a.out, struct {
					FormatVersion int `json:"format_version"`
					hookSetInfoJSON
				}{schemaVersion, info})
			}
			fmt.Fprintf(a.out, "%s  (%s)\n", s.Name, s.File)
			if s.Description != "" {
				fmt.Fprintf(a.out, "  description:   %s\n", s.Description)
			}
			fmt.Fprintf(a.out, "  every profile: %v\n", s.EveryProfile)
			for _, h := range s.Harnesses() {
				b := s.Bodies[h]
				if b.Files != nil {
					fmt.Fprintf(a.out, "  %s: files %s (in %s)\n", h, strings.Join(b.Files, ", "), s.FilesDir)
					continue
				}
				for _, ev := range b.EventNames() {
					fmt.Fprintf(a.out, "  %s: %s, %d group(s)\n", h, ev, len(b.Events[ev]))
				}
			}
			var users []string
			for _, p := range info.Profiles {
				users = append(users, p.Profile+" ("+reasonText(p.Reason)+")")
			}
			fmt.Fprintf(a.out, "  applies to:    %s\n", joinOr(users, "no profile"))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

func newHookSetsImportCmd() *cobra.Command {
	var name, description string
	var everyProfile bool
	cmd := &cobra.Command{
		Use:   "import <harness> --name <set>",
		Short: "Create a hook set from the hooks of a harness's default setup (the default setup is not modified)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp(cmd)
			if err != nil {
				return err
			}
			def, ok := a.set.Get(args[0])
			if !ok {
				return fmt.Errorf("unknown harness %q (run: aprofile harness list)", args[0])
			}
			if !profile.NameValid(name) {
				return fmt.Errorf("--name %q must match ^[a-z][a-z0-9-]{0,62}[a-z0-9]$", name)
			}
			s, err := hooksets.ImportFromDefault(def, a.paths.HooksDir(), name, description, everyProfile)
			if err != nil {
				return err
			}
			b := s.Bodies[def.Name]
			what := fmt.Sprintf("%d event(s)", len(b.Events))
			if b.Files != nil {
				what = fmt.Sprintf("%d file(s) copied to %s", len(b.Files), s.FilesDir)
			}
			fmt.Fprintf(a.out, "created hook set %s from the %s default setup (%s) in %s\n", s.Name, def.Name, what, s.File)
			fmt.Fprintf(a.out, "  attach it with: aprofile edit %s/<name> --hook-sets %s, then: aprofile sync\n", def.Name, s.Name)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "name of the new hook set")
	cmd.Flags().StringVar(&description, "description", "", "one-line description")
	cmd.Flags().BoolVar(&everyProfile, "every-profile", false, "apply the set to every content profile of the harness")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newHookSetsReapplyCmd() *cobra.Command {
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "reapply <harness>/<name>",
		Short: "Re-place the current entries of a profile's hook sets, replacing diverged ones after a backup",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHookSetsReapply(cmd, args[0], dryRun, yes)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "list the changes without making them")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm without asking (required off a terminal)")
	return cmd
}

func runHookSetsReapply(cmd *cobra.Command, ref string, dryRun, yes bool) error {
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
	def, ok := a.set.Get(h)
	if !ok {
		return fmt.Errorf("unknown harness %q", h)
	}
	if p.ShareLevel() == profile.ShareAll {
		return exitErrorf(ExitInvalidForLevel, "profile %s uses share level all, which uses the hooks of the default setup; there is nothing to reapply", p.ID())
	}
	if def.Hooks == nil {
		return fmt.Errorf("harness %q does not support hook sets", def.Name)
	}
	lib, _ := a.loadHookLibrary()
	recPath := a.paths.ManagedHooksPath(h, n)
	rec, err := hooksets.LoadRecord(recPath)
	if err != nil {
		return err
	}
	configDir := a.paths.ProfileConfigDir(h, n)
	hp, err := hooksets.PlanHooks(def, p, lib, rec, configDir, hooksets.PlanOptions{Reapply: true})
	if err != nil {
		return err
	}
	prefix := ""
	if dryRun {
		prefix = "(dry-run) "
	}
	fmt.Fprintf(a.out, "%sreapply hook sets for %s:\n", prefix, p.ID())
	printPlan(a, hp.Steps)
	for _, m := range hp.MissingSets {
		fmt.Fprintf(a.errOut, "aprofile: hook set %q is referenced but missing; its entries are left unchanged\n", m)
	}
	for _, m := range hp.InvalidSets {
		fmt.Fprintf(a.errOut, "aprofile: %s; its entries are left unchanged\n", m)
	}
	for _, d := range hp.Divergences {
		fmt.Fprintf(a.errOut, "aprofile: not re-placed: %s\n", d.String())
	}
	if dryRun || len(hp.Steps) == 0 {
		if len(hp.Steps) == 0 && !dryRun {
			fmt.Fprintln(a.out, "nothing to re-place")
		}
		return nil
	}
	// FR-028: reapply writes only after a confirmation (or --yes), and backs up
	// every existing file it changes first, diverged entries or not.
	err = confirmOrYes(cmd, fmt.Sprintf("Re-place the hook entries of %s (a backup is made first)?", p.ID()), yes)
	if err == errDeclined {
		fmt.Fprintln(a.out, "aborted; nothing changed")
		return nil
	}
	if err != nil {
		return err
	}
	var touched []string
	seen := map[string]bool{}
	for _, s := range hp.Steps {
		if !seen[s.Path] {
			seen[s.Path] = true
			touched = append(touched, s.Path)
		}
	}
	backup, err := backupPaths(a.paths, h, n, touched, time.Now())
	if err != nil {
		return err
	}
	if backup != "" {
		fmt.Fprintf(a.out, "  backed up to %s\n", backup)
	}
	if _, _, err := layer.Apply(hp.Steps, layer.ApplyOptions{Yes: true, Backup: func([]string) (string, error) {
		return backup, nil // taken above, before any step ran
	}}); err != nil {
		return err
	}
	if _, err := hp.Record.Save(recPath); err != nil {
		return err
	}
	if len(hp.Divergences) > 0 {
		fmt.Fprintf(a.out, "re-placed the hook sets of %s; %d entr(ies) could not be re-placed (see above)\n", p.ID(), len(hp.Divergences))
		return nil
	}
	fmt.Fprintf(a.out, "re-placed the hook sets of %s\n", p.ID())
	return nil
}
