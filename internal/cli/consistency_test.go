package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/mcp"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/testutil"
)

// runCLI runs the root command in-process with stdin input and returns combined
// output and the error.
func runCLIInput(t *testing.T, input string, args ...string) (string, error) {
	t.Helper()
	root := NewRootCmd()
	out := &bytes.Buffer{}
	root.SetIn(strings.NewReader(input))
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func mustCLI(t *testing.T, args ...string) string {
	t.Helper()
	out, err := runCLIInput(t, "", args...)
	if err != nil {
		t.Fatalf("aprofile %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func fakeRunning(t *testing.T, running, known bool) {
	t.Helper()
	orig := profileRunning
	t.Cleanup(func() { profileRunning = orig })
	profileRunning = func(string) (profile.Process, bool, bool) {
		if running {
			return profile.Process{PID: 4242, Name: "claude"}, true, true
		}
		return profile.Process{}, false, known
	}
}

// T082: when the running check cannot tell, rename asks first; off a terminal it
// needs --force (exit 3 without it) and changes nothing.
func TestRenameUnknownRunningNeedsForce(t *testing.T) {
	testutil.HermeticHome(t)
	mustCLI(t, "add", "claude", "old", "--auth-kind", "oauth-login")
	fakeRunning(t, false, false)

	out, err := runCLIInput(t, "", "rename", "claude/old", "new")
	if exitCode(err) != ExitNeedsConfirmation {
		t.Fatalf("rename with unknown running state off a TTY: exit %d, want %d (%v)\n%s", exitCode(err), ExitNeedsConfirmation, err, out)
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error should name --force: %v", err)
	}
	paths, _ := config.ResolvePaths()
	if _, err := os.Stat(paths.ProfileStorePath("claude", "old")); err != nil {
		t.Fatalf("refused rename must leave the profile: %v", err)
	}

	// On a terminal, "no" aborts and "yes" renames.
	orig := stdinIsTTY
	t.Cleanup(func() { stdinIsTTY = orig })
	stdinIsTTY = func(*cobra.Command) bool { return true }
	out, err = runCLIInput(t, "n\n", "rename", "claude/old", "new")
	if err != nil || !strings.Contains(out, "cannot tell whether profile claude/old is running") {
		t.Fatalf("declined rename: err=%v\n%s", err, out)
	}
	if _, err := os.Stat(paths.ProfileStorePath("claude", "new")); err == nil {
		t.Fatalf("declined rename must change nothing")
	}
	if _, err := runCLIInput(t, "y\n", "rename", "claude/old", "new"); err != nil {
		t.Fatalf("confirmed rename: %v", err)
	}
	stdinIsTTY = orig

	// --force skips the question.
	if out, err := runCLIInput(t, "", "rename", "claude/new", "newer", "--force"); err != nil {
		t.Fatalf("rename --force: %v\n%s", err, out)
	}
}

// FR-006a: a profile known to be running is refused with exit 4 unless --force.
func TestRenameRunningRefused(t *testing.T) {
	testutil.HermeticHome(t)
	mustCLI(t, "add", "claude", "old", "--auth-kind", "oauth-login")
	fakeRunning(t, true, true)
	if _, err := runCLIInput(t, "", "rename", "claude/old", "new"); exitCode(err) != ExitProfileRunning {
		t.Fatalf("running profile: exit %d, want %d (%v)", exitCode(err), ExitProfileRunning, err)
	}
	mustCLI(t, "rename", "claude/old", "new", "--force")
}

// T075: --auth-kind clears the references that do not belong to the new kind.
func TestEditAuthKindClearsForeignRefs(t *testing.T) {
	testutil.HermeticHome(t)
	mustCLI(t, "add", "claude", "wk", "--auth-kind", "api-key", "--auth-key", "env:MY_KEY")
	mustCLI(t, "edit", "claude/wk", "--auth-kind", "oauth-login")
	paths, _ := config.ResolvePaths()
	p, err := profile.NewStore(paths).Load("claude", "wk")
	if err != nil {
		t.Fatal(err)
	}
	if p.AuthKind != "oauth-login" || len(p.AuthRefs) != 0 {
		t.Fatalf("auth after kind change = %s %v, want oauth-login and no refs", p.AuthKind, p.AuthRefs)
	}

	// A kind that needs a key it does not get is refused, naming --auth-key.
	_, err = runCLIInput(t, "", "edit", "claude/wk", "--auth-kind", "auth-token")
	if err == nil || !strings.Contains(err.Error(), "--auth-key") {
		t.Fatalf("kind without its key should be refused naming --auth-key, got %v", err)
	}
	mustCLI(t, "edit", "claude/wk", "--auth-kind", "auth-token", "--auth-key", "env:TOK")
	p, _ = profile.NewStore(paths).Load("claude", "wk")
	if len(p.AuthRefs) != 1 || p.AuthRefs["ANTHROPIC_AUTH_TOKEN"].Raw != "env:TOK" {
		t.Fatalf("refs = %v, want only ANTHROPIC_AUTH_TOKEN=env:TOK", p.AuthRefs)
	}
}

// T075: a repeated --env NAME in add is stored once, the last value winning.
func TestAddDedupesRepeatedEnv(t *testing.T) {
	testutil.HermeticHome(t)
	mustCLI(t, "add", "claude", "wk", "--auth-kind", "oauth-login",
		"--env", "TEAM=a", "--env", "TEAM=b", "--env-literal", "MODE=x", "--env-literal", "MODE=y", "--env", "MODE=z")
	paths, _ := config.ResolvePaths()
	p, err := profile.NewStore(paths).Load("claude", "wk")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]profile.ExtraEnvVar{}
	for _, e := range p.ExtraEnv {
		got[e.Name] = append(got[e.Name], e)
	}
	if len(got) != 2 {
		t.Errorf("stored variables = %+v, want TEAM and MODE only", p.ExtraEnv)
	}
	if want := (profile.ExtraEnvVar{Name: "TEAM", Value: "b"}); len(got["TEAM"]) != 1 || got["TEAM"][0] != want {
		t.Errorf("TEAM = %+v, want [%+v]", got["TEAM"], want)
	}
	// --env-literal is applied after --env, so its last value (y, stored as a
	// literal) wins over --env MODE=z.
	if want := (profile.ExtraEnvVar{Name: "MODE", Value: "y", Literal: true}); len(got["MODE"]) != 1 || got["MODE"][0] != want {
		t.Errorf("MODE = %+v, want [%+v]", got["MODE"], want)
	}
}

// T075: a server named twice in mcp copy is converted (or replaced) once.
func TestSelectServersDedupes(t *testing.T) {
	servers := []mcp.Server{{Name: "gh"}, {Name: "web"}}
	got, err := selectServers(servers, []string{"gh", "web", "gh"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "gh" || got[1].Name != "web" {
		t.Fatalf("selected = %+v, want gh, web once each", got)
	}
}

// T069: add and rename reject a wrapper name another profile already has, naming
// that profile.
func TestWrapperNameClashRejected(t *testing.T) {
	home := testutil.HermeticHome(t)
	// Rename checks whether the profile is running; pin the answer to "not
	// running" so the test never depends on the processes of the machine.
	fakeRunning(t, false, true)
	testutil.WriteFile(t, filepath.Join(home.ConfigHome, "aprofile", "harnesses.d", "cx.yaml"),
		"name: cx\nbinary: cx\nwrapper_prefix: claude-x\nconfig_env:\n  CX_DIR: \".\"\n")
	mustCLI(t, "add", "claude", "x-aa", "--auth-kind", "oauth-login")

	_, err := runCLIInput(t, "", "add", "cx", "aa")
	if err == nil || !strings.Contains(err.Error(), "claude/x-aa") || !strings.Contains(err.Error(), "claude-x-aa") {
		t.Fatalf("add should name the clashing profile and wrapper, got %v", err)
	}
	mustCLI(t, "add", "cx", "bb")
	_, err = runCLIInput(t, "", "rename", "cx/bb", "aa")
	if err == nil || !strings.Contains(err.Error(), "claude/x-aa") {
		t.Fatalf("rename should name the clashing profile, got %v", err)
	}
	paths, _ := config.ResolvePaths()
	if _, err := os.Stat(paths.ProfileStorePath("cx", "bb")); err != nil {
		t.Fatalf("refused rename must leave cx/b: %v", err)
	}
	// Renaming the clashing profile itself away frees the name.
	mustCLI(t, "rename", "claude/x-aa", "x-cc")
	mustCLI(t, "rename", "cx/bb", "aa")
}

// Imported credential files live outside the config dir: rename moves them and
// rewrites the profile's references; remove --purge deletes them.
func TestRenameAndPurgeMoveImportedCredentials(t *testing.T) {
	testutil.HermeticHome(t)
	paths, _ := config.ResolvePaths()
	oldCreds := paths.CredentialsDir("claude", "old")
	mustCLI(t, "add", "claude", "old", "--auth-kind", "api-key", "--auth-key", "file:"+filepath.Join(oldCreds, "api_key"),
		"--env-literal", "GOOGLE_APPLICATION_CREDENTIALS="+filepath.Join(oldCreds, "credentials"),
		"--env", "OTHER=file:/elsewhere/x")
	testutil.WriteFile(t, filepath.Join(oldCreds, "api_key"), "FAKE\n")
	testutil.WriteFile(t, filepath.Join(oldCreds, "credentials"), "{}\n")
	fakeRunning(t, false, true)

	mustCLI(t, "rename", "claude/old", "new")
	newCreds := paths.CredentialsDir("claude", "new")
	if _, err := os.Stat(oldCreds); !os.IsNotExist(err) {
		t.Errorf("old credentials dir still exists: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(newCreds, "api_key")); err != nil || string(data) != "FAKE\n" {
		t.Fatalf("credential file not moved: %v %q", err, data)
	}
	p, err := profile.NewStore(paths).Load("claude", "new")
	if err != nil {
		t.Fatal(err)
	}
	if got := p.AuthRefs["ANTHROPIC_API_KEY"].Raw; got != "file:"+filepath.Join(newCreds, "api_key") {
		t.Errorf("auth ref = %q, want the moved path", got)
	}
	env := map[string]string{}
	for _, e := range p.ExtraEnv {
		env[e.Name] = e.Value
	}
	if env["GOOGLE_APPLICATION_CREDENTIALS"] != filepath.Join(newCreds, "credentials") {
		t.Errorf("path variable = %q, want the moved path", env["GOOGLE_APPLICATION_CREDENTIALS"])
	}
	if env["OTHER"] != "file:/elsewhere/x" {
		t.Errorf("unrelated reference changed: %q", env["OTHER"])
	}

	mustCLI(t, "remove", "claude", "new", "--purge", "--yes")
	if _, err := os.Stat(newCreds); !os.IsNotExist(err) {
		t.Errorf("remove --purge left the imported credentials: %v", err)
	}
}
