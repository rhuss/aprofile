package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/harness"
)

func newHarnessCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "harness",
		Short: "Inspect harness definitions",
	}
	cmd.AddCommand(newHarnessListCmd(), newHarnessShowCmd())
	return cmd
}

type harnessErrorJSON struct {
	File    string `json:"file"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

type harnessListItemJSON struct {
	Name          string   `json:"name"`
	Binary        string   `json:"binary"`
	WrapperPrefix string   `json:"wrapper_prefix"`
	Source        string   `json:"source"`
	OverrideFiles []string `json:"override_files,omitempty"`
}

type harnessListJSON struct {
	FormatVersion int                   `json:"format_version"`
	Harnesses     []harnessListItemJSON `json:"harnesses"`
	Errors        []harnessErrorJSON    `json:"errors,omitempty"`
}

func newHarnessListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List harnesses with the source of each definition",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runHarnessList(cmd, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

func runHarnessList(cmd *cobra.Command, asJSON bool) error {
	a, err := newApp(cmd)
	if err != nil {
		return err
	}

	if asJSON {
		out := harnessListJSON{FormatVersion: schemaVersion}
		for _, d := range a.set.All() {
			summary, files := harnessSource(d)
			out.Harnesses = append(out.Harnesses, harnessListItemJSON{
				Name:          d.Name,
				Binary:        d.Binary,
				WrapperPrefix: d.WrapperPrefix,
				Source:        summary,
				OverrideFiles: files,
			})
		}
		for _, e := range a.set.Errors() {
			out.Errors = append(out.Errors, harnessErrorJSON{File: e.File, Field: e.Field, Message: e.Msg})
		}
		return writeJSON(a.out, out)
	}

	for _, d := range a.set.All() {
		summary, _ := harnessSource(d)
		fmt.Fprintf(a.out, "%-12s %-12s (%s)\n", d.Name, d.Binary, summary)
	}
	for _, e := range a.set.Errors() {
		fmt.Fprintf(a.errOut, "aprofile: harness definition error: %s\n", e.Error())
	}
	return nil
}

func newHarnessShowCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Show one harness definition and the origin of each field",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHarnessShow(cmd, args[0], asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

// harnessShowJSON embeds the effective definition (which carries its per-field
// source map) and adds the schema version.
type harnessShowJSON struct {
	FormatVersion int `json:"format_version"`
	*harness.Definition
}

func runHarnessShow(cmd *cobra.Command, name string, asJSON bool) error {
	a, err := newApp(cmd)
	if err != nil {
		return err
	}
	d, ok := a.set.Get(name)
	if !ok {
		// Surface a load error for a file that tried to define this harness.
		for _, e := range a.set.Errors() {
			if strings.Contains(e.Error(), name) {
				return fmt.Errorf("harness %q is not available: %s", name, e.Error())
			}
		}
		return fmt.Errorf("unknown harness %q (run: aprofile harness list)", name)
	}

	if asJSON {
		return writeJSON(a.out, harnessShowJSON{FormatVersion: schemaVersion, Definition: d})
	}

	src := func(field string) string {
		if o := d.Source[field]; o != "" && o != "builtin" {
			return o
		}
		return "built-in"
	}
	line := func(field, label, value string) {
		if value == "" {
			return
		}
		fmt.Fprintf(a.out, "  %-24s %-28s (%s)\n", label+":", value, src(field))
	}

	fmt.Fprintln(a.out, d.Name)
	line("binary", "binary", d.Binary)
	line("wrapper_prefix", "wrapper_prefix", d.WrapperPrefix)
	var ce []string
	for _, e := range d.ConfigEnv {
		ce = append(ce, e.Var+"="+e.Path)
	}
	line("config_env", "config_env", strings.Join(ce, " "))
	line("default_config_location", "default_config_location", d.DefaultConfigLocation)
	line("model_var", "model_var", d.ModelVar)
	line("endpoint_var", "endpoint_var", d.EndpointVar)
	line("default_auth_kind", "default_auth_kind", d.DefaultAuthKind)
	var kinds []string
	for _, k := range d.AuthKinds {
		kinds = append(kinds, k.Name)
	}
	line("auth_kinds", "auth_kinds", strings.Join(kinds, ", "))
	if len(d.Classification) > 0 {
		line("classification", "classification", fmt.Sprintf("%d rule(s)", len(d.Classification)))
	}
	if d.MCP != nil {
		line("mcp", "mcp", d.MCP.Location+" ("+d.MCP.Flavor+")")
	}
	if d.SkillsLocation != nil {
		line("skills_location", "skills_location", d.SkillsLocation.Path+" ["+d.SkillsLocation.Scope+"]")
	}
	line("plugin_mechanism", "plugin_mechanism", d.PluginMechanism)
	line("omarchy_provider", "omarchy_provider", d.OmarchyProvider)
	line("capabilities", "capabilities", strings.Join(d.Capabilities, ", "))
	if d.ExecHook != nil {
		line("exec_hook", "exec_hook", strings.Join(d.ExecHook.Operations, ", "))
	}
	return nil
}

// harnessSource summarizes where a definition's fields came from (FR-010): "built-in"
// when every field is built-in, "built-in + overrides" plus the files when some
// fields are overridden, or the defining files when the harness is file-defined.
func harnessSource(d *harness.Definition) (summary string, files []string) {
	hasBuiltin := false
	fileSet := map[string]bool{}
	for _, origin := range d.Source {
		if origin == "" || origin == "builtin" {
			hasBuiltin = true
			continue
		}
		fileSet[origin] = true
	}
	for f := range fileSet {
		files = append(files, f)
	}
	sort.Strings(files)
	switch {
	case len(files) == 0:
		return "built-in", nil
	case hasBuiltin:
		return "built-in + overrides from " + strings.Join(files, ", "), files
	default:
		return strings.Join(files, ", "), files
	}
}
