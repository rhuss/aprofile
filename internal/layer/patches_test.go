package layer_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/layer"
)

// patchDef loads a data-only definition with the given config_patches YAML block,
// so the patch values have the types the real loader produces.
func patchDef(t *testing.T, patches string) *harness.Definition {
	t.Helper()
	src := "name: pt\nbinary: pt\nwrapper_prefix: pt\nconfig_env:\n  PT_DIR: \".\"\nconfig_patches:\n" + patches
	set := harness.LoadFromBytes(map[string][]byte{"pt.yaml": []byte(src)})
	d, ok := set.Get("pt")
	if !ok {
		t.Fatalf("load: %v", set.Errors())
	}
	return d
}

func writeMode(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	d := json.NewDecoder(strings.NewReader(string(data)))
	d.UseNumber()
	if err := d.Decode(&m); err != nil {
		t.Fatalf("decode %s: %v\n%s", path, err, data)
	}
	return m
}

// T071 (FR-019): a JSON patch merges only the patched keys, deep, keeps every
// other key (also inside a patched object), keeps the file mode, and is idempotent.
func TestConfigPatchJSONMergeOnly(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "settings.json")
	writeMode(t, file, `{"theme":"dark","big":12345678901234567890,"env":{"KEEP":"1","SET":"old"},"permissions":{"allow":["Bash"]}}`, 0o640)
	def := patchDef(t, "  - file: settings.json\n    merge:\n      env: { SET: new, ADDED: \"2\" }\n      autoUpdates: false\n")

	out := layer.ApplyConfigPatches(def, dir, false)
	if len(out) != 1 || out[0].Err != nil || !out[0].Changed || out[0].Skipped != "" {
		t.Fatalf("outcome: %+v", out)
	}
	if want := []string{"autoUpdates", "env.ADDED", "env.SET"}; !reflect.DeepEqual(out[0].Keys, want) {
		t.Errorf("keys = %v, want %v", out[0].Keys, want)
	}
	m := readJSON(t, file)
	env := m["env"].(map[string]any)
	if env["KEEP"] != "1" || env["SET"] != "new" || env["ADDED"] != "2" || m["autoUpdates"] != false {
		t.Errorf("merged content wrong: %v", m)
	}
	if m["theme"] != "dark" || m["permissions"] == nil {
		t.Errorf("unknown keys must stay: %v", m)
	}
	if n, _ := m["big"].(json.Number); n.String() != "12345678901234567890" {
		t.Errorf("an untouched number must keep its exact value: %v", m["big"])
	}
	if fi, _ := os.Stat(file); fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, want 0640", fi.Mode().Perm())
	}

	// Idempotent: a second run changes nothing and does not rewrite the file.
	before, _ := os.ReadFile(file)
	fiBefore, _ := os.Stat(file)
	again := layer.ApplyConfigPatches(def, dir, false)
	if again[0].Changed || len(again[0].Keys) != 0 || again[0].Err != nil {
		t.Errorf("second run must be a no-op: %+v", again[0])
	}
	after, _ := os.ReadFile(file)
	fiAfter, _ := os.Stat(file)
	if string(before) != string(after) || !fiBefore.ModTime().Equal(fiAfter.ModTime()) {
		t.Error("an unchanged patch must not rewrite the file")
	}
}

func TestConfigPatchTOMLMergeOnly(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.toml")
	writeMode(t, file, "model = \"o3\"\n\n[features]\nkeep = true\nset = 1\n\n[mcp_servers.x]\ncommand = \"x\"\n", 0o600)
	def := patchDef(t, "  - file: config.toml\n    merge:\n      features: { set: 2, added: \"yes\" }\n      check_for_update_on_startup: false\n")

	out := layer.ApplyConfigPatches(def, dir, false)
	if out[0].Err != nil || !out[0].Changed {
		t.Fatalf("outcome: %+v", out[0])
	}
	data, _ := os.ReadFile(file)
	var m map[string]any
	if err := toml.Unmarshal(data, &m); err != nil {
		t.Fatalf("decode: %v\n%s", err, data)
	}
	feat := m["features"].(map[string]any)
	if feat["keep"] != true || feat["set"] != int64(2) || feat["added"] != "yes" || m["check_for_update_on_startup"] != false {
		t.Errorf("merged content wrong: %v", m)
	}
	if m["model"] != "o3" || m["mcp_servers"] == nil {
		t.Errorf("unknown keys must stay: %v", m)
	}
	if fi, _ := os.Stat(file); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", fi.Mode().Perm())
	}
	if again := layer.ApplyConfigPatches(def, dir, false); again[0].Changed {
		t.Errorf("second run must be a no-op: %+v", again[0])
	}
}

// A dry run reports the change without writing.
func TestConfigPatchDryRun(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "settings.json")
	writeMode(t, file, `{"a":1}`, 0o644)
	def := patchDef(t, "  - { file: settings.json, merge: { b: 2 } }\n")
	out := layer.ApplyConfigPatches(def, dir, true)
	if !out[0].Changed || len(out[0].Keys) != 1 {
		t.Errorf("dry run must report the change: %+v", out[0])
	}
	if data, _ := os.ReadFile(file); string(data) != `{"a":1}` {
		t.Errorf("dry run wrote the file: %s", data)
	}
}

// Patches merge into existing files only; a link (a shared entry pointing into the
// default setup) is never written through, and an unparseable file is an error that
// leaves the file alone.
func TestConfigPatchSkipsAbsentLinkedAndInvalid(t *testing.T) {
	dir := t.TempDir()
	defaults := t.TempDir()
	target := filepath.Join(defaults, "settings.json")
	writeMode(t, target, `{"a":1}`, 0o644)
	if err := os.Symlink(target, filepath.Join(dir, "settings.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(defaults, filepath.Join(dir, "linkeddir")); err != nil {
		t.Fatal(err)
	}
	writeMode(t, filepath.Join(dir, "broken.json"), `{not json`, 0o644)
	def := patchDef(t, "  - { file: settings.json, merge: { b: 2 } }\n"+
		"  - { file: linkeddir/settings.json, merge: { b: 2 } }\n"+
		"  - { file: absent.json, merge: { b: 2 } }\n"+
		"  - { file: broken.json, merge: { b: 2 } }\n")

	out := layer.ApplyConfigPatches(def, dir, false)
	if len(out) != 4 {
		t.Fatalf("want 4 outcomes: %+v", out)
	}
	for i, wantSkip := range []string{"link", "link", "does not exist", ""} {
		if wantSkip != "" && (!strings.Contains(out[i].Skipped, wantSkip) || out[i].Changed || out[i].Err != nil) {
			t.Errorf("patch %d: want skipped (%s), got %+v", i, wantSkip, out[i])
		}
	}
	if out[3].Err == nil || out[3].Changed {
		t.Errorf("an unparseable file must be an error: %+v", out[3])
	}
	if data, _ := os.ReadFile(target); string(data) != `{"a":1}` {
		t.Errorf("the default setup's file was modified: %s", data)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "broken.json")); string(data) != `{not json` {
		t.Errorf("an unparseable file must stay as it is: %s", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "absent.json")); !os.IsNotExist(err) {
		t.Error("a patch must not create an absent file")
	}
}

// Two patches of one file apply in order and the file is written once.
func TestConfigPatchSameFileTwice(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "settings.json")
	writeMode(t, file, `{"a":{"x":1}}`, 0o644)
	def := patchDef(t, "  - { file: settings.json, merge: { a: { y: 2 } } }\n  - { file: settings.json, merge: { a: { y: 3, z: 4 } } }\n")
	out := layer.ApplyConfigPatches(def, dir, true)
	if !out[0].Changed || !out[1].Changed {
		t.Errorf("both patches change the file: %+v", out)
	}
	out = layer.ApplyConfigPatches(def, dir, false)
	m := readJSON(t, file)
	a := m["a"].(map[string]any)
	if a["x"] != json.Number("1") || a["y"] != json.Number("3") || a["z"] != json.Number("4") {
		t.Errorf("content = %v", m)
	}
	if again := layer.ApplyConfigPatches(def, dir, false); again[0].Changed || again[1].Changed {
		t.Errorf("the pair must be idempotent: %+v", again)
	}
}
