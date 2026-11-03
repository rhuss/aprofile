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
	paths, err := config.ResolvePaths()
	if err != nil {
		return nil, err
	}
	settings, err := config.LoadSettings(paths.ConfigDir)
	if err != nil {
		return nil, err
	}
	set, err := harness.Load(paths.HarnessesDir())
	if err != nil {
		return nil, err
	}
	return &app{
		paths:    paths,
		settings: settings,
		set:      set,
		store:    profile.NewStore(paths),
		out:      cmd.OutOrStdout(),
		errOut:   cmd.ErrOrStderr(),
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
	)
	return root
}

// Execute runs the root command and returns a process exit code.
func Execute() int {
	if err := NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(cmdErr(), "aprofile: "+err.Error())
		return 1
	}
	return 0
}
