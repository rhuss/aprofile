package launch_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/launch"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/secret"
)

func plan(t *testing.T, p *profile.Profile, d *harness.Definition, doc launch.Doc, rec launch.Record) launch.Result {
	t.Helper()
	res, err := launch.Plan(p, d, doc, rec)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	return res
}

func stepKinds(res launch.Result) map[string]string {
	m := map[string]string{}
	for _, s := range res.Steps {
		m[s.File+"#"+s.Location] = string(s.Kind)
	}
	return m
}

// envOf decodes the env block a plan would write into settings.json.
func writtenJSON(t *testing.T, res launch.Result, file string) map[string]any {
	t.Helper()
	data, ok := res.Writes()[file]
	if !ok {
		t.Fatalf("plan writes no %s; steps %+v", file, res.Steps)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("written %s is not JSON: %v\n%s", file, err, data)
	}
	return m
}

func TestPlanClaudeDesiredEntries(t *testing.T) {
	d := builtin(t, "claude")
	p := claudeProfile(t)
	p.ExtraEnv = append(p.ExtraEnv,
		profile.ExtraEnvVar{Name: "CLAUDE_CODE_USE_VERTEX", Value: "1"},
		profile.ExtraEnvVar{Name: "CLOUD_ML_REGION", Value: "env:CLOUD_ML_REGION"})
	doc := settingsDoc(t, `{"model":"opus","env":{"MLFLOW_TRACKING_URI":"http://ml"},"permissions":{"allow":["x"]}}`)
	res := plan(t, p, d, doc, launch.Record{})
	m := writtenJSON(t, res, "settings.json")
	env := m["env"].(map[string]any)
	want := map[string]string{
		"ANTHROPIC_MODEL":               "gw-model",
		"ANTHROPIC_BASE_URL":            "http://127.0.0.1:1",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL": "gw-haiku",
		"CLAUDE_CODE_USE_VERTEX":        "1",
		"APROFILE_SET_VARS":             "ANTHROPIC_MODEL ANTHROPIC_BASE_URL ANTHROPIC_DEFAULT_HAIKU_MODEL OTEL_TOKEN CLAUDE_CODE_USE_VERTEX CLOUD_ML_REGION",
		"MLFLOW_TRACKING_URI":           "http://ml",
		"ANTHROPIC_API_KEY":             "",
		"ANTHROPIC_AUTH_TOKEN":          "",
		"CLAUDE_CODE_OAUTH_TOKEN":       "",
		"CLAUDE_CODE_USE_BEDROCK":       "",
	}
	for k, v := range want {
		if got, ok := env[k]; !ok || got != v {
			t.Errorf("env.%s = %v (present %v), want %q", k, got, ok, v)
		}
	}
	// A reference-valued extra stays out of the folder; a wrapper-only pass-through of
	// a cleared variable gets no blank entry either.
	for _, k := range []string{"OTEL_TOKEN", "CLOUD_ML_REGION"} {
		if _, ok := env[k]; ok {
			t.Errorf("env.%s must not be placed", k)
		}
	}
	if m["model"] != "opus" || m["permissions"] == nil {
		t.Errorf("user keys changed: %v", m)
	}
	helper, _ := m["apiKeyHelper"].(string)
	if !strings.Contains(helper, "'/keys/work'") {
		t.Errorf("apiKeyHelper = %q", helper)
	}
	ks := stepKinds(res)
	if ks["settings.json#env.ANTHROPIC_MODEL"] != "set" || ks["settings.json#apiKeyHelper"] != "set" {
		t.Errorf("steps = %v", ks)
	}
	if _, ok := ks["settings.json#env.MLFLOW_TRACKING_URI"]; ok {
		t.Error("user entry must not be a step")
	}
	if !res.NewRecord.Has("settings.json", "env.ANTHROPIC_API_KEY") || !res.NewRecord.Has("settings.json", "apiKeyHelper") {
		t.Errorf("record = %+v", res.NewRecord)
	}
	if res.NewRecord.Has("settings.json", "env.MLFLOW_TRACKING_URI") {
		t.Error("user entry recorded")
	}
}

func TestPlanUpToDateIsEmpty(t *testing.T) {
	d := builtin(t, "claude")
	p := claudeProfile(t)
	res := plan(t, p, d, settingsDoc(t, `{}`), launch.Record{})
	data := res.Writes()["settings.json"]
	again := plan(t, p, d, launch.Doc{Files: map[string]launch.FileDoc{"settings.json": launch.ParseFileDoc("settings.json", data)}}, res.NewRecord)
	if len(again.Steps) != 0 || len(again.Writes()) != 0 {
		t.Errorf("up-to-date plan: steps %+v writes %v", again.Steps, again.Writes())
	}
	if !reflect.DeepEqual(again.NewRecord.Entries, res.NewRecord.Entries) {
		t.Error("up-to-date plan changes the record")
	}
}

func TestPlanAbsentSettingsCreated(t *testing.T) {
	res := plan(t, claudeProfile(t), builtin(t, "claude"), launch.Doc{Files: map[string]launch.FileDoc{}}, launch.Record{})
	m := writtenJSON(t, res, "settings.json")
	for k := range m {
		if k != "env" && k != "apiKeyHelper" {
			t.Errorf("created settings.json holds %q", k)
		}
	}
}

func TestPlanNullEnv(t *testing.T) {
	res := plan(t, claudeProfile(t), builtin(t, "claude"), settingsDoc(t, `{"env":null}`), launch.Record{})
	if env, _ := writtenJSON(t, res, "settings.json")["env"].(map[string]any); env["ANTHROPIC_MODEL"] != "gw-model" {
		t.Errorf("null env not treated as empty: %v", env)
	}
}

func TestPlanReplaceUserRestoreRemove(t *testing.T) {
	d := builtin(t, "claude")
	p := claudeProfile(t)
	// First placement over a user entry.
	res := plan(t, p, d, settingsDoc(t, `{"env":{"ANTHROPIC_BASE_URL":"http://user"}}`), launch.Record{})
	if k := stepKinds(res)["settings.json#env.ANTHROPIC_BASE_URL"]; k != "replace-user" {
		t.Errorf("first placement over user entry: %q", k)
	}
	for _, s := range res.Steps {
		if strings.Contains(s.Note, "http://user") {
			t.Error("a step note shows a value")
		}
	}
	placed := res.Writes()["settings.json"]
	rec := res.NewRecord

	// User edits a placed entry: restored.
	var m map[string]any
	_ = json.Unmarshal(placed, &m)
	m["env"].(map[string]any)["ANTHROPIC_MODEL"] = "user-pick"
	delete(m["env"].(map[string]any), "ANTHROPIC_DEFAULT_HAIKU_MODEL")
	edited, _ := json.Marshal(m)
	res = plan(t, p, d, settingsDoc(t, string(edited)), rec)
	ks := stepKinds(res)
	if ks["settings.json#env.ANTHROPIC_MODEL"] != "restore" || ks["settings.json#env.ANTHROPIC_DEFAULT_HAIKU_MODEL"] != "restore" {
		t.Errorf("restore steps = %v", ks)
	}

	// Model removed from the profile: only env.ANTHROPIC_MODEL goes.
	p2 := claudeProfile(t)
	p2.Model = ""
	res = plan(t, p2, d, settingsDoc(t, string(placed)), rec)
	ks = stepKinds(res)
	if ks["settings.json#env.ANTHROPIC_MODEL"] != "remove" || len(res.Steps) != 2 { // model + APROFILE_SET_VARS change
		t.Errorf("remove steps = %v", ks)
	}
	env := writtenJSON(t, res, "settings.json")["env"].(map[string]any)
	if _, ok := env["ANTHROPIC_MODEL"]; ok {
		t.Error("model entry not removed")
	}
	if env["ANTHROPIC_BASE_URL"] != "http://127.0.0.1:1" {
		t.Error("other entries changed")
	}

	// Switching to a login kind removes the key command; the neutralizing entries
	// stay (spec 005 FR-001: every own settings file neutralizes the list).
	p3 := claudeProfile(t)
	p3.AuthKind, p3.AuthRefs = "oauth-login", nil
	res = plan(t, p3, d, settingsDoc(t, string(placed)), rec)
	m3 := writtenJSON(t, res, "settings.json")
	if _, ok := m3["apiKeyHelper"]; ok {
		t.Error("apiKeyHelper not removed")
	}
	if got, ok := m3["env"].(map[string]any)["ANTHROPIC_API_KEY"]; !ok || got != "" {
		t.Errorf("neutralizing entry = %v (present %v), want kept empty", got, ok)
	}
	if k, ok := stepKinds(res)["settings.json#env.ANTHROPIC_API_KEY"]; ok {
		t.Errorf("neutralizing entry got a %s step", k)
	}
}

func TestPlanShareAllLinked(t *testing.T) {
	p := claudeProfile(t)
	p.Share = profile.ShareAll
	fd := launch.ParseFileDoc("settings.json", []byte(`{"env":{"ANTHROPIC_BASE_URL":"http://default"}}`))
	fd.Linked = true
	rec := launch.Record{Entries: []launch.Entry{{File: "settings.json", Location: "env.ANTHROPIC_MODEL"}}}
	res := plan(t, p, builtin(t, "claude"), launch.Doc{Files: map[string]launch.FileDoc{"settings.json": fd}}, rec)
	if len(res.Steps) != 0 || len(res.Writes()) != 0 {
		t.Errorf("linked file: steps %+v writes %v", res.Steps, res.Writes())
	}
	if len(res.NewRecord.Entries) != 0 {
		t.Errorf("record not dropped: %+v", res.NewRecord)
	}
	if len(res.Unplaced) != 1 || res.Unplaced[0].Location != "env.ANTHROPIC_BASE_URL" {
		t.Errorf("unplaced = %+v", res.Unplaced)
	}

	// Codex at all still places its auth block.
	cp := codexProfile(t)
	cp.Share = profile.ShareAll
	cres := plan(t, cp, builtin(t, "codex"), codexDoc(t, codexTOML), launch.Record{})
	if _, ok := cres.Writes()["config.toml"]; !ok {
		t.Error("codex at all should still place its auth block")
	}
}

func TestPlanUnplaced(t *testing.T) {
	p := claudeProfile(t)
	p.AuthKind, p.AuthRefs = "oauth-login", nil
	p.Model, p.Endpoint, p.ExtraEnv = "", "", []profile.ExtraEnvVar{{Name: "MY_EXTRA", Value: "file:/x"}}
	res := plan(t, p, builtin(t, "claude"), settingsDoc(t,
		`{"apiKeyHelper":"cat /k","env":{"ANTHROPIC_DEFAULT_OPUS_MODEL":"x","CLAUDE_CODE_USE_VERTEX":"1","MY_EXTRA":"y","MLFLOW":"z"}}`), launch.Record{})
	var got []string
	for _, e := range res.Unplaced {
		got = append(got, e.Location)
	}
	// The user's CLAUDE_CODE_USE_VERTEX is on the auth-clearing list: the login
	// profile now wants an empty entry there (spec 005 FR-001), so it is replaced,
	// not reported as unplaced.
	want := []string{"apiKeyHelper", "env.ANTHROPIC_DEFAULT_OPUS_MODEL", "env.MY_EXTRA"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unplaced = %v, want %v", got, want)
	}
	if k := stepKinds(res)["settings.json#env.CLAUDE_CODE_USE_VERTEX"]; k != string(launch.StepReplaceUser) {
		t.Errorf("CLAUDE_CODE_USE_VERTEX step = %q, want replace-user", k)
	}
}

func TestPlanErrors(t *testing.T) {
	d := builtin(t, "claude")
	for name, content := range map[string]string{
		"parse":      `{not json`,
		"env shape":  `{"env":["x"]}`,
		"env string": `{"env":"x"}`,
	} {
		_, err := launch.Plan(claudeProfile(t), d, settingsDoc(t, content), launch.Record{})
		if err == nil || !strings.Contains(err.Error(), "settings.json") {
			t.Errorf("%s: want an error naming settings.json, got %v", name, err)
		}
		if name != "parse" && !strings.Contains(err.Error(), "env") {
			t.Errorf("%s: error should name the key env: %v", name, err)
		}
	}
	// A login profile wants the neutralizing entries (spec 005 FR-001), so a parse
	// error is an error for it (spec 005 edge case "settings file does not parse").
	p := claudeProfile(t)
	p.AuthKind, p.AuthRefs, p.Model, p.Endpoint, p.ExtraEnv = "oauth-login", nil, "", "", nil
	if _, err := launch.Plan(p, d, settingsDoc(t, `{not json`), launch.Record{}); err == nil || !strings.Contains(err.Error(), "settings.json") {
		t.Errorf("login profile with a broken file: want an error naming settings.json, got %v", err)
	}
	// A profile that wants nothing in the file (the definition has no folder env
	// location, so no neutralizing entries; a login kind places no key command): a
	// parse error is a note, not an error.
	noEnv := *d
	fl := *d.FolderLaunch
	fl.Env = ""
	noEnv.FolderLaunch = &fl
	res, err := launch.Plan(p, &noEnv, settingsDoc(t, `{not json`), launch.Record{})
	if err != nil || len(res.Notes) == 0 {
		t.Errorf("profile wanting nothing with a broken file: err=%v notes=%v", err, res.Notes)
	}
	// Codex: selected provider not a table.
	_, err = launch.Plan(codexProfile(t), builtin(t, "codex"), codexDoc(t, "model_provider = \"p\"\nmodel_providers = { p = \"x\" }\n"), launch.Record{})
	if err == nil || !strings.Contains(err.Error(), "config.toml") || !strings.Contains(err.Error(), "model_providers.p") {
		t.Errorf("provider not a table: %v", err)
	}
	_, err = launch.Plan(codexProfile(t), builtin(t, "codex"), codexDoc(t, "model_provider = [\n"), launch.Record{})
	if err == nil || !strings.Contains(err.Error(), "config.toml") {
		t.Errorf("toml parse error: %v", err)
	}
}

func decodeTOML(t *testing.T, data []byte) map[string]any {
	t.Helper()
	m := map[string]any{}
	if err := toml.Unmarshal(data, &m); err != nil {
		t.Fatalf("written config.toml invalid: %v\n%s", err, data)
	}
	return m
}

func provider(m map[string]any, name string) map[string]any {
	ps, _ := m["model_providers"].(map[string]any)
	p, _ := ps[name].(map[string]any)
	return p
}

func TestPlanCodexProviderAuth(t *testing.T) {
	d := builtin(t, "codex")
	src := codexTOML + "experimental_bearer_token = \"sk-never-recorded\"\n"
	// experimental_bearer_token belongs to the provider table above (it is the last
	// table opened), the shape a hand-written file can have.
	res := plan(t, codexProfile(t), d, codexDoc(t, src), launch.Record{})
	m := decodeTOML(t, res.Writes()["config.toml"])
	blk := provider(m, "enmaas")
	if _, has := blk["env_key"]; has {
		t.Error("env_key not replaced")
	}
	auth, _ := blk["auth"].(map[string]any)
	if auth["command"] != "sh" {
		t.Errorf("auth = %v", auth)
	}
	if blk["experimental_bearer_token"] != "sk-never-recorded" || blk["base_url"] != "https://gw.example/v1" {
		t.Errorf("other provider keys changed: %v", blk)
	}
	e, ok := res.NewRecord.Get("config.toml", "model_providers.enmaas.auth")
	if !ok || !reflect.DeepEqual(e.Replaced, map[string]string{"env_key": `"OPENAI_API_KEY"`}) {
		t.Errorf("record entry = %+v", e)
	}
	recJSON, _ := json.Marshal(res.NewRecord)
	stepsJSON, _ := json.Marshal(res.Steps)
	if strings.Contains(string(recJSON)+string(stepsJSON), "sk-never-recorded") {
		t.Error("the bearer token leaked into the record or a step")
	}
	if len(res.Steps) != 1 || res.Steps[0].Kind != launch.StepSet || !strings.Contains(res.Steps[0].Note, "env_key") {
		t.Errorf("steps = %+v", res.Steps)
	}
	if len(res.Reencoded) != 1 || res.Reencoded[0] != "config.toml" {
		t.Errorf("reencoded = %v", res.Reencoded)
	}

	placed := res.Writes()["config.toml"]
	rec := res.NewRecord
	// Idempotent.
	again := plan(t, codexProfile(t), d, codexDoc(t, string(placed)), rec)
	if len(again.Steps) != 0 || len(again.Writes()) != 0 {
		t.Errorf("second plan: %+v", again.Steps)
	}

	// Auth removal restores env_key.
	login := codexProfile(t)
	login.AuthKind, login.AuthRefs = "chatgpt-login", nil
	res = plan(t, login, d, codexDoc(t, string(placed)), rec)
	blk = provider(decodeTOML(t, res.Writes()["config.toml"]), "enmaas")
	if _, has := blk["auth"]; has || blk["env_key"] != "OPENAI_API_KEY" {
		t.Errorf("after removal: %v", blk)
	}
	if len(res.Steps) != 1 || res.Steps[0].Kind != launch.StepRemove || !strings.Contains(res.Steps[0].Note, "restored env_key") {
		t.Errorf("removal steps = %+v", res.Steps)
	}
	if len(res.NewRecord.Entries) != 0 {
		t.Error("record not emptied")
	}

	// Selected provider switch moves the block and restores the old provider.
	switched := strings.Replace(string(placed), `model_provider = 'enmaas'`, `model_provider = 'other'`, 1)
	switched = strings.Replace(switched, `model_provider = "enmaas"`, `model_provider = "other"`, 1)
	switched += "\n[model_providers.other]\nname = \"other\"\nenv_key = \"OTHER_KEY\"\n"
	res = plan(t, codexProfile(t), d, codexDoc(t, switched), rec)
	m = decodeTOML(t, res.Writes()["config.toml"])
	if old := provider(m, "enmaas"); old["auth"] != nil || old["env_key"] != "OPENAI_API_KEY" {
		t.Errorf("old provider = %v", old)
	}
	if nw := provider(m, "other"); nw["auth"] == nil || nw["env_key"] != nil {
		t.Errorf("new provider = %v", nw)
	}
	if e, ok := res.NewRecord.Get("config.toml", "model_providers.other.auth"); !ok || e.Replaced["env_key"] != `"OTHER_KEY"` {
		t.Errorf("record after switch = %+v", res.NewRecord)
	}
}

func TestPlanCodexEnvKeyRef(t *testing.T) {
	p := codexProfile(t)
	p.AuthRefs = map[string]secret.Reference{"OPENAI_API_KEY": ref(t, "env:GLM_KEY")}
	res := plan(t, p, builtin(t, "codex"), codexDoc(t, codexTOML), launch.Record{})
	auth := provider(decodeTOML(t, res.Writes()["config.toml"]), "enmaas")["auth"].(map[string]any)
	args, _ := auth["args"].([]any)
	if len(args) != 2 || !strings.Contains(args[1].(string), "${GLM_KEY:-}") {
		t.Errorf("env auth args = %v", args)
	}
}

func TestPlanCodexNoProviderNoWrites(t *testing.T) {
	res := plan(t, codexProfile(t), builtin(t, "codex"), codexDoc(t, "model = \"x\"\n"), launch.Record{})
	if len(res.Steps) != 0 || len(res.Writes()) != 0 {
		t.Errorf("no provider: %+v", res.Steps)
	}
}

// Checkpoint 2 finding 1: an override that removes folder_launch (or moves the env
// location) still removes the entries aprofile placed (FR-005).
func TestPlanRemovesEntriesWhenDefinitionDropsLocations(t *testing.T) {
	d := builtin(t, "claude")
	p := claudeProfile(t)
	res := plan(t, p, d, settingsDoc(t, `{"env":{"MLFLOW":"x"}}`), launch.Record{})
	placed := res.Writes()["settings.json"]

	stripped := *d
	stripped.FolderLaunch = nil
	again := plan(t, p, &stripped, settingsDoc(t, string(placed)), res.NewRecord)
	m := writtenJSON(t, again, "settings.json")
	if _, has := m["apiKeyHelper"]; has {
		t.Error("apiKeyHelper not removed")
	}
	env := m["env"].(map[string]any)
	if len(env) != 1 || env["MLFLOW"] != "x" {
		t.Errorf("env after removal = %v", env)
	}
	if len(again.NewRecord.Entries) != 0 {
		t.Errorf("record not emptied: %+v", again.NewRecord)
	}
}

// A hook variable named as an env: key source makes the key wrapper-only like any
// other own launch variable. (A hook can never set a variable on the auth-clearing
// list: RunEnvHook rejects those names, so FR-006a needs no hook exception.)
func TestDecideKeyFromHookVariable(t *testing.T) {
	p := claudeProfile(t)
	p.AuthRefs = map[string]secret.Reference{"ANTHROPIC_API_KEY": ref(t, "env:HOOK_KEY")}
	ss := launch.DecideWith(p, builtin(t, "claude"), settingsDoc(t, `{}`), []harness.EnvVar{{Name: "HOOK_KEY", Value: "v"}})
	expect(t, ss, launch.KindKey, launch.CarrierWrapper, launch.ReasonClearedSource)
}

// placedSettings returns the settings.json content and record after a first plan.
func placedSettings(t *testing.T, p *profile.Profile, d *harness.Definition, src string) (map[string]any, launch.Record) {
	t.Helper()
	res := plan(t, p, d, settingsDoc(t, src), launch.Record{})
	return writtenJSON(t, res, "settings.json"), res.NewRecord
}

func encodeSettings(t *testing.T, m map[string]any) string {
	t.Helper()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// FR-005 for the key command: a user's own apiKeyHelper is replaced and named
// (never its value); a placed one the user edited or deleted is restored.
func TestPlanKeyCommandReplaceUserAndRestore(t *testing.T) {
	d := builtin(t, "claude")
	p := claudeProfile(t)
	res := plan(t, p, d, settingsDoc(t, `{"apiKeyHelper":"cat /mine/secret-path"}`), launch.Record{})
	if k := stepKinds(res)["settings.json#apiKeyHelper"]; k != string(launch.StepReplaceUser) {
		t.Errorf("user apiKeyHelper step = %q, want replace-user", k)
	}
	steps, _ := json.Marshal(res.Steps)
	if strings.Contains(string(steps), "/mine/secret-path") {
		t.Error("a step names the user's value")
	}
	m, rec := placedSettings(t, p, d, `{}`)
	want := m["apiKeyHelper"]
	for name, edit := range map[string]func(map[string]any){
		"edited":  func(m map[string]any) { m["apiKeyHelper"] = "echo other" },
		"deleted": func(m map[string]any) { delete(m, "apiKeyHelper") },
	} {
		cp := map[string]any{}
		for k, v := range m {
			cp[k] = v
		}
		edit(cp)
		res := plan(t, p, d, settingsDoc(t, encodeSettings(t, cp)), rec)
		if k := stepKinds(res)["settings.json#apiKeyHelper"]; k != string(launch.StepRestore) {
			t.Errorf("%s: step = %q, want restore", name, k)
		}
		if got := writtenJSON(t, res, "settings.json")["apiKeyHelper"]; got != want {
			t.Errorf("%s: apiKeyHelper = %v", name, got)
		}
	}
}

// FR-005: an extra variable deleted from the profile removes exactly its entry and
// APROFILE_SET_VARS no longer lists it.
func TestPlanExtraVariableDeleted(t *testing.T) {
	d := builtin(t, "claude")
	p := claudeProfile(t)
	m, rec := placedSettings(t, p, d, `{"env":{"MY_TRACE":"1"}}`)
	p.ExtraEnv = p.ExtraEnv[1:] // drop ANTHROPIC_DEFAULT_HAIKU_MODEL
	res := plan(t, p, d, settingsDoc(t, encodeSettings(t, m)), rec)
	kinds := stepKinds(res)
	if kinds["settings.json#env.ANTHROPIC_DEFAULT_HAIKU_MODEL"] != string(launch.StepRemove) {
		t.Errorf("steps = %v", kinds)
	}
	env := writtenJSON(t, res, "settings.json")["env"].(map[string]any)
	if _, has := env["ANTHROPIC_DEFAULT_HAIKU_MODEL"]; has {
		t.Error("entry not removed")
	}
	if env["MY_TRACE"] != "1" || env["ANTHROPIC_MODEL"] != "gw-model" {
		t.Errorf("other entries changed: %v", env)
	}
	if strings.Contains(env["APROFILE_SET_VARS"].(string), "ANTHROPIC_DEFAULT_HAIKU_MODEL") {
		t.Errorf("APROFILE_SET_VARS = %v", env["APROFILE_SET_VARS"])
	}
	if res.NewRecord.Has("settings.json", "env.ANTHROPIC_DEFAULT_HAIKU_MODEL") {
		t.Error("record still lists the removed entry")
	}
}

// FR-005 for the Codex auth block: a user's own auth block in the selected
// provider is replaced; a placed one the user changed or deleted is restored.
// FR-020b: an auth block a login profile did not place is reported as unplaced.
func TestPlanCodexAuthReplaceUserRestoreUnplaced(t *testing.T) {
	d := builtin(t, "codex")
	p := codexProfile(t)
	user := codexTOML + "\n[model_providers.enmaas.auth]\ncommand = \"my-token-cmd\"\n"
	res := plan(t, p, d, codexDoc(t, user), launch.Record{})
	if k := stepKinds(res)["config.toml#model_providers.enmaas.auth"]; k != string(launch.StepReplaceUser) {
		t.Errorf("user auth step = %q, want replace-user", k)
	}

	first := plan(t, p, d, codexDoc(t, codexTOML), launch.Record{})
	placed := decodeTOML(t, first.Writes()["config.toml"])
	want := provider(placed, "enmaas")["auth"]
	for name, edit := range map[string]func(map[string]any){
		"changed": func(b map[string]any) { b["auth"] = map[string]any{"command": "other"} },
		"deleted": func(b map[string]any) { delete(b, "auth") },
	} {
		doc := decodeTOML(t, first.Writes()["config.toml"])
		edit(provider(doc, "enmaas"))
		data, err := toml.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		res := plan(t, p, d, codexDoc(t, string(data)), first.NewRecord)
		if k := stepKinds(res)["config.toml#model_providers.enmaas.auth"]; k != string(launch.StepRestore) {
			t.Errorf("%s: step = %q, want restore", name, k)
		}
		if got := provider(decodeTOML(t, res.Writes()["config.toml"]), "enmaas")["auth"]; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: auth = %v, want %v", name, got, want)
		}
		if e, _ := res.NewRecord.Get("config.toml", "model_providers.enmaas.auth"); e.Replaced["env_key"] == "" {
			t.Errorf("%s: replaced env_key lost: %+v", name, e)
		}
	}

	login := codexProfile(t)
	login.AuthKind, login.AuthRefs = "chatgpt-login", nil
	res = plan(t, login, d, codexDoc(t, user), launch.Record{})
	found := false
	for _, u := range res.Unplaced {
		found = found || (u.File == "config.toml" && u.Location == "model_providers.enmaas.auth")
	}
	if !found {
		t.Errorf("unplaced = %+v", res.Unplaced)
	}
}

// loginProfile is an interactive-login Claude profile with no launch variables.
func loginProfile(share profile.Share) *profile.Profile {
	return &profile.Profile{Harness: "claude", Name: "login", AuthKind: "oauth-login", Share: share}
}

// envOf returns the env object of a written settings.json.
func envOf(t *testing.T, res launch.Result) map[string]any {
	t.Helper()
	env, _ := writtenJSON(t, res, "settings.json")["env"].(map[string]any)
	return env
}

// Spec 005 FR-001, FR-003: a login profile with its own settings file neutralizes
// the whole auth-clearing list, recorded as placed entries; a second plan is empty.
func TestPlanLoginNeutralizesAuthClearing(t *testing.T) {
	d := builtin(t, "claude")
	for _, share := range []profile.Share{profile.ShareContent, profile.ShareNone} {
		for name, doc := range map[string]launch.Doc{
			"absent": {Files: map[string]launch.FileDoc{}},
			"own":    settingsDoc(t, `{"permissions":{"allow":["x"]}}`),
		} {
			p := loginProfile(share)
			res := plan(t, p, d, doc, launch.Record{})
			env := envOf(t, res)
			names := d.UnsetAuthVars()
			if len(names) == 0 {
				t.Fatal("claude has no auth-clearing list")
			}
			if len(env) != len(names) {
				t.Errorf("%s/%s: env holds %d entries, want %d: %v", share, name, len(env), len(names), env)
			}
			for _, v := range names {
				if got, ok := env[v]; !ok || got != "" {
					t.Errorf("%s/%s: env.%s = %v (present %v), want empty", share, name, v, got, ok)
				}
				if !res.NewRecord.Has("settings.json", "env."+v) {
					t.Errorf("%s/%s: env.%s not recorded", share, name, v)
				}
			}
			if _, ok := env["APROFILE_SET_VARS"]; ok {
				t.Errorf("%s/%s: APROFILE_SET_VARS placed for a profile without variables", share, name)
			}
			again := plan(t, p, d, launch.Doc{Files: map[string]launch.FileDoc{"settings.json": launch.ParseFileDoc("settings.json", res.Writes()["settings.json"])}}, res.NewRecord)
			if len(again.Steps) != 0 || len(again.Writes()) != 0 {
				t.Errorf("%s/%s: second plan: steps %+v", share, name, again.Steps)
			}
		}
	}
}

// Spec 005 US1 scenario 4: a hand-deleted neutralizing entry is restored.
func TestPlanLoginRestoresDeletedEntry(t *testing.T) {
	d := builtin(t, "claude")
	p := loginProfile(profile.ShareContent)
	m, rec := placedSettings(t, p, d, `{}`)
	delete(m["env"].(map[string]any), "CLAUDE_CODE_USE_VERTEX")
	res := plan(t, p, d, settingsDoc(t, encodeSettings(t, m)), rec)
	if k := stepKinds(res)["settings.json#env.CLAUDE_CODE_USE_VERTEX"]; k != string(launch.StepRestore) || len(res.Steps) != 1 {
		t.Errorf("steps = %v, want one restore of env.CLAUDE_CODE_USE_VERTEX", stepKinds(res))
	}
	if got, ok := envOf(t, res)["CLAUDE_CODE_USE_VERTEX"]; !ok || got != "" {
		t.Errorf("restored entry = %v (present %v)", got, ok)
	}
	// A deleted entry dropped no value: no replaced-value note.
	if strings.Contains(strings.Join(res.Notes, "\n"), "is replaced by an empty entry") {
		t.Errorf("unexpected note for a deleted entry: %q", res.Notes)
	}

	// A hand-set value in a placed entry is restored to empty, with the note.
	m["env"].(map[string]any)["CLAUDE_CODE_USE_VERTEX"] = "1"
	res = plan(t, p, d, settingsDoc(t, encodeSettings(t, m)), rec)
	if k := stepKinds(res)["settings.json#env.CLAUDE_CODE_USE_VERTEX"]; k != string(launch.StepRestore) {
		t.Errorf("step = %q, want restore", k)
	}
	if !strings.Contains(strings.Join(res.Notes, "\n"), "own env CLAUDE_CODE_USE_VERTEX is replaced by an empty entry") {
		t.Errorf("notes = %q, want the replaced-value note", res.Notes)
	}
}

// Spec 005 edge case (spec 004 FR-004): a user's own provider switch in the
// profile's settings file is replaced by the empty entry and named, never its value.
func TestPlanLoginReplacesUserSwitch(t *testing.T) {
	d := builtin(t, "claude")
	res := plan(t, loginProfile(profile.ShareContent), d, settingsDoc(t, `{"env":{"CLAUDE_CODE_USE_VERTEX":"vertex-secret-1"}}`), launch.Record{})
	if k := stepKinds(res)["settings.json#env.CLAUDE_CODE_USE_VERTEX"]; k != string(launch.StepReplaceUser) {
		t.Errorf("step = %q, want replace-user", k)
	}
	if got := envOf(t, res)["CLAUDE_CODE_USE_VERTEX"]; got != "" {
		t.Errorf("env.CLAUDE_CODE_USE_VERTEX = %v", got)
	}
	steps, _ := json.Marshal(res.Steps)
	rec, _ := json.Marshal(res.NewRecord)
	if strings.Contains(string(steps)+string(rec), "vertex-secret-1") {
		t.Error("the user's value leaked into a step or the record")
	}
	// The dropped value gets its own note with the way to keep it, never the value.
	notes := strings.Join(res.Notes, "\n")
	if !strings.Contains(notes, "own env CLAUDE_CODE_USE_VERTEX is replaced") ||
		!strings.Contains(notes, "aprofile edit claude/login --env CLAUDE_CODE_USE_VERTEX=<value>") {
		t.Errorf("notes = %q, want the replaced-value note with the edit hint", res.Notes)
	}
	if strings.Contains(notes, "vertex-secret-1") {
		t.Error("the user's value leaked into a note")
	}

	// A user entry that is already empty (or null) loses nothing: no note.
	for _, v := range []string{`""`, `null`} {
		res := plan(t, loginProfile(profile.ShareContent), d, settingsDoc(t, `{"env":{"CLAUDE_CODE_USE_VERTEX":`+v+`}}`), launch.Record{})
		if strings.Contains(strings.Join(res.Notes, "\n"), "is replaced by an empty entry") {
			t.Errorf("user value %s: unexpected note %q", v, res.Notes)
		}
	}
}

// Spec 005 FR-004: at share level all the linked file gets nothing, the record is
// dropped and a hand-added auth-clearing entry is still reported as unplaced.
func TestPlanLoginShareAllPlacesNothing(t *testing.T) {
	d := builtin(t, "claude")
	p := loginProfile(profile.ShareAll)
	fd := launch.ParseFileDoc("settings.json", []byte(`{"env":{"CLAUDE_CODE_USE_VERTEX":"1"}}`))
	fd.Linked = true
	rec := launch.Record{Entries: []launch.Entry{{File: "settings.json", Location: "env.ANTHROPIC_API_KEY"}}}
	res := plan(t, p, d, launch.Doc{Files: map[string]launch.FileDoc{"settings.json": fd}}, rec)
	if len(res.Steps) != 0 || len(res.Writes()) != 0 || len(res.NewRecord.Entries) != 0 {
		t.Errorf("linked file: steps %+v writes %v record %+v", res.Steps, res.Writes(), res.NewRecord)
	}
	if len(res.Unplaced) != 1 || res.Unplaced[0].Location != "env.CLAUDE_CODE_USE_VERTEX" {
		t.Errorf("unplaced = %+v", res.Unplaced)
	}
	// Absent file at all: the classification links it, so nothing is desired.
	res = plan(t, p, d, launch.Doc{Files: map[string]launch.FileDoc{}}, launch.Record{})
	if len(res.Steps) != 0 || len(res.Writes()) != 0 {
		t.Errorf("absent file at all: steps %+v", res.Steps)
	}
}

// Spec 005 edge case: a profile changed from all to content gets the entries on
// the next plan (its settings file is now its own copy).
func TestPlanLoginShareAllToContent(t *testing.T) {
	d := builtin(t, "claude")
	p := loginProfile(profile.ShareContent)
	res := plan(t, p, d, settingsDoc(t, `{"theme":"dark"}`), launch.Record{})
	env := envOf(t, res)
	if got, ok := env["CLAUDE_CODE_USE_VERTEX"]; !ok || got != "" {
		t.Errorf("env.CLAUDE_CODE_USE_VERTEX = %v (present %v)", got, ok)
	}
	if k := stepKinds(res)["settings.json#env.CLAUDE_CODE_USE_VERTEX"]; k != string(launch.StepSet) {
		t.Errorf("step = %q, want set", k)
	}
}

// Spec 005 edge case: a definition override that drops a name from the
// auth-clearing list removes the placed entry on the next plan.
func TestPlanLoginOverrideDropsClearedName(t *testing.T) {
	d := builtin(t, "claude")
	p := loginProfile(profile.ShareContent)
	m, rec := placedSettings(t, p, d, `{}`)
	over := *d
	over.AuthClears = nil
	for _, v := range d.AuthClears {
		if v != "CLAUDE_CODE_USE_MANTLE" {
			over.AuthClears = append(over.AuthClears, v)
		}
	}
	res := plan(t, p, &over, settingsDoc(t, encodeSettings(t, m)), rec)
	if k := stepKinds(res)["settings.json#env.CLAUDE_CODE_USE_MANTLE"]; k != string(launch.StepRemove) || len(res.Steps) != 1 {
		t.Errorf("steps = %v, want one remove", stepKinds(res))
	}
	if _, ok := envOf(t, res)["CLAUDE_CODE_USE_MANTLE"]; ok {
		t.Error("entry not removed")
	}
	if res.NewRecord.Has("settings.json", "env.CLAUDE_CODE_USE_MANTLE") {
		t.Error("record still lists the dropped name")
	}
}

// Spec 005 T005: a definition without folder_launch (an override removed it) and a
// record of neutralizing entries removes them without dereferencing the nil
// folder_launch.
func TestPlanLoginDefinitionWithoutFolderLaunch(t *testing.T) {
	d := builtin(t, "claude")
	p := loginProfile(profile.ShareContent)
	m, rec := placedSettings(t, p, d, `{"env":{"MY_TRACE":"1"}}`)
	stripped := *d
	stripped.FolderLaunch = nil
	res := plan(t, p, &stripped, settingsDoc(t, encodeSettings(t, m)), rec)
	env := envOf(t, res)
	if len(env) != 1 || env["MY_TRACE"] != "1" {
		t.Errorf("env after removal = %v", env)
	}
	if len(res.NewRecord.Entries) != 0 {
		t.Errorf("record not emptied: %+v", res.NewRecord)
	}
}

// Spec 005 FR-002: names the profile supplies through the wrapper are never blanked.
func TestPlanWrapperSuppliedNamesNotBlanked(t *testing.T) {
	d := builtin(t, "claude")
	all := d.UnsetAuthVars()
	check := func(name string, p *profile.Profile, skip map[string]string) {
		t.Helper()
		res := plan(t, p, d, settingsDoc(t, `{}`), launch.Record{})
		env := envOf(t, res)
		for _, v := range all {
			got, ok := env[v]
			if want, skipped := skip[v]; skipped {
				if want == "" && ok {
					t.Errorf("%s: env.%s = %v must not be placed", name, v, got)
				}
				if want != "" && got != want {
					t.Errorf("%s: env.%s = %v, want %q", name, v, got, want)
				}
				continue
			}
			if !ok || got != "" {
				t.Errorf("%s: env.%s = %v (present %v), want empty", name, v, got, ok)
			}
		}
	}

	tok := &profile.Profile{Harness: "claude", Name: "tok", AuthKind: "oauth-token", Share: profile.ShareContent,
		AuthRefs: map[string]secret.Reference{"CLAUDE_CODE_OAUTH_TOKEN": ref(t, "env:TOK")}}
	check("oauth-token", tok, map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": ""})

	cleared := &profile.Profile{Harness: "claude", Name: "key", AuthKind: "api-key", Share: profile.ShareContent,
		AuthRefs: map[string]secret.Reference{"ANTHROPIC_API_KEY": ref(t, "env:ANTHROPIC_API_KEY")}}
	expect(t, launch.Decide(cleared, d, settingsDoc(t, `{}`)), launch.KindKey, launch.CarrierWrapper, launch.ReasonClearedSource)
	check("api-key cleared-source", cleared, map[string]string{"ANTHROPIC_API_KEY": ""})

	vertex := loginProfile(profile.ShareContent)
	vertex.ExtraEnv = []profile.ExtraEnvVar{{Name: "CLAUDE_CODE_USE_VERTEX", Value: "1"}}
	check("login with extra switch", vertex, map[string]string{"CLAUDE_CODE_USE_VERTEX": "1"})

	// A folder-carried key: its own variable is blanked, the key command serves it
	// (spec 004 FR-006a unchanged).
	folderKey := claudeProfile(t)
	folderKey.Model, folderKey.Endpoint, folderKey.ExtraEnv = "", "", nil
	check("folder-carried api-key", folderKey, nil)
}

// Spec 005 edge case: an auth kind change from a folder-carried api-key to an
// oauth-token removes the empty entry for the new kind's own variable.
func TestPlanAuthKindChangeToOAuthToken(t *testing.T) {
	d := builtin(t, "claude")
	p := claudeProfile(t)
	p.Model, p.Endpoint, p.ExtraEnv = "", "", nil
	m, rec := placedSettings(t, p, d, `{}`)
	if got, ok := m["env"].(map[string]any)["CLAUDE_CODE_OAUTH_TOKEN"]; !ok || got != "" {
		t.Fatalf("api-key profile: env.CLAUDE_CODE_OAUTH_TOKEN = %v (present %v)", got, ok)
	}
	p.AuthKind = "oauth-token"
	p.AuthRefs = map[string]secret.Reference{"CLAUDE_CODE_OAUTH_TOKEN": ref(t, "env:TOK")}
	res := plan(t, p, d, settingsDoc(t, encodeSettings(t, m)), rec)
	ks := stepKinds(res)
	if ks["settings.json#env.CLAUDE_CODE_OAUTH_TOKEN"] != string(launch.StepRemove) || ks["settings.json#apiKeyHelper"] != string(launch.StepRemove) {
		t.Errorf("steps = %v", ks)
	}
	env := envOf(t, res)
	if _, ok := env["CLAUDE_CODE_OAUTH_TOKEN"]; ok {
		t.Error("the token variable is still blanked")
	}
	if got, ok := env["ANTHROPIC_API_KEY"]; !ok || got != "" {
		t.Errorf("env.ANTHROPIC_API_KEY = %v (present %v), want empty", got, ok)
	}
}

// --- Spec 006 (T008): feature-state-to-desired-entries derivation ---

// featureProfile is a claude profile with telemetry enabled.
func featureProfile(t *testing.T, params map[string]string) *profile.Profile {
	t.Helper()
	p := claudeProfile(t)
	p.Model, p.Endpoint, p.ExtraEnv = "", "", nil
	p.Features = []profile.FeatureState{{Feature: "telemetry", Params: params}}
	return p
}

func TestPlanFeatureTelemetryBothSignalsOn(t *testing.T) {
	d := builtin(t, "claude")
	p := featureProfile(t, map[string]string{
		"traces": "on", "logs": "on", "endpoint": "https://otlp.example.com",
	})
	res := plan(t, p, d, settingsDoc(t, `{}`), launch.Record{})
	env := envOf(t, res)
	want := map[string]string{
		"CLAUDE_CODE_ENABLE_TELEMETRY":        "1",
		"OTEL_TRACES_EXPORTER":                "otlp",
		"OTEL_LOGS_EXPORTER":                  "otlp",
		"OTEL_EXPORTER_OTLP_ENDPOINT":         "https://otlp.example.com",
		"OTEL_EXPORTER_OTLP_PROTOCOL":         "http/protobuf", // per-harness default (FR-004)
		"CLAUDE_CODE_ENHANCED_TELEMETRY_BETA": "1",             // when: traces
	}
	for k, v := range want {
		if got, ok := env[k]; !ok || got != v {
			t.Errorf("env.%s = %v (present %v), want %q", k, got, ok, v)
		}
		if !res.NewRecord.Has("settings.json", "env."+k) {
			t.Errorf("env.%s not recorded as placed", k)
		}
	}
	ks := stepKinds(res)
	if ks["settings.json#env.OTEL_TRACES_EXPORTER"] != "set" {
		t.Errorf("steps = %v", ks)
	}
}

func TestPlanFeatureOffSignalPlacesNone(t *testing.T) {
	// FR-011: a signal that is off places its exporter entry as none, so an
	// inherited OTEL_* value cannot turn it back on; the when-gated beta flag is
	// skipped entirely.
	d := builtin(t, "claude")
	p := featureProfile(t, map[string]string{
		"traces": "off", "logs": "on", "endpoint": "https://otlp.example.com",
	})
	res := plan(t, p, d, settingsDoc(t, `{}`), launch.Record{})
	env := envOf(t, res)
	if got := env["OTEL_TRACES_EXPORTER"]; got != "none" {
		t.Errorf("env.OTEL_TRACES_EXPORTER = %v, want none", got)
	}
	if got := env["OTEL_LOGS_EXPORTER"]; got != "otlp" {
		t.Errorf("env.OTEL_LOGS_EXPORTER = %v, want otlp", got)
	}
	if _, ok := env["CLAUDE_CODE_ENHANCED_TELEMETRY_BETA"]; ok {
		t.Error("the traces beta flag must not be placed with traces off")
	}
}

func TestPlanFeatureParamReferenceAndProtocol(t *testing.T) {
	d := builtin(t, "claude")
	p := featureProfile(t, map[string]string{
		"traces": "on", "logs": "off", "endpoint": "http://collector:4317", "protocol": "grpc",
	})
	res := plan(t, p, d, settingsDoc(t, `{}`), launch.Record{})
	env := envOf(t, res)
	if got := env["OTEL_EXPORTER_OTLP_ENDPOINT"]; got != "http://collector:4317" {
		t.Errorf("endpoint param reference not resolved: %v", got)
	}
	if got := env["OTEL_EXPORTER_OTLP_PROTOCOL"]; got != "grpc" {
		t.Errorf("protocol override not applied: %v", got)
	}
}

func TestPlanFeatureAutomodeLogLiteral(t *testing.T) {
	d := builtin(t, "claude")
	p := claudeProfile(t)
	p.Model, p.Endpoint, p.ExtraEnv = "", "", nil
	p.Features = []profile.FeatureState{{Feature: "automode-log"}}
	res := plan(t, p, d, settingsDoc(t, `{}`), launch.Record{})
	env := envOf(t, res)
	if got := env["AUTOMODE_DECISION_LOG"]; got != "1" {
		t.Errorf("env.AUTOMODE_DECISION_LOG = %v, want 1", got)
	}
	if !res.NewRecord.Has("settings.json", "env.AUTOMODE_DECISION_LOG") {
		t.Error("automode-log entry not recorded")
	}
}

// Spec 006 edge case: a share level change by other means leaves the feature
// entries unplaceable; the plan reports a problem naming the file and the way
// out instead of writing into the linked default setup.
func TestPlanFeatureLinkedFileIsAProblem(t *testing.T) {
	d := builtin(t, "claude")
	p := featureProfile(t, map[string]string{
		"traces": "on", "logs": "on", "endpoint": "https://otlp.example.com",
	})
	fd := launch.ParseFileDoc("settings.json", []byte(`{"env":{}}`))
	fd.Linked = true
	res, err := launch.Plan(p, d, launch.Doc{Files: map[string]launch.FileDoc{"settings.json": fd}}, launch.Record{})
	if err == nil {
		t.Fatalf("feature entries into a linked file should be a problem: %+v", res.Steps)
	}
	if !strings.Contains(err.Error(), "settings.json") || !strings.Contains(err.Error(), "--share content") {
		t.Errorf("error should name the file and the fix: %v", err)
	}
	if len(res.Writes()) != 0 {
		t.Error("no file may be written")
	}
}

func TestPlanFeatureDisableRemovesExactly(t *testing.T) {
	// SC-002 / FR-010: disabling removes exactly the placed entries, restoring a
	// hand-written value the feature replaced.
	d := builtin(t, "claude")
	p := featureProfile(t, map[string]string{
		"traces": "on", "logs": "on", "endpoint": "https://otlp.example.com",
	})
	// A hand-written exporter the feature replaces.
	res := plan(t, p, d, settingsDoc(t, `{"env":{"OTEL_LOGS_EXPORTER":"console","USER_KEY":"user"}}`), launch.Record{})
	rec := res.NewRecord
	if k := stepKinds(res)["settings.json#env.OTEL_LOGS_EXPORTER"]; k != "replace-user" {
		t.Errorf("replacing a hand-written exporter: %q", k)
	}
	if e, ok := rec.Get("settings.json", "env.OTEL_LOGS_EXPORTER"); !ok || e.Replaced[""] != `"console"` {
		t.Errorf("replaced hand-written value not recorded: %+v", e)
	}

	p.Features = nil
	res = plan(t, p, d, settingsDoc(t, encodeSettings(t, writtenJSON(t, res, "settings.json"))), rec)
	env := envOf(t, res)
	for _, k := range []string{"CLAUDE_CODE_ENABLE_TELEMETRY", "OTEL_TRACES_EXPORTER",
		"OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_PROTOCOL", "CLAUDE_CODE_ENHANCED_TELEMETRY_BETA"} {
		if _, ok := env[k]; ok {
			t.Errorf("env.%s still placed after disable", k)
		}
	}
	if got, ok := env["OTEL_LOGS_EXPORTER"]; !ok || got != "console" {
		t.Errorf("hand-written value not restored: %v (present %v)", got, ok)
	}
	if got, ok := env["USER_KEY"]; !ok || got != "user" {
		t.Errorf("unrelated user entry changed: %v", env)
	}
	// The feature's locations are gone from the record; the profile's own launch
	// entries (the neutralizing blanks) legitimately remain.
	for _, k := range []string{"CLAUDE_CODE_ENABLE_TELEMETRY", "OTEL_TRACES_EXPORTER", "OTEL_LOGS_EXPORTER",
		"OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_PROTOCOL", "CLAUDE_CODE_ENHANCED_TELEMETRY_BETA", "AUTOMODE_DECISION_LOG"} {
		if res.NewRecord.Has("settings.json", "env."+k) {
			t.Errorf("record still lists env.%s after disable", k)
		}
	}
}

func TestPlanFeatureIdempotent(t *testing.T) {
	d := builtin(t, "claude")
	p := featureProfile(t, map[string]string{
		"traces": "on", "logs": "on", "endpoint": "https://otlp.example.com",
	})
	res := plan(t, p, d, settingsDoc(t, `{"env":{"USER_KEY":"user"}}`), launch.Record{})
	data := res.Writes()["settings.json"]
	again := plan(t, p, d, launch.Doc{Files: map[string]launch.FileDoc{"settings.json": launch.ParseFileDoc("settings.json", data)}}, res.NewRecord)
	if len(again.Steps) != 0 || len(again.Writes()) != 0 {
		t.Errorf("second sync is not a no-op: steps %+v writes %v", again.Steps, again.Writes())
	}
}

func TestPlanFeatureEnvCollisionIsError(t *testing.T) {
	// FR-015 at plan time: a feature env name colliding with a hand-set extra env
	// var is a reported problem, never silently resolved.
	d := builtin(t, "claude")
	p := featureProfile(t, map[string]string{
		"traces": "on", "logs": "on", "endpoint": "https://otlp.example.com",
	})
	p.ExtraEnv = []profile.ExtraEnvVar{{Name: "OTEL_TRACES_EXPORTER", Value: "console", Literal: true}}
	_, err := launch.Plan(p, d, settingsDoc(t, `{}`), launch.Record{})
	if err == nil || !strings.Contains(err.Error(), "OTEL_TRACES_EXPORTER") {
		t.Fatalf("collision should error naming the variable: %v", err)
	}
}

// --- Spec 006 (T025): Codex telemetry placement in config.toml ---

// codexFeatureProfile is a codex profile with telemetry enabled.
func codexFeatureProfile(t *testing.T, params map[string]string) *profile.Profile {
	t.Helper()
	p := codexProfile(t)
	p.Features = []profile.FeatureState{{Feature: "telemetry", Params: params}}
	return p
}

func tomlGet(t *testing.T, data []byte, path ...string) any {
	t.Helper()
	m := decodeTOML(t, data)
	cur := any(m)
	for _, seg := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("%v is not a table at %s", cur, seg)
		}
		cur = mm[seg]
	}
	return cur
}

func TestPlanCodexTelemetryPlacement(t *testing.T) {
	d := builtin(t, "codex")
	p := codexFeatureProfile(t, map[string]string{
		"traces": "on", "logs": "on", "endpoint": "https://otlp.example.com",
	})
	res := plan(t, p, d, codexDoc(t, "model = \"glm-5\"\n"), launch.Record{})
	data, ok := res.Writes()["config.toml"]
	if !ok {
		t.Fatalf("plan writes no config.toml; steps %+v", res.Steps)
	}
	// Both exporters in the externally-tagged shape (research R1), protocol
	// default otlp-http json (FR-004).
	exp := tomlGet(t, data, "otel", "exporter")
	if got := fmt.Sprint(tomlGet(t, data, "otel", "exporter", "otlp-http", "endpoint")); got != "https://otlp.example.com" {
		t.Errorf("exporter endpoint = %v (%T)", exp, exp)
	}
	if got := tomlGet(t, data, "otel", "exporter", "otlp-http", "protocol"); got != "json" {
		t.Errorf("exporter protocol = %v, want json", got)
	}
	if got := tomlGet(t, data, "otel", "trace_exporter", "otlp-http", "protocol"); got != "json" {
		t.Errorf("trace_exporter protocol = %v, want json", got)
	}
	// The user's model key survives.
	if got := tomlGet(t, data, "model"); got != "glm-5" {
		t.Errorf("model key changed: %v", got)
	}
	if !res.NewRecord.Has("config.toml", "otel.exporter") || !res.NewRecord.Has("config.toml", "otel.trace_exporter") {
		t.Errorf("nested TOML locations not recorded: %+v", res.NewRecord)
	}

	// otlp-grpc switch case.
	p2 := codexFeatureProfile(t, map[string]string{
		"traces": "on", "logs": "on", "endpoint": "https://e", "protocol": "otlp-grpc",
	})
	res2 := plan(t, p2, d, codexDoc(t, ""), launch.Record{})
	if _, ok := tomlGet(t, res2.Writes()["config.toml"], "otel", "exporter").(map[string]any); !ok {
		t.Fatalf("exporter not a table: %v", tomlGet(t, res2.Writes()["config.toml"], "otel", "exporter"))
	}
	if _, has := tomlGet(t, res2.Writes()["config.toml"], "otel", "exporter").(map[string]any)["otlp-grpc"]; !has {
		t.Errorf("otlp-grpc variant not selected: %v", tomlGet(t, res2.Writes()["config.toml"], "otel", "exporter"))
	}

	// An off signal places its exporter key as none (FR-011, R5).
	p3 := codexFeatureProfile(t, map[string]string{
		"traces": "off", "logs": "on", "endpoint": "https://e",
	})
	res3 := plan(t, p3, d, codexDoc(t, ""), launch.Record{})
	if got := tomlGet(t, res3.Writes()["config.toml"], "otel", "trace_exporter"); got != "none" {
		t.Errorf("trace_exporter = %v, want none", got)
	}
}

func TestPlanCodexTelemetryReplaceAndRestore(t *testing.T) {
	d := builtin(t, "codex")
	p := codexFeatureProfile(t, map[string]string{
		"traces": "on", "logs": "on", "endpoint": "https://otlp.example.com",
	})
	// A hand-written [otel] block: exporter replaced, sibling key preserved.
	src := `
[otel]
environment = "prod"
exporter = { statsig = {} }
`
	res := plan(t, p, d, codexDoc(t, src), launch.Record{})
	rec := res.NewRecord
	e, _ := rec.Get("config.toml", "otel.exporter")
	if e.Replaced[""] == "" {
		t.Fatalf("replaced hand-written exporter not recorded: %+v", e)
	}
	data := res.Writes()["config.toml"]
	if got := tomlGet(t, data, "otel", "environment"); got != "prod" {
		t.Errorf("hand-written sibling key lost: %v", got)
	}

	// Disabling removes only aprofile's keys and restores the replaced value.
	p.Features = nil
	res2 := plan(t, p, d, codexDoc(t, string(data)), rec)
	data2 := res2.Writes()["config.toml"]
	if got := tomlGet(t, data2, "otel", "environment"); got != "prod" {
		t.Errorf("hand-written sibling key lost on removal: %v", got)
	}
	exp := tomlGet(t, data2, "otel", "exporter")
	statsig, ok := exp.(map[string]any)
	if !ok || len(statsig) != 1 || statsig["statsig"] == nil {
		t.Errorf("replaced exporter not restored: %v", exp)
	}
	if _, has := tomlGet(t, data2, "otel").(map[string]any)["trace_exporter"]; has {
		t.Error("trace_exporter still placed after disable")
	}
	if res2.NewRecord.Has("config.toml", "otel.exporter") {
		t.Error("record still lists otel.exporter after disable")
	}
}

func TestPlanCodexTelemetryIdempotent(t *testing.T) {
	d := builtin(t, "codex")
	p := codexFeatureProfile(t, map[string]string{
		"traces": "on", "logs": "on", "endpoint": "https://otlp.example.com",
	})
	res := plan(t, p, d, codexDoc(t, "model = \"glm-5\"\n"), launch.Record{})
	data := res.Writes()["config.toml"]
	again := plan(t, p, d, launch.Doc{Files: map[string]launch.FileDoc{"config.toml": launch.ParseFileDoc("config.toml", data)}}, res.NewRecord)
	if len(again.Steps) != 0 || len(again.Writes()) != 0 {
		t.Errorf("second sync is not a no-op: steps %+v writes %v", again.Steps, again.Writes())
	}
}

// Review checkpoint finding 1: a feature location a harnesses.d override moved
// or removed is actually deleted from config.toml, not just dropped from the
// record while sync claims a removal.
func TestPlanCodexStaleFeatureLocationRemoved(t *testing.T) {
	set, err := harness.LoadBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	d, _ := set.Get("codex")
	p := codexFeatureProfile(t, map[string]string{
		"traces": "on", "logs": "on", "endpoint": "https://otlp.example.com",
	})
	res := plan(t, p, d, codexDoc(t, ""), launch.Record{})
	rec := res.NewRecord
	data := res.Writes()["config.toml"]
	if _, ok := tomlGet(t, data, "otel", "exporter").(map[string]any); !ok {
		t.Fatalf("exporter not placed:\n%s", data)
	}
	// An override that renamed the exporter location: the old location is no
	// longer in the mapping.
	moved := *d
	fm := moved.Features["telemetry"]
	fm.Place = []harness.FeaturePlace{
		{File: "config.toml", Location: "otel.logs_exporter", When: "logs", Value: harness.FeatureValue{Str: "otlp"}},
	}
	moved.Features = map[string]harness.FeatureMapping{"telemetry": fm}
	res2 := plan(t, p, &moved, codexDoc(t, string(data)), rec)
	data2, ok := res2.Writes()["config.toml"]
	if !ok {
		t.Fatalf("plan writes nothing; steps %+v", res2.Steps)
	}
	otel, ok := tomlGet(t, data2, "otel").(map[string]any)
	if !ok {
		t.Fatalf("no [otel] table:\n%s", data2)
	}
	if _, has := otel["exporter"]; has {
		t.Errorf("stale otel.exporter not removed:\n%s", data2)
	}
	if _, has := otel["trace_exporter"]; has {
		t.Errorf("stale otel.trace_exporter not removed:\n%s", data2)
	}
	if otel["logs_exporter"] != "otlp" {
		t.Errorf("moved location not placed:\n%s", data2)
	}
	if res2.NewRecord.Has("config.toml", "otel.exporter") {
		t.Error("record still lists the stale location")
	}
}

// Review checkpoint finding 5: disabling on a fresh file leaves no empty [otel]
// header behind.
func TestPlanCodexDisablePrunesEmptyParents(t *testing.T) {
	d := builtin(t, "codex")
	p := codexFeatureProfile(t, map[string]string{
		"traces": "on", "logs": "on", "endpoint": "https://otlp.example.com",
	})
	res := plan(t, p, d, codexDoc(t, ""), launch.Record{})
	data := res.Writes()["config.toml"]
	p.Features = nil
	res2 := plan(t, p, d, codexDoc(t, string(data)), res.NewRecord)
	if len(res2.Writes()) == 0 {
		t.Fatalf("disable writes nothing")
	}
	data2 := res2.Writes()["config.toml"]
	if strings.Contains(string(data2), "otel") {
		t.Errorf("empty [otel] header left behind:\n%s", data2)
	}
}
