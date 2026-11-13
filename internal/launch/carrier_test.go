package launch_test

import (
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/launch"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/secret"
)

func builtin(t *testing.T, name string) *harness.Definition {
	t.Helper()
	set, err := harness.LoadBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	d, ok := set.Get(name)
	if !ok {
		t.Fatalf("builtin %s missing: %v", name, set.Errors())
	}
	return d
}

func ref(t *testing.T, raw string) secret.Reference {
	t.Helper()
	r, err := secret.ParseReference(raw)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// claudeProfile is an API-key Claude profile with model, endpoint, a literal alias
// and a reference-valued extra variable.
func claudeProfile(t *testing.T) *profile.Profile {
	return &profile.Profile{
		Harness: "claude", Name: "work", AuthKind: "api-key", Share: profile.ShareContent,
		AuthRefs: map[string]secret.Reference{"ANTHROPIC_API_KEY": ref(t, "file:/keys/work")},
		Model:    "gw-model", Endpoint: "http://127.0.0.1:1",
		ExtraEnv: []profile.ExtraEnvVar{
			{Name: "ANTHROPIC_DEFAULT_HAIKU_MODEL", Value: "gw-haiku"},
			{Name: "OTEL_TOKEN", Value: "file:/keys/otel"},
		},
	}
}

func settingsDoc(t *testing.T, content string) launch.Doc {
	t.Helper()
	return launch.Doc{Files: map[string]launch.FileDoc{"settings.json": launch.ParseFileDoc("settings.json", []byte(content))}}
}

func byKind(ss []launch.Setting) map[string]launch.Setting {
	m := map[string]launch.Setting{}
	for _, s := range ss {
		m[s.Kind] = s
	}
	return m
}

func expect(t *testing.T, ss []launch.Setting, kind string, carrier launch.Carrier, reason launch.Reason) launch.Setting {
	t.Helper()
	s, ok := byKind(ss)[kind]
	if !ok {
		t.Fatalf("no setting %q in %+v", kind, ss)
	}
	if s.Carrier != carrier || s.Reason != reason {
		t.Errorf("%s: carrier=%s reason=%q, want %s %q", kind, s.Carrier, s.Reason, carrier, reason)
	}
	return s
}

func TestDecideClaudeAPIKey(t *testing.T) {
	d := builtin(t, "claude")
	ss := launch.Decide(claudeProfile(t), d, settingsDoc(t, `{}`))
	m := expect(t, ss, "model", launch.CarrierFolder, "")
	if m.Variable != "ANTHROPIC_MODEL" || m.Value != "gw-model" {
		t.Errorf("model setting = %+v", m)
	}
	expect(t, ss, "endpoint", launch.CarrierFolder, "")
	expect(t, ss, "extra:ANTHROPIC_DEFAULT_HAIKU_MODEL", launch.CarrierFolder, "")
	expect(t, ss, "extra:OTEL_TOKEN", launch.CarrierWrapper, launch.ReasonReferenceValue)
	k := expect(t, ss, "key", launch.CarrierFolder, "")
	if k.Variable != "ANTHROPIC_API_KEY" || k.Ref == nil || k.EnvDependent || k.Value != "" {
		t.Errorf("key setting = %+v", k)
	}
	// Order: model, endpoint, extras in profile order, key.
	want := []string{"model", "endpoint", "extra:ANTHROPIC_DEFAULT_HAIKU_MODEL", "extra:OTEL_TOKEN", "key"}
	for i, s := range ss {
		if i >= len(want) || s.Kind != want[i] {
			t.Fatalf("order = %v, want %v", kinds(ss), want)
		}
	}
}

func kinds(ss []launch.Setting) []string {
	var out []string
	for _, s := range ss {
		out = append(out, s.Kind)
	}
	return out
}

func TestDecideEnvKeyIsEnvDependent(t *testing.T) {
	p := claudeProfile(t)
	p.AuthRefs = map[string]secret.Reference{"ANTHROPIC_API_KEY": ref(t, "env:MY_KEY")}
	k := expect(t, launch.Decide(p, builtin(t, "claude"), settingsDoc(t, `{}`)), "key", launch.CarrierFolder, "")
	if !k.EnvDependent {
		t.Error("env: key should be env-dependent")
	}
}

func TestDecideClearedSource(t *testing.T) {
	p := claudeProfile(t)
	p.AuthKind = "auth-token"
	p.AuthRefs = map[string]secret.Reference{"ANTHROPIC_AUTH_TOKEN": ref(t, "env:ANTHROPIC_AUTH_TOKEN")}
	expect(t, launch.Decide(p, builtin(t, "claude"), settingsDoc(t, `{}`)), "key", launch.CarrierWrapper, launch.ReasonClearedSource)

	// A launch_clear variable as key source is cleared too.
	p.AuthRefs = map[string]secret.Reference{"ANTHROPIC_AUTH_TOKEN": ref(t, "env:ANTHROPIC_MODEL")}
	expect(t, launch.Decide(p, builtin(t, "claude"), settingsDoc(t, `{}`)), "key", launch.CarrierWrapper, launch.ReasonClearedSource)

	// Codex env:OPENAI_API_KEY, as import produces it.
	cp := codexProfile(t)
	cp.AuthRefs = map[string]secret.Reference{"OPENAI_API_KEY": ref(t, "env:OPENAI_API_KEY")}
	expect(t, launch.Decide(cp, builtin(t, "codex"), codexDoc(t, codexTOML)), "key", launch.CarrierWrapper, launch.ReasonClearedSource)
}

func TestDecideAuthKinds(t *testing.T) {
	p := claudeProfile(t)
	p.AuthKind = "oauth-token"
	p.AuthRefs = map[string]secret.Reference{"CLAUDE_CODE_OAUTH_TOKEN": ref(t, "file:/k")}
	expect(t, launch.Decide(p, builtin(t, "claude"), settingsDoc(t, `{}`)), "key", launch.CarrierWrapper, launch.ReasonAuthKind)

	p.AuthKind = "oauth-login"
	p.AuthRefs = nil
	if _, ok := byKind(launch.Decide(p, builtin(t, "claude"), settingsDoc(t, `{}`)))["key"]; ok {
		t.Error("login auth kind has no key setting")
	}
	cp := codexProfile(t)
	cp.AuthKind = "chatgpt-login"
	cp.AuthRefs = nil
	if _, ok := byKind(launch.Decide(cp, builtin(t, "codex"), codexDoc(t, codexTOML)))["key"]; ok {
		t.Error("chatgpt-login has no key setting")
	}
}

func TestDecideShareAll(t *testing.T) {
	p := claudeProfile(t)
	p.Share = profile.ShareAll
	// The settings file is a link into the default setup.
	fd := launch.ParseFileDoc("settings.json", []byte(`{}`))
	fd.Linked = true
	ss := launch.Decide(p, builtin(t, "claude"), launch.Doc{Files: map[string]launch.FileDoc{"settings.json": fd}})
	for _, k := range []string{"model", "endpoint", "extra:ANTHROPIC_DEFAULT_HAIKU_MODEL", "key"} {
		expect(t, ss, k, launch.CarrierWrapper, launch.ReasonShareAll)
	}
	// Absent file at all: sync links it, so it is share-all too.
	ss = launch.Decide(p, builtin(t, "claude"), launch.Doc{Files: map[string]launch.FileDoc{}})
	expect(t, ss, "model", launch.CarrierWrapper, launch.ReasonShareAll)

	// A Codex API-key profile at all still has a folder-carried key: config.toml is
	// owned at every level.
	cp := codexProfile(t)
	cp.Share = profile.ShareAll
	expect(t, launch.Decide(cp, builtin(t, "codex"), codexDoc(t, codexTOML)), "key", launch.CarrierFolder, "")
}

func TestDecideUpgradeKeepsOwnFileAtAll(t *testing.T) {
	// A profile from before share levels loads as all but keeps its own (not linked)
	// settings.json; the folder carries the settings there.
	p := claudeProfile(t)
	p.Share = ""
	p.ShareImplicit = true
	expect(t, launch.Decide(p, builtin(t, "claude"), settingsDoc(t, `{}`)), "model", launch.CarrierFolder, "")
}

const codexTOML = `model_provider = "enmaas"

[model_providers.enmaas]
name = "enmaas"
base_url = "https://gw.example/v1"
env_key = "OPENAI_API_KEY"
`

func codexProfile(t *testing.T) *profile.Profile {
	return &profile.Profile{
		Harness: "codex", Name: "work-glm", AuthKind: "api-key", Share: profile.ShareContent,
		AuthRefs: map[string]secret.Reference{"OPENAI_API_KEY": ref(t, "file:/keys/glm")},
		Model:    "glm-5", Endpoint: "https://gw.example/v1",
	}
}

func codexDoc(t *testing.T, content string) launch.Doc {
	t.Helper()
	return launch.Doc{Files: map[string]launch.FileDoc{"config.toml": launch.ParseFileDoc("config.toml", []byte(content))}}
}

func TestDecideCodex(t *testing.T) {
	ss := launch.Decide(codexProfile(t), builtin(t, "codex"), codexDoc(t, codexTOML))
	expect(t, ss, "model", launch.CarrierWrapper, launch.ReasonHarnessRewrites)
	expect(t, ss, "endpoint", launch.CarrierWrapper, launch.ReasonNoSupport)
	expect(t, ss, "key", launch.CarrierFolder, "")

	// No custom provider selected.
	expect(t, launch.Decide(codexProfile(t), builtin(t, "codex"), codexDoc(t, "model = \"x\"\n")), "key", launch.CarrierWrapper, launch.ReasonNoProvider)
	// Selected provider not defined in the file.
	expect(t, launch.Decide(codexProfile(t), builtin(t, "codex"), codexDoc(t, "model_provider = \"other\"\n")), "key", launch.CarrierWrapper, launch.ReasonNoProvider)
	// Absent config.toml.
	expect(t, launch.Decide(codexProfile(t), builtin(t, "codex"), launch.Doc{Files: map[string]launch.FileDoc{}}), "key", launch.CarrierWrapper, launch.ReasonNoProvider)
}

func TestDecideNoSupportHarnesses(t *testing.T) {
	for _, name := range []string{"opencode", "pi"} {
		set, _ := harness.LoadBuiltins()
		d, ok := set.Get(name)
		if !ok {
			continue
		}
		p := &profile.Profile{Harness: name, Name: "x", Model: "m", Endpoint: "http://e",
			ExtraEnv: []profile.ExtraEnvVar{{Name: "FOO", Value: "bar"}}}
		if len(d.AuthKinds) > 0 {
			for _, k := range d.AuthKinds {
				if len(k.Vars) > 0 {
					p.AuthKind = k.Name
					p.AuthRefs = map[string]secret.Reference{}
					for v := range k.Vars {
						p.AuthRefs[v] = ref(t, "file:/k")
					}
					break
				}
			}
		}
		for _, s := range launch.Decide(p, d, launch.Doc{Files: map[string]launch.FileDoc{}}) {
			if s.Carrier != launch.CarrierWrapper {
				t.Errorf("%s: %s is folder-carried", name, s.Kind)
			}
			if s.Kind != "key" && s.Reason != launch.ReasonNoSupport {
				t.Errorf("%s: %s reason %q, want no-support", name, s.Kind, s.Reason)
			}
		}
	}
}

func TestDecideHookEnv(t *testing.T) {
	ss := launch.DecideWith(claudeProfile(t), builtin(t, "claude"), settingsDoc(t, `{}`), []harness.EnvVar{{Name: "HOOK_VAR", Value: "v"}})
	expect(t, ss, "hook:HOOK_VAR", launch.CarrierWrapper, launch.ReasonHookEnv)
}

func TestDecideIsPure(t *testing.T) {
	p := claudeProfile(t)
	d := builtin(t, "claude")
	doc := settingsDoc(t, `{"env":{"X":"1"}}`)
	a := launch.Decide(p, d, doc)
	b := launch.Decide(p, d, doc)
	if len(a) != len(b) {
		t.Fatal("Decide is not deterministic")
	}
	for i := range a {
		if a[i].Kind != b[i].Kind || a[i].Carrier != b[i].Carrier {
			t.Fatal("Decide is not deterministic")
		}
	}
}

// Checkpoint 2 finding 2: an env: key read from one of the profile's own launch
// variables is cleared by every wrapper, so the folder cannot carry it.
func TestDecideKeyFromOwnLaunchVariable(t *testing.T) {
	p := claudeProfile(t)
	p.AuthRefs = map[string]secret.Reference{"ANTHROPIC_API_KEY": ref(t, "env:OTEL_TOKEN")}
	expect(t, launch.Decide(p, builtin(t, "claude"), settingsDoc(t, `{}`)), "key", launch.CarrierWrapper, launch.ReasonClearedSource)
}
