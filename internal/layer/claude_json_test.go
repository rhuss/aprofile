package layer_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/layer"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/testutil"
)

// T077: without CLAUDE_CONFIG_DIR, Claude Code keeps .claude.json next to its config
// dir, at ~/.claude.json, not inside ~/.claude. The definition says so through
// default_locations; every other entry still lives under ~/.claude.
func TestClaudeJSONDefaultLocation(t *testing.T) {
	d := claudeDef(t)
	home := "/home/u"
	if got, want := d.DefaultPathUnder(home, ".claude.json"), filepath.Join(home, ".claude.json"); got != want {
		t.Errorf("default .claude.json = %s, want %s", got, want)
	}
	if got, want := d.DefaultPathUnder(home, "settings.json"), filepath.Join(home, ".claude", "settings.json"); got != want {
		t.Errorf("default settings.json = %s, want %s", got, want)
	}
	if got, want := d.DefaultPathUnder(home, ""), filepath.Join(home, ".claude"); got != want {
		t.Errorf("default config dir = %s, want %s", got, want)
	}
}

// T077: the key names Claude Code 2.1.319 keeps in ~/.claude.json, classified so that
// credentials and state never reach a new profile. Only key names are listed here;
// the real file is never read by a test (Constitution VII).
func TestClaudeJSONKeyClassification(t *testing.T) {
	d := claudeDef(t)
	rule := layer.Classify(d, ".claude.json").Rule
	if rule == nil || rule.Layer != harness.LayerMixed {
		t.Fatal(".claude.json must be a mixed entry")
	}
	want := map[harness.Layer][]string{
		harness.LayerCredentials: {"oauthAccount", "account", "primaryApiKey", "customApiKeyResponses"},
		harness.LayerOwned:       {"mcpServers"},
		harness.LayerSettings:    {"autoUpdates", "showSpinnerTree", "theme", "verbose", "editorMode"},
		harness.LayerState: {
			"projects", "numStartups", "userID", "machineID", "anonymousId", "firstStartTime",
			"tipsHistory", "tipsHistoryByCommand", "tipLifetimeShownCounts", "migrationVersion",
			"hasCompletedOnboarding", "lastOnboardingVersion", "lastReleaseNotesSeen", "installMethod",
			"cachedGrowthBookFeatures", "cachedUsageUtilization", "modelAccessCache", "groveConfigCache",
			"githubRepoPaths", "skillUsage", "toolUsage", "pluginUsage", "seenNotifications",
			"sonnet1m45MigrationComplete", "opusProMigrationComplete", "changelogLastFetched",
			"apiCreditLinkStatusCache", "orgModelDefaultCache", "passesEligibilityCache", "env",
		},
	}
	for l, keys := range want {
		for _, k := range keys {
			got, ok := layer.ClassifyKey(rule, k)
			if !ok || got != l {
				t.Errorf("key %q = %q (declared %v), want %q", k, got, ok, l)
			}
		}
	}
}

// T077: sync seeds a new profile's .claude.json from ~/.claude.json: owned and
// preference keys are copied, credentials and state keys never are.
func TestClaudeJSONSeededFromHomeFile(t *testing.T) {
	h := testutil.HermeticHome(t)
	testutil.SeedDefaults(t, h, "claude")
	if _, err := os.Stat(filepath.Join(h.Root, ".claude.json")); err != nil {
		t.Fatalf("fixture must place .claude.json at ~/.claude.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.Root, ".claude", ".claude.json")); err == nil {
		t.Fatal("fixture must not place .claude.json inside ~/.claude")
	}
	d := claudeDef(t)
	configDir := filepath.Join(h.DataHome, "aprofile", "profiles", "claude", "work")
	p := &profile.Profile{Harness: "claude", Name: "work", Share: profile.ShareContent}
	if _, err := layer.PrepareConfigDir(d, p, configDir, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(configDir, ".claude.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"mcpServers", "autoUpdates", "showSpinnerTree"} {
		if _, ok := m[k]; !ok {
			t.Errorf("key %q should be seeded from ~/.claude.json", k)
		}
	}
	for _, k := range []string{"oauthAccount", "primaryApiKey", "userID", "projects", "numStartups", "tipsHistory"} {
		if _, ok := m[k]; ok {
			t.Errorf("key %q must never be seeded", k)
		}
	}
	for _, canary := range []string{"fixture-primary-key", "fixture-user-id", "me@example.com"} {
		if strings.Contains(string(data), canary) {
			t.Errorf("seeded file leaks %q", canary)
		}
	}
}

// fakeMixedDef is a data-only harness with one mixed JSON file that declares only
// its owned key, so every other key is undeclared.
func fakeMixedDef(defaultDir string) *harness.Definition {
	return &harness.Definition{
		Name:                  "fake",
		DefaultConfigLocation: defaultDir,
		Classification: []harness.EntryRule{{
			Entry: "app.json",
			Layer: harness.LayerMixed,
			Keys:  map[string]harness.Layer{"mcp": harness.LayerOwned, "session": harness.LayerState},
		}},
	}
}

// T081: undeclared keys are seeded like settings keys, except a key whose name looks
// like a credential: an unknown key is never trusted with a secret (Principle IV).
func TestSeedSkipsUndeclaredCredentialLikeKeys(t *testing.T) {
	h := testutil.HermeticHome(t)
	defaultDir := filepath.Join(h.Root, "default")
	testutil.WriteFile(t, filepath.Join(defaultDir, "app.json"),
		`{"mcp":{},"session":"s","theme":"dark","keybinds":{"a":"b"},"vendorApiKey":"k1","authToken":"t1","clientSecret":"c1","sessionKey":"x1"}`)
	def := fakeMixedDef(defaultDir)
	configDir := filepath.Join(h.DataHome, "cfg")
	if _, err := layer.PrepareConfigDir(def, contentProfile(), configDir, false); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(configDir, "app.json"))
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"mcp", "theme", "keybinds"} {
		if _, ok := m[k]; !ok {
			t.Errorf("key %q should be seeded", k)
		}
	}
	for _, k := range []string{"session", "vendorApiKey", "authToken", "clientSecret", "sessionKey"} {
		if _, ok := m[k]; ok {
			t.Errorf("key %q must not be seeded", k)
		}
	}
}

func TestLooksLikeCredentialKey(t *testing.T) {
	for _, k := range []string{"primaryApiKey", "api_key", "authToken", "oauthAccount", "clientSecret", "password", "sessionKey", "userCredentials", "cookie"} {
		if !layer.LooksLikeCredentialKey(k) {
			t.Errorf("%q should look like a credential", k)
		}
	}
	for _, k := range []string{"theme", "autoUpdates", "keybinds", "verbose", "editorMode", "provider"} {
		if layer.LooksLikeCredentialKey(k) {
			t.Errorf("%q should not look like a credential", k)
		}
	}
}

// T071: drift covers every seeded key (declared settings or shared keys and
// undeclared keys); owned, credentials, state and never-seeded credential-like keys
// are not drift.
func TestMixedSeededDrift(t *testing.T) {
	rule := &harness.EntryRule{
		Entry: "app.json",
		Layer: harness.LayerMixed,
		Keys: map[string]harness.Layer{
			"mcp":     harness.LayerOwned,
			"acct":    harness.LayerCredentials,
			"count":   harness.LayerState,
			"theme":   harness.LayerSettings,
			"oldPref": harness.LayerShared,
		},
	}
	profileFile := []byte(`{"mcp":1,"acct":1,"count":1,"theme":"light","oldPref":1,"extra":1,"vendorToken":1,"same":{"a":1,"b":2},"onlyHere":1}`)
	defaultFile := []byte(`{"mcp":2,"acct":2,"count":2,"theme":"dark","oldPref":2,"extra":2,"vendorToken":2,"same":{"b":2,"a":1},"onlyThere":1}`)
	drift, err := layer.MixedSeededDrift(rule, profileFile, defaultFile, ".json")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"extra", "oldPref", "theme"}; !reflect.DeepEqual(drift, want) {
		t.Errorf("drift = %v, want %v", drift, want)
	}
	if d, _ := layer.MixedSeededDrift(rule, []byte("a=1"), []byte("a=2"), ".toml"); len(d) != 0 {
		t.Errorf("non-JSON files have no drift findings, got %v", d)
	}
}

// T081: the undeclared top-level keys of each mixed file in the default setup, with
// the credential-like ones (never seeded) listed apart.
func TestUndeclaredDefaultKeys(t *testing.T) {
	h := testutil.HermeticHome(t)
	defaultDir := filepath.Join(h.Root, "default")
	def := fakeMixedDef(defaultDir)
	got, err := layer.UndeclaredDefaultKeys(def)
	if err != nil || len(got) != 0 {
		t.Fatalf("no default file: got %v, %v", got, err)
	}
	testutil.WriteFile(t, filepath.Join(defaultDir, "app.json"), `{"mcp":{},"session":"s","theme":"dark","zoom":2,"vendorApiKey":"k"}`)
	got, err = layer.UndeclaredDefaultKeys(def)
	if err != nil {
		t.Fatal(err)
	}
	want := []layer.UndeclaredKeys{{Entry: "app.json", Seeded: []string{"theme", "zoom"}, NotSeeded: []string{"vendorApiKey"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("undeclared = %+v, want %+v", got, want)
	}
	// A default file that is not a JSON object is skipped, not an error.
	testutil.WriteFile(t, filepath.Join(defaultDir, "app.json"), `not json`)
	if got, err := layer.UndeclaredDefaultKeys(def); err != nil || len(got) != 0 {
		t.Errorf("unparsable default file: got %v, %v", got, err)
	}
}

// T077: the fixture's ~/.claude.json declares every key it holds, so doctor has no
// undeclared keys to report for the shipped Claude definition.
func TestClaudeFixtureKeysAllDeclared(t *testing.T) {
	h := testutil.HermeticHome(t)
	testutil.SeedDefaults(t, h, "claude")
	got, err := layer.UndeclaredDefaultKeys(claudeDef(t))
	if err != nil || len(got) != 0 {
		t.Errorf("undeclared = %+v, %v", got, err)
	}
}
