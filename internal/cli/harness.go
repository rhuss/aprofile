package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/harness"
)

func newHarnessCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "harness",
		Short: "Inspect harness definitions",
	}
	cmd.AddCommand(newHarnessListCmd(), newHarnessShowCmd(), newHarnessClassifyCmd())
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
	// Levels is the effective treatment per borrowable category and share level
	// (FR-022), after FR-009a: a mixed or owned settings-like file is never linked.
	Levels map[string]harness.LevelTreatment `json:"levels"`
}

// borrowableCategories lists the categories of the level table.
var borrowableCategories = []harness.Layer{harness.LayerContent, harness.LayerSettings, harness.LayerPluginStore, harness.LayerHookLocation}

func effectiveLevels(d *harness.Definition) map[string]harness.LevelTreatment {
	out := map[string]harness.LevelTreatment{}
	for _, c := range borrowableCategories {
		out[string(c)] = harness.LevelTreatment{
			All: d.Treatment(c, harness.LevelAll), Content: d.Treatment(c, harness.LevelContent), None: d.Treatment(c, harness.LevelNone),
		}
	}
	return out
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
		return writeJSON(a.out, harnessShowJSON{FormatVersion: schemaVersion, Definition: d, Levels: effectiveLevels(d)})
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
		// The scope field is ignored since feature 002: skills follow the content layer.
		line("skills_location", "skills_location", d.SkillsLocation.Path)
	}
	line("plugin_mechanism", "plugin_mechanism", d.PluginMechanism)
	line("omarchy_provider", "omarchy_provider", d.OmarchyProvider)
	line("capabilities", "capabilities", strings.Join(d.Capabilities, ", "))
	if d.ExecHook != nil {
		line("exec_hook", "exec_hook", strings.Join(d.ExecHook.Operations, ", "))
	}
	if d.Hooks != nil {
		line("hooks", "hooks", d.Hooks.Location+" ("+d.Hooks.Form+")")
	}

	// FR-022: what each share level shares for this harness.
	fmt.Fprintln(a.out, "  share levels:")
	fmt.Fprintf(a.out, "    %-14s %-10s %-10s %-10s\n", "category", "all", "content", "none")
	lv := effectiveLevels(d)
	for _, c := range borrowableCategories {
		row := lv[string(c)]
		fmt.Fprintf(a.out, "    %-14s %-10s %-10s %-10s %s\n", c, row.All, row.Content, row.None, categoryEntries(d, c))
	}
	for _, diff := range harnessDifferences(d) {
		fmt.Fprintf(a.out, "    - %s\n", diff)
	}
	if len(d.HomeEscapes) > 0 {
		fmt.Fprintln(a.out, "  $HOME escapes (shared with every profile; no share level separates them):")
		for _, he := range d.HomeEscapes {
			fmt.Fprintf(a.out, "    %s (%s)\n", he.Path, he.What)
		}
	}
	fmt.Fprintln(a.out, "  classification:")
	for _, r := range d.Classification {
		origin := r.Origin
		if origin == "" || origin == "builtin" {
			origin = "built-in"
		}
		extra := ""
		if r.CompanionOf != "" {
			extra = " (companion of " + r.CompanionOf + ")"
		}
		if r.Deprecated {
			extra += " (deprecated layer shared, read as content)"
		}
		fmt.Fprintf(a.out, "    %-36s %-13s (%s)%s\n", r.Entry, r.Layer, origin, extra)
	}
	return nil
}

// categoryEntries lists the entries of one category, for the per-level table.
func categoryEntries(d *harness.Definition, c harness.Layer) string {
	var out []string
	for _, r := range d.Classification {
		if r.Layer == c && !r.IsGlob() {
			out = append(out, r.Entry)
		}
	}
	if c == harness.LayerHookLocation && d.Hooks != nil {
		out = append(out, d.Hooks.Location)
	}
	if len(out) == 0 {
		return ""
	}
	return "[" + strings.Join(out, " ") + "]"
}

// classifyCategories are the categories harness classify accepts (contracts/cli.md).
var classifyCategories = []string{"content", "settings", "plugin-store", "state", "credentials", "owned"}

func newHarnessClassifyCmd() *cobra.Command {
	var as string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "classify <harness> <entry> --as <category>",
		Short: "Add or change one classification rule in harnesses.d (merged with the built-in rules by entry)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHarnessClassify(cmd, args[0], args[1], as, dryRun)
		},
	}
	cmd.Flags().StringVar(&as, "as", "", "category: "+strings.Join(classifyCategories, ", "))
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show the file that would be written")
	_ = cmd.MarkFlagRequired("as")
	return cmd
}

func runHarnessClassify(cmd *cobra.Command, name, entry, as string, dryRun bool) error {
	a, err := newApp(cmd)
	if err != nil {
		return err
	}
	d, ok := a.set.Get(name)
	if !ok {
		return fmt.Errorf("unknown harness %q (run: aprofile harness list)", name)
	}
	valid := false
	for _, c := range classifyCategories {
		if c == as {
			valid = true
		}
	}
	if !valid {
		return fmt.Errorf("category %q must be one of %s", as, strings.Join(classifyCategories, ", "))
	}
	path, data, err := classifyOverride(a.paths.HarnessesDir(), d, entry, as)
	if err != nil {
		return err
	}
	// Validate the result before writing: the merged definition must still load.
	if err := checkOverrideLoads(path, data, name); err != nil {
		return err
	}
	if dryRun {
		fmt.Fprintf(a.out, "(dry-run) would write %s:\n%s", path, data)
		return nil
	}
	if err := config.WriteFileAtomic(path, data, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "classified %s %s as %s in %s (run: aprofile sync)\n", name, entry, as, path)
	return nil
}

// classifyOverride returns the harnesses.d file for the harness (the one that
// already defines or overrides it, else <name>.yaml) with one rule upserted by
// entry. Comments and other fields of an existing file are kept.
func classifyOverride(dir string, d *harness.Definition, entry, as string) (string, []byte, error) {
	path := filepath.Join(dir, d.Name+".yaml")
	if src := d.Source["classification"]; src != "" && src != "builtin" {
		path = src
	} else if src := d.Source["name"]; src != "" && src != "builtin" {
		path = src
	}
	var doc yaml.Node
	if data, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return "", nil, fmt.Errorf("parse %s: %w", path, err)
		}
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
		root := doc.Content[0]
		root.Content = append(root.Content, scalar("name"), scalar(d.Name))
	}
	root := doc.Content[0]
	var seq *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "classification" {
			seq = root.Content[i+1]
		}
	}
	if seq == nil || seq.Kind != yaml.SequenceNode {
		seq = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		root.Content = append(root.Content, scalar("classification"), seq)
	}
	rule := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Style: yaml.FlowStyle,
		Content: []*yaml.Node{scalar("entry"), scalar(entry), scalar("layer"), scalar(as)}}
	norm := strings.TrimSuffix(entry, "/")
	// One rule per entry: the first matching rule is replaced and later duplicates
	// are dropped, so the file never holds two rules for the same entry.
	replaced := false
	kept := seq.Content[:0]
	for _, r := range seq.Content {
		match := false
		for j := 0; j+1 < len(r.Content); j += 2 {
			if r.Content[j].Value == "entry" && strings.TrimSuffix(r.Content[j+1].Value, "/") == norm {
				match = true
			}
		}
		switch {
		case !match:
			kept = append(kept, r)
		case !replaced:
			kept = append(kept, rule)
			replaced = true
		}
	}
	seq.Content = kept
	if !replaced {
		seq.Content = append(seq.Content, rule)
	}
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return "", nil, err
	}
	return path, out, nil
}

func scalar(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v} }

// checkOverrideLoads loads the harnesses.d dir with the new file content in a
// scratch copy and fails when the harness would no longer load.
func checkOverrideLoads(path string, data []byte, name string) error {
	tmp, err := os.MkdirTemp("", "aprofile-classify-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if entries, err := os.ReadDir(filepath.Dir(path)); err == nil {
		for _, e := range entries {
			if filepath.Ext(e.Name()) != ".yaml" || e.Name() == filepath.Base(path) {
				continue
			}
			if b, err := os.ReadFile(filepath.Join(filepath.Dir(path), e.Name())); err == nil {
				_ = os.WriteFile(filepath.Join(tmp, e.Name()), b, 0o644)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(tmp, filepath.Base(path)), data, 0o644); err != nil {
		return err
	}
	set, err := harness.Load(tmp)
	if err != nil {
		return err
	}
	for _, e := range set.Errors() {
		if strings.HasSuffix(e.File, filepath.Base(path)) {
			return fmt.Errorf("the new rule would make the definition invalid: %s", e.Msg)
		}
	}
	if _, ok := set.Get(name); !ok {
		return fmt.Errorf("harness %q would no longer load", name)
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
