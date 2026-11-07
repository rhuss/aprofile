package harness_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
)

// TestBuiltinBackends covers T087: the backend to environment mapping cc-deck import
// uses is definition data, in the order the definition lists it.
func TestBuiltinBackends(t *testing.T) {
	set, err := harness.LoadBuiltins()
	if err != nil || len(set.Errors()) != 0 {
		t.Fatalf("LoadBuiltins: %v %v", err, set.Errors())
	}
	claude, _ := set.Get("claude")
	vertex, ok := claude.Backends["vertex"]
	if !ok {
		t.Fatalf("claude declares no vertex backend: %+v", claude.Backends)
	}
	want := harness.BackendEnv{
		{Name: "CLAUDE_CODE_USE_VERTEX", Value: "1"},
		{Name: "ANTHROPIC_VERTEX_PROJECT_ID", Value: "{project}"},
		{Name: "CLOUD_ML_REGION", Value: "{region}"},
	}
	if !reflect.DeepEqual(vertex.Env, want) {
		t.Errorf("vertex env = %+v, want %+v", vertex.Env, want)
	}
	if vertex.CredentialsVar != "GOOGLE_APPLICATION_CREDENTIALS" {
		t.Errorf("vertex credentials_var = %q", vertex.CredentialsVar)
	}
	if b, ok := claude.Backends["anthropic"]; !ok || len(b.Env) != 0 || b.CredentialsVar != "" {
		t.Errorf("claude anthropic backend = %+v, %v; want declared and empty", b, ok)
	}
	// cc-deck's backends per harness (cc-deck internal/profile/*.go Backends()).
	for h, names := range map[string][]string{
		"codex":    {"openai"},
		"opencode": {"anthropic", "openai"},
	} {
		d, _ := set.Get(h)
		for _, n := range names {
			if _, ok := d.Backends[n]; !ok {
				t.Errorf("%s declares no %s backend", h, n)
			}
		}
	}
}

func TestBackendExpand(t *testing.T) {
	b := harness.Backend{Env: harness.BackendEnv{
		{Name: "SWITCH", Value: "1"},
		{Name: "PROJECT", Value: "{project}"},
		{Name: "REGION", Value: "{region}"},
		{Name: "BOTH", Value: "{project}/{region}"},
	}}
	got := b.Expand(map[string]string{"project": "p1"})
	want := harness.BackendEnv{
		{Name: "SWITCH", Value: "1"},
		{Name: "PROJECT", Value: "p1"},
		{Name: "BOTH", Value: "p1/"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Expand = %+v, want %+v (a value that expands to empty is left out)", got, want)
	}
}

func TestBackendsValidation(t *testing.T) {
	base := `name: tool
binary: tool
wrapper_prefix: tool
config_env: { TOOL_HOME: "." }
model_var: TOOL_MODEL
auth_kinds:
  - name: api-key
    vars: { TOOL_KEY: ref }
`
	cases := map[string]string{
		"bad var name":           "backends:\n  cloud:\n    env: { \"BAD-NAME\": \"1\" }\n",
		"unknown placeholder":    "backends:\n  cloud:\n    env: { TOOL_CLOUD: \"{zone}\" }\n",
		"aprofile var":           "backends:\n  cloud:\n    env: { APROFILE_X: \"1\" }\n",
		"config_env var":         "backends:\n  cloud:\n    env: { TOOL_HOME: \"1\" }\n",
		"auth var":               "backends:\n  cloud:\n    env: { TOOL_KEY: \"1\" }\n",
		"model var":              "backends:\n  cloud:\n    env: { TOOL_MODEL: \"m\" }\n",
		"multi-line value":       "backends:\n  cloud:\n    env: { TOOL_CLOUD: \"a\\nb\" }\n",
		"bad credentials_var":    "backends:\n  cloud:\n    credentials_var: \"1BAD\"\n",
		"credentials_var is env": "backends:\n  cloud:\n    env: { TOOL_CRED: x }\n    credentials_var: TOOL_CRED\n",
		"bad backend name":       "backends:\n  Cloud_X: {}\n",
		"env not a mapping":      "backends:\n  cloud:\n    env: [a, b]\n",
	}
	for name, extra := range cases {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "tool.yaml"), []byte(base+extra), 0o644); err != nil {
			t.Fatal(err)
		}
		set, _ := harness.Load(dir)
		if _, ok := set.Get("tool"); ok {
			t.Errorf("%s: definition loaded, want a load error", name)
			continue
		}
		errs := set.Errors()
		if len(errs) != 1 || !strings.Contains(errs[0].Error(), "backends") {
			t.Errorf("%s: errors %v, want one naming field backends", name, errs)
		}
	}

	ok := base + "backends:\n  cloud:\n    env: { TOOL_CLOUD: \"1\", TOOL_PROJECT: \"{project}\", TOOL_REGION: \"{region}\" }\n    credentials_var: TOOL_CRED\n  plain: {}\n"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tool.yaml"), []byte(ok), 0o644); err != nil {
		t.Fatal(err)
	}
	set, _ := harness.Load(dir)
	d, found := set.Get("tool")
	if !found {
		t.Fatalf("valid backends rejected: %v", set.Errors())
	}
	if len(d.Backends) != 2 || d.Backends["cloud"].CredentialsVar != "TOOL_CRED" {
		t.Errorf("backends = %+v", d.Backends)
	}
}

// TestBackendsOverrideMerge: a harnesses.d file adds a backend to a built-in by key,
// keeping the built-in ones (maps merge by key).
func TestBackendsOverrideMerge(t *testing.T) {
	dir := t.TempDir()
	override := "name: claude\nbackends:\n  bedrock:\n    env: { CLAUDE_CODE_USE_BEDROCK: \"1\", AWS_REGION: \"{region}\" }\n"
	if err := os.WriteFile(filepath.Join(dir, "claude.yaml"), []byte(override), 0o644); err != nil {
		t.Fatal(err)
	}
	set, _ := harness.Load(dir)
	if errs := set.Errors(); len(errs) != 0 {
		t.Fatalf("load errors: %v", errs)
	}
	d, _ := set.Get("claude")
	if _, ok := d.Backends["vertex"]; !ok {
		t.Errorf("built-in vertex backend lost by the override")
	}
	if got := d.Backends["bedrock"].Env; len(got) != 2 || got[0].Name != "CLAUDE_CODE_USE_BEDROCK" {
		t.Errorf("bedrock env = %+v", got)
	}
	if !strings.HasSuffix(d.Source["backends"], "claude.yaml") {
		t.Errorf("source of backends = %q, want the override file", d.Source["backends"])
	}
}
