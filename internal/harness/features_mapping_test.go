package harness_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
)

// Spec 006 T004/T005: the capability-mapping schema (features: key) of harness
// definitions.

// baseDef is a minimal valid definition with a classified settings file, used as
// the carrier for feature mappings in these tests.
const baseDef = `
name: testh
binary: testh
wrapper_prefix: testh
config_env:
  TESTH_HOME: "."
default_config_location: "~/.testh"
classification:
  - { entry: "settings.json", layer: settings }
  - { entry: "config.toml", layer: owned }
`

const telemetryMapping = `
features:
  telemetry:
    params:
      traces: {}
      logs: {}
      endpoint: {}
      protocol:
        default: http/protobuf
        values: [grpc, http/json, http/protobuf]
    place:
      - { file: settings.json, location: env.TESTH_TELEMETRY, value: "1" }
      - { file: settings.json, location: env.TESTH_TRACES, value: otlp, when: traces }
      - { file: settings.json, location: env.TESTH_TRACES, value: none, when-not: traces }
      - { file: settings.json, location: env.TESTH_LOGS, value: otlp, when: logs }
      - { file: settings.json, location: env.TESTH_LOGS, value: none, when-not: logs }
      - { file: settings.json, location: env.TESTH_ENDPOINT, value: "{param:endpoint}" }
      - { file: settings.json, location: env.TESTH_PROTOCOL, value: "{param:protocol}" }
`

func TestFeatureMappingValid(t *testing.T) {
	d, err := harness.ParseBuiltinForTest([]byte(baseDef + telemetryMapping))
	if err != nil {
		t.Fatalf("valid mapping failed to load: %v", err)
	}
	fm, ok := d.FeatureMapping("telemetry")
	if !ok {
		t.Fatal("telemetry mapping missing")
	}
	if got := fm.Params["protocol"].Default; got != "http/protobuf" {
		t.Errorf("protocol default = %q", got)
	}
	if len(fm.Place) != 7 {
		t.Errorf("place rules = %d, want 7", len(fm.Place))
	}
	if fm.Place[1].When != "traces" || fm.Place[2].WhenNot != "traces" {
		t.Errorf("when/when-not gates not parsed: %+v", fm.Place[1:3])
	}
	if fm.Place[5].Value.Str != "{param:endpoint}" {
		t.Errorf("param reference not parsed: %+v", fm.Place[5].Value)
	}
	// SupportedFeatures lists the catalog features the harness declares.
	if got := d.SupportedFeatures(); len(got) != 1 || got[0] != "telemetry" {
		t.Errorf("SupportedFeatures = %v", got)
	}
}

func TestFeatureMappingUnknownFeatureID(t *testing.T) {
	_, err := harness.ParseBuiltinForTest([]byte(baseDef + `
features:
  time-travel:
    place:
      - { file: settings.json, location: env.X, value: "1" }
`))
	if err == nil || !strings.Contains(err.Error(), "time-travel") {
		t.Errorf("unknown feature id should fail naming it: %v", err)
	}
}

func TestFeatureMappingUnknownParamName(t *testing.T) {
	_, err := harness.ParseBuiltinForTest([]byte(baseDef + `
features:
  telemetry:
    params:
      flavor: {}
    place:
      - { file: settings.json, location: env.X, value: "1" }
`))
	if err == nil || !strings.Contains(err.Error(), "flavor") {
		t.Errorf("unknown param name should fail naming it: %v", err)
	}
}

func TestFeatureMappingDefaultOutsideValues(t *testing.T) {
	_, err := harness.ParseBuiltinForTest([]byte(baseDef + `
features:
  telemetry:
    params:
      protocol:
        default: smoke-signals
        values: [grpc, http/json]
    place:
      - { file: settings.json, location: env.X, value: "{param:protocol}" }
`))
	if err == nil || !strings.Contains(err.Error(), "smoke-signals") {
		t.Errorf("default outside values should fail naming it: %v", err)
	}
}

func TestFeatureMappingEnumWithoutValues(t *testing.T) {
	_, err := harness.ParseBuiltinForTest([]byte(baseDef + `
features:
  telemetry:
    params:
      protocol: {}
    place:
      - { file: settings.json, location: env.X, value: "{param:protocol}" }
`))
	if err == nil || !strings.Contains(err.Error(), "protocol") {
		t.Errorf("enum param without values should fail naming it: %v", err)
	}
}

func TestFeatureMappingUnclassifiedFile(t *testing.T) {
	_, err := harness.ParseBuiltinForTest([]byte(baseDef + `
features:
  telemetry:
    params:
      traces: {}
    place:
      - { file: unclassified.json, location: env.X, value: "1" }
`))
	if err == nil || !strings.Contains(err.Error(), "unclassified.json") {
		t.Errorf("unclassified placement file should fail naming it: %v", err)
	}
}

func TestFeatureMappingBadWhen(t *testing.T) {
	// when must name a bool param of the feature; endpoint is a string param.
	_, err := harness.ParseBuiltinForTest([]byte(baseDef + `
features:
  telemetry:
    params:
      endpoint: {}
    place:
      - { file: settings.json, location: env.X, value: "1", when: endpoint }
`))
	if err == nil || !strings.Contains(err.Error(), "endpoint") {
		t.Errorf("when on a non-bool param should fail naming it: %v", err)
	}
	// when and when-not together are contradictory.
	_, err = harness.ParseBuiltinForTest([]byte(baseDef + `
features:
  telemetry:
    params:
      traces: {}
      logs: {}
    place:
      - { file: settings.json, location: env.X, value: "1", when: traces, when-not: logs }
`))
	if err == nil || !strings.Contains(err.Error(), "when") {
		t.Errorf("when and when-not together should fail: %v", err)
	}
}

func TestFeatureMappingBadValueReference(t *testing.T) {
	// A {param:...} reference must name a declared param of the feature.
	_, err := harness.ParseBuiltinForTest([]byte(baseDef + `
features:
  telemetry:
    params:
      traces: {}
    place:
      - { file: settings.json, location: env.X, value: "{param:nope}" }
`))
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("unknown param reference should fail naming it: %v", err)
	}
}

func TestFeatureMappingBadEnvLocation(t *testing.T) {
	_, err := harness.ParseBuiltinForTest([]byte(baseDef + `
features:
  telemetry:
    params: {}
    place:
      - { file: settings.json, location: "env.BAD;NAME", value: "1" }
`))
	if err == nil || !strings.Contains(err.Error(), "BAD;NAME") {
		t.Errorf("invalid env name in location should fail naming it: %v", err)
	}
}

func TestFeatureMappingOverrideReplaces(t *testing.T) {
	dir := t.TempDir()
	// Override the built-in claude telemetry mapping: a harnesses.d file replaces
	// the feature's place list wholesale (same merge rule as other definition keys).
	override := `
name: claude
features:
  telemetry:
    params:
      traces: {}
      logs: {}
      endpoint: {}
      protocol:
        default: http/protobuf
        values: [grpc, http/json, http/protobuf]
    place:
      - { file: settings.json, location: env.CUSTOM_TELEMETRY, value: "1" }
`
	if err := os.WriteFile(filepath.Join(dir, "claude.yaml"), []byte(override), 0o644); err != nil {
		t.Fatal(err)
	}
	set, err := harness.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if errs := set.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected load errors: %v", errs)
	}
	d, _ := set.Get("claude")
	fm, ok := d.FeatureMapping("telemetry")
	if !ok {
		t.Fatal("telemetry mapping missing after override")
	}
	if len(fm.Place) != 1 || fm.Place[0].Location != "env.CUSTOM_TELEMETRY" {
		t.Errorf("override should replace the place list: %+v", fm.Place)
	}
	// automode-log, untouched by the override, stays.
	if _, ok := d.FeatureMapping("automode-log"); !ok {
		t.Error("automode-log mapping lost after override")
	}
}

// ResolveFeatureParams applies defaults and validates values against the
// harness's enum (FR-004).
func TestResolveFeatureParams(t *testing.T) {
	d, err := harness.ParseBuiltinForTest([]byte(baseDef + telemetryMapping))
	if err != nil {
		t.Fatal(err)
	}
	// Defaults: omitted signals off, protocol default applied.
	got, err := d.ResolveFeatureParams("telemetry", map[string]string{
		"traces": "on", "endpoint": "https://otlp.example.com",
	})
	if err != nil {
		t.Fatalf("ResolveFeatureParams: %v", err)
	}
	if got["logs"] != "off" || got["protocol"] != "http/protobuf" {
		t.Errorf("defaults not applied: %+v", got)
	}
	// An out-of-enum value is rejected naming the value.
	if _, err := d.ResolveFeatureParams("telemetry", map[string]string{
		"traces": "on", "endpoint": "e", "protocol": "otlp-grpc",
	}); err == nil || !strings.Contains(err.Error(), "otlp-grpc") {
		t.Errorf("out-of-enum protocol should fail naming it: %v", err)
	}
	// A bad bool value is rejected.
	if _, err := d.ResolveFeatureParams("telemetry", map[string]string{"traces": "maybe"}); err == nil {
		t.Error("bad bool value should fail")
	}
	// An unknown param name is rejected.
	if _, err := d.ResolveFeatureParams("telemetry", map[string]string{"flavor": "vanilla"}); err == nil {
		t.Error("unknown param should fail")
	}
	// An unsupported feature is rejected naming the harness.
	if _, err := d.ResolveFeatureParams("automode-log", nil); err == nil || !strings.Contains(err.Error(), "testh") {
		t.Errorf("unsupported feature should fail naming the harness: %v", err)
	}
}

// FeatureEnvNames lists the env names a feature places on a harness
// (record-independent; used by the collision checks).
func TestFeatureEnvNames(t *testing.T) {
	d, err := harness.ParseBuiltinForTest([]byte(baseDef + telemetryMapping))
	if err != nil {
		t.Fatal(err)
	}
	names := d.FeatureEnvNames("telemetry")
	want := []string{"TESTH_ENDPOINT", "TESTH_LOGS", "TESTH_PROTOCOL", "TESTH_TELEMETRY", "TESTH_TRACES"}
	if len(names) != len(want) {
		t.Fatalf("FeatureEnvNames = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("FeatureEnvNames[%d] = %q, want %q", i, names[i], want[i])
		}
	}
	if got := d.FeatureEnvNames("automode-log"); len(got) != 0 {
		t.Errorf("FeatureEnvNames(automode-log) = %v, want none", got)
	}
}
