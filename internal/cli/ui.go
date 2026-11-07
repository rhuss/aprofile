package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/layer"
	"github.com/cc-deck/aprofile/internal/tui"
)

func newUICmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Browse and edit profiles interactively (add, edit, copy, remove)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUI(cmd)
		},
	}
	return cmd
}

// runUI drives the interactive browse view. Every action it offers routes through the
// same operations the non-interactive commands use (Constitution VI, SC-008): it never
// touches the store, wrappers or registry directly. It requires a terminal; off one it
// errors and points at the non-interactive commands (FR-043).
func runUI(cmd *cobra.Command) error {
	if !bothTTY(cmd) {
		return fmt.Errorf("aprofile ui requires an interactive terminal; use the commands (add, edit, copy, remove, list) instead")
	}
	for {
		a, err := newApp(cmd)
		if err != nil {
			return err
		}
		rows, err := a.browseRows()
		if err != nil {
			return err
		}
		res, err := tui.RunBrowse(os.Stdin, cmd.OutOrStdout(), rows, colorEnabled(cmd.OutOrStdout()))
		if err != nil {
			return err
		}
		switch res.Action {
		case tui.ActionQuit:
			return nil
		case tui.ActionAdd:
			reportUIErr(a, uiAdd(cmd, a))
		case tui.ActionEdit:
			if res.Selected != nil {
				reportUIErr(a, uiEdit(cmd, a, res.Selected))
			}
		case tui.ActionCopy:
			reportUIErr(a, uiCopy(cmd, a, res.Selected))
		case tui.ActionRemove:
			if res.Selected != nil {
				reportUIErr(a, uiRemove(cmd, res.Selected))
			}
		}
	}
}

func reportUIErr(a *app, err error) {
	if err != nil {
		fmt.Fprintf(a.errOut, "aprofile: %v\n", err)
	}
}

// browseRows builds the browse display rows from the profile store and the on-disk
// wrapper state (FR-043). It reuses the same wrapper-status computation list/show use.
func (a *app) browseRows() ([]tui.ProfileRow, error) {
	profiles, _, err := a.store.List()
	if err != nil {
		return nil, err
	}
	rows := make([]tui.ProfileRow, 0, len(profiles))
	for _, p := range profiles {
		def, _ := a.set.Get(p.Harness)
		rows = append(rows, tui.ProfileRow{
			Harness:       p.Harness,
			Name:          p.Name,
			Label:         p.DisplayLabel(),
			Color:         p.Color,
			Icon:          p.Icon,
			Primary:       p.Primary,
			WrapperStatus: a.wrapperStatus(p, def),
		})
	}
	return rows, nil
}

// uiAdd collects a new profile through forms and creates it via the shared create path.
func uiAdd(cmd *cobra.Command, a *app) error {
	var harnesses []string
	for _, d := range a.set.All() {
		harnesses = append(harnesses, d.Name)
	}
	id, ok, err := tui.RunIdentityForm(os.Stdin, cmd.OutOrStdout(), harnesses)
	if err != nil || !ok {
		return err
	}
	if !harness.NameValid(id.Name) {
		return fmt.Errorf("profile name %q must match ^[a-z][a-z0-9-]{0,62}[a-z0-9]$", id.Name)
	}
	def, ok := a.set.Get(id.Harness)
	if !ok {
		return fmt.Errorf("unknown harness %q", id.Harness)
	}
	if a.store.Exists(id.Harness, id.Name) {
		return fmt.Errorf("profile %s/%s already exists", id.Harness, id.Name)
	}
	// Same guards as the `add` command (Constitution VI parity): a leftover config dir
	// must be adopted explicitly (FR-004) so a re-added profile never silently inherits a
	// prior account's credentials and state; warn when the harness binary is missing.
	configDir := a.paths.ProfileConfigDir(id.Harness, id.Name)
	if layer.DirExists(configDir) {
		return fmt.Errorf("a config dir already exists at %s without a profile; use `aprofile add %s %s --adopt` to reuse it", configDir, id.Harness, id.Name)
	}
	if _, err := exec.LookPath(def.Binary); err != nil {
		fmt.Fprintf(a.errOut, "aprofile: warning: harness binary %q not found on PATH; the wrapper will report this when started\n", def.Binary)
	}
	v, ok, err := tui.RunProfileForm(os.Stdin, cmd.OutOrStdout(), profileFormConfig(a, def, &profileFlags{share: a.settings.EffectiveDefaultShare()}))
	if err != nil || !ok {
		return err
	}
	f := &profileFlags{}
	applyFormValues(f, v)
	return createProfileFromFlags(a, def, id.Harness, id.Name, f)
}

// uiEdit edits the selected profile through the form and the shared edit path.
func uiEdit(cmd *cobra.Command, a *app, sel *tui.ProfileRow) error {
	p, err := a.store.Load(sel.Harness, sel.Name)
	if err != nil {
		return err
	}
	def, ok := a.set.Get(sel.Harness)
	if !ok {
		return fmt.Errorf("unknown harness %q", sel.Harness)
	}
	v, ok, err := tui.RunProfileForm(os.Stdin, cmd.OutOrStdout(), profileFormConfig(a, def, formEditDefaults(p)))
	if err != nil || !ok {
		return err
	}
	return applyEditForm(a, cmd, p, def, v)
}

// uiCopy collects a copy through the form and runs the same copy operation the command
// uses.
func uiCopy(cmd *cobra.Command, a *app, sel *tui.ProfileRow) error {
	profiles, _, err := a.store.List()
	if err != nil {
		return err
	}
	var sources []string
	for _, p := range profiles {
		sources = append(sources, p.ID())
	}
	initial := tui.CopyValues{}
	if sel != nil {
		initial.Source = sel.Harness + "/" + sel.Name
	}
	v, ok, err := tui.RunCopyForm(os.Stdin, cmd.OutOrStdout(), sources, initial, shareOptions(""))
	if err != nil || !ok {
		return err
	}
	return runCopy(cmd, v.Source, v.Target, uiCopyFlags(v))
}

// uiCopyFlags maps the copy form's values onto the copy flags through the command's
// own mapping, then adds the auth key reference and adopt choice the form collects,
// the same values the --auth-key and --adopt flags carry (Constitution VI).
func uiCopyFlags(v tui.CopyValues) *copyFlags {
	f := copyFlagsFromForm(v)
	f.authKey = strings.TrimSpace(v.AuthKey)
	f.adopt = v.Adopt
	return f
}

// uiRemove confirms and removes the selected profile through the same remove operation
// (purge is only honored after the user confirms it in the form).
func uiRemove(cmd *cobra.Command, sel *tui.ProfileRow) error {
	ref := sel.Harness + "/" + sel.Name
	confirmed, purge, err := tui.RunRemoveForm(os.Stdin, cmd.OutOrStdout(), ref)
	if err != nil {
		return err
	}
	if !confirmed {
		return nil
	}
	return runRemove(cmd, sel.Harness, sel.Name, purge, true)
}
