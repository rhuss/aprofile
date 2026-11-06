package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/hooksets"
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
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "with --share: list the config dir changes without making them")
	cmd.Flags().BoolVar(&f.yes, "yes", false, "with --share: confirm changes that replace profile data (required off a terminal)")
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
	"--share", "--hook-sets", "--exclude-hook-sets",
}

// anyProfileFlagChanged reports whether the user set any value flag on an edit.
func anyProfileFlagChanged(cmd *cobra.Command) bool {
	for _, name := range []string{
		"model", "endpoint", "auth-kind", "auth-key", "env", "env-literal",
		"color", "icon", "label", "primary", "herdr-agent",
		"share", "hook-sets", "exclude-hook-sets",
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
	}
	for _, ref := range p.AuthRefs {
		f.authKey = ref.Raw
		break
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
	if f.dryRun && !shareGiven {
		return fmt.Errorf("--dry-run previews a share level change; pass --share (current: %s)", oldLevel)
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
		// FR-003: only settable for a harness whose definition declares a model var
		// (see research.md). Error rather than store a value the wrapper would drop.
		if f.model != "" && def.ModelVar == "" {
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
		upsertEnv(p, profile.ExtraEnvVar{Name: name, Value: val, Literal: true})
	}
	p.Share = newLevel
	if shareGiven {
		p.ShareImplicit = false // the user set the level: it is stored from now on (FR-033)
	}
	if err := a.applyHookSetFlags(p, def, f.hookSets, f.excludeHookSets, changed("hook-sets"), changed("exclude-hook-sets"), newLevel != oldLevel); err != nil {
		return err
	}
	if shareGiven {
		done, err := a.changeShare(cmd, p, def, oldLevel, newLevel, f.dryRun, f.yes)
		if err != nil || !done {
			return err
		}
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
	return nil
}

// changeShare applies the config dir part of a share level change as one operation
// (FR-016, FR-017, FR-017a): it plans with the same planner sync uses, prints the
// plan, refuses while the profile runs, asks once (or requires --yes) when a step
// replaces profile data, backs that data up, and applies exactly the listed steps.
// It returns done=false when nothing should be saved (dry run or declined).
func (a *app) changeShare(cmd *cobra.Command, p *profile.Profile, def *harness.Definition, from, to profile.Share, dryRun, yes bool) (bool, error) {
	configDir := a.paths.ProfileConfigDir(p.Harness, p.Name)
	steps, err := layer.Plan(def, to, "", configDir)
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
	return true, nil
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
	if changed("auth-key") {
		if f.authKey == "" {
			p.AuthRefs = nil
			return nil
		}
		refObj, err := secret.ParseAuthReference(f.authKey)
		if err != nil {
			return fmt.Errorf("--auth-key: %w", err)
		}
		vars := kind.VarNames()
		if len(vars) == 0 {
			return fmt.Errorf("auth kind %q takes no key", kindName)
		}
		if len(vars) > 1 {
			return fmt.Errorf("auth kind %q sets multiple variables; set them with --env", kindName)
		}
		p.AuthRefs = map[string]secret.Reference{vars[0]: refObj}
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
	cmd.Flags().BoolVar(&force, "force", false, "rename even if a harness started for this profile is running")
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

	oldConfigDir := a.paths.ProfileConfigDir(h, n)
	if _, running, _ := profileRunning(oldConfigDir); !force && running {
		return fmt.Errorf("profile %s is in use by a running harness; stop it or pass --force", p.ID())
	}

	newConfigDir := a.paths.ProfileConfigDir(h, newName)
	oldRecord, newRecord := a.paths.ManagedHooksPath(h, n), a.paths.ManagedHooksPath(h, newName)
	if _, err := os.Stat(newRecord); err == nil {
		// Checked before anything moves, so a refused rename changes nothing.
		return fmt.Errorf("a managed-hooks record already exists at %s; remove it if no profile uses it", newRecord)
	}
	// Move the config dir if it exists.
	if dirExists(oldConfigDir) {
		if dirExists(newConfigDir) {
			return fmt.Errorf("a config dir already exists at %s", newConfigDir)
		}
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
	// Move the store file (write new, remove old).
	p.Name = newName
	if err := a.store.Save(p); err != nil {
		return err
	}
	if err := a.store.Remove(h, n); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "renamed %s/%s to %s/%s (run: aprofile sync)\n", h, n, h, newName)
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
	cmd.Flags().BoolVar(&purge, "purge", false, "also delete the profile's config dir (credentials and state)")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt for --purge")
	return cmd
}

func runRemove(cmd *cobra.Command, h, n string, purge, yes bool) error {
	a, err := newApp(cmd)
	if err != nil {
		return err
	}
	if !a.store.Exists(h, n) {
		return fmt.Errorf("profile %s/%s does not exist", h, n)
	}
	configDir := a.paths.ProfileConfigDir(h, n)

	if purge {
		err := confirmOrYes(cmd, fmt.Sprintf("Delete config dir %s including credentials and state?", configDir), yes)
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
		// The entries the record lists went with the config dir; a stale record
		// would let a later profile of the same name claim the user's own groups.
		if err := os.Remove(a.paths.ManagedHooksPath(h, n)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove managed-hooks record: %w", err)
		}
		fmt.Fprintf(a.out, "removed profile %s/%s and purged its config dir (run: aprofile sync)\n", h, n)
	} else {
		fmt.Fprintf(a.out, "removed profile %s/%s; config dir retained at %s (run: aprofile sync)\n", h, n, configDir)
	}
	return nil
}

func confirm(cmd *cobra.Command, prompt string) bool {
	fmt.Fprintf(cmd.OutOrStdout(), "%s [y/N] ", prompt)
	r := bufio.NewReader(cmd.InOrStdin())
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}
