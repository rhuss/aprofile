package launch_test

import (
	"encoding/json"
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

	// Switching to a login kind removes the key command and the neutralizing entries.
	p3 := claudeProfile(t)
	p3.AuthKind, p3.AuthRefs = "oauth-login", nil
	res = plan(t, p3, d, settingsDoc(t, string(placed)), rec)
	m3 := writtenJSON(t, res, "settings.json")
	if _, ok := m3["apiKeyHelper"]; ok {
		t.Error("apiKeyHelper not removed")
	}
	if _, ok := m3["env"].(map[string]any)["ANTHROPIC_API_KEY"]; ok {
		t.Error("neutralizing entry not removed")
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
	want := []string{"apiKeyHelper", "env.ANTHROPIC_DEFAULT_OPUS_MODEL", "env.CLAUDE_CODE_USE_VERTEX", "env.MY_EXTRA"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unplaced = %v, want %v", got, want)
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
	// Every setting wrapper-only: a parse error is a note, not an error.
	p := claudeProfile(t)
	p.AuthKind, p.AuthRefs, p.Model, p.Endpoint, p.ExtraEnv = "oauth-login", nil, "", "", nil
	res, err := launch.Plan(p, d, settingsDoc(t, `{not json`), launch.Record{})
	if err != nil || len(res.Notes) == 0 {
		t.Errorf("wrapper-only profile with a broken file: err=%v notes=%v", err, res.Notes)
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
