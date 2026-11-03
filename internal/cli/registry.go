package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/registry"
)

func newRegistryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registry",
		Short: "Inspect and export the profile registry",
	}
	cmd.AddCommand(newRegistryPathCmd(), newRegistryExportCmd())
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

func newRegistryExportCmd() *cobra.Command {
	var format, outputDir string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export the registry in another tool's format (only on explicit request)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRegistryExport(cmd, format, outputDir)
		},
	}
	cmd.Flags().StringVar(&format, "format", "omarchy", "export format (omarchy)")
	cmd.Flags().StringVar(&outputDir, "output-dir", "", "override the output directory")
	return cmd
}

func runRegistryExport(cmd *cobra.Command, format, outputDir string) error {
	if format != "omarchy" {
		return fmt.Errorf("unsupported export format %q (supported: omarchy)", format)
	}
	a, err := newApp(cmd)
	if err != nil {
		return err
	}
	profiles, failures, err := a.store.List()
	if err != nil {
		return err
	}
	for _, f := range failures {
		fmt.Fprintf(a.errOut, "aprofile: profile %s failed to load; skipped\n", f.ID())
	}

	var exportProfiles []registry.OmarchyProfile
	for _, p := range profiles {
		def, ok := a.set.Get(p.Harness)
		if !ok || def.OmarchyProvider == "" {
			fmt.Fprintf(a.errOut, "aprofile: profile %s has no Omarchy provider; skipped\n", p.ID())
			continue
		}
		exportProfiles = append(exportProfiles, registry.OmarchyProfile{
			Harness:  p.Harness,
			Name:     p.Name,
			Provider: def.OmarchyProvider,
			Label:    p.DisplayLabel(),
			Home:     a.paths.ProfileConfigDir(p.Harness, p.Name),
			Primary:  p.Primary,
		})
	}

	dir := outputDir
	if dir == "" {
		dir = a.paths.OmarchyAccountsDir()
	}
	written, err := registry.ExportOmarchy(dir, exportProfiles)
	if err != nil {
		return err
	}
	if len(written) == 0 {
		fmt.Fprintf(a.out, "omarchy export up to date in %s\n", dir)
		return nil
	}
	for _, path := range written {
		fmt.Fprintf(a.out, "wrote %s\n", path)
	}
	return nil
}
