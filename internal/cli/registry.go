package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRegistryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registry",
		Short: "Inspect the profile registry",
	}
	cmd.AddCommand(newRegistryPathCmd())
	return cmd
}

func newRegistryPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the registry file location",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := newApp(cmd)
			if err != nil {
				return err
			}
			fmt.Fprintln(a.out, a.paths.RegistryPath())
			return nil
		},
	}
}
