package layer_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/layer"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/testutil"
)

// mixedRule is a synthetic mixed-file rule exercising every layer, so the merge and
// drift helpers are tested from the definition data alone (Principle I: no
// harness-name branching).
func mixedRule() *harness.EntryRule {
	return &harness.EntryRule{
		Entry: "config.json",
		Layer: harness.LayerMixed,
		Keys: map[string]harness.Layer{
			"ownedA":    harness.LayerOwned,
			"ownedB":    harness.LayerOwned,
			"credKey":   harness.LayerCredentials,
			"stateKey":  harness.LayerState,
			"sharedKey": harness.LayerShared,
		},
	}
}

func TestMergeMixedOwnedMergesOwnedOnly(t *testing.T) {
	rule := mixedRule()
	dst := []byte(`{"ownedA":"old","credKey":"keep","sharedKey":"s","unknown":{"deep":[1,2,3]}}`)
	src := []byte(`{"ownedA":"new","ownedB":{"x":1},"credKey":"SECRET","stateKey":"sess","sharedKey":"other","unknown":"changed"}`)

	out, err := layer.MergeMixedOwned(rule, dst, src, ".json")
	if err != nil {
		t.Fatalf("MergeMixedOwned: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("merged output is not valid JSON: %v\n%s", err, out)
	}

	// Owned keys are merged from the source.
	if m["ownedA"] != "new" {
		t.Errorf("ownedA = %v, want new (owned keys come from source)", m["ownedA"])
	}
	if !reflect.DeepEqual(m["ownedB"], map[string]any{"x": float64(1)}) {
		t.Errorf("ownedB not merged from source: %v", m["ownedB"])
	}

	// Non-owned keys are preserved from the destination, never taken from the source.
	if m["credKey"] != "keep" {
		t.Errorf("credKey (credentials) must keep the dst value, got %v", m["credKey"])
	}
	if m["sharedKey"] != "s" {
		t.Errorf("sharedKey (shared) must keep the dst value, got %v", m["sharedKey"])
	}
	if _, ok := m["stateKey"]; ok {
		t.Error("stateKey (state, not owned) must not be merged in from source")
	}

	// An unclassified key is preserved from the destination byte-for-byte.
	if !reflect.DeepEqual(m["unknown"], map[string]any{"deep": []any{float64(1), float64(2), float64(3)}}) {
		t.Errorf("unknown key not preserved from dst: %v", m["unknown"])
	}
}

func TestMergeMixedOwnedRejectsNonJSON(t *testing.T) {
	if _, err := layer.MergeMixedOwned(mixedRule(), []byte("a=1"), []byte("a=2"), ".toml"); err == nil {
		t.Error("expected an error for a non-JSON mixed merge (TOML support is a later task)")
	}
}

func TestMixedSeededDriftDetectsChange(t *testing.T) {
	rule := mixedRule()
	drift, err := layer.MixedSeededDrift(rule,
		[]byte(`{"sharedKey":"changed","ownedA":"x"}`),
		[]byte(`{"sharedKey":"original","ownedA":"y"}`), ".json")
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 1 || drift[0] != "sharedKey" {
		t.Errorf("drift = %v, want [sharedKey]", drift)
	}

	// Key order differences in a value must NOT be reported as drift.
	drift2, _ := layer.MixedSeededDrift(rule,
		[]byte(`{"sharedKey":{"a":1,"b":2}}`),
		[]byte(`{"sharedKey":{"b":2,"a":1}}`), ".json")
	if len(drift2) != 0 {
		t.Errorf("expected no drift for key-reordered equal values, got %v", drift2)
	}
}

// contentProfile is a profile at share level content (mixed files are seeded at
// all and content; at none they start minimal, see plan_test.go).
func contentProfile() *profile.Profile {
	return &profile.Profile{Harness: "fake", Name: "p", Share: profile.ShareContent}
}

// TestSeedMixedSharedKeysSeededOnce proves shared keys are seeded once from the
// default setup and never rewritten afterwards (FR-013), while credentials/state
// keys are excluded from the seed.
func TestSeedMixedSharedKeysSeededOnce(t *testing.T) {
	h := testutil.HermeticHome(t)
	defaultDir := filepath.Join(h.Root, "default")
	testutil.WriteFile(t, filepath.Join(defaultDir, "config.json"),
		`{"ownedA":"o","credKey":"c","stateKey":"st","sharedKey":"seed"}`)

	def := &harness.Definition{
		Name:                  "fake",
		DefaultConfigLocation: defaultDir,
		Classification:        []harness.EntryRule{*mixedRule()},
	}
	configDir := filepath.Join(h.DataHome, "cfg")
	if _, err := layer.PrepareConfigDir(def, contentProfile(), configDir, false); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(configDir, "config.json")
	var m map[string]any
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("seeded file not valid JSON: %v", err)
	}
	if m["sharedKey"] != "seed" {
		t.Errorf("shared key not seeded from default setup: %v", m["sharedKey"])
	}
	if m["ownedA"] != "o" {
		t.Errorf("owned key not seeded: %v", m["ownedA"])
	}
	if _, ok := m["credKey"]; ok {
		t.Error("credential key must not be seeded into a new profile")
	}
	if _, ok := m["stateKey"]; ok {
		t.Error("state key must not be seeded into a new profile")
	}

	// Edit the seeded shared key, then prepare again: it must not be rewritten.
	testutil.WriteFile(t, target, `{"sharedKey":"edited","ownedA":"o"}`)
	if _, err := layer.PrepareConfigDir(def, contentProfile(), configDir, false); err != nil {
		t.Fatal(err)
	}
	data2, _ := os.ReadFile(target)
	var m2 map[string]any
	if err := json.Unmarshal(data2, &m2); err != nil {
		t.Fatal(err)
	}
	if m2["sharedKey"] != "edited" {
		t.Errorf("seeded shared key was rewritten on a later sync (must seed once): %v", m2["sharedKey"])
	}
}
