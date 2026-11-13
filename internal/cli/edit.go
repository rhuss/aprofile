package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/hooksets"
	"github.com/cc-deck/aprofile/internal/launch"
	"github.com/cc-deck/aprofile/internal/layer"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/secret"
	"github.com/cc-deck/aprofile/internal/tui"
)

func newEditCmd() *cobra.Command {
	f := &profileFlags{}
	cmd := &cobra.Command{
		Use:   "edit <harness>/<name>",
		Short: "Update a profile's fields (does not write wrappers; run sync after)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEdit(cmd, args[0], f)
		},
	}
	f.bind(cmd)
	cmd.Flags().BoolVar(&f.noGroup, "no-group", false, "leave the profile's history group: its history becomes its own again (fresh and empty; the group's history is not changed)")
	cmd.MarkFlagsMutuallyExclusive("group", "no-group")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "with --share, --group or --no-group: list the changes without making them")
	cmd.Flags().BoolVar(&f.yes, "yes", false, "with --share, --group or --no-group: confirm changes that replace, move or set aside profile data (required off a terminal)")
	return cmd
}

func runEdit(cmd *cobra.Command, ref string, f *profileFlags) error {
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

	// edit with no field flags: open the form on a terminal (FR-043) or, off a
	// terminal, fail naming the flags and never block on a form (Scenario 9 AS3).
	if !anyProfileFlagChanged(cmd) {
		if !bothTTY(cmd) {
			return fmt.Errorf("edit %s: nothing to change; pass at least one of %s (or run from a terminal to use the form)",
				ref, strings.Join(editValueFlags, ", "))
		}
		v, ok, err := tui.RunProfileForm(os.Stdin, cmd.OutOrStdout(), profileFormConfig(a, def, formEditDefaults(p)))
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(a.out, "edit cancelled")
			return nil
		}
		return applyEditForm(a, cmd, p, def, v)
	}

	return applyEditFields(a, cmd, p, def, f, cmd.Flags().Changed)
}

// editValueFlags names the flags a non-interactive edit can change, used in the error
// message when edit is run off a terminal with nothing to change (Scenario 9 AS3).
var editValueFlags = []string{
	"--model", "--endpoint", "--auth-kind", "--auth-key", "--env", "--env-literal",
	"--color", "--icon", "--label", "--primary", "--herdr-agent",
	"--share", "--hook-sets", "--exclude-hook-sets", "--group", "--no-group",
}

// anyProfileFlagChanged reports whether the user set any value flag on an edit.
func anyProfileFlagChanged(cmd *cobra.Command) bool {
	for _, name := range []string{
		"model", "endpoint", "auth-kind", "auth-key", "env", "env-literal",
		"color", "icon", "label", "primary", "herdr-agent",
		"share", "hook-sets", "exclude-hook-sets", "group", "no-group",
	} {
		if cmd.Flags().Changed(name) {
			return true
		}
	}
	return false
}

// formEditDefaults builds the pre-fill flags for an edit form from the current profile,
// including the existing auth reference (a reference, never a secret value; FR-028).
func formEditDefaults(p *profile.Profile) *profileFlags {
	f := &profileFlags{
		label:      p.Label,
		color:      p.Color,
		icon:       p.Icon,
		model:      p.Model,
		endpoint:   p.Endpoint,
		authKind:   p.AuthKind,
		herdrAgent: p.HerdrAgent,
		primary:    p.Primary,

		share:           string(p.ShareLevel()),
		hookSets:        append([]string(nil), p.HookSets...),
		excludeHookSets: append([]string(nil), p.ExcludeHookSets...),
		group:           p.HistoryGroup,
	}
	// The form has one key field. Pre-fill the reference of the lexically first
	// variable so the choice is stable when a profile holds several.
	vars := make([]string, 0, len(p.AuthRefs))
	for v := range p.AuthRefs {
		vars = append(vars, v)
	}
	if len(vars) > 0 {
		sort.Strings(vars)
		f.authKey = p.AuthRefs[vars[0]].Raw
	}
	return f
}

// applyEditForm applies a form's collected values to an existing profile through the
// same code path the flag-driven edit uses (Constitution VI). Auth is applied only when
// the user chose a different kind or supplied a key, so leaving the key blank never
// clears existing credentials.
func applyEditForm(a *app, cmd *cobra.Command, p *profile.Profile, def *harness.Definition, v tui.FormValues) error {
	f := &profileFlags{}
	applyFormValues(f, v)
	f.noGroup = v.HistoryGroup == "" && p.HistoryGroup != ""
	changed := func(name string) bool {
		switch name {
		case "auth-key":
			return v.AuthKey != ""
		case "auth-kind":
			return v.AuthKind != "" && v.AuthKind != p.AuthKind
		case "env", "env-literal":
			return false // the form does not collect extra env
		case "share":
			return v.Share != "" && profile.Share(v.Share) != p.ShareLevel()
		case "hook-sets":
			return !sameStrings(v.HookSets, p.HookSets)
		case "exclude-hook-sets":
			return !sameStrings(v.ExcludeHookSets, p.ExcludeHookSets)
		case "group":
			return v.HistoryGroup != "" && v.HistoryGroup != p.HistoryGroup
		case "no-group":
			return v.HistoryGroup == "" && p.HistoryGroup != ""
		default:
			return true
		}
	}
	return applyEditFields(a, cmd, p, def, f, changed)
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as := append([]string(nil), a...)
	bs := append([]string(nil), b...)
	sortStrings(as)
	sortStrings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

// applyEditFields applies the "changed" value flags to an existing profile and saves
// it. Both edit paths (flags and form) call it; the caller's changed function decides
// which fields to touch.
func applyEditFields(a *app, cmd *cobra.Command, p *profile.Profile, def *harness.Definition, f *profileFlags, changed func(string) bool) error {
	oldLevel := p.ShareLevel()
	newLevel := oldLevel
	if changed("share") {
		lv, err := profile.ParseShare(f.share)
		if err != nil {
			return err
		}
		newLevel = lv
	}
	// --share with the current level re-applies that level's plan, which completes
	// the steps sync only reports (FR-017a).
	shareGiven := changed("share")
	// --no-group=false is no change (it never leaves the group).
	groupGiven := changed("group") || (changed("no-group") && f.noGroup)
	if f.dryRun && !shareGiven && !groupGiven {
		return fmt.Errorf("--dry-run previews a share level or history group change; pass --share (current: %s), --group or --no-group", oldLevel)
	}
	// FR-009: an unknown group, or a home join the default location cannot serve,
	// fails before anything changes (exit 2).
	if changed("group") && f.group != "" {
		if _, err := a.checkGroupJoin(def, f.group); err != nil {
			return err
		}
	}
	if changed("color") {
		if err := validateColor(f.color); err != nil {
			return err
		}
		p.Color = f.color
	}
	if changed("icon") {
		if err := validateIcon(f.icon); err != nil {
			return err
		}
		p.Icon = f.icon
	}
	if changed("label") {
		p.Label = f.label
	}
	if changed("model") {
		// FR-003: only settable for a harness whose definition can carry a model (a
		// model variable or launch argument). Error rather than store a dropped value.
		if f.model != "" && !def.CarriesModel() {
			return fmt.Errorf("harness %q has no model variable; --model is not supported for it", def.Name)
		}
		p.Model = f.model
	}
	if changed("endpoint") {
		if f.endpoint != "" && def.EndpointVar == "" {
			return fmt.Errorf("harness %q has no endpoint variable; --endpoint is not supported for it", def.Name)
		}
		p.Endpoint = f.endpoint
	}
	if changed("herdr-agent") {
		p.HerdrAgent = f.herdrAgent
	}
	if changed("auth-kind") || changed("auth-key") {
		if err := applyAuthEdit(p, def, f, changed); err != nil {
			return err
		}
	}
	for _, kv := range f.env {
		name, val, err := splitEnv(kv)
		if err != nil {
			return err
		}
		if !secret.IsReference(val) {
			if err := secret.ValidateValue(val, false); err != nil {
				return fmt.Errorf("--env %s: %w", name, err)
			}
		} else if _, err := secret.ParseReference(val); err != nil {
			return fmt.Errorf("--env %s: %w", name, err)
		}
		// Spec 006 FR-015: an extra env variable named like a feature's entry is
		// rejected in this direction too, naming the variable.
		if err := checkFeatureEnvCollision(p, def, name); err != nil {
			return fmt.Errorf("--env %s: %w", name, err)
		}
		upsertEnv(p, profile.ExtraEnvVar{Name: name, Value: val})
	}
	for _, kv := range f.envLiteral {
		name, val, err := splitEnv(kv)
		if err != nil {
			return err
		}
		if err := secret.ValidateValue(val, true); err != nil {
			return fmt.Errorf("--env-literal %s: %w", name, err)
		}
		if err := checkFeatureEnvCollision(p, def, name); err != nil {
			return fmt.Errorf("--env-literal %s: %w", name, err)
		}
		upsertEnv(p, profile.ExtraEnvVar{Name: name, Value: val, Literal: true})
	}
	p.Share = newLevel
	if shareGiven {
		p.ShareImplicit = false // the user set the level: it is stored from now on (FR-033)
	}
	// Spec 006 FR-014: a profile at share level all carries no feature entries;
	// the move clears the feature state so none is left stranded as unrepairable
	// drift (sync removes the orphaned placed entries through normal record
	// maintenance).
	var clearedFeatures []string
	if newLevel == profile.ShareAll && len(p.Features) > 0 {
		for _, fs := range p.Features {
			clearedFeatures = append(clearedFeatures, fs.Feature)
		}
		p.Features = nil
	}
	if err := a.applyHookSetFlags(p, def, f.hookSets, f.excludeHookSets, changed("hook-sets"), changed("exclude-hook-sets"), newLevel != oldLevel); err != nil {
		return err
	}
	// Share level and history group are independent; with both, the share change
	// runs first (contracts/cli.md). The group change's confirmation is resolved
	// before the share change writes anything, so exit 3 changes nothing (FR-013).
	groupTo := f.group
	if changed("no-group") && f.noGroup {
		groupTo = ""
	}
	groupYes := f.yes
	if shareGiven && groupGiven && !f.dryRun {
		confirmed, ok, err := a.confirmGroupFirst(cmd, p, def, groupTo, f.yes)
		if err != nil || !ok {
			return err
		}
		groupYes = groupYes || confirmed
	}
	if shareGiven {
		// With a group change in the same edit, the history entries belong to the
		// group change alone: the share plan leaves them untouched, so the join is
		// planned against the same entries in the dry run and in the real run
		// (FR-013, SC-005).
		done, err := a.changeShare(cmd, p, def, oldLevel, newLevel, f.dryRun, f.yes, groupGiven)
		if err != nil {
			return err
		}
		if !done && !f.dryRun {
			return nil
		}
		if done && groupGiven {
			// The share change is applied: record it before the group change, which
			// may still fail or be declined.
			if err := a.store.Save(p); err != nil {
				return err
			}
		}
	}
	if groupGiven {
		done, err := a.changeGroup(cmd, p, def, groupTo, f.dryRun, groupYes, shareGiven)
		if err != nil || !done {
			return err
		}
	}
	if f.dryRun {
		return nil
	}
	if changed("primary") {
		p.Primary = f.primary
		if p.Primary {
			if err := clearOtherPrimaries(a, p); err != nil {
				return err
			}
		}
	}

	if err := a.store.Save(p); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "updated profile %s (run: aprofile sync)\n", p.ID())
	if newLevel != oldLevel {
		fmt.Fprintf(a.out, "  share level: %s -> %s: %s\n", oldLevel, newLevel, levelDescription(newLevel))
		fmt.Fprintln(a.out, "  the wrapper and the registry are updated by: aprofile sync")
	}
	if len(clearedFeatures) > 0 {
		fmt.Fprintf(a.out, "  features disabled: %s (share level all carries no feature entries; their placed entries are removed by the next sync)\n", strings.Join(clearedFeatures, ", "))
	}
	return nil
}

// changeShare applies the config dir part of a share level change as one operation
// (FR-016, FR-017, FR-017a): it plans with the same planner sync uses, prints the
// plan, refuses while the profile runs, asks once (or requires --yes) when a step
// replaces profile data, backs that data up, and applies exactly the listed steps.
// It returns done=false when nothing should be saved (dry run or declined).
func (a *app) changeShare(cmd *cobra.Command, p *profile.Profile, def *harness.Definition, from, to profile.Share, dryRun, yes, skipHistory bool) (bool, error) {
	configDir := a.paths.ProfileConfigDir(p.Harness, p.Name)
	// A member's history links stay as they are: membership is independent of the
	// share level (FR-012, FR-018). skipHistory leaves every history entry to a
	// group change of the same edit.
	po := a.historyPlanOptions(def, p)
	if skipHistory {
		// The group change owns every link into the group store, including those
		// of entries that are no longer history (it detaches them), so each is
		// listed once and dry run and real run list the same steps.
		po.SkipHistory = true
	}
	steps, err := layer.PlanWith(def, to, "", configDir, po)
	if err != nil {
		return false, err
	}
	replaces := false
	for _, s := range steps {
		if s.Confirm {
			replaces = true
		}
	}
	header := fmt.Sprintf("share level %s -> %s for %s:", from, to, p.ID())
	if dryRun {
		header = "(dry-run) " + header
	}
	fmt.Fprintln(a.out, header)
	printPlan(a, steps)
	lib, _ := a.loadHookLibrary()
	recPath := a.paths.ManagedHooksPath(p.Harness, p.Name)
	rec, err := hooksets.LoadRecord(recPath)
	if err != nil {
		return false, err
	}
	// The hook steps come from the same planner the applied change runs afterwards
	// (SyncProfile), evaluated against the hook location as the level steps leave
	// it, so the dry run lists exactly what is applied (FR-016, SC-006).
	preview, err := hooksets.PreviewAfter(def, p, lib, rec, configDir, steps)
	if err != nil {
		return false, err
	}
	for _, s := range preview.Steps {
		fmt.Fprintln(a.out, "  "+s.String())
	}
	// Spec 004 FR-008a: what the level change means for the launch settings the
	// profile folder carries.
	launchEffects, linkedNow := launchLevelEffects(def, p, from, to)
	for _, l := range launchEffects {
		fmt.Fprintln(a.out, "  "+l)
	}
	if replaces {
		fmt.Fprintln(a.out, "profile data replaced: yes (backed up first; marked [replaces profile data])")
	} else {
		fmt.Fprintln(a.out, "profile data replaced: no; nothing is deleted")
	}
	if dryRun {
		return false, nil
	}

	proc, running, known := profileRunning(configDir)
	if running {
		return false, exitErrorf(ExitProfileRunning, "profile %s is in use by a running process (pid %d %s); stop it before changing its share level", p.ID(), proc.PID, proc.Name)
	}
	if !known {
		err := confirmOrYes(cmd, fmt.Sprintf("cannot tell whether profile %s is running; continue only if it is stopped?", p.ID()), yes)
		if err == errDeclined {
			fmt.Fprintln(a.out, "aborted; nothing changed")
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
	if replaces {
		err := confirmOrYes(cmd, fmt.Sprintf("Replace the profile data marked above for %s (a backup is made first)?", p.ID()), yes)
		if err == errDeclined {
			fmt.Fprintln(a.out, "aborted; nothing changed")
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
	applied, _, err := layer.Apply(steps, layer.ApplyOptions{Yes: true, Backup: func(paths []string) (string, error) {
		return backupPaths(a.paths, p.Harness, p.Name, paths, time.Now())
	}})
	if err != nil {
		return false, err
	}
	for _, s := range applied {
		if s.Backup != "" {
			fmt.Fprintf(a.out, "  backed up %s to %s\n", s.Entry, s.Backup)
		}
	}
	// The hook-set part of the level change (FR-017a): place the sets that now
	// apply and remove the managed entries of those that no longer do.
	if def.Hooks != nil {
		hp, hookApplied, err := hooksets.SyncProfile(def, p, lib, recPath, configDir, false)
		if err != nil {
			return false, err
		}
		if !sameSteps(preview.Steps, hookApplied) {
			// Only possible if the profile changed between plan and apply.
			fmt.Fprintln(a.errOut, "aprofile: warning: the applied hook changes differ from the listed plan:")
			for _, s := range hookApplied {
				fmt.Fprintln(a.errOut, "  "+s.String())
			}
		}
		for _, d := range hp.Divergences {
			fmt.Fprintf(a.errOut, "aprofile: note: %s\n", d.String())
		}
		for _, m := range append(hp.MissingSets, hp.InvalidSets...) {
			fmt.Fprintf(a.errOut, "aprofile: warning: hook set %s\n", m)
		}
	}
	// A file that is now a link holds no aprofile-owned entries: drop them from the
	// launch record (spec 004 FR-008a).
	// The level change is already applied here, so a record that cannot be updated
	// is a warning, not a failure. A readable record is cleaned by the next sync
	// anyway (the planner never records a linked file); an unreadable one makes sync
	// fail for the profile until it is removed, so the warning names it.
	if len(linkedNow) > 0 {
		recPath := a.paths.ManagedLaunchPath(p.Harness, p.Name)
		if err := dropLaunchRecordFiles(recPath, linkedNow); err != nil {
			fmt.Fprintf(a.errOut, "aprofile: warning: profile %s: launch record not updated: %v; fix or remove %s, then run: aprofile sync\n", p.ID(), err, recPath)
		}
	}
	return true, nil
}

// launchLevelEffects returns the plan lines for the launch settings of a share level
// change and the folder_launch files that become links (spec 004 FR-008a). A
// profile without launch settings gets no lines, unless the folder neutralizes the
// auth-clearing list for it (spec 005 FR-001: every profile with a folder env
// location and its own settings file carries those entries, launch settings or not).
func launchLevelEffects(def *harness.Definition, p *profile.Profile, from, to profile.Share) (lines, linkedNow []string) {
	// Spec 006 FR-014: moving to share level all clears the feature state, so no
	// state is left stranded as unrepairable drift; the level plan says so.
	if to == profile.ShareAll && len(p.Features) > 0 {
		var names []string
		for _, fs := range p.Features {
			names = append(names, fs.Feature)
		}
		lines = append(lines, fmt.Sprintf("features disabled: %s (a profile at share level all carries no feature entries)", strings.Join(names, ", ")))
	}
	if def.FolderLaunch == nil {
		return lines, nil
	}
	neutralizes := def.FolderLaunch.Env != "" && len(def.UnsetAuthVars()) > 0
	if !neutralizes && len(launch.Decide(p, def, launch.Doc{Files: map[string]launch.FileDoc{}})) == 0 {
		return nil, nil
	}
	linkedAt := func(rule *harness.EntryRule, level profile.Share) bool {
		return rule.Layer.Borrowable() && def.Treatment(rule.Layer, string(level)) == harness.TreatLink
	}
	seen := map[string]bool{}
	for _, f := range []string{def.FolderLaunch.EnvFile(), keyCommandFile(def)} {
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		for i := range def.Classification {
			r := &def.Classification[i]
			if r.IsGlob() || r.Path() != f {
				continue
			}
			switch was, now := linkedAt(r, from), linkedAt(r, to); {
			case !was && now:
				lines = append(lines, fmt.Sprintf("launch settings become wrapper-only (%s is linked)", f))
				linkedNow = append(linkedNow, f)
			case was && !now:
				lines = append(lines, fmt.Sprintf("launch settings will be placed into %s by the next sync", f))
			}
		}
	}
	return lines, linkedNow
}

func keyCommandFile(def *harness.Definition) string {
	if def.FolderLaunch == nil || def.FolderLaunch.KeyCommand == nil {
		return ""
	}
	return def.FolderLaunch.KeyCommand.File()
}

// dropLaunchRecordFiles removes the record entries of the given files.
func dropLaunchRecordFiles(path string, files []string) error {
	return launch.UpdateRecord(path, func(rec *launch.Record) error {
		drop := map[string]bool{}
		for _, f := range files {
			drop[f] = true
		}
		kept := rec.Entries[:0]
		for _, e := range rec.Entries {
			if !drop[e.File] {
				kept = append(kept, e)
			}
		}
		rec.Entries = kept
		return nil
	})
}

// sameSteps reports whether two plans list the same steps, in order.
func sameSteps(a, b []layer.Step) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].String() != b[i].String() {
			return false
		}
	}
	return true
}

// profileRunning is a variable so tests can simulate a running profile.
var profileRunning = profile.RunningProcess

// printPlan prints a change plan, one step per line.
func printPlan(a *app, steps []layer.Step) {
	if len(steps) == 0 {
		fmt.Fprintln(a.out, "  (no config dir changes)")
	}
	for _, s := range steps {
		line := "  " + s.String()
		if s.Confirm {
			line += " [replaces profile data]"
		}
		fmt.Fprintln(a.out, line)
	}
}

func applyAuthEdit(p *profile.Profile, def *harness.Definition, f *profileFlags, changed func(string) bool) error {
	kindName := p.AuthKind
	if changed("auth-kind") {
		kindName = f.authKind
	}
	kind, ok := def.AuthKind(kindName)
	if !ok {
		return fmt.Errorf("harness %q has no auth kind %q", def.Name, kindName)
	}
	p.AuthKind = kindName
	vars := kind.VarNames()
	if changed("auth-key") {
		if f.authKey == "" {
			p.AuthRefs = nil
			return nil
		}
		refObj, err := secret.ParseAuthReference(f.authKey)
		if err != nil {
			return fmt.Errorf("--auth-key: %w", err)
		}
		if len(vars) == 0 {
			return fmt.Errorf("auth kind %q takes no key", kindName)
		}
		if len(vars) > 1 {
			return fmt.Errorf("auth kind %q sets multiple variables; set them with --env", kindName)
		}
		p.AuthRefs = map[string]secret.Reference{vars[0]: refObj}
		return nil
	}
	// The kind changed without a key: keep only the references of variables the new
	// kind sets, so a reference of the old kind never lingers in the profile (FR-001).
	var kept map[string]secret.Reference
	for _, v := range vars {
		if ref, ok := p.AuthRefs[v]; ok {
			if kept == nil {
				kept = map[string]secret.Reference{}
			}
			kept[v] = ref
		}
	}
	p.AuthRefs = kept
	if len(vars) == 1 && len(kept) == 0 {
		return fmt.Errorf("auth kind %q needs a key: pass --auth-key env:NAME (or file:/abs/path) together with --auth-kind", kindName)
	}
	return nil
}

func upsertEnv(p *profile.Profile, v profile.ExtraEnvVar) {
	for i := range p.ExtraEnv {
		if p.ExtraEnv[i].Name == v.Name {
			p.ExtraEnv[i] = v
			return
		}
	}
	p.ExtraEnv = append(p.ExtraEnv, v)
}

func newRenameCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "rename <harness>/<name> <new-name>",
		Short: "Rename a profile, moving its file and config dir together",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRename(cmd, args[0], args[1], force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "rename even if a harness started for this profile is running, or when that cannot be checked")
	return cmd
}

func runRename(cmd *cobra.Command, ref, newName string, force bool) error {
	a, err := newApp(cmd)
	if err != nil {
		return err
	}
	h, n, err := parseProfileRef(ref)
	if err != nil {
		return err
	}
	if !harness.NameValid(newName) {
		return fmt.Errorf("new name %q must match ^[a-z][a-z0-9-]{0,62}[a-z0-9]$", newName)
	}
	p, err := a.store.Load(h, n)
	if err != nil {
		return err
	}
	if a.store.Exists(h, newName) {
		return fmt.Errorf("profile %s/%s already exists", h, newName)
	}
	// FR-002: the new wrapper name must not be another profile's.
	if err := checkWrapperClash(a, h, newName, p.ID()); err != nil {
		return err
	}

	oldConfigDir := a.paths.ProfileConfigDir(h, n)
	if !force {
		proc, running, known := profileRunning(oldConfigDir)
		if running {
			return exitErrorf(ExitProfileRunning, "profile %s is in use by a running harness (pid %d %s); stop it or pass --force", p.ID(), proc.PID, proc.Name)
		}
		if !known {
			// The platform cannot tell (FR-006a): ask, as a share level change does.
			err := confirmOrYes(cmd, fmt.Sprintf("cannot tell whether profile %s is running; continue only if it is stopped?", p.ID()), false)
			if err == errDeclined {
				fmt.Fprintln(a.out, "aborted; nothing changed")
				return nil
			}
			if err != nil {
				return exitErrorf(ExitNeedsConfirmation, "cannot tell whether profile %s is running; stop it and re-run with --force (nothing changed)", p.ID())
			}
		}
	}

	newConfigDir := a.paths.ProfileConfigDir(h, newName)
	oldRecord, newRecord := a.paths.ManagedHooksPath(h, n), a.paths.ManagedHooksPath(h, newName)
	oldLaunch, newLaunch := a.paths.ManagedLaunchPath(h, n), a.paths.ManagedLaunchPath(h, newName)
	oldCreds, newCreds := a.paths.CredentialsDir(h, n), a.paths.CredentialsDir(h, newName)
	// Checked before anything moves, so a refused rename changes nothing.
	if _, err := os.Stat(newRecord); err == nil {
		return fmt.Errorf("a managed-hooks record already exists at %s; remove it if no profile uses it", newRecord)
	}
	if _, err := os.Stat(newLaunch); err == nil {
		return fmt.Errorf("a managed-launch record already exists at %s; remove it if no profile uses it", newLaunch)
	}
	if layer.DirExists(oldConfigDir) && layer.DirExists(newConfigDir) {
		return fmt.Errorf("a config dir already exists at %s", newConfigDir)
	}
	if layer.DirExists(oldCreds) && layer.DirExists(newCreds) {
		return fmt.Errorf("an imported credentials dir already exists at %s; remove it if no profile uses it", newCreds)
	}
	// The set-aside history of a history group join belongs to the profile and
	// moves with it (spec 003, FR-019).
	oldAside, newAside := a.paths.SetAsideDir(h, n), a.paths.SetAsideDir(h, newName)
	if _, err := os.Lstat(newAside); err == nil {
		return fmt.Errorf("set-aside history already exists at %s; move it away if no profile uses it", newAside)
	}
	// Move the config dir if it exists.
	if layer.DirExists(oldConfigDir) {
		if err := os.MkdirAll(filepath.Dir(newConfigDir), 0o755); err != nil {
			return err
		}
		if err := os.Rename(oldConfigDir, newConfigDir); err != nil {
			return fmt.Errorf("move config dir: %w", err)
		}
	}
	// The managed-hooks record belongs to the profile: it moves with it, so the next
	// sync still knows which hook entries it placed (FR-028).
	if err := moveRecord(oldRecord, newRecord); err != nil {
		return err
	}
	// So does the record of the launch entries it placed (spec 004, research R6).
	if err := launch.Move(oldLaunch, newLaunch); err != nil {
		return fmt.Errorf("move managed-launch record: %w", err)
	}
	// The credential files import copied for this profile move with it, and the
	// profile's references into that dir follow (FR-006a).
	if layer.DirExists(oldCreds) {
		if err := os.MkdirAll(filepath.Dir(newCreds), 0o700); err != nil {
			return err
		}
		if err := os.Rename(oldCreds, newCreds); err != nil {
			return fmt.Errorf("move imported credentials dir: %w", err)
		}
	}
	if err := rewriteCredentialPaths(p, oldCreds, newCreds); err != nil {
		return err
	}
	if layer.DirExists(oldAside) {
		if err := os.MkdirAll(filepath.Dir(newAside), 0o755); err != nil {
			return err
		}
		if err := os.Rename(oldAside, newAside); err != nil {
			return fmt.Errorf("move set-aside history: %w", err)
		}
	}
	// Move the store file (write new, remove old).
	p.Name = newName
	if err := a.store.Save(p); err != nil {
		return err
	}
	if err := a.store.Remove(h, n); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "renamed %s/%s to %s/%s; %s (run: aprofile sync)\n", h, n, h, newName, oldWrapperNote(a, h, n))
	return nil
}

// rewriteCredentialPaths points every reference and path value of p that lies in
// the old imported credentials dir at the same file in the new one: file: auth
// references, file: extra variables, and the plain path values import writes for a
// path variable (for example a credentials-file variable). Other values are left
// alone.
func rewriteCredentialPaths(p *profile.Profile, oldDir, newDir string) error {
	move := func(path string) (string, bool) {
		rel, err := filepath.Rel(oldDir, filepath.Clean(path))
		if err != nil || !filepath.IsAbs(path) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", false
		}
		return filepath.Join(newDir, rel), true
	}
	for v, ref := range p.AuthRefs {
		if ref.Kind != secret.KindFile {
			continue
		}
		if np, ok := move(ref.Value); ok {
			nr, err := secret.ParseReference("file:" + np)
			if err != nil {
				return fmt.Errorf("rewrite %s reference: %w", v, err)
			}
			p.AuthRefs[v] = nr
		}
	}
	for i, e := range p.ExtraEnv {
		val, prefix := e.Value, ""
		if strings.HasPrefix(val, "file:") {
			val, prefix = strings.TrimPrefix(val, "file:"), "file:"
		} else if secret.IsReference(val) {
			continue
		}
		if np, ok := move(val); ok {
			p.ExtraEnv[i].Value = prefix + np
		}
	}
	return nil
}

// moveRecord moves a profile's managed-hooks record to its new name, if there is
// one. The caller has checked that no record exists at the target.
func moveRecord(from, to string) error {
	if _, err := os.Stat(from); os.IsNotExist(err) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	if err := os.Rename(from, to); err != nil {
		return fmt.Errorf("move managed-hooks record: %w", err)
	}
	return nil
}

func newRemoveCmd() *cobra.Command {
	var purge, yes bool
	cmd := &cobra.Command{
		Use:   "remove <harness> <name>",
		Short: "Remove a profile (config dir retained unless --purge)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRemove(cmd, args[0], args[1], purge, yes)
		},
	}
	cmd.Flags().BoolVar(&purge, "purge", false, "also delete the profile's config dir (credentials and state) and its imported credential files")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt for --purge")
	return cmd
}

func runRemove(cmd *cobra.Command, h, n string, purge, yes bool) error {
	if err := validateProfileID(h, n); err != nil {
		return err
	}
	a, err := newApp(cmd)
	if err != nil {
		return err
	}
	if !a.store.Exists(h, n) {
		return fmt.Errorf("profile %s/%s does not exist", h, n)
	}
	configDir := a.paths.ProfileConfigDir(h, n)
	credsDir := a.paths.CredentialsDir(h, n)

	if purge {
		var extra []string
		if layer.DirExists(credsDir) {
			extra = append(extra, "the imported credential files in "+credsDir)
		}
		if layer.DirExists(a.paths.SetAsideDir(h, n)) {
			extra = append(extra, "the set-aside history in "+a.paths.SetAsideDir(h, n))
		}
		prompt := fmt.Sprintf("Delete config dir %s including credentials and state?", configDir)
		if len(extra) > 0 {
			prompt = fmt.Sprintf("Delete config dir %s including credentials and state, and %s?", configDir, strings.Join(extra, " and "))
		}
		err := confirmOrYes(cmd, prompt, yes)
		if err == errDeclined {
			fmt.Fprintln(a.out, "aborted")
			return nil
		}
		if err != nil {
			return exitErrorf(ExitNeedsConfirmation, "--purge deletes %s (credentials and state); re-run with --yes to confirm", configDir)
		}
	}

	if err := a.store.Remove(h, n); err != nil {
		return err
	}
	if purge {
		if err := os.RemoveAll(configDir); err != nil {
			return fmt.Errorf("purge config dir: %w", err)
		}
		// The credential files import copied for this profile (FR-004: deleted only
		// with the confirmed purge).
		if err := os.RemoveAll(credsDir); err != nil {
			return fmt.Errorf("purge imported credentials dir: %w", err)
		}
		// The profile's set-aside history (spec 003, FR-019). os.RemoveAll of the
		// config dir removes a member's history links themselves and never follows
		// them, so no group store and nothing in the default setup is touched.
		if err := os.RemoveAll(a.paths.SetAsideDir(h, n)); err != nil {
			return fmt.Errorf("purge set-aside history: %w", err)
		}
		// The entries the record lists went with the config dir; a stale record
		// would let a later profile of the same name claim the user's own groups.
		if err := os.Remove(a.paths.ManagedHooksPath(h, n)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove managed-hooks record: %w", err)
		}
		if err := launch.Delete(a.paths.ManagedLaunchPath(h, n)); err != nil {
			return fmt.Errorf("remove managed-launch record: %w", err)
		}
		fmt.Fprintf(a.out, "removed profile %s/%s and purged its config dir; %s (run: aprofile sync)\n", h, n, oldWrapperNote(a, h, n))
	} else {
		fmt.Fprintf(a.out, "removed profile %s/%s; config dir retained at %s; %s (run: aprofile sync)\n", h, n, configDir, oldWrapperNote(a, h, n))
	}
	return nil
}

// oldWrapperNote says that a removed or renamed profile's wrapper stays until the
// next sync removes it (FR-006b: only sync writes or removes wrappers).
func oldWrapperNote(a *app, h, n string) string {
	if def, ok := a.set.Get(h); ok {
		return fmt.Sprintf("its wrapper %s is removed by the next aprofile sync", def.WrapperName(n))
	}
	return "its old wrapper is removed by the next aprofile sync"
}

func confirm(cmd *cobra.Command, prompt string) bool {
	fmt.Fprintf(cmd.OutOrStdout(), "%s [y/N] ", prompt)
	r := bufio.NewReader(cmd.InOrStdin())
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}
