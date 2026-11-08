// Package cli implements the cobra command surface. Commands are thin wrappers
// over the internal packages; every operation is available non-interactively
// (Constitution VI, FR-040).
package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/history"
	"github.com/cc-deck/aprofile/internal/layer"
	"github.com/cc-deck/aprofile/internal/profile"
)

// app bundles the resolved paths and loaded state for one command invocation. It
// is rebuilt per command so tests that set HOME/XDG via t.Setenv are honored.
type app struct {
	paths    *config.Paths
	settings *config.Settings
	set      *harness.Set
	store    *profile.Store
	out      io.Writer
	errOut   io.Writer
}

func newApp(cmd *cobra.Command) (*app, error) {
	paths, settings, err := config.Resolve()
	if err != nil {
		return nil, err
	}
	set, err := harness.Load(paths.HarnessesDir())
	if err != nil {
		return nil, err
	}
	errOut := cmd.ErrOrStderr()
	store := profile.NewStore(paths)
	// A profile that loads with a repaired field (an invalid color or icon is
	// ignored) is reported once per command on stderr.
	store.Warn = func(msg string) { fmt.Fprintf(errOut, "warning: %s\n", msg) }
	return &app{
		paths:    paths,
		settings: settings,
		set:      set,
		store:    store,
		out:      cmd.OutOrStdout(),
		errOut:   errOut,
	}, nil
}

// NewRootCmd builds the aprofile root command with all slice-one subcommands.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "aprofile",
		Short:         "Manage side-by-side profiles for coding harnesses",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		newAddCmd(),
		newListCmd(),
		newShowCmd(),
		newEditCmd(),
		newRenameCmd(),
		newRemoveCmd(),
		newSyncCmd(),
		newLoginCmd(),
		newDoctorCmd(),
		newRegistryCmd(),
		newCopyCmd(),
		newMCPCmd(),
		newHarnessCmd(),
		newImportCmd(),
		newUICmd(),
		newSharingHelpCmd(),
		newSettingsCmd(),
		newHookSetsCmd(),
		newGroupCmd(),
	)
	return root
}

// Execute runs the root command and returns a process exit code: the code of an
// ExitError (2, 3 or 4, contracts/cli.md), 1 for any other error, 0 on success.
func Execute() int {
	err := NewRootCmd().Execute()
	if err != nil {
		fmt.Fprintln(cmdErr(), "aprofile: "+err.Error())
	}
	return exitCode(err)
}

// groups returns the history group manager (spec 003).
func (a *app) groups() *history.Manager { return history.NewManager(a.paths, a.set, a.store) }

// prepareOptions returns the config-dir preparation options for a profile with
// its history group store: nil store for an isolated profile, history entries left
// unchanged for a profile of a group that does not exist (FR-022c).
func (a *app) prepareOptions(def *harness.Definition, p *profile.Profile) layer.PrepareOptions {
	var opts layer.PrepareOptions
	st, err := a.groups().StoreFor(def, p)
	switch {
	case err != nil:
		opts.SkipHistory = true
	case st != nil:
		opts.HistoryStore = st
	}
	return opts
}
