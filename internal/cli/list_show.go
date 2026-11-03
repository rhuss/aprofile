package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/layer"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/secret"
	"github.com/cc-deck/aprofile/internal/wrapper"
)

// schemaVersion is the --json schema version for list/show output (FR-041).
const schemaVersion = 1

type extraEnvJSON struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`                // reference | literal
	Reference string `json:"reference,omitempty"` // only for references (never a secret)
}

type profileJSON struct {
	Harness        string            `json:"harness"`
	Name           string            `json:"name"`
	Label          string            `json:"label"`
	Color          string            `json:"color"`
	Icon           string            `json:"icon"`
	Primary        bool              `json:"primary"`
	AuthKind       string            `json:"auth_kind"`
	AuthRefs       map[string]string `json:"auth_refs,omitempty"`
	Model          string            `json:"model,omitempty"`
	Endpoint       string            `json:"endpoint,omitempty"`
	ExtraEnv       []extraEnvJSON    `json:"extra_env,omitempty"`
	HerdrAgent     string            `json:"herdr_agent,omitempty"`
	HasCredentials bool              `json:"has_credentials"`
	WrapperStatus  string            `json:"wrapper_status"`
	ConfigDir      string            `json:"config_dir"`
	WrapperPath    string            `json:"wrapper_path"`
}

type listJSON struct {
	FormatVersion int           `json:"format_version"`
	Profiles      []profileJSON `json:"profiles"`
}

func newListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List profiles grouped by harness",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runList(cmd, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

func runList(cmd *cobra.Command, asJSON bool) error {
	a, err := newApp(cmd)
	if err != nil {
		return err
	}
	profiles, failures, err := a.store.List()
	if err != nil {
		return err
	}

	if asJSON {
		out := listJSON{FormatVersion: schemaVersion}
		for _, p := range profiles {
			out.Profiles = append(out.Profiles, a.profileToJSON(p))
		}
		return writeJSON(a.out, out)
	}

	if len(profiles) == 0 {
		fmt.Fprintln(a.out, "no profiles yet (create one with: aprofile add <harness> <name>)")
	}
	var lastHarness string
	for _, p := range profiles {
		if p.Harness != lastHarness {
			fmt.Fprintf(a.out, "%s:\n", p.Harness)
			lastHarness = p.Harness
		}
		def, _ := a.set.Get(p.Harness)
		status := a.wrapperStatus(p, def)
		primary := ""
		if p.Primary {
			primary = " (primary)"
		}
		fmt.Fprintf(a.out, "  %s%s  [%s]\n", styleLabel(a.out, p.DisplayLabel(), p.Color, p.Icon), primary, status)
	}
	for _, f := range failures {
		fmt.Fprintf(a.errOut, "aprofile: profile %s failed to load: %v\n", f.ID(), f.Err)
	}
	return nil
}

func newShowCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "show <harness>/<name>",
		Short: "Show one profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runShow(cmd, args[0], asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

func runShow(cmd *cobra.Command, ref string, asJSON bool) error {
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
	pj := a.profileToJSON(p)

	if asJSON {
		return writeJSON(a.out, pj)
	}
	fmt.Fprintf(a.out, "%s\n", styleLabel(a.out, p.DisplayLabel(), p.Color, p.Icon))
	fmt.Fprintf(a.out, "  harness:        %s\n", p.Harness)
	fmt.Fprintf(a.out, "  name:           %s\n", p.Name)
	fmt.Fprintf(a.out, "  primary:        %v\n", p.Primary)
	fmt.Fprintf(a.out, "  auth kind:      %s\n", p.AuthKind)
	fmt.Fprintf(a.out, "  credentials:    %s\n", presentStr(pj.HasCredentials))
	if p.Model != "" {
		fmt.Fprintf(a.out, "  model:          %s\n", p.Model)
	}
	if p.Endpoint != "" {
		fmt.Fprintf(a.out, "  endpoint:       %s\n", p.Endpoint)
	}
	fmt.Fprintf(a.out, "  config dir:     %s\n", pj.ConfigDir)
	fmt.Fprintf(a.out, "  wrapper:        %s [%s]\n", pj.WrapperPath, pj.WrapperStatus)
	return nil
}

// profileToJSON builds the secret-free JSON representation of a profile.
func (a *app) profileToJSON(p *profile.Profile) profileJSON {
	def, _ := a.set.Get(p.Harness)
	configDir := a.paths.ProfileConfigDir(p.Harness, p.Name)
	pj := profileJSON{
		Harness:       p.Harness,
		Name:          p.Name,
		Label:         p.DisplayLabel(),
		Color:         p.Color,
		Icon:          p.Icon,
		Primary:       p.Primary,
		AuthKind:      p.AuthKind,
		Model:         p.Model,
		Endpoint:      p.Endpoint,
		HerdrAgent:    p.HerdrAgent,
		WrapperStatus: a.wrapperStatus(p, def),
		ConfigDir:     configDir,
	}
	if def != nil {
		pj.HasCredentials = layer.HasCredentials(def, configDir)
		pj.WrapperPath = filepath.Join(a.paths.BinDir, def.WrapperPrefix+"-"+p.Name)
	}
	if len(p.AuthRefs) > 0 {
		pj.AuthRefs = map[string]string{}
		for v, ref := range p.AuthRefs {
			pj.AuthRefs[v] = ref.Raw // a reference, never a secret value
		}
	}
	for _, e := range p.ExtraEnv {
		ej := extraEnvJSON{Name: e.Name}
		if !e.Literal && secret.IsReference(e.Value) {
			ej.Kind = "reference"
			ej.Reference = e.Value
		} else {
			ej.Kind = "literal" // value withheld (FR-028: no secret values in --json)
		}
		pj.ExtraEnv = append(pj.ExtraEnv, ej)
	}
	return pj
}

// wrapperStatus reports whether a profile's on-disk wrapper matches what sync
// would render (FR-006b): synced | stale | missing.
func (a *app) wrapperStatus(p *profile.Profile, def *harness.Definition) string {
	if def == nil {
		return "unknown"
	}
	path := filepath.Join(a.paths.BinDir, def.WrapperPrefix+"-"+p.Name)
	existing, err := os.ReadFile(path)
	if err != nil {
		return "missing"
	}
	configDir := a.paths.ProfileConfigDir(p.Harness, p.Name)
	hookEnv, err := harness.RunEnvHook(def, harness.HookContext{
		Harness:   p.Harness,
		Profile:   p.Name,
		ConfigDir: configDir,
		AuthKind:  p.AuthKind,
		Model:     p.Model,
		Endpoint:  p.Endpoint,
	})
	if err != nil {
		return "unknown"
	}
	want, err := wrapper.RenderWith(p, def, configDir, wrapper.RenderOptions{HookEnv: hookEnv})
	if err != nil {
		return "unknown"
	}
	if string(existing) == want {
		return "synced"
	}
	return "stale"
}

func presentStr(present bool) string {
	if present {
		return "present"
	}
	return "none"
}
