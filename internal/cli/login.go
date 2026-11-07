package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/layer"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/wrapper"
)

// execLogin replaces this process with the harness's login command. Tests swap it
// for a recorder so no real harness is started.
var execLogin = syscall.Exec

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

// runLogin runs the harness's own login flow under the profile's environment
// (FR-039): the exec hook's login operation when the definition declares one,
// otherwise (or when the hook answers "not supported" or is not installed) the
// definition's login_command. Which one runs is data, never a harness-name branch
// (Constitution Principle I).
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
	// FR-006: an undeclared auth kind would run the login with every auth variable
	// cleared and none set; refuse it as sync does.
	if fi := wrapper.AuthKindIssue(p, def); fi != nil {
		return fmt.Errorf("profile %s: %s; fix: %s", p.ID(), fi.Message, fi.Fix)
	}

	useHook := def.HookDeclares("login")
	argv := def.LoginArgv()
	if !useHook && argv == nil {
		return fmt.Errorf("harness %q has no login flow: its definition declares no login_command and its exec hook no login operation; add one in %s", h, a.paths.HarnessesDir())
	}
	var bin string
	if !useHook {
		if bin, err = exec.LookPath(def.Binary); err != nil {
			return fmt.Errorf("harness binary %q not found on PATH", def.Binary)
		}
	}

	// Ensure the config dir exists so credentials land in the profile.
	configDir := a.paths.ProfileConfigDir(h, n)
	if _, err := layer.PrepareConfigDir(def, p, configDir, false); err != nil {
		return err
	}

	// The login runs with the environment the profile's wrapper gives the harness,
	// including the exec hook's env variables; a declared env operation that fails (or
	// whose executable is missing) stops the login as it stops sync's wrapper render
	// (FR-011, FR-027).
	hctx := wrapper.HookContext(p, configDir)
	hookEnv, err := harness.RunEnvHook(def, hctx)
	if err != nil {
		return fmt.Errorf("profile %s: %w", p.ID(), err)
	}
	env, err := loginEnv(p, def, configDir, hookEnv, os.Environ())
	if err != nil {
		return err
	}

	if useHook {
		fmt.Fprintf(a.out, "running %s login for profile %s (config dir: %s)\n", harness.HookExecutable(h), p.ID(), configDir)
		herr := harness.RunLoginHook(def, hctx, env, a.out, a.errOut)
		if herr == nil {
			return nil
		}
		if !errors.Is(herr, harness.ErrHookUnsupported) && !errors.Is(herr, harness.ErrHookNotFound) {
			return herr
		}
		if argv == nil {
			return fmt.Errorf("profile %s: %w, and the definition declares no login_command to fall back to", p.ID(), herr)
		}
		if bin, err = exec.LookPath(def.Binary); err != nil {
			return fmt.Errorf("harness binary %q not found on PATH", def.Binary)
		}
	}

	// Replace this process with the harness so the login is fully interactive
	// (inherits the terminal).
	fmt.Fprintf(a.out, "launching %s for profile %s login (config dir: %s)\n", strings.Join(argv, " "), p.ID(), configDir)
	if note := def.LoginCommand.Note; note != "" {
		fmt.Fprintln(a.out, note)
	}
	return execLogin(bin, argv, env)
}

// loginEnv builds the process environment for an interactive login from the
// caller's environment: the environment the profile's wrapper gives the harness
// (wrapper.Environ shares the wrapper's environment model), with hookEnv, the exec
// hook's env output, as caller-overridable variables listed in APROFILE_SET_VARS.
func loginEnv(p *profile.Profile, def *harness.Definition, configDir string, hookEnv []harness.EnvVar, environ []string) ([]string, error) {
	return wrapper.Environ(p, def, configDir, wrapper.RenderOptions{HookEnv: hookEnv}, environ)
}
