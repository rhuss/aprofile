package layer_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/layer"
)

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, b)
	}
	return m
}

const profileSettings = `{
  "permissions": {"allow": ["Bash(ls)", "Bash(work-only)"], "deny": ["Read(.env)"]},
  "statusLine": "old",
  "hooks": {"PreToolUse": [{"matcher": "mine"}]},
  "profileOnly": true
}`

const defaultSettings = `{
  "permissions": {"allow": ["Bash(ls)", "Bash(make)"]},
  "statusLine": "new",
  "hooks": {"PreToolUse": [{"matcher": "default"}]},
  "enabledPlugins": {"x": true}
}`

// T038/FR-018: merge rules.
func TestMergeSettings(t *testing.T) {
	out, removed, changed, err := layer.MergeSettings([]byte(profileSettings), []byte(defaultSettings), nil, []string{"hooks"})
	if err != nil {
		t.Fatal(err)
	}
	m := decode(t, out)
	allow := m["permissions"].(map[string]any)["allow"].([]any)
	if !reflect.DeepEqual(allow, []any{"Bash(ls)", "Bash(work-only)", "Bash(make)"}) {
		t.Errorf("lists gain missing default entries in order, profile entries stay: %v", allow)
	}
	if _, ok := m["permissions"].(map[string]any)["deny"]; !ok {
		t.Error("profile-only nested keys are kept")
	}
	if m["statusLine"] != "new" {
		t.Errorf("scalars take the default's value: %v", m["statusLine"])
	}
	if m["profileOnly"] != true {
		t.Error("profile-only top-level keys are kept")
	}
	if !reflect.DeepEqual(m["hooks"], decode(t, []byte(profileSettings))["hooks"]) {
		t.Errorf("the hook location key is never touched: %v", m["hooks"])
	}
	if _, ok := m["enabledPlugins"]; !ok {
		t.Error("keys only the default has are added")
	}
	if len(removed) != 0 {
		t.Errorf("merge removes nothing: %v", removed)
	}
	for _, want := range []string{"statusLine", "enabledPlugins", "permissions.allow"} {
		if !containsPrefix(changed, want) {
			t.Errorf("changed should list %s: %v", want, changed)
		}
	}

	// Named keys only.
	out, _, changed, err = layer.MergeSettings([]byte(profileSettings), []byte(defaultSettings), []string{"statusLine"}, []string{"hooks"})
	if err != nil {
		t.Fatal(err)
	}
	m = decode(t, out)
	if _, ok := m["enabledPlugins"]; ok || m["statusLine"] != "new" {
		t.Errorf("only the named key is pulled: %v", m)
	}
	if len(changed) != 1 {
		t.Errorf("changed = %v", changed)
	}
}

// T038/US3 AS6: replace makes the pulled keys an exact copy and lists what goes.
func TestReplaceSettings(t *testing.T) {
	out, removed, _, err := layer.ReplaceSettings([]byte(profileSettings), []byte(defaultSettings), nil, []string{"hooks"})
	if err != nil {
		t.Fatal(err)
	}
	m := decode(t, out)
	allow := m["permissions"].(map[string]any)["allow"].([]any)
	if !reflect.DeepEqual(allow, []any{"Bash(ls)", "Bash(make)"}) {
		t.Errorf("replace copies the default exactly: %v", allow)
	}
	if _, ok := m["profileOnly"]; ok {
		t.Error("replace of the whole file drops profile-only keys")
	}
	if !reflect.DeepEqual(m["hooks"], decode(t, []byte(profileSettings))["hooks"]) {
		t.Error("the hook location key is kept as the profile's")
	}
	for _, want := range []string{`permissions.allow["Bash(work-only)"]`, "permissions.deny", "profileOnly"} {
		if !containsPrefix(removed, want) {
			t.Errorf("removed should list %s: %v", want, removed)
		}
	}
}

func TestSettingsInvalidJSON(t *testing.T) {
	if _, _, _, err := layer.MergeSettings([]byte("{bad"), []byte(defaultSettings), nil, nil); err == nil {
		t.Error("an invalid profile file must fail")
	}
	if _, _, _, err := layer.ReplaceSettings([]byte(profileSettings), []byte("[1]"), nil, nil); err == nil {
		t.Error("a default file that is not an object must fail")
	}
}

func TestSecretWarnings(t *testing.T) {
	doc := `{"env": {"ANTHROPIC_API_KEY": "abc123", "TOKEN_REF": "env:X", "PLAIN": "hello", "GH": "ghp_abcdef"}, "statusLine": "x"}`
	got := strings.Join(layer.SecretWarnings([]byte(doc)), ",")
	if got != "env.ANTHROPIC_API_KEY,env.GH" {
		t.Errorf("SecretWarnings = %q", got)
	}
	if strings.Contains(got, "abc123") {
		t.Error("warnings must never contain values")
	}
}

func containsPrefix(list []string, prefix string) bool {
	for _, s := range list {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}
