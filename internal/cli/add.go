package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/secret"
	"github.com/cc-deck/aprofile/internal/tui"
)

// profileFlags holds the value flags shared by add and edit.
type profileFlags struct {
	model      string
	endpoint   string
	authKind   string
	authKey    string
	env        []string
	envLiteral []string
	color      string
	icon       string
	label      string
	primary    bool
	herdrAgent string
	adopt      bool
}

func (f *profileFlags) bind(cmd *cobra.Command) {
	fl := cmd.Flags()
	fl.StringVar(&f.model, "model", "", "model to set via the harness's model variable")
	fl.StringVar(&f.endpoint, "endpoint", "", "API base URL (endpoint) for the harness")
	fl.StringVar(&f.authKind, "auth-kind", "", "authentication kind (see the harness definition)")
	fl.StringVar(&f.authKey, "auth-key", "", "secret reference for the auth kind: env:NAME or file:/abs/path")
	fl.StringArrayVar(&f.env, "env", nil, "extra env var NAME=ref|value (repeatable)")
	fl.StringArrayVar(&f.envLiteral, "env-literal", nil, "extra env var NAME=VALUE stored as a literal (repeatable)")
	fl.StringVar(&f.color, "color", "", "ANSI color name or #rrggbb")
	fl.StringVar(&f.icon, "icon", "", "a single Unicode grapheme")
	fl.StringVar(&f.label, "label", "", "human-readable label (defaults to the name)")
	fl.BoolVar(&f.primary, "primary", false, "mark this profile primary for its harness")
	fl.StringVar(&f.herdrAgent, "herdr-agent", "", "set the HERDR_AGENT hint")
}

func newAddCmd() *cobra.Command {
	f := &profileFlags{}
	cmd := &cobra.Command{
		Use:   "add <harness> <name>",
		Short: "Create a profile",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdd(cmd, args[0], args[1], f)
		},
	}
	f.bind(cmd)
	cmd.Flags().BoolVar(&f.adopt, "adopt", false, "reuse an existing leftover config dir for this (harness, name)")
	return cmd
}

func runAdd(cmd *cobra.Command, harnessName, name string, f *profileFlags) error {
	a, err := newApp(cmd)
	if err != nil {
		return err
	}
	if !harness.NameValid(name) {
		return fmt.Errorf("profile name %q must match ^[a-z][a-z0-9-]{0,62}[a-z0-9]$", name)
	}
	def, ok := a.set.Get(harnessName)
	if !ok {
		return fmt.Errorf("unknown harness %q (run: aprofile harness list)", harnessName)
	}
	if a.store.Exists(harnessName, name) {
		return fmt.Errorf("profile %s/%s already exists", harnessName, name)
	}

	// FR-004: a leftover config dir for this (harness, name) without a profile must
	// be adopted explicitly.
	configDir := a.paths.ProfileConfigDir(harnessName, name)
	if dirExists(configDir) && !f.adopt {
		return fmt.Errorf("a config dir already exists at %s without a profile; pass --adopt to reuse it", configDir)
	}

	// Warn when the harness binary is missing (edge case): the profile is still
	// created and sync still writes a wrapper.
	if _, err := exec.LookPath(def.Binary); err != nil {
		fmt.Fprintf(a.errOut, "aprofile: warning: harness binary %q not found on PATH; the wrapper will report this when started\n", def.Binary)
	}

	// A profile whose auth kind carries its credential in an environment variable has
	// no credential without a reference. When the user did not supply one, open the
	// form on a terminal (FR-043) or, off a terminal, fail naming the missing flag and
	// never block on a form (Scenario 9 AS3).
	if missing := requiredAddFlags(def, f); len(missing) > 0 {
		if !bothTTY(cmd) {
			return fmt.Errorf("profile %s/%s is missing required %s: pass %s env:NAME (or file:/abs/path), choose an interactive kind with --auth-kind, or run from a terminal to use the form",
				harnessName, name, strings.Join(missing, ", "), strings.Join(missing, " "))
		}
		v, ok, err := tui.RunProfileForm(os.Stdin, cmd.OutOrStdout(), profileFormConfig(def, f))
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(a.out, "add cancelled")
			return nil
		}
		applyFormValues(f, v)
	}

	return createProfileFromFlags(a, def, harnessName, name, f)
}

// createProfileFromFlags builds, validates and saves a profile from the shared value
// flags. Both the flag-driven add and the form-driven add route through it, so a
// form-entered profile is identical to the equivalent command (Constitution VI,
// SC-008).
func createProfileFromFlags(a *app, def *harness.Definition, harnessName, name string, f *profileFlags) error {
	p := &profile.Profile{
		Harness:    harnessName,
		Name:       name,
		Label:      f.label,
		Color:      f.color,
		Icon:       f.icon,
		Primary:    f.primary,
		Model:      f.model,
		Endpoint:   f.endpoint,
		HerdrAgent: f.herdrAgent,
	}
	if err := applyValueFlags(p, def, f); err != nil {
		return err
	}
	if err := p.Validate(); err != nil {
		return err
	}
	if p.Primary {
		if err := clearOtherPrimaries(a, p); err != nil {
			return err
		}
	}
	if err := a.store.Save(p); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "created profile %s (run: aprofile sync)\n", p.ID())
	return nil
}

// requiredAddFlags returns the value flags an add cannot proceed without. The auth key
// is required when the resolved auth kind declares at least one environment variable
// (the credential is that variable's reference) and none was supplied. An interactive
// auth kind (no variables) needs no key.
func requiredAddFlags(def *harness.Definition, f *profileFlags) []string {
	kindName := f.authKind
	if kindName == "" {
		kindName = def.DefaultAuthKind
	}
	kind, ok := def.AuthKind(kindName)
	if !ok || len(kind.VarNames()) == 0 {
		return nil
	}
	if f.authKey != "" {
		return nil
	}
	return []string{"--auth-key"}
}

// profileFormConfig derives a form configuration from the harness definition (Principle
// I) and pre-fills it with any flags already given.
func profileFormConfig(def *harness.Definition, f *profileFlags) tui.ProfileFormConfig {
	var kinds []string
	for _, k := range def.AuthKinds {
		kinds = append(kinds, k.Name)
	}
	return tui.ProfileFormConfig{
		AuthKinds:        kinds,
		SupportsModel:    def.ModelVar != "",
		SupportsEndpoint: def.EndpointVar != "",
		Values:           formValuesFromFlags(f),
	}
}

// formValuesFromFlags copies the value flags into the form's pre-fill values.
func formValuesFromFlags(f *profileFlags) tui.FormValues {
	return tui.FormValues{
		Label:      f.label,
		Color:      f.color,
		Icon:       f.icon,
		Model:      f.model,
		Endpoint:   f.endpoint,
		AuthKind:   f.authKind,
		AuthKey:    f.authKey,
		HerdrAgent: f.herdrAgent,
		Primary:    f.primary,
	}
}

// applyFormValues writes a form's collected values back onto the shared flags so the
// create/edit path treats them exactly like command-line flags (Constitution VI).
func applyFormValues(f *profileFlags, v tui.FormValues) {
	f.label = v.Label
	f.color = v.Color
	f.icon = v.Icon
	f.model = v.Model
	f.endpoint = v.Endpoint
	f.authKind = v.AuthKind
	f.authKey = v.AuthKey
	f.herdrAgent = v.HerdrAgent
	f.primary = v.Primary
}

// applyValueFlags validates and applies the shared value flags onto a profile.
func applyValueFlags(p *profile.Profile, def *harness.Definition, f *profileFlags) error {
	if err := validateColor(f.color); err != nil {
		return err
	}
	if err := validateIcon(f.icon); err != nil {
		return err
	}

	// FR-003: model/endpoint are only settable for a harness whose definition declares
	// the backing env variable. Error here rather than silently storing a value the
	// wrapper would drop. Documented limitation: in slice one OpenCode and pi declare
	// neither and Codex declares only the endpoint (see research.md).
	if f.model != "" && def.ModelVar == "" {
		return fmt.Errorf("harness %q has no model variable; --model is not supported for it", def.Name)
	}
	if f.endpoint != "" && def.EndpointVar == "" {
		return fmt.Errorf("harness %q has no endpoint variable; --endpoint is not supported for it", def.Name)
	}

	// Auth kind: default from the definition when not given.
	kindName := f.authKind
	if kindName == "" {
		kindName = def.DefaultAuthKind
	}
	if kindName != "" {
		kind, ok := def.AuthKind(kindName)
		if !ok {
			return fmt.Errorf("harness %q has no auth kind %q", def.Name, kindName)
		}
		p.AuthKind = kindName
		if f.authKey != "" {
			ref, err := secret.ParseAuthReference(f.authKey)
			if err != nil {
				return fmt.Errorf("--auth-key: %w", err)
			}
			vars := kind.VarNames()
			if len(vars) == 0 {
				return fmt.Errorf("auth kind %q takes no key (it uses an interactive login)", kindName)
			}
			if len(vars) > 1 {
				return fmt.Errorf("auth kind %q sets multiple variables; set them with --env instead", kindName)
			}
			if p.AuthRefs == nil {
				p.AuthRefs = map[string]secret.Reference{}
			}
			p.AuthRefs[vars[0]] = ref
		}
	} else if f.authKey != "" {
		return fmt.Errorf("--auth-key requires --auth-kind")
	}

	// Extra env (references or values subject to FR-028).
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
		p.ExtraEnv = append(p.ExtraEnv, profile.ExtraEnvVar{Name: name, Value: val})
	}
	for _, kv := range f.envLiteral {
		name, val, err := splitEnv(kv)
		if err != nil {
			return err
		}
		if err := secret.ValidateValue(val, true); err != nil {
			return fmt.Errorf("--env-literal %s: %w", name, err)
		}
		p.ExtraEnv = append(p.ExtraEnv, profile.ExtraEnvVar{Name: name, Value: val, Literal: true})
	}
	return nil
}

func splitEnv(kv string) (name, value string, err error) {
	i := strings.IndexByte(kv, '=')
	if i <= 0 {
		return "", "", fmt.Errorf("expected NAME=value, got %q", kv)
	}
	name = kv[:i]
	value = kv[i+1:]
	if !validEnvName(name) {
		return "", "", fmt.Errorf("invalid env var name %q", name)
	}
	return name, value, nil
}

// clearOtherPrimaries moves the primary flag off any other profile of the same
// harness (FR-001) and reports the change.
func clearOtherPrimaries(a *app, p *profile.Profile) error {
	peers, err := a.store.ListByHarness(p.Harness)
	if err != nil {
		return err
	}
	for _, peer := range peers {
		if peer.Name == p.Name || !peer.Primary {
			continue
		}
		peer.Primary = false
		if err := a.store.Save(peer); err != nil {
			return err
		}
		fmt.Fprintf(a.out, "moved primary flag from %s to %s\n", peer.ID(), p.ID())
	}
	return nil
}

func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}
