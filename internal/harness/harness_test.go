package harness_test

import (
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
)

func TestLoadBuiltinsClaude(t *testing.T) {
	set, err := harness.LoadBuiltins()
	if err != nil {
		t.Fatalf("LoadBuiltins: %v", err)
	}
	if errs := set.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected load errors: %v", errs)
	}
	d, ok := set.Get("claude")
	if !ok {
		t.Fatal("claude definition not loaded")
	}
	if d.Binary != "claude" || d.WrapperPrefix != "claude" {
		t.Errorf("unexpected binary/prefix: %+v", d)
	}
	if d.ModelVar != "ANTHROPIC_MODEL" || d.EndpointVar != "ANTHROPIC_BASE_URL" {
		t.Errorf("unexpected model/endpoint vars: %+v", d)
	}
	if d.OmarchyProvider != "anthropic" {
		t.Errorf("omarchy provider = %q", d.OmarchyProvider)
	}
	if !d.HasCapability("mcp") || !d.HasCapability("login") {
		t.Errorf("missing capabilities: %v", d.Capabilities)
	}
}

func TestConfigEnvOrderPreserved(t *testing.T) {
	set, _ := harness.LoadBuiltins()
	d, _ := set.Get("claude")
	if len(d.ConfigEnv) != 1 {
		t.Fatalf("expected 1 config_env entry, got %d", len(d.ConfigEnv))
	}
	if d.ConfigEnv[0].Var != "CLAUDE_CONFIG_DIR" || d.ConfigEnv[0].Path != "." {
		t.Errorf("config_env entry = %+v", d.ConfigEnv[0])
	}
}

func TestMixedFileKeyParsing(t *testing.T) {
	set, _ := harness.LoadBuiltins()
	d, _ := set.Get("claude")

	var mixed *harness.EntryRule
	for i := range d.Classification {
		if d.Classification[i].Entry == ".claude.json" {
			mixed = &d.Classification[i]
			break
		}
	}
	if mixed == nil {
		t.Fatal(".claude.json classification rule not found")
	}
	if mixed.Layer != harness.LayerMixed {
		t.Errorf(".claude.json layer = %q, want mixed", mixed.Layer)
	}
	want := map[string]harness.Layer{
		"oauthAccount": harness.LayerCredentials,
		"mcpServers":   harness.LayerOwned,
		"projects":     harness.LayerState,
		"numStartups":  harness.LayerState,
	}
	for k, w := range want {
		if mixed.Keys[k] != w {
			t.Errorf("mixed key %q = %q, want %q", k, mixed.Keys[k], w)
		}
	}
}

func TestAuthKindsAndVars(t *testing.T) {
	set, _ := harness.LoadBuiltins()
	d, _ := set.Get("claude")

	k, ok := d.AuthKind("oauth-token")
	if !ok {
		t.Fatal("oauth-token auth kind missing")
	}
	if _, has := k.Vars["CLAUDE_CODE_OAUTH_TOKEN"]; !has {
		t.Errorf("oauth-token should set CLAUDE_CODE_OAUTH_TOKEN, got %v", k.Vars)
	}
	// oauth-login has no env vars.
	login, ok := d.AuthKind("oauth-login")
	if !ok {
		t.Fatal("oauth-login auth kind missing")
	}
	if len(login.Vars) != 0 {
		t.Errorf("oauth-login should have no vars, got %v", login.Vars)
	}
	// AllAuthVars de-duplicates and sorts across kinds.
	all := d.AllAuthVars()
	for _, want := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN"} {
		if !contains(all, want) {
			t.Errorf("AllAuthVars missing %q: %v", want, all)
		}
	}
	if d.DefaultAuthKind != "auth-token" {
		t.Errorf("default auth kind = %q", d.DefaultAuthKind)
	}
}

func TestInvalidDefinitionReportedWithoutBlockingOthers(t *testing.T) {
	// A bad definition is isolated; the other definitions keep working (FR-012).
	good := []byte("name: good\nbinary: good\nwrapper_prefix: good\nconfig_env:\n  GOOD_DIR: \".\"\n")
	bad := []byte("name: bad\nbinary: bad\n") // missing wrapper_prefix and config_env

	set := harness.LoadFromBytes(map[string][]byte{
		"good.yaml": good,
		"bad.yaml":  bad,
	})
	if _, ok := set.Get("good"); !ok {
		t.Error("good definition should load despite a sibling error")
	}
	errs := set.Errors()
	if len(errs) == 0 {
		t.Fatal("expected a load error for the bad definition")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e.File, "bad.yaml") {
			found = true
			if e.Field == "" {
				t.Errorf("load error should name a field: %v", e)
			}
		}
	}
	if !found {
		t.Errorf("no error naming bad.yaml: %v", errs)
	}
}

func TestNameValidation(t *testing.T) {
	ok := []string{"claude", "codex", "my-harness", "a1", "work2"}
	bad := []string{"", "-lead", "Trailing", "has_underscore", "ends-", "9start"}
	for _, s := range ok {
		if !harness.NameValid(s) {
			t.Errorf("NameValid(%q) = false, want true", s)
		}
	}
	for _, s := range bad {
		if harness.NameValid(s) {
			t.Errorf("NameValid(%q) = true, want false", s)
		}
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
