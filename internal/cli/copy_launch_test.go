package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/testutil"
	"github.com/pelletier/go-toml/v2"
)

// Copying settings must carry the user's pre-feature values, rather than turn
// the source's managed values into originals in the target's recovery record.
func TestCopySyncedFeaturesRestoreUserSettings(t *testing.T) {
	for _, tc := range []struct{ name, harness, feature, file, original string }{
		{"claude automode absent", "claude", "automode-log", "settings.json", `{"env":{"KEEP":"user"}}`},
		{"claude automode original", "claude", "automode-log", "settings.json", `{"env":{"KEEP":"user","AUTOMODE_DECISION_LOG":"original"}}`},
		{"claude telemetry absent", "claude", "telemetry", "settings.json", `{"env":{"KEEP":"user"}}`},
		{"claude telemetry original", "claude", "telemetry", "settings.json", `{"env":{"KEEP":"user","CLAUDE_CODE_ENABLE_TELEMETRY":"original","OTEL_EXPORTER_OTLP_ENDPOINT":"https://original.example"}}`},
		{"codex telemetry absent", "codex", "telemetry", "config.toml", "model = \"user\"\n"},
		{"codex telemetry original", "codex", "telemetry", "config.toml", "model = \"user\"\n[otel]\nexporter = { otlp-http = { endpoint = \"https://original.example\", headers = { Authorization = \"fixture-original\" } } }\n"},
	} {
		for _, overwrite := range []bool{false, true} {
			name := tc.name
			if overwrite {
				name += " overwrite"
			}
			t.Run(name, func(t *testing.T) {
				testutil.HermeticHome(t)
				paths, err := config.ResolvePaths()
				if err != nil {
					t.Fatal(err)
				}
				add := func(name string) {
					args := []string{"add", tc.harness, name, "--share", "content"}
					if tc.harness == "claude" {
						args = append(args, "--auth-kind", "oauth-login")
					}
					mustCLI(t, args...)
				}
				enable := func(name string) {
					args := []string{"features", tc.harness + "/" + name, "--enable", tc.feature}
					if tc.feature == "telemetry" {
						args = append(args, "--traces", "on", "--logs", "on", "--endpoint", "https://managed.example")
					}
					mustCLI(t, args...)
				}
				add("src")
				srcFile := filepath.Join(paths.ProfileConfigDir(tc.harness, "src"), tc.file)
				putFile(t, srcFile, tc.original)
				enable("src")
				mustCLI(t, "sync")
				sourceBefore, err := os.ReadFile(srcFile)
				if err != nil {
					t.Fatal(err)
				}
				dstFile := filepath.Join(paths.ProfileConfigDir(tc.harness, "dst"), tc.file)
				args := []string{"copy", tc.harness + "/src", tc.harness + "/dst"}
				if overwrite {
					add("dst")
					if tc.harness == "claude" {
						putFile(t, dstFile, `{"env":{"AUTOMODE_DECISION_LOG":"unrelated-target","CLAUDE_CODE_ENABLE_TELEMETRY":"unrelated-target"}}`)
					} else {
						putFile(t, dstFile, "[otel]\nexporter = \"unrelated-target\"\n")
					}
					enable("dst")
					mustCLI(t, "sync")
					// An absent target settings file is copied too; its old recovery record
					// must not restore unrelated target values over the source's originals.
					if tc.harness == "claude" {
						if err := os.Remove(dstFile); err != nil {
							t.Fatal(err)
						}
					}
					args = append(args, "--overwrite", "--yes")
				}
				mustCLI(t, args...)
				mustCLI(t, "sync")
				mustCLI(t, "features", tc.harness+"/dst", "--disable", tc.feature)
				mustCLI(t, "sync")
				got, err := os.ReadFile(dstFile)
				if err != nil {
					t.Fatal(err)
				}
				decode := func(data []byte) map[string]any {
					var doc map[string]any
					var err error
					if tc.harness == "codex" {
						err = toml.Unmarshal(data, &doc)
					} else {
						err = json.Unmarshal(data, &doc)
					}
					if err != nil {
						t.Fatal(err)
					}
					return doc
				}
				actual, want := decode(got), decode([]byte(tc.original))
				if tc.harness == "claude" {
					// Normal sync also places auth neutralization entries independently of
					// these features; only compare the feature variables and user sentinel.
					a := actual["env"].(map[string]any)
					w := want["env"].(map[string]any)
					for _, key := range []string{"KEEP", "AUTOMODE_DECISION_LOG", "CLAUDE_CODE_ENABLE_TELEMETRY", "OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_TRACES_EXPORTER", "OTEL_LOGS_EXPORTER", "OTEL_EXPORTER_OTLP_PROTOCOL"} {
						if !reflect.DeepEqual(a[key], w[key]) {
							t.Errorf("%s = %#v, want %#v; settings: %s", key, a[key], w[key], got)
						}
					}
				} else {
					if !reflect.DeepEqual(actual["otel"], want["otel"]) {
						t.Errorf("otel = %#v, want %#v; settings: %s", actual["otel"], want["otel"], got)
					}
					if actual["model"] != "user" {
						t.Errorf("user model lost: %s", got)
					}
				}
				sourceAfter, err := os.ReadFile(srcFile)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(sourceBefore, sourceAfter) {
					t.Error("copy changed source settings")
				}
			})
		}
	}
}

// Missing source files and existing target settings are not replaced by copy;
// their target recovery values must survive even when feature state is carried.
func TestCopyKeepsRecoveryForUntouchedTargetFiles(t *testing.T) {
	for _, harness := range []string{"claude", "codex"} {
		t.Run(harness, func(t *testing.T) {
			testutil.HermeticHome(t)
			paths, err := config.ResolvePaths()
			if err != nil {
				t.Fatal(err)
			}
			file := "settings.json"
			if harness == "codex" {
				file = "config.toml"
			}
			for _, name := range []string{"src", "dst"} {
				args := []string{"add", harness, name, "--share", "content"}
				if harness == "claude" {
					args = append(args, "--auth-kind", "oauth-login")
				}
				mustCLI(t, args...)
				mustCLI(t, "features", harness+"/"+name, "--enable", "telemetry", "--traces", "on", "--endpoint", "https://managed.example")
			}
			dstFile := filepath.Join(paths.ProfileConfigDir(harness, "dst"), file)
			if harness == "claude" {
				putFile(t, dstFile, `{"env":{"CLAUDE_CODE_ENABLE_TELEMETRY":"target-original"}}`)
			} else {
				putFile(t, dstFile, "[otel]\ntrace_exporter = 'target-original'\n")
			}
			mustCLI(t, "sync")
			if harness == "codex" {
				if err := os.Remove(filepath.Join(paths.ProfileConfigDir(harness, "src"), file)); err != nil {
					t.Fatal(err)
				}
			}
			mustCLI(t, "copy", harness+"/src", harness+"/dst", "--overwrite", "--yes")
			mustCLI(t, "sync")
			mustCLI(t, "features", harness+"/dst", "--disable", "telemetry")
			mustCLI(t, "sync")
			data, err := os.ReadFile(dstFile)
			if err != nil {
				t.Fatal(err)
			}
			if harness == "claude" {
				var doc struct{ Env map[string]string }
				if err := json.Unmarshal(data, &doc); err != nil {
					t.Fatal(err)
				}
				if doc.Env["CLAUDE_CODE_ENABLE_TELEMETRY"] != "target-original" {
					t.Errorf("untouched target original lost: %s", data)
				}
			} else {
				var doc struct {
					Otel struct {
						TraceExporter string `toml:"trace_exporter"`
					}
				}
				if err := toml.Unmarshal(data, &doc); err != nil {
					t.Fatal(err)
				}
				if doc.Otel.TraceExporter != "target-original" {
					t.Errorf("untouched target original lost: %s", data)
				}
			}
		})
	}
}
