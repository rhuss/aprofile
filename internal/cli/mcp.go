package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/mcp"
)

func newMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Inspect and copy a profile's MCP servers",
	}
	cmd.AddCommand(newMCPListCmd(), newMCPCopyCmd())
	return cmd
}

type mcpServerJSON struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
	Command   string `json:"command,omitempty"`
	URL       string `json:"url,omitempty"`
	Enabled   *bool  `json:"enabled,omitempty"`
}

type mcpListJSON struct {
	FormatVersion int             `json:"format_version"`
	Profile       string          `json:"profile"`
	Servers       []mcpServerJSON `json:"servers"`
}

func newMCPListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list <harness>/<name>",
		Short: "List a profile's MCP servers",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMCPList(cmd, args[0], asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

func runMCPList(cmd *cobra.Command, ref string, asJSON bool) error {
	a, err := newApp(cmd)
	if err != nil {
		return err
	}
	h, n, err := parseProfileRef(ref)
	if err != nil {
		return err
	}
	if _, err := a.store.Load(h, n); err != nil {
		return err
	}
	def, ok := a.set.Get(h)
	if !ok {
		return fmt.Errorf("unknown harness %q", h)
	}
	servers, err := readMCPServers(a, def, h, n)
	if err != nil {
		return err
	}

	if asJSON {
		out := mcpListJSON{FormatVersion: schemaVersion, Profile: h + "/" + n, Servers: []mcpServerJSON{}}
		for _, s := range servers {
			out.Servers = append(out.Servers, mcpServerJSON{
				Name: s.Name, Transport: string(s.Transport), Command: mcp.DisplayValue(s.Command), URL: mcp.RedactURL(s.URL), Enabled: s.Enabled,
			})
		}
		return writeJSON(a.out, out)
	}

	if def.MCP == nil {
		fmt.Fprintf(a.out, "%s/%s: harness %q has no MCP support\n", h, n, h)
		return nil
	}
	if len(servers) == 0 {
		fmt.Fprintf(a.out, "%s/%s: no MCP servers\n", h, n)
		return nil
	}
	for _, s := range servers {
		// A url can carry a secret in its userinfo, query, fragment or path; show it
		// with those parts masked, as a conversion would drop them.
		loc := mcp.DisplayValue(s.Command)
		if s.IsRemote() {
			loc = mcp.RedactURL(s.URL)
		}
		fmt.Fprintf(a.out, "  %s  [%s]  %s\n", s.Name, s.Transport, loc)
	}
	return nil
}

func newMCPCopyCmd() *cobra.Command {
	var overwrite, asJSON bool
	cmd := &cobra.Command{
		Use:   "copy <from-harness>/<name> <to-harness>/<name> [server...]",
		Short: "Convert MCP servers from one profile into another",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMCPCopy(cmd, args[0], args[1], args[2:], overwrite, asJSON)
		},
	}
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "replace servers that already exist in the target")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a machine-readable ConversionReport")
	return cmd
}

func runMCPCopy(cmd *cobra.Command, fromRef, toRef string, names []string, overwrite, asJSON bool) error {
	a, err := newApp(cmd)
	if err != nil {
		return err
	}
	fh, fn, err := parseProfileRef(fromRef)
	if err != nil {
		return err
	}
	th, tn, err := parseProfileRef(toRef)
	if err != nil {
		return err
	}
	if _, err := a.store.Load(fh, fn); err != nil {
		return err
	}
	if _, err := a.store.Load(th, tn); err != nil {
		return err
	}
	fromDef, ok := a.set.Get(fh)
	if !ok {
		return fmt.Errorf("unknown harness %q", fh)
	}
	toDef, ok := a.set.Get(th)
	if !ok {
		return fmt.Errorf("unknown harness %q", th)
	}
	if fromDef.MCP == nil {
		return fmt.Errorf("source harness %q has no MCP support", fh)
	}

	servers, err := readMCPServers(a, fromDef, fh, fn)
	if err != nil {
		return err
	}
	selected, err := selectServers(servers, names)
	if err != nil {
		return err
	}

	report := copyReportJSON{FormatVersion: schemaVersion, Source: fh + "/" + fn, Target: th + "/" + tn, Mode: "mcp-copy"}

	if toDef.MCP == nil || !toDef.HasCapability("mcp") {
		report.MCP = mcp.SkipAll(selected, "target harness has no mcp capability")
	} else {
		dstRel, _ := mcpFileRel(toDef)
		dstPath := filepath.Join(a.paths.ProfileConfigDir(th, tn), dstRel)
		existing, err := os.ReadFile(dstPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		out, rep, err := mcp.MergeInto(mcp.Flavor(toDef.MCP.Flavor), existing, selected, overwrite)
		if err != nil {
			return err
		}
		if err := config.WriteFileAtomic(dstPath, out, ownedFileMode(dstPath)); err != nil {
			return err
		}
		report.MCP = rep
	}

	if asJSON {
		return writeJSON(a.out, report)
	}
	fmt.Fprintf(a.out, "mcp copy %s -> %s\n", report.Source, report.Target)
	if len(report.MCP.Converted) > 0 {
		fmt.Fprintf(a.out, "  converted: %v\n", report.MCP.Converted)
	}
	if len(report.MCP.Replaced) > 0 {
		fmt.Fprintf(a.out, "  replaced: %v\n", report.MCP.Replaced)
	}
	for _, s := range report.MCP.Skipped {
		fmt.Fprintf(a.out, "  skipped: %s (%s)\n", s.Name, s.Reason)
	}
	for _, d := range report.MCP.Drops {
		fmt.Fprintf(a.out, "  dropped: %s %s (%s)\n", d.Server, d.Field, d.Reason)
	}
	return nil
}

// readMCPServers reads and parses a profile's MCP servers from its config dir.
func readMCPServers(a *app, def *harness.Definition, h, n string) ([]mcp.Server, error) {
	if def.MCP == nil {
		return nil, nil
	}
	rel, ok := mcpFileRel(def)
	if !ok {
		return nil, nil
	}
	data, err := os.ReadFile(filepath.Join(a.paths.ProfileConfigDir(h, n), rel))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return mcp.ParseDoc(mcp.Flavor(def.MCP.Flavor), data)
}

// selectServers filters servers by the requested names, erroring on an unknown name.
// An empty names slice selects all servers. A name given twice selects its server
// once, so the report lists it once (FR-037).
func selectServers(servers []mcp.Server, names []string) ([]mcp.Server, error) {
	if len(names) == 0 {
		return servers, nil
	}
	byName := map[string]mcp.Server{}
	for _, s := range servers {
		byName[s.Name] = s
	}
	var out []mcp.Server
	seen := map[string]bool{}
	for _, name := range names {
		s, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("MCP server %q not found in the source profile", name)
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, s)
	}
	return out, nil
}
