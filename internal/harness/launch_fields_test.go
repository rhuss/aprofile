package harness_test

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
)

// --- Feature 004 T003: folder_launch, launch_clear, model_arg, folder_key ---

func TestBuiltinClaudeLaunchFields(t *testing.T) {
	set, err := harness.LoadBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	d, ok := set.Get("claude")
	if !ok {
		t.Fatalf("claude not loaded: %v", set.Errors())
	}
	if d.FolderLaunch == nil {
		t.Fatal("claude folder_launch missing")
	}
	if d.FolderLaunch.Env != "settings.json#env" {
		t.Errorf("folder_launch.env = %q", d.FolderLaunch.Env)
	}
	kc := d.FolderLaunch.KeyCommand
	if kc == nil || kc.Location != "settings.json#apiKeyHelper" || kc.Form != harness.KeyFormShell {
		t.Fatalf("key_command = %+v", kc)
	}
	if kc.File() != "settings.json" || kc.Key() != "apiKeyHelper" {
		t.Errorf("key_command File/Key = %q %q", kc.File(), kc.Key())
	}
	if f, k := d.FolderLaunch.EnvFile(), d.FolderLaunch.EnvKey(); f != "settings.json" || k != "env" {
		t.Errorf("env File/Key = %q %q", f, k)
	}
	want := []string{"ANTHROPIC_MODEL", "ANTHROPIC_BASE_URL", "ANTHROPIC_DEFAULT_OPUS_MODEL",
		"ANTHROPIC_DEFAULT_SONNET_MODEL", "ANTHROPIC_DEFAULT_HAIKU_MODEL",
		"ANTHROPIC_DEFAULT_FABLE_MODEL", "CLAUDE_CODE_SUBAGENT_MODEL"}
	if !reflect.DeepEqual(d.LaunchClear, want) {
		t.Errorf("launch_clear = %v, want %v", d.LaunchClear, want)
	}
	if len(d.ModelArg) != 0 {
		t.Errorf("claude model_arg = %v, want none", d.ModelArg)
	}
	for kind, want := range map[string]bool{"api-key": true, "auth-token": true, "oauth-token": false, "oauth-login": false} {
		if got := d.FolderKeyFor(kind); got != want {
			t.Errorf("FolderKeyFor(%s) = %v, want %v", kind, got, want)
		}
	}
	if !d.CarriesModel() {
		t.Error("claude CarriesModel should be true (model_var)")
	}
}

func TestBuiltinCodexLaunchFields(t *testing.T) {
	set, err := harness.LoadBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	d, ok := set.Get("codex")
	if !ok {
		t.Fatalf("codex not loaded: %v", set.Errors())
	}
	if d.FolderLaunch == nil || d.FolderLaunch.Env != "" {
		t.Fatalf("codex folder_launch = %+v, want key_command only", d.FolderLaunch)
	}
	kc := d.FolderLaunch.KeyCommand
	if kc == nil || kc.Location != "config.toml#model_providers" || kc.Form != harness.KeyFormProviderAuth ||
		kc.Selector != "model_provider" || !reflect.DeepEqual(kc.Replaces, []string{"env_key"}) {
		t.Fatalf("codex key_command = %+v", kc)
	}
	if !reflect.DeepEqual(d.ModelArg, []string{"-c", "model={model:toml}"}) {
		t.Errorf("codex model_arg = %v", d.ModelArg)
	}
	if !reflect.DeepEqual(d.LaunchClear, []string{"OPENAI_BASE_URL"}) {
		t.Errorf("codex launch_clear = %v", d.LaunchClear)
	}
	if !d.FolderKeyFor("api-key") || d.FolderKeyFor("chatgpt-login") {
		t.Error("codex folder_key: want api-key true, chatgpt-login false")
	}
	if d.ModelVar != "" {
		t.Errorf("codex model_var = %q, want none", d.ModelVar)
	}
	if !d.CarriesModel() {
		t.Error("codex CarriesModel should be true (model_arg)")
	}
}

func TestBuiltinOthersHaveNoLaunchFields(t *testing.T) {
	set, _ := harness.LoadBuiltins()
	for _, name := range []string{"opencode", "pi"} {
		d, ok := set.Get(name)
		if !ok {
			continue
		}
		if d.FolderLaunch != nil || len(d.ModelArg) != 0 {
			t.Errorf("%s: unexpected launch fields %+v %v", name, d.FolderLaunch, d.ModelArg)
		}
	}
}

func TestExpandModelArg(t *testing.T) {
	d := &harness.Definition{ModelArg: []string{"-c", "model={model:toml}"}}
	cases := map[string][]string{
		"1.5":        {"-c", `model="1.5"`},
		`a"b`:        {"-c", `model="a\"b"`},
		`back\slash`: {"-c", `model="back\\slash"`},
	}
	for model, want := range cases {
		if got := d.ExpandModelArg(model); !reflect.DeepEqual(got, want) {
			t.Errorf("ExpandModelArg(%q) = %q, want %q", model, got, want)
		}
	}
	raw := &harness.Definition{ModelArg: []string{"--model", "{model}"}}
	if got := raw.ExpandModelArg(`x"y`); !reflect.DeepEqual(got, []string{"--model", `x"y`}) {
		t.Errorf("raw ExpandModelArg = %q", got)
	}
	if got := d.ExpandModelArg(""); got != nil {
		t.Errorf("empty model should expand to nil, got %q", got)
	}
	none := &harness.Definition{}
	if got := none.ExpandModelArg("m"); got != nil {
		t.Errorf("no model_arg should expand to nil, got %q", got)
	}
}

func TestCarriesModel(t *testing.T) {
	if (&harness.Definition{}).CarriesModel() {
		t.Error("empty definition carries no model")
	}
	if !(&harness.Definition{ModelVar: "M"}).CarriesModel() {
		t.Error("model_var carries the model")
	}
	if !(&harness.Definition{ModelArg: []string{"{model}"}}).CarriesModel() {
		t.Error("model_arg carries the model")
	}
}

func TestLaunchFieldsOverrideMerge(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "claude.yaml")
	writeExec(t, file, "name: claude\nfolder_launch:\n  env: null\nlaunch_clear: [ANTHROPIC_MODEL]\n")
	set, _ := harness.Load(dir)
	if errs := set.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	d, _ := set.Get("claude")
	if d.FolderLaunch == nil || d.FolderLaunch.Env != "" {
		t.Errorf("folder_launch.env should be removed by null, got %+v", d.FolderLaunch)
	}
	if d.FolderLaunch == nil || d.FolderLaunch.KeyCommand == nil {
		t.Error("folder_launch.key_command should survive the by-key merge")
	}
	if !reflect.DeepEqual(d.LaunchClear, []string{"ANTHROPIC_MODEL"}) {
		t.Errorf("launch_clear should be replaced, got %v", d.LaunchClear)
	}
	if d.Source["launch_clear"] != file || d.Source["folder_launch"] != file || d.Source["model_arg"] != "builtin" {
		t.Errorf("sources = %v", d.Source)
	}

	dir2 := t.TempDir()
	writeExec(t, filepath.Join(dir2, "codex.yaml"), "name: codex\nmodel_arg: null\n")
	set2, _ := harness.Load(dir2)
	c, ok := set2.Get("codex")
	if !ok {
		t.Fatalf("codex: %v", set2.Errors())
	}
	if len(c.ModelArg) != 0 || c.CarriesModel() {
		t.Errorf("model_arg null should remove it, got %v", c.ModelArg)
	}
}

func TestLaunchFieldsValidation(t *testing.T) {
	base := "name: lf\nbinary: lf\nwrapper_prefix: lf\nconfig_env:\n  LF_DIR: \".\"\nmodel_var: LF_MODEL\nendpoint_var: LF_URL\n" +
		"classification:\n  - { entry: settings.json, layer: settings }\n  - { entry: config.toml, layer: owned }\n" +
		"auth_kinds:\n  - name: key\n    vars: { LF_KEY: ref }\n"
	cases := map[string]struct{ src, field string }{
		"env not file#key":      {base + "folder_launch:\n  env: settings.json\n", "folder_launch"},
		"env file unclassified": {base + "folder_launch:\n  env: other.json#env\n", "folder_launch"},
		"key location bad":      {base + "folder_launch:\n  key_command: { location: \"#x\", form: shell }\n", "folder_launch"},
		"key file unclassified": {base + "folder_launch:\n  key_command: { location: \"x.json#k\", form: shell }\n", "folder_launch"},
		"unknown form":          {base + "folder_launch:\n  key_command: { location: \"settings.json#k\", form: magic }\n", "folder_launch"},
		"provider no selector":  {base + "folder_launch:\n  key_command: { location: \"config.toml#p\", form: provider-auth }\n", "folder_launch"},
		"launch_clear bad":      {base + "launch_clear: [\"1BAD\"]\n", "launch_clear"},
		"launch_clear contract": {base + "launch_clear: [APROFILE_NAME]\n", "launch_clear"},
		"launch_clear redirect": {base + "launch_clear: [LF_DIR]\n", "launch_clear"},
		"model_arg missing":     {base + "model_arg: [\"-m\", \"x\"]\n", "model_arg"},
		"model_arg twice":       {base + "model_arg: [\"{model}\", \"{model:toml}\"]\n", "model_arg"},
		"folder_key no command": {strings.Replace(base, "vars: { LF_KEY: ref }\n", "vars: { LF_KEY: ref }\n    folder_key: true\n", 1), "auth_kinds"},
	}
	for name, c := range cases {
		set := harness.LoadFromBytes(map[string][]byte{"lf.yaml": []byte(c.src)})
		if _, ok := set.Get("lf"); ok {
			t.Errorf("%s: definition should be rejected", name)
			continue
		}
		errs := set.Errors()
		if len(errs) != 1 || errs[0].Field != c.field || errs[0].File != "lf.yaml" {
			t.Errorf("%s: want one %s error naming lf.yaml, got %v", name, c.field, errs)
		}
	}
	ok := base + "folder_launch:\n  env: settings.json#env\n  key_command: { location: \"config.toml#p\", form: provider-auth, selector: sel, replaces: [env_key] }\n" +
		"launch_clear: [LF_MODEL, LF_URL]\nmodel_arg: [\"-c\", \"model={model:toml}\"]\n"
	set := harness.LoadFromBytes(map[string][]byte{"lf.yaml": []byte(ok)})
	if _, found := set.Get("lf"); !found {
		t.Errorf("valid launch fields rejected: %v", set.Errors())
	}
}

// The auth_clears rule that rejected the model and endpoint variables existed for
// the ${VAR:-default} caller override, which feature 004 removes.
func TestAuthClearsMayNameModelAndEndpoint(t *testing.T) {
	base := "name: ac\nbinary: ac\nwrapper_prefix: ac\nconfig_env:\n  AC_DIR: \".\"\nmodel_var: AC_MODEL\nendpoint_var: AC_URL\n"
	for _, v := range []string{"AC_MODEL", "AC_URL"} {
		set := harness.LoadFromBytes(map[string][]byte{"ac.yaml": []byte(base + "auth_clears: [" + v + "]\n")})
		if _, ok := set.Get("ac"); !ok {
			t.Errorf("auth_clears naming %s should be valid: %v", v, set.Errors())
		}
	}
}

func TestLaunchFieldsInAllFieldNames(t *testing.T) {
	set, _ := harness.LoadBuiltins()
	d, _ := set.Get("claude")
	for _, f := range []string{"folder_launch", "launch_clear", "model_arg"} {
		if d.Source[f] != "builtin" {
			t.Errorf("source for %s = %q, want builtin", f, d.Source[f])
		}
	}
}
