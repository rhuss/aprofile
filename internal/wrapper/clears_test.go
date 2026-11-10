package wrapper_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/secret"
	"github.com/cc-deck/aprofile/internal/wrapper"
)

// unsetLine returns the wrapper's auth unset line (the one listing the auth vars).
func unsetLine(t *testing.T, script, mustHave string) string {
	t.Helper()
	for _, l := range strings.Split(script, "\n") {
		if strings.HasPrefix(l, "unset ") && strings.Contains(l, mustHave) {
			return l
		}
	}
	t.Fatalf("no unset line containing %s\n---\n%s", mustHave, script)
	return ""
}

// T063: the Claude provider switches are unset with the auth variables, so an
// exported CLAUDE_CODE_USE_VERTEX cannot redirect a profile (FR-027).
func TestRenderUnsetsAuthClears(t *testing.T) {
	s := renderWork(t)
	line := unsetLine(t, s, "ANTHROPIC_API_KEY")
	for _, v := range []string{
		"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN",
		"CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_FOUNDRY",
		"ANTHROPIC_VERTEX_PROJECT_ID", "CLOUD_ML_REGION", "ANTHROPIC_BEDROCK_BASE_URL",
		"ANTHROPIC_VERTEX_BASE_URL", "AWS_BEARER_TOKEN_BEDROCK",
		"CLAUDE_CODE_SKIP_VERTEX_AUTH", "CLAUDE_CODE_SKIP_BEDROCK_AUTH",
	} {
		if !strings.Contains(" "+line+" ", " "+v+" ") {
			t.Errorf("unset line missing %s: %s", v, line)
		}
	}
}

// cloudDef is a data-only harness whose auth_clears names a provider switch that
// one auth kind ("cloud") sets itself. The binary is env, so running the wrapper
// prints the environment the harness would see.
func cloudDef(t *testing.T) *harness.Definition {
	t.Helper()
	src := `name: cloudy
binary: env
wrapper_prefix: cloudy
config_env:
  CLOUDY_DIR: "."
auth_clears: [CLOUDY_USE_CLOUD, CLOUDY_REGION]
auth_kinds:
  - name: login
    vars: {}
  - name: cloud
    vars: { CLOUDY_USE_CLOUD: ref }
`
	set := harness.LoadFromBytes(map[string][]byte{"cloudy.yaml": []byte(src)})
	d, ok := set.Get("cloudy")
	if !ok {
		t.Fatalf("load: %v", set.Errors())
	}
	return d
}

func runRendered(t *testing.T, p *profile.Profile, def *harness.Definition, env []string) string {
	t.Helper()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "cfg")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := wrapper.Render(p, def, cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "w")
	if err := os.WriteFile(path, []byte(s), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", path)
	cmd.Env = append([]string{"PATH=/usr/bin:/bin"}, env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("wrapper failed: %v\n%s", err, out)
	}
	return string(out)
}

// Clears apply to every kind except the one that sets the variable.
func TestRenderClearsExceptOwnKind(t *testing.T) {
	def := cloudDef(t)
	shell := []string{"CLOUDY_USE_CLOUD=from-shell", "CLOUDY_REGION=shell-region"}

	login := &profile.Profile{Harness: "cloudy", Name: "a", AuthKind: "login"}
	out := runRendered(t, login, def, shell)
	if strings.Contains(out, "CLOUDY_USE_CLOUD=") || strings.Contains(out, "CLOUDY_REGION=") {
		t.Errorf("a login profile must not inherit the shell's provider switch:\n%s", out)
	}

	ref, _ := secret.ParseReference("env:MY_CLOUD_FLAG")
	cloud := &profile.Profile{Harness: "cloudy", Name: "b", AuthKind: "cloud",
		AuthRefs: map[string]secret.Reference{"CLOUDY_USE_CLOUD": ref}}
	out = runRendered(t, cloud, def, append(shell, "MY_CLOUD_FLAG=1"))
	if !strings.Contains(out, "CLOUDY_USE_CLOUD=1\n") {
		t.Errorf("the cloud kind must set its own cleared var:\n%s", out)
	}
	if strings.Contains(out, "CLOUDY_REGION=") {
		t.Errorf("a clear the kind does not set stays cleared:\n%s", out)
	}

	// A profile may still opt in with an extra variable.
	optIn := &profile.Profile{Harness: "cloudy", Name: "c", AuthKind: "login",
		ExtraEnv: []profile.ExtraEnvVar{{Name: "CLOUDY_REGION", Value: "eu", Literal: true}}}
	out = runRendered(t, optIn, def, shell)
	if !strings.Contains(out, "CLOUDY_REGION=eu\n") {
		t.Errorf("a profile's extra variable must survive the clears:\n%s", out)
	}
}

// Spec 005 SC-002: neutralizing entries in the profile's own settings.json change
// nothing in the wrapper. An oauth-token wrapper still sets the token and clears
// the rest of the auth-clearing list; an oauth-login wrapper clears the whole list;
// both render byte-identically with and without the entries in the folder.
func TestRenderUnchangedByNeutralizingEntries(t *testing.T) {
	def := claudeDef(t)
	tokRef, _ := secret.ParseReference("env:WORK_TOKEN")
	profiles := map[string]*profile.Profile{
		"oauth-token": {Harness: "claude", Name: "tok", AuthKind: "oauth-token", Share: profile.ShareContent,
			AuthRefs: map[string]secret.Reference{"CLAUDE_CODE_OAUTH_TOKEN": tokRef}},
		"oauth-login": {Harness: "claude", Name: "login", AuthKind: "oauth-login", Share: profile.ShareContent},
	}
	for name, p := range profiles {
		bare := filepath.Join(t.TempDir(), "cfg")
		withEntries := filepath.Join(t.TempDir(), "cfg")
		var entries []string
		for _, v := range def.UnsetAuthVars() {
			if name == "oauth-token" && v == "CLAUDE_CODE_OAUTH_TOKEN" {
				continue
			}
			entries = append(entries, `"`+v+`":""`)
		}
		for dir, content := range map[string]string{bare: `{}`, withEntries: `{"env":{` + strings.Join(entries, ",") + `}}`} {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		a, err := wrapper.Render(p, def, bare)
		if err != nil {
			t.Fatal(err)
		}
		b, err := wrapper.Render(p, def, withEntries)
		if err != nil {
			t.Fatal(err)
		}
		if strings.ReplaceAll(a, bare, "CFG") != strings.ReplaceAll(b, withEntries, "CFG") {
			t.Errorf("%s: neutralizing entries change the wrapper:\n--- without\n%s\n--- with\n%s", name, a, b)
		}
		// The wrapper clears the whole list, then assigns its own token from the
		// source captured before the unset.
		line := unsetLine(t, a, "ANTHROPIC_API_KEY")
		for _, v := range def.UnsetAuthVars() {
			if !strings.Contains(" "+line+" ", " "+v+" ") {
				t.Errorf("%s: unset line missing %s: %s", name, v, line)
			}
		}
		if name == "oauth-token" {
			assign := strings.Index(a, `CLAUDE_CODE_OAUTH_TOKEN="${_aprofile_src_WORK_TOKEN}"`)
			if assign < 0 || assign < strings.Index(a, line) {
				t.Errorf("oauth-token: the wrapper no longer sets the token after the unset:\n%s", a)
			}
		}
	}
}
