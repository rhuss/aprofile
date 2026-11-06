package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/layer"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/secret"
)

func newLoginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login <harness>/<name>",
		Short: "Run the harness's login flow with the profile's environment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogin(cmd, args[0])
		},
	}
	return cmd
}

func runLogin(cmd *cobra.Command, ref string) error {
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
	def, ok := a.set.Get(h)
	if !ok {
		return fmt.Errorf("unknown harness %q", h)
	}

	bin, err := exec.LookPath(def.Binary)
	if err != nil {
		return fmt.Errorf("harness binary %q not found on PATH", def.Binary)
	}

	// Ensure the config dir exists so credentials land in the profile.
	configDir := a.paths.ProfileConfigDir(h, n)
	if _, err := layer.PrepareConfigDir(def, p, configDir, false); err != nil {
		return err
	}

	env, err := loginEnv(p, def, configDir)
	if err != nil {
		return err
	}

	// Replace this process with the harness so the login is fully interactive
	// (inherits the terminal). The exec-hook `login` operation (T045) will handle
	// harness-specific login subcommands where a definition provides one.
	fmt.Fprintf(a.out, "launching %s for profile %s login (config dir: %s)\n", def.Binary, p.ID(), configDir)
	return syscall.Exec(bin, []string{def.Binary}, env)
}

// loginEnv builds the process environment for an interactive login: the profile's
// config_env redirects, the APROFILE_* contract, and the selected auth kind's
// resolved variables, with every other auth kind's variable removed.
func loginEnv(p *profile.Profile, def *harness.Definition, configDir string) ([]string, error) {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 {
			env[kv[:i]] = kv[i+1:]
		}
	}
	// Clear every auth variable, then set the selected kind's.
	for _, v := range def.AllAuthVars() {
		delete(env, v)
	}
	// config_env redirects.
	for _, e := range def.ConfigEnv {
		env[e.Var] = harness.ConfigEnvPath(configDir, e.Path)
	}
	// Contract variables.
	env["APROFILE_HARNESS"] = p.Harness
	env["APROFILE_NAME"] = p.Name
	env["APROFILE_LABEL"] = p.DisplayLabel()
	env["APROFILE_COLOR"] = p.Color
	env["APROFILE_ICON"] = p.Icon
	env["APROFILE_CONFIG_DIR"] = configDir
	env["APROFILE_SHARE"] = string(p.ShareLevel())
	// Resolved auth for the selected kind.
	if kind, ok := def.AuthKind(p.AuthKind); ok {
		for _, v := range kind.VarNames() {
			if refObj, has := p.AuthRefs[v]; has {
				val, err := secret.Resolve(refObj)
				if err != nil {
					return nil, fmt.Errorf("profile %s: %w", p.ID(), err)
				}
				env[v] = val
			}
		}
	}
	// Model/endpoint.
	if def.ModelVar != "" && p.Model != "" {
		env[def.ModelVar] = p.Model
	}
	if def.EndpointVar != "" && p.Endpoint != "" {
		env[def.EndpointVar] = p.Endpoint
	}
	if p.HerdrAgent != "" {
		env["HERDR_AGENT"] = p.HerdrAgent
	}
	// Extra env, matching the wrapper: caller-overridable, so set only when the
	// variable is not already present; references are resolved fail-closed.
	for _, e := range p.ExtraEnv {
		if _, present := env[e.Name]; present {
			continue
		}
		if !e.Literal && secret.IsReference(e.Value) {
			ref, err := secret.ParseReference(e.Value)
			if err != nil {
				return nil, fmt.Errorf("profile %s: extra env %q: %w", p.ID(), e.Name, err)
			}
			val, err := secret.Resolve(ref)
			if err != nil {
				return nil, fmt.Errorf("profile %s: extra env %q: %w", p.ID(), e.Name, err)
			}
			env[e.Name] = val
			continue
		}
		env[e.Name] = e.Value
	}

	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out, nil
}
