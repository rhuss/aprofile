package launch_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/launch"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/secret"
	"github.com/cc-deck/aprofile/internal/testutil"
)

type applyEnv struct {
	cfg, rec string
}

func newApplyEnv(t *testing.T) applyEnv {
	h := testutil.HermeticHome(t)
	cfg := filepath.Join(h.DataHome, "aprofile", "profiles", "claude", "work")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	return applyEnv{cfg: cfg, rec: filepath.Join(h.DataHome, "aprofile", "managed-launch", "claude", "work.json")}
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return m
}

func TestApplyWritesKeepsModeAndUserValues(t *testing.T) {
	e := newApplyEnv(t)
	settings := filepath.Join(e.cfg, "settings.json")
	orig := "{\"permissions\": {\"allow\": [\"Bash(a && b > c)\"]}, \"model\": \"opus\"}"
	if err := os.WriteFile(settings, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := launch.Apply(claudeProfile(t), builtin(t, "claude"), e.cfg, e.rec, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) == 0 {
		t.Fatal("no steps")
	}
	fi, _ := os.Stat(settings)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
	data, _ := os.ReadFile(settings)
	if !strings.Contains(string(data), `"Bash(a && b > c)"`) {
		t.Errorf("user value not kept verbatim:\n%s", data)
	}
	m := readJSON(t, settings)
	if m["model"] != "opus" {
		t.Error("model key changed")
	}
	if env := m["env"].(map[string]any); env["ANTHROPIC_MODEL"] != "gw-model" {
		t.Errorf("env = %v", env)
	}
	if rec, _ := launch.Load(e.rec); !rec.Has("settings.json", "apiKeyHelper") {
		t.Errorf("record not saved: %+v", rec)
	}
	// No temp files left in the config dir.
	ents, _ := os.ReadDir(e.cfg)
	if len(ents) != 1 {
		t.Errorf("config dir holds %d entries", len(ents))
	}

	// Second Apply is a no-op that writes nothing.
	st1, _ := os.Stat(settings)
	rt1, _ := os.Stat(e.rec)
	time.Sleep(15 * time.Millisecond)
	res, err = launch.Apply(claudeProfile(t), builtin(t, "claude"), e.cfg, e.rec, false)
	if err != nil || len(res.Steps) != 0 {
		t.Fatalf("second apply: %+v %v", res.Steps, err)
	}
	st2, _ := os.Stat(settings)
	rt2, _ := os.Stat(e.rec)
	if !st1.ModTime().Equal(st2.ModTime()) || !rt1.ModTime().Equal(rt2.ModTime()) {
		t.Error("second apply rewrote a file")
	}
}

func TestApplyCreatesAbsentFile(t *testing.T) {
	e := newApplyEnv(t)
	if _, err := launch.Apply(claudeProfile(t), builtin(t, "claude"), e.cfg, e.rec, false); err != nil {
		t.Fatal(err)
	}
	m := readJSON(t, filepath.Join(e.cfg, "settings.json"))
	if len(m) != 2 {
		t.Errorf("created file holds %v", m)
	}
}

func TestApplyKeepsHarnessChangeBetweenPlanAndWrite(t *testing.T) {
	e := newApplyEnv(t)
	settings := filepath.Join(e.cfg, "settings.json")
	if err := os.WriteFile(settings, []byte(`{"model":"opus"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := launch.SetBeforeWrite(func() {
		// The harness writes /model and a permission while sync plans.
		_ = os.WriteFile(settings, []byte(`{"model":"haiku","permissions":{"allow":["x"]}}`), 0o644)
	})
	defer restore()
	if _, err := launch.Apply(claudeProfile(t), builtin(t, "claude"), e.cfg, e.rec, false); err != nil {
		t.Fatal(err)
	}
	m := readJSON(t, settings)
	if m["model"] != "haiku" || m["permissions"] == nil {
		t.Errorf("the harness's change was lost: %v", m)
	}
	if env, _ := m["env"].(map[string]any); env["ANTHROPIC_MODEL"] != "gw-model" {
		t.Errorf("launch entries missing: %v", m)
	}
}

func TestApplyNeverWritesThroughLink(t *testing.T) {
	e := newApplyEnv(t)
	def := filepath.Join(filepath.Dir(e.cfg), "default-settings.json")
	content := `{"theme":"dark"}`
	if err := os.WriteFile(def, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(def, filepath.Join(e.cfg, "settings.json")); err != nil {
		t.Fatal(err)
	}
	p := claudeProfile(t)
	p.Share = profile.ShareAll
	res, err := launch.Apply(p, builtin(t, "claude"), e.cfg, e.rec, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) != 0 {
		t.Errorf("steps for a linked file: %+v", res.Steps)
	}
	if data, _ := os.ReadFile(def); string(data) != content {
		t.Errorf("default setup's file changed: %s", data)
	}
	if _, err := os.Stat(e.rec); !os.IsNotExist(err) {
		t.Error("a record was written for a linked file")
	}
}

func TestApplyDryRunWritesNothing(t *testing.T) {
	e := newApplyEnv(t)
	cfgToml := filepath.Join(e.cfg, "config.toml")
	src := "# my comment\n" + codexTOML
	if err := os.WriteFile(cfgToml, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := launch.Apply(codexProfile(t), builtin(t, "codex"), e.cfg, e.rec, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) != 1 || len(res.Reencoded) != 1 {
		t.Errorf("dry run: steps %+v reencoded %v", res.Steps, res.Reencoded)
	}
	if data, _ := os.ReadFile(cfgToml); string(data) != src {
		t.Error("dry run wrote config.toml")
	}
	if _, err := os.Stat(e.rec); !os.IsNotExist(err) {
		t.Error("dry run wrote the record")
	}
	// Applying re-encodes the TOML file (comments dropped) and is idempotent after.
	if _, err := launch.Apply(codexProfile(t), builtin(t, "codex"), e.cfg, e.rec, false); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cfgToml)
	if strings.Contains(string(data), "my comment") || !strings.Contains(string(data), "auth") {
		t.Errorf("config.toml after apply:\n%s", data)
	}
	res, err = launch.Apply(codexProfile(t), builtin(t, "codex"), e.cfg, e.rec, false)
	if err != nil || len(res.Steps) != 0 || len(res.Reencoded) != 0 {
		t.Errorf("second codex apply: %+v %v %v", res.Steps, res.Reencoded, err)
	}
}

func TestApplyParseErrorLeavesFile(t *testing.T) {
	e := newApplyEnv(t)
	settings := filepath.Join(e.cfg, "settings.json")
	if err := os.WriteFile(settings, []byte(`{broken`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := launch.Apply(claudeProfile(t), builtin(t, "claude"), e.cfg, e.rec, false)
	if err == nil || !strings.Contains(err.Error(), "settings.json") {
		t.Fatalf("want an error naming settings.json, got %v", err)
	}
	if data, _ := os.ReadFile(settings); string(data) != `{broken` {
		t.Error("broken file changed")
	}
}

func TestStatus(t *testing.T) {
	e := newApplyEnv(t)
	st, err := launch.Status(claudeProfile(t), builtin(t, "claude"), e.cfg, e.rec)
	if err != nil || st.Sync != "stale" {
		t.Fatalf("before sync: %+v %v", st.Sync, err)
	}
	if _, err := launch.Apply(claudeProfile(t), builtin(t, "claude"), e.cfg, e.rec, false); err != nil {
		t.Fatal(err)
	}
	st, _ = launch.Status(claudeProfile(t), builtin(t, "claude"), e.cfg, e.rec)
	if st.Sync != "synced" || len(st.Settings) == 0 {
		t.Errorf("after sync: %+v", st)
	}
	settings := filepath.Join(e.cfg, "settings.json")
	m := readJSON(t, settings)
	m["env"].(map[string]any)["ANTHROPIC_MODEL"] = "x"
	data, _ := json.Marshal(m)
	_ = os.WriteFile(settings, data, 0o644)
	if st, _ = launch.Status(claudeProfile(t), builtin(t, "claude"), e.cfg, e.rec); st.Sync != "stale" {
		t.Errorf("after an edit: %s", st.Sync)
	}
	_ = os.WriteFile(settings, []byte(`{x`), 0o644)
	if st, _ = launch.Status(claudeProfile(t), builtin(t, "claude"), e.cfg, e.rec); st.Sync != "unknown" || st.Err == nil {
		t.Errorf("broken file: %+v", st)
	}
}

// FR-005: when a harnesses.d override drops folder_launch, Apply reads the files
// the record lists and removes exactly the placed entries (review finding: the
// record file was not read, so the entries stayed and the record was dropped).
func TestApplyRemovesEntriesWhenDefinitionDropsFolderLaunch(t *testing.T) {
	e := newApplyEnv(t)
	settings := filepath.Join(e.cfg, "settings.json")
	if err := os.WriteFile(settings, []byte(`{"model":"opus","env":{"MY_TRACE":"1"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	def := builtin(t, "claude")
	if _, err := launch.Apply(claudeProfile(t), def, e.cfg, e.rec, false); err != nil {
		t.Fatal(err)
	}
	dropped := *def
	dropped.FolderLaunch = nil
	res, err := launch.Apply(claudeProfile(t), &dropped, e.cfg, e.rec, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) == 0 {
		t.Fatal("no remove steps")
	}
	m := readJSON(t, settings)
	if _, has := m["apiKeyHelper"]; has {
		t.Error("apiKeyHelper not removed")
	}
	env, _ := m["env"].(map[string]any)
	if len(env) != 1 || env["MY_TRACE"] != "1" {
		t.Errorf("env = %v, want only the user's MY_TRACE", env)
	}
	if m["model"] != "opus" {
		t.Error("model key changed")
	}
	if _, err := os.Stat(e.rec); !os.IsNotExist(err) {
		t.Errorf("record not removed: %v", err)
	}
	st, err := launch.Status(claudeProfile(t), &dropped, e.cfg, e.rec)
	if err != nil || st.Sync != launch.SyncSynced {
		t.Errorf("status after removal = %s %v", st.Sync, err)
	}
}

// FR-004/FR-009: the record is written ahead of the files, so a write that fails
// never loses the replaced env_key; the next Apply completes and keeps it recorded.
func TestApplyRecordsReplacedSettingBeforeWriting(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	e := newApplyEnv(t)
	cfgFile := filepath.Join(e.cfg, "config.toml")
	if err := os.WriteFile(cfgFile, []byte(codexTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(e.cfg, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(e.cfg, 0o755) })
	if _, err := launch.Apply(codexProfile(t), builtin(t, "codex"), e.cfg, e.rec, false); err == nil {
		t.Fatal("write into a read-only config dir succeeded")
	}
	if data, _ := os.ReadFile(cfgFile); string(data) != codexTOML {
		t.Fatalf("config.toml changed on a failed write:\n%s", data)
	}
	rec, err := launch.Load(e.rec)
	if err != nil {
		t.Fatal(err)
	}
	ent, ok := rec.Get("config.toml", "model_providers.enmaas.auth")
	if !ok || ent.Replaced["env_key"] == "" {
		t.Fatalf("record not written ahead with the replaced env_key: %+v", rec)
	}
	if err := os.Chmod(e.cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := launch.Apply(codexProfile(t), builtin(t, "codex"), e.cfg, e.rec, false); err != nil {
		t.Fatal(err)
	}
	rec, _ = launch.Load(e.rec)
	if ent, ok := rec.Get("config.toml", "model_providers.enmaas.auth"); !ok || ent.Replaced["env_key"] == "" {
		t.Errorf("replaced env_key lost after the retry: %+v", rec)
	}
	data, _ := os.ReadFile(cfgFile)
	blk := provider(decodeTOML(t, data), "enmaas")
	if _, has := blk["env_key"]; has || blk["auth"] == nil {
		t.Errorf("provider block after retry = %v", blk)
	}
}

// FR-011: a profile whose extra variable has the name of the model variable
// supplies that variable once (the extra wins), so a second Apply writes nothing
// (review finding: the two values alternated on every sync).
func TestApplyExtraNamedLikeModelVarIsIdempotent(t *testing.T) {
	e := newApplyEnv(t)
	p := claudeProfile(t)
	p.ExtraEnv = append(p.ExtraEnv, profile.ExtraEnvVar{Name: "ANTHROPIC_MODEL", Value: "extra-model"})
	n := 0
	for _, s := range launch.Decide(p, builtin(t, "claude"), launch.Doc{}) {
		if s.Variable == "ANTHROPIC_MODEL" {
			n++
			if s.Value != "extra-model" {
				t.Errorf("ANTHROPIC_MODEL = %q, want the extra's value", s.Value)
			}
		}
	}
	if n != 1 {
		t.Fatalf("ANTHROPIC_MODEL supplied %d times", n)
	}
	if _, err := launch.Apply(p, builtin(t, "claude"), e.cfg, e.rec, false); err != nil {
		t.Fatal(err)
	}
	res, err := launch.Apply(p, builtin(t, "claude"), e.cfg, e.rec, false)
	if err != nil || len(res.Steps) != 0 {
		t.Fatalf("second apply: %+v %v", res.Steps, err)
	}
	env := readJSON(t, filepath.Join(e.cfg, "settings.json"))["env"].(map[string]any)
	if env["ANTHROPIC_MODEL"] != "extra-model" {
		t.Errorf("env ANTHROPIC_MODEL = %v", env["ANTHROPIC_MODEL"])
	}
	if sv, _ := env["APROFILE_SET_VARS"].(string); strings.Count(" "+sv+" ", " ANTHROPIC_MODEL ") != 1 {
		t.Errorf("APROFILE_SET_VARS = %q", sv)
	}
}

// Apply and Status call the exec hook's env operation even when the folder carries
// no launch variable, so a key read from a hook variable gets the carrier the
// wrapper gives it (wrapper, cleared source) and no key command is placed (FR-012).
func TestApplyStatusUseHookEnvForTheKey(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\ncat >/dev/null\necho HOOK_KEY=from-hook\n"
	if err := os.WriteFile(filepath.Join(dir, "aprofile-harness-claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	e := newApplyEnv(t) // sets a hermetic PATH, so the hook dir is added after it
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/usr/bin:/bin")
	def := *builtin(t, "claude")
	def.ExecHook = &harness.ExecHook{Operations: []string{"env"}}
	p := &profile.Profile{Harness: "claude", Name: "work", AuthKind: "api-key", Share: profile.ShareContent,
		AuthRefs: map[string]secret.Reference{"ANTHROPIC_API_KEY": ref(t, "env:HOOK_KEY")}}
	st, err := launch.Status(p, &def, e.cfg, e.rec)
	if err != nil || st.Err != nil {
		t.Fatalf("status: %v %v", err, st.Err)
	}
	expect(t, st.Settings, launch.KindKey, launch.CarrierWrapper, launch.ReasonClearedSource)
	if _, err := launch.Apply(p, &def, e.cfg, e.rec, false); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(e.cfg, "settings.json")); err == nil && strings.Contains(string(data), "apiKeyHelper") {
		t.Errorf("the folder got a key command for a wrapper-carried key:\n%s", data)
	}
}

// The write-ahead record never claims a user's own entry: when the write fails, a
// user value the profile would have replaced stays the user's, and a later sync
// that no longer sets the variable leaves it alone (FR-004, FR-005).
func TestApplyFailedWriteKeepsUserEntryUnowned(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	e := newApplyEnv(t)
	settings := filepath.Join(e.cfg, "settings.json")
	if err := os.WriteFile(settings, []byte(`{"env":{"MY_VAR":"users-own"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	p := claudeProfile(t)
	p.ExtraEnv = append(p.ExtraEnv, profile.ExtraEnvVar{Name: "MY_VAR", Value: "profile"})
	if err := os.Chmod(e.cfg, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(e.cfg, 0o755) })
	if _, err := launch.Apply(p, builtin(t, "claude"), e.cfg, e.rec, false); err == nil {
		t.Fatal("write into a read-only config dir succeeded")
	}
	if rec, _ := launch.Load(e.rec); rec.Has("settings.json", "env.MY_VAR") {
		t.Fatalf("the write-ahead record claims the user's MY_VAR: %+v", rec)
	}
	if err := os.Chmod(e.cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	p.ExtraEnv = p.ExtraEnv[:len(p.ExtraEnv)-1]
	if _, err := launch.Apply(p, builtin(t, "claude"), e.cfg, e.rec, false); err != nil {
		t.Fatal(err)
	}
	if env := readJSON(t, settings)["env"].(map[string]any); env["MY_VAR"] != "users-own" {
		t.Errorf("the user's MY_VAR was removed or changed: %v", env)
	}
}

// Spec 005 FR-005a: a login profile whose folder holds only neutralizing entries is
// self-contained, with no wrapper-only items, and synced after Apply.
func TestStatusLoginNeutralizingOnlyIsSelfContained(t *testing.T) {
	e := newApplyEnv(t)
	p := &profile.Profile{Harness: "claude", Name: "work", AuthKind: "oauth-login", Share: profile.ShareContent}
	d := builtin(t, "claude")
	if _, err := launch.Apply(p, d, e.cfg, e.rec, false); err != nil {
		t.Fatal(err)
	}
	env, _ := readJSON(t, filepath.Join(e.cfg, "settings.json"))["env"].(map[string]any)
	if got, ok := env["CLAUDE_CODE_USE_VERTEX"]; !ok || got != "" {
		t.Errorf("env.CLAUDE_CODE_USE_VERTEX = %v (present %v), want empty", got, ok)
	}
	st, err := launch.Status(p, d, e.cfg, e.rec)
	if err != nil {
		t.Fatal(err)
	}
	f := st.Folder()
	if f.Status != launch.FolderSelfContained || len(f.WrapperOnly) != 0 || f.Sync != launch.SyncSynced {
		t.Errorf("folder = %+v", f)
	}
}
