package cli

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/testutil"
)

// Missing disabled-state resolution hides the parameters needed to enable a feature.
func TestDisabledFeaturesDiscoverDefaults(t *testing.T) {
	h := testutil.HermeticHome(t)
	for _, tc := range []struct{ harness, protocol string }{{"claude", "http/protobuf"}, {"codex", "otlp-http json"}} {
		testutil.SeedDefaults(t, h, tc.harness)
		args := []string{"add", tc.harness, "work", "--auth-kind", "oauth-login", "--share", "content"}
		if tc.harness == "codex" {
			args = []string{"add", "codex", "work", "--auth-kind", "api-key", "--auth-key", "file:/k", "--share", "content"}
		}
		if _, err := runCLI(t, args...); err != nil {
			t.Fatal(err)
		}
		for _, asJSON := range []bool{false, true} {
			args = []string{"features", tc.harness + "/work"}
			if asJSON {
				args = append(args, "--json")
			}
			out, err := runCLI(t, args...)
			if err != nil {
				t.Fatal(err)
			}
			if !asJSON {
				for _, want := range []string{"traces:", "logs:", "endpoint:", tc.protocol} {
					if !strings.Contains(out, want) {
						t.Errorf("disabled %s hides %q: %s", tc.harness, want, out)
					}
				}
				continue
			}
			var status struct {
				Groups []struct {
					Features []featureJSON `json:"features"`
				} `json:"groups"`
			}
			if err := json.Unmarshal([]byte(out), &status); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, g := range status.Groups {
				for _, f := range g.Features {
					if f.ID == "telemetry" {
						found = true
						want := map[string]string{"traces": "off", "logs": "off", "endpoint": "", "protocol": tc.protocol}
						if f.Enabled || !reflect.DeepEqual(f.Params, want) {
							t.Errorf("disabled %s = %+v, want defaults %v", tc.harness, f, want)
						}
					}
				}
			}
			if !found {
				t.Fatal("missing telemetry")
			}
		}
	}
}

// A catalog-valid protocol may still be invalid on this harness. Reporting must
// expose its remedy, and sync must preserve the previously placed exporters.
func TestHarnessInvalidStoredFeatureIsActionable(t *testing.T) {
	h := testutil.HermeticHome(t)
	testutil.SeedDefaults(t, h, "codex")
	if out, err := runCLI(t, "add", "codex", "work", "--auth-kind", "api-key", "--auth-key", "file:/k", "--share", "content"); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if _, err := runCLI(t, "features", "codex/work", "--traces", "on", "--endpoint", "https://e"); err != nil {
		t.Fatal(err)
	}
	if out, err := runCLI(t, "sync"); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	paths, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	settings := paths.ProfileConfigDir("codex", "work") + "/config.toml"
	before, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	store := profile.NewStore(paths)
	p, err := store.Load("codex", "work")
	if err != nil {
		t.Fatal(err)
	}
	p.Features[0].Params["protocol"] = "http/protobuf"
	if err := store.Save(p); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"features", "codex/work"}, {"features", "codex/work", "--json"}, {"show", "codex/work"}, {"show", "codex/work", "--json"}, {"sync"}} {
		out, err := runCLI(t, args...)
		if err != nil {
			out += "\n" + err.Error()
		}
		for _, want := range []string{"http/protobuf", "codex", "aprofile features codex/work"} {
			if !strings.Contains(out, want) {
				t.Errorf("%v does not report %q: %s", args, want, out)
			}
		}
	}
	after, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("sync changed settings for invalid stored feature:\nbefore %s\nafter %s", before, after)
	}
}
