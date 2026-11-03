package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/harness"
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
		v, ok, err := tui.RunProfileForm(os.Stdin, cmd.OutOrStdout(), profileFormConfig(def, formEditDefaults(p)))
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(a.out, "edit cancelled")
			return nil
		}
		return applyEditForm(a, p, def, v)
	}

	return applyEditFields(a, p, def, f, cmd.Flags().Changed)
}

// editValueFlags names the flags a non-interactive edit can change, used in the error
// message when edit is run off a terminal with nothing to change (Scenario 9 AS3).
var editValueFlags = []string{
	"--model", "--endpoint", "--auth-kind", "--auth-key", "--env", "--env-literal",
	"--color", "--icon", "--label", "--primary", "--herdr-agent",
}

// anyProfileFlagChanged reports whether the user set any value flag on an edit.
func anyProfileFlagChanged(cmd *cobra.Command) bool {
	for _, name := range []string{
		"model", "endpoint", "auth-kind", "auth-key", "env", "env-literal",
		"color", "icon", "label", "primary", "herdr-agent",
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
func applyEditForm(a *app, p *profile.Profile, def *harness.Definition, v tui.FormValues) error {
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
		default:
			return true
		}
	}
	return applyEditFields(a, p, def, f, changed)
}

// applyEditFields applies the "changed" value flags to an existing profile and saves
// it. Both edit paths (flags and form) call it; the caller's changed function decides
// which fields to touch.
func applyEditFields(a *app, p *profile.Profile, def *harness.Definition, f *profileFlags, changed func(string) bool) error {
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
	return nil
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
	if !force && profile.Running(oldConfigDir) {
		return fmt.Errorf("profile %s is in use by a running harness; stop it or pass --force", p.ID())
	}

	newConfigDir := a.paths.ProfileConfigDir(h, newName)
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
		if !yes {
			if !isTTY(os.Stdin) {
				return fmt.Errorf("--purge deletes %s (credentials and state); re-run with --yes to confirm", configDir)
			}
			if !confirm(cmd, fmt.Sprintf("Delete config dir %s including credentials and state?", configDir)) {
				fmt.Fprintln(a.out, "aborted")
				return nil
			}
		}
	}

	if err := a.store.Remove(h, n); err != nil {
		return err
	}
	if purge {
		if err := os.RemoveAll(configDir); err != nil {
			return fmt.Errorf("purge config dir: %w", err)
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
