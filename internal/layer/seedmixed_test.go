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

func decodeObject(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("not a JSON object: %v\n%s", err, data)
	}
	return m
}

// SeedMixed is the single source of a new profile's mixed-file content: the
// default setup's file without credentials and state keys at all and content,
// a minimal document at none or when the default setup has none (FR-011, FR-013).
func TestSeedMixedByLevel(t *testing.T) {
	h := testutil.HermeticHome(t)
	defaultDir := filepath.Join(h.Root, "default")
	testutil.WriteFile(t, filepath.Join(defaultDir, "config.json"),
		`{"ownedA":"o","credKey":"c","stateKey":"st","sharedKey":"seed","vendorToken":"t","theme":"dark"}`)
	rule := mixedRule()
	def := &harness.Definition{Name: "fake", DefaultConfigLocation: defaultDir, Classification: []harness.EntryRule{*rule}}

	for _, lv := range []profile.Share{profile.ShareAll, profile.ShareContent} {
		s := layer.SeedMixed(def, rule, lv, h.Root)
		m := decodeObject(t, s.Data)
		want := map[string]any{"ownedA": "o", "sharedKey": "seed", "theme": "dark"}
		if !reflect.DeepEqual(m, want) {
			t.Errorf("%s: seed = %v, want %v", lv, m, want)
		}
		if s.Source != filepath.Join(defaultDir, "config.json") {
			t.Errorf("%s: source = %q", lv, s.Source)
		}
	}

	none := layer.SeedMixed(def, rule, profile.ShareNone, h.Root)
	if string(none.Data) != "{}\n" || none.Source != "" {
		t.Errorf("none: seed = %q from %q, want a minimal document", none.Data, none.Source)
	}

	missing := &harness.Definition{Name: "fake", DefaultConfigLocation: filepath.Join(h.Root, "absent"), Classification: []harness.EntryRule{*rule}}
	if s := layer.SeedMixed(missing, rule, profile.ShareContent, h.Root); string(s.Data) != "{}\n" || s.Source != "" {
		t.Errorf("no default file: seed = %q from %q, want a minimal document", s.Data, s.Source)
	}
}

// A non-JSON mixed file cannot be filtered key by key, so seeding never copies the
// default setup's file verbatim (it may hold credential keys): it starts minimal.
func TestSeedMixedNonJSONNeverCopiesRawData(t *testing.T) {
	h := testutil.HermeticHome(t)
	defaultDir := filepath.Join(h.Root, "default")
	testutil.WriteFile(t, filepath.Join(defaultDir, "config.toml"), "api_key = \"CANARY-SECRET\"\nmodel = \"x\"\n")
	rule := &harness.EntryRule{Entry: "config.toml", Layer: harness.LayerMixed, Keys: map[string]harness.Layer{"api_key": harness.LayerCredentials}}
	def := &harness.Definition{Name: "fake", DefaultConfigLocation: defaultDir, Classification: []harness.EntryRule{*rule}}

	s := layer.SeedMixed(def, rule, profile.ShareContent, h.Root)
	if strings.Contains(string(s.Data), "CANARY-SECRET") {
		t.Fatalf("non-JSON seed copied the default file verbatim: %q", s.Data)
	}

	// The planner uses the same helper.
	configDir := filepath.Join(h.DataHome, "cfg")
	if _, err := layer.PrepareConfigDir(def, contentProfile(), configDir, false); err != nil {
		t.Fatal(err)
	}
	if got := readFileString(t, filepath.Join(configDir, "config.toml")); strings.Contains(got, "CANARY-SECRET") {
		t.Errorf("sync seeded a non-JSON mixed file verbatim: %q", got)
	}
}

// MergeMixedKeys copies exactly the keys of the named layers and reports them;
// every other key of dst stays as it was.
func TestMergeMixedKeysSelectedLayers(t *testing.T) {
	rule := mixedRule()
	dst := []byte(`{"ownedA":"old","credKey":"keep","stateKey":"s0","unknown":[1]}`)
	src := []byte(`{"ownedA":"new","ownedB":"b","credKey":"src-cred","stateKey":"s1","sharedKey":"x","unknown":[2]}`)

	out, keys, err := layer.MergeMixedKeys(rule, dst, src, ".json", harness.LayerOwned, harness.LayerCredentials)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"ownedA": "new", "ownedB": "b", "credKey": "src-cred", "stateKey": "s0", "unknown": []any{1.0}}
	if m := decodeObject(t, out); !reflect.DeepEqual(m, want) {
		t.Errorf("merged = %v, want %v", m, want)
	}
	if !reflect.DeepEqual(keys, []string{"credKey", "ownedA", "ownedB"}) {
		t.Errorf("copied keys = %v", keys)
	}

	// No layers: nothing copied, but both documents are still validated.
	if _, keys, err := layer.MergeMixedKeys(rule, dst, src, ".json"); err != nil || len(keys) != 0 {
		t.Errorf("no layers: keys %v err %v", keys, err)
	}
	if _, _, err := layer.MergeMixedKeys(rule, dst, []byte("{not json"), ".json", harness.LayerOwned); err == nil {
		t.Error("a malformed source must be an error")
	}
	if _, _, err := layer.MergeMixedKeys(rule, dst, src, ".toml", harness.LayerOwned); err == nil {
		t.Error("a non-JSON file must be an error")
	}
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
