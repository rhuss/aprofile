package wrapper_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/secret"
	"github.com/cc-deck/aprofile/internal/wrapper"
)

// envDef is a data-only harness with every part of the environment contract: a
// config_env redirect, model and endpoint variables, two auth kinds and auth_clears.
// Its binary is env, so running the wrapper prints what the harness would see.
func envDef(t *testing.T) *harness.Definition {
	t.Helper()
	src := `name: envy
binary: env
wrapper_prefix: envy
config_env:
  ENVY_DIR: "."
model_var: ENVY_MODEL
endpoint_var: ENVY_URL
auth_clears: [ENVY_USE_CLOUD, ENVY_REGION]
auth_kinds:
  - name: key
    vars: { ENVY_KEY: ref }
  - name: login
    vars: {}
default_auth_kind: key
`
	set := harness.LoadFromBytes(map[string][]byte{"envy.yaml": []byte(src)})
	d, ok := set.Get("envy")
	if !ok {
		t.Fatalf("load: %v", set.Errors())
	}
	return d
}

func mustRef(t *testing.T, s string) secret.Reference {
	t.Helper()
	r, err := secret.ParseReference(s)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// runScript writes the wrapper for p and runs it with exactly env, returning the
// environment the harness (env) printed.
func runScript(t *testing.T, p *profile.Profile, def *harness.Definition, cfg string, opts wrapper.RenderOptions, env []string) map[string]string {
	t.Helper()
	s, err := wrapper.RenderWith(p, def, cfg, opts)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "w")
	if err := os.WriteFile(path, []byte(s), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", path)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("wrapper failed: %v\n%s", err, out)
	}
	m := map[string]string{}
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if i := strings.IndexByte(line, '='); i > 0 {
			m[line[:i]] = line[i+1:]
		}
	}
	return m
}

func toMap(kvs []string) map[string]string {
	m := map[string]string{}
	for _, kv := range kvs {
		if i := strings.IndexByte(kv, '='); i > 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}

// M8: Environ (the login environment) is the environment the rendered wrapper gives
// the harness, for the same caller environment, including the exec-hook env
// variables, the nested-session cleanup and APROFILE_SET_VARS.
func TestEnvironMatchesRenderedWrapper(t *testing.T) {
	def := envDef(t)
	cfg := t.TempDir()
	keyFile := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(keyFile, []byte("filekey\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := &profile.Profile{Harness: "envy", Name: "work", AuthKind: "key", Model: "m1", Endpoint: "https://e",
		Color: "#112233", Icon: "x", HerdrAgent: "agent",
		AuthRefs: map[string]secret.Reference{"ENVY_KEY": mustRef(t, "file:"+keyFile)},
		ExtraEnv: []profile.ExtraEnvVar{
			{Name: "TEAM", Value: "platform", Literal: true},
			{Name: "FROM_SRC", Value: "env:SRC"},
			{Name: "ENVY_REGION", Value: "env:ENVY_REGION"},
		}}
	opts := wrapper.RenderOptions{HookEnv: []harness.EnvVar{{Name: "HOOK_LIT", Value: "hv"}, {Name: "HOOK_REF", Value: "env:SRC2"}}}

	callers := map[string][]string{
		"plain": {"PATH=/usr/bin:/bin", "SRC=s1", "SRC2=s2", "ENVY_REGION=us-east", "ENVY_USE_CLOUD=1", "ENVY_KEY=leak", "TEAM="},
		"nested": {"PATH=/usr/bin:/bin", "SRC=s1", "SRC2=s2", "ENVY_REGION=eu",
			"APROFILE_NAME=other", "APROFILE_HARNESS=envy", "APROFILE_SET_VARS=ENVY_MODEL OTHER_ONLY TEAM",
			"ENVY_MODEL=other-model", "ENVY_URL=https://other", "OTHER_ONLY=x", "TEAM=other"},
		"same-profile overrides": {"PATH=/usr/bin:/bin", "SRC=s1", "SRC2=s2", "ENVY_REGION=ap",
			"APROFILE_NAME=work", "APROFILE_HARNESS=envy", "ENVY_MODEL=mine", "HOOK_LIT=mine"},
	}
	for name, caller := range callers {
		t.Run(name, func(t *testing.T) {
			got := runScript(t, p, def, cfg, opts, caller)
			env, err := wrapper.Environ(p, def, cfg, opts, caller)
			if err != nil {
				t.Fatalf("Environ: %v", err)
			}
			want := toMap(env)
			for _, shellOnly := range []string{"PWD", "OLDPWD", "SHLVL", "_"} {
				delete(got, shellOnly)
				delete(want, shellOnly)
			}
			var diffs []string
			for k, v := range want {
				if g, ok := got[k]; !ok || g != v {
					diffs = append(diffs, k+": Environ="+v+" wrapper="+g)
				}
			}
			for k, v := range got {
				if _, ok := want[k]; !ok {
					diffs = append(diffs, k+": only in wrapper ("+v+")")
				}
			}
			sort.Strings(diffs)
			if len(diffs) > 0 {
				t.Errorf("Environ differs from the wrapper:\n  %s", strings.Join(diffs, "\n  "))
			}
		})
	}
}

// Corr8: an extra variable that passes a cleared variable through from the caller
// (`--env ENVY_REGION=env:ENVY_REGION`) reads the caller's value from before the
// auth clears, in the wrapper and in the login environment.
func TestExtraReferenceToClearedVariable(t *testing.T) {
	def := envDef(t)
	cfg := t.TempDir()
	p := &profile.Profile{Harness: "envy", Name: "c", AuthKind: "login",
		ExtraEnv: []profile.ExtraEnvVar{{Name: "ENVY_REGION", Value: "env:ENVY_REGION"}, {Name: "R2", Value: "env:ENVY_USE_CLOUD"}}}
	caller := []string{"PATH=/usr/bin:/bin", "ENVY_REGION=us-east5", "ENVY_USE_CLOUD=1"}

	got := runScript(t, p, def, cfg, wrapper.RenderOptions{}, caller)
	if got["ENVY_REGION"] != "us-east5" || got["R2"] != "1" {
		t.Errorf("wrapper: ENVY_REGION=%q R2=%q, want us-east5 and 1", got["ENVY_REGION"], got["R2"])
	}
	if _, has := got["ENVY_USE_CLOUD"]; has {
		t.Error("the cleared ENVY_USE_CLOUD must stay cleared")
	}
	for k := range got {
		if strings.HasPrefix(k, "_aprofile") {
			t.Errorf("wrapper leaks its temporary %s", k)
		}
	}
	env, err := wrapper.Environ(p, def, cfg, wrapper.RenderOptions{}, caller)
	if err != nil {
		t.Fatalf("Environ: %v", err)
	}
	m := toMap(env)
	if m["ENVY_REGION"] != "us-east5" || m["R2"] != "1" {
		t.Errorf("Environ: ENVY_REGION=%q R2=%q, want us-east5 and 1", m["ENVY_REGION"], m["R2"])
	}
	if _, has := m["ENVY_USE_CLOUD"]; has {
		t.Error("Environ: the cleared ENVY_USE_CLOUD must stay cleared")
	}

	// Unset in the caller: both fail closed naming the reference.
	if _, err := wrapper.Environ(p, def, cfg, wrapper.RenderOptions{}, []string{"PATH=/usr/bin:/bin", "ENVY_USE_CLOUD=1"}); err == nil ||
		!strings.Contains(err.Error(), "env:ENVY_REGION") {
		t.Errorf("Environ with the source unset: want an error naming env:ENVY_REGION, got %v", err)
	}
}
