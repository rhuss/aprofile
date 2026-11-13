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

// historyDef builds a minimal user definition with the given classification lines
// (spec 003, contracts/harness-definition.md).
func historyDef(extra, rules string) []byte {
	return []byte("name: hist\nbinary: hist\nwrapper_prefix: hist\nconfig_env:\n  HIST_DIR: \".\"\n" + extra + "classification:\n" + rules)
}

func TestHistoryRuleFieldsValidation(t *testing.T) {
	cases := []struct {
		name    string
		extra   string
		rules   string
		wantErr string // substring of the load error, "" for valid
	}{
		{"history per-file dir", "", "  - { entry: \"projects/\", layer: state, history: true, merge: per-file }\n", ""},
		{"history merge none", "", "  - { entry: \"history.jsonl\", layer: state, history: true, merge: none }\n", ""},
		{"history merge default", "", "  - { entry: \"history.jsonl\", layer: state, history: true }\n", ""},
		{"history on content", "", "  - { entry: \"skills/\", layer: content, history: true }\n", "history"},
		{"history on credentials", "", "  - { entry: \"auth.json\", layer: credentials, history: true }\n", "history"},
		{"history on glob", "", "  - { entry: \"*.jsonl\", layer: state, history: true }\n", "history"},
		{"history on mixed", "", "  - { entry: \"x.json\", layer: mixed, keys: { a: state }, history: true }\n", "history"},
		{"merge without history", "", "  - { entry: \"projects/\", layer: state, merge: per-file }\n", "merge"},
		{"merge unknown value", "", "  - { entry: \"projects/\", layer: state, history: true, merge: lines }\n", "merge"},
		{"per-file on a file", "", "  - { entry: \"history.jsonl\", layer: state, history: true, merge: per-file }\n", "per-file"},
		// FR-003: a history entry is shared as a whole, so it must not hold other
		// classified entries (credentials above all).
		{"history holds credentials", "", "  - { entry: \"agent/auth.json\", layer: credentials }\n  - { entry: \"agent/\", layer: state, history: true, merge: per-file }\n", "contains"},
		{"history holds a glob rule", "", "  - { entry: \"agent/*.json\", layer: owned }\n  - { entry: \"agent/\", layer: state, history: true, merge: per-file }\n", "contains"},
		{"history inside history", "", "  - { entry: \"a/\", layer: state, history: true, merge: per-file }\n  - { entry: \"a/b/\", layer: state, history: true, merge: per-file }\n", "contains"},
		{"history and a second rule", "", "  - { entry: \"plans/\", layer: state, history: true, merge: per-file }\n  - { entry: \"plans/\", layer: credentials }\n", "exactly one rule"},
		{"history next to a prefix sibling", "", "  - { entry: \"agent-x/\", layer: credentials }\n  - { entry: \"agent/\", layer: state, history: true, merge: per-file }\n", ""},
		{"history note accepted", "history_note: \"older versions may not read newer sessions\"\n", "  - { entry: \"projects/\", layer: state, history: true, merge: per-file }\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			set := harness.LoadFromBytes(map[string][]byte{"hist.yaml": historyDef(tc.extra, tc.rules)})
			errs := set.Errors()
			if tc.wantErr == "" {
				if len(errs) != 0 {
					t.Fatalf("unexpected load errors: %v", errs)
				}
				return
			}
			if len(errs) == 0 {
				t.Fatalf("expected a load error containing %q", tc.wantErr)
			}
			if !strings.Contains(errs[0].Error(), tc.wantErr) {
				t.Errorf("load error %q does not mention %q", errs[0].Error(), tc.wantErr)
			}
			if errs[0].Field != "classification" {
				t.Errorf("load error field = %q, want classification", errs[0].Field)
			}
		})
	}
}

func TestHistoryRulesAndSupportsGroups(t *testing.T) {
	rules := "" +
		"  - { entry: \"projects/\", layer: state, history: true, merge: per-file }\n" +
		"  - { entry: \"history.jsonl\", layer: state, history: true }\n" +
		"  - { entry: \"index\", layer: state, database: true, history: true }\n" +
		"  - { entry: \"logs/\", layer: state }\n"
	set := harness.LoadFromBytes(map[string][]byte{"hist.yaml": historyDef("history_note: \"note\"\n", rules)})
	if errs := set.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected load errors: %v", errs)
	}
	d, _ := set.Get("hist")
	if !d.SupportsGroups() {
		t.Fatal("SupportsGroups() = false, want true")
	}
	if d.HistoryNote != "note" {
		t.Errorf("HistoryNote = %q", d.HistoryNote)
	}
	got := map[string]harness.MergeMode{}
	for _, r := range d.HistoryRules() {
		if !r.History {
			t.Errorf("HistoryRules returned non-history rule %q", r.Entry)
		}
		got[r.Entry] = r.EffectiveMerge()
	}
	want := map[string]harness.MergeMode{
		"projects/":     harness.MergePerFile,
		"history.jsonl": harness.MergeNone,
		"index":         harness.MergeNone,
		"index-wal":     harness.MergeNone, // database companions inherit history and merge
		"index-shm":     harness.MergeNone,
		"index-journal": harness.MergeNone,
		"index.lock":    harness.MergeNone,
	}
	if len(got) != len(want) {
		t.Fatalf("HistoryRules = %v, want %v", got, want)
	}
	for e, m := range want {
		if g, ok := got[e]; !ok || g != m {
			t.Errorf("history rule %q = %q (present %v), want %q", e, g, ok, m)
		}
	}

	plain := harness.LoadFromBytes(map[string][]byte{"p.yaml": historyDef("", "  - { entry: \"logs/\", layer: state }\n")})
	pd, _ := plain.Get("hist")
	if pd.SupportsGroups() || len(pd.HistoryRules()) != 0 {
		t.Error("a definition without history rules must not support groups")
	}
}

func TestHistoryNoteSourceRecorded(t *testing.T) {
	set, _ := harness.LoadBuiltins()
	d, _ := set.Get("claude")
	if _, ok := d.Source["history_note"]; !ok {
		t.Error("history_note missing from the field source map (allFieldNames)")
	}
}
