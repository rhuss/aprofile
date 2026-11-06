package hooksets_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/hooksets"
	"github.com/cc-deck/aprofile/internal/layer"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/testutil"
)

func builtin(t *testing.T, name string) *harness.Definition {
	t.Helper()
	set, _ := harness.LoadBuiltins()
	d, ok := set.Get(name)
	if !ok {
		t.Fatalf("no built-in %s", name)
	}
	return d
}

const jiraSet = `name: jira
description: Link Jira keys
bodies:
  claude:
    PostToolUse:
      - matcher: Bash
        hooks: [{ type: command, command: /opt/hooks/jira.sh }]
  opencode:
    files: [jira.ts]
`

const rtkSet = `name: rtk
every_profile: true
bodies:
  claude:
    PreToolUse:
      - matcher: Bash
        hooks: [{ type: command, command: rtk-hook }]
`

func library(t *testing.T, files map[string]string) (string, map[string]*hooksets.Set) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		testutil.WriteFile(t, filepath.Join(dir, name+".yaml"), body)
	}
	lib, errs := hooksets.LoadLibrary(dir)
	if len(errs) != 0 {
		t.Fatalf("library errors: %v", errs)
	}
	return dir, lib
}

// T042: library load and validation.
func TestLibraryValidation(t *testing.T) {
	_, lib := library(t, map[string]string{"jira": jiraSet, "rtk": rtkSet})
	if len(lib) != 2 || !lib["rtk"].EveryProfile || lib["jira"].Description != "Link Jira keys" {
		t.Fatalf("library = %+v", lib)
	}
	claude, opencode, codex := builtin(t, "claude"), builtin(t, "opencode"), builtin(t, "codex")
	if err := hooksets.ValidateFor(lib["jira"], claude); err != nil {
		t.Errorf("jira/claude should be valid: %v", err)
	}
	if err := hooksets.ValidateFor(lib["jira"], opencode); err != nil {
		t.Errorf("jira/opencode should be valid: %v", err)
	}
	if err := hooksets.ValidateFor(lib["jira"], codex); err == nil || !strings.Contains(err.Error(), "no body for harness \"codex\"") {
		t.Errorf("jira has no codex body: %v", err)
	}
	// A body whose shape does not match the form.
	_, bad := library(t, map[string]string{"xx": "name: xx\nbodies:\n  opencode:\n    PreToolUse: [{matcher: a}]\n"})
	if err := hooksets.ValidateFor(bad["xx"], opencode); err == nil {
		t.Error("an event-groups body for a files harness must be rejected")
	}

	dir := t.TempDir()
	testutil.WriteFile(t, filepath.Join(dir, "leak.yaml"), "name: leak\nbodies:\n  claude:\n    Stop: [{hooks: [{type: command, command: \"sk-ant-secret\"}]}]\n")
	testutil.WriteFile(t, filepath.Join(dir, "wrong.yaml"), "name: other\nbodies: {}\n")
	lib2, errs := hooksets.LoadLibrary(dir)
	if len(errs) != 2 {
		t.Errorf("a secret literal and a name mismatch are load errors: %v", errs)
	}
	for name, s := range lib2 {
		if s.LoadErr == nil || hooksets.Applies(s, &profile.Profile{Harness: "claude", Name: "p", Share: profile.ShareContent, HookSets: []string{name}}) {
			t.Errorf("a set that failed to load is kept as a placeholder that never applies: %+v", s)
		}
	}
}

// T042: applies(S, P) truth table from data-model.md.
func TestApplies(t *testing.T) {
	_, lib := library(t, map[string]string{"jira": jiraSet, "rtk": rtkSet})
	cases := []struct {
		share    profile.Share
		named    []string
		excluded []string
		set      string
		harness  string
		want     bool
	}{
		{profile.ShareContent, []string{"jira"}, nil, "jira", "claude", true},
		{profile.ShareContent, nil, nil, "jira", "claude", false},
		{profile.ShareContent, nil, nil, "rtk", "claude", true},
		{profile.ShareContent, nil, []string{"rtk"}, "rtk", "claude", false},
		{profile.ShareNone, nil, nil, "rtk", "claude", false},
		{profile.ShareNone, []string{"rtk"}, nil, "rtk", "claude", true},
		{profile.ShareAll, []string{"jira"}, nil, "jira", "claude", false},
		{profile.ShareAll, nil, nil, "rtk", "claude", false},
		{profile.ShareContent, nil, nil, "rtk", "codex", false},
	}
	for _, c := range cases {
		p := &profile.Profile{Harness: c.harness, Name: "p", Share: c.share, HookSets: c.named, ExcludeHookSets: c.excluded}
		if got := hooksets.Applies(lib[c.set], p); got != c.want {
			t.Errorf("Applies(%s, %s %s named=%v excl=%v) = %v, want %v", c.set, c.harness, c.share, c.named, c.excluded, got, c.want)
		}
	}
}

type hookEnv struct {
	t      *testing.T
	def    *harness.Definition
	dir    string
	lib    map[string]*hooksets.Set
	rec    *hooksets.Record
	libDir string
}

func (h *hookEnv) sync(p *profile.Profile) *hooksets.HookPlan {
	h.t.Helper()
	hp, err := hooksets.PlanHooks(h.def, p, h.lib, h.rec, h.dir, hooksets.PlanOptions{})
	if err != nil {
		h.t.Fatalf("PlanHooks: %v", err)
	}
	if _, _, err := layer.Apply(hp.Steps, layer.ApplyOptions{}); err != nil {
		h.t.Fatalf("Apply: %v", err)
	}
	h.rec = hp.Record
	return hp
}

func (h *hookEnv) hooks() map[string][]map[string]any {
	h.t.Helper()
	var doc struct {
		Hooks map[string][]map[string]any `json:"hooks"`
	}
	data, err := os.ReadFile(filepath.Join(h.dir, "settings.json"))
	if err != nil {
		h.t.Fatal(err)
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		h.t.Fatal(err)
	}
	return doc.Hooks
}

func matchers(groups []map[string]any) string {
	var out []string
	for _, g := range groups {
		out = append(out, g["matcher"].(string))
	}
	return strings.Join(out, ",")
}

// T042/FR-028/SC-008: event-groups place, update and detach managed groups and
// keep every group aprofile did not place, in a deterministic order.
func TestEventGroupsLifecycle(t *testing.T) {
	libDir, lib := library(t, map[string]string{"jira": jiraSet, "rtk": rtkSet})
	h := &hookEnv{t: t, def: builtin(t, "claude"), dir: t.TempDir(), lib: lib, libDir: libDir}
	testutil.WriteFile(t, filepath.Join(h.dir, "settings.json"),
		`{"permissions":{"allow":["x"]},"hooks":{"PreToolUse":[{"matcher":"mine","hooks":[]}]}}`)
	p := &profile.Profile{Harness: "claude", Name: "work", Share: profile.ShareContent, HookSets: []string{"jira"}}

	hp := h.sync(p)
	if len(hp.Steps) != 1 || hp.Steps[0].Kind != layer.StepPlaceHook {
		t.Fatalf("expected one place-hook step, got %+v", hp.Steps)
	}
	got := h.hooks()
	if matchers(got["PreToolUse"]) != "mine,Bash" || matchers(got["PostToolUse"]) != "Bash" {
		t.Errorf("user groups first, then sets alphabetically: %v", got)
	}
	if len(h.rec.Entries) != 2 {
		t.Errorf("record holds both managed groups: %+v", h.rec.Entries)
	}
	data, _ := os.ReadFile(filepath.Join(h.dir, "settings.json"))
	if !strings.Contains(string(data), `"permissions"`) {
		t.Error("other settings keys are kept")
	}
	// Idempotent.
	if hp := h.sync(p); len(hp.Steps) != 0 {
		t.Errorf("second sync changes nothing, got %+v", hp.Steps)
	}

	// A hand-added group survives; detaching jira removes only jira's group.
	doc := map[string]any{}
	_ = json.Unmarshal(data, &doc)
	hooks := doc["hooks"].(map[string]any)
	hooks["PostToolUse"] = append(hooks["PostToolUse"].([]any), map[string]any{"matcher": "hand", "hooks": []any{}})
	b, _ := json.Marshal(doc)
	testutil.WriteFile(t, filepath.Join(h.dir, "settings.json"), string(b))
	p.HookSets = nil
	h.sync(p)
	got = h.hooks()
	if matchers(got["PostToolUse"]) != "hand" {
		t.Errorf("only the managed jira group is removed: %v", got["PostToolUse"])
	}
	if matchers(got["PreToolUse"]) != "mine,Bash" {
		t.Errorf("rtk (every profile) stays: %v", got["PreToolUse"])
	}

	// Updating a set replaces its managed group.
	testutil.WriteFile(t, filepath.Join(libDir, "rtk.yaml"), strings.Replace(rtkSet, "rtk-hook", "rtk-hook --v2", 1))
	h.lib, _ = hooksets.LoadLibrary(libDir)
	h.sync(p)
	got = h.hooks()
	if len(got["PreToolUse"]) != 2 || !strings.Contains(mustJSON(got["PreToolUse"][1]), "--v2") {
		t.Errorf("updated set replaces its group: %v", got["PreToolUse"])
	}

	// A none profile does not get the every-profile set.
	p.Share = profile.ShareNone
	h.sync(p)
	if got := h.hooks(); matchers(got["PreToolUse"]) != "mine" {
		t.Errorf("every-profile sets do not apply at none: %v", got["PreToolUse"])
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// T042/FR-028: a managed group the user edited is left alone, reported and not
// re-added; reapply re-places it.
func TestEventGroupsDiverged(t *testing.T) {
	_, lib := library(t, map[string]string{"jira": jiraSet})
	h := &hookEnv{t: t, def: builtin(t, "claude"), dir: t.TempDir(), lib: lib}
	testutil.WriteFile(t, filepath.Join(h.dir, "settings.json"), `{}`)
	p := &profile.Profile{Harness: "claude", Name: "work", Share: profile.ShareContent, HookSets: []string{"jira"}}
	h.sync(p)
	// Edit the managed group by hand.
	testutil.WriteFile(t, filepath.Join(h.dir, "settings.json"),
		`{"hooks":{"PostToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"/opt/hooks/jira.sh --edited"}]}]}}`)
	hp := h.sync(p)
	if len(hp.Divergences) != 1 || hp.Divergences[0].Set != "jira" {
		t.Fatalf("expected one divergence, got %+v", hp.Divergences)
	}
	got := h.hooks()
	if len(got["PostToolUse"]) != 1 || !strings.Contains(mustJSON(got["PostToolUse"][0]), "--edited") {
		t.Errorf("the edited group is kept and the set's group is not re-added: %v", got)
	}
	// Still diverged on the next sync (the record keeps it).
	if hp := h.sync(p); len(hp.Divergences) != 1 || len(hp.Steps) != 0 {
		t.Errorf("divergence keeps being reported, nothing written: %+v", hp)
	}
	// Reapply replaces the edited copy after confirmation.
	rp, err := hooksets.PlanHooks(h.def, p, h.lib, h.rec, h.dir, hooksets.PlanOptions{Reapply: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(rp.Steps) != 1 || !rp.Steps[0].Confirm {
		t.Fatalf("reapply needs one confirmed step: %+v", rp.Steps)
	}
	if _, _, err := layer.Apply(rp.Steps, layer.ApplyOptions{Yes: true, Backup: func([]string) (string, error) { return "b", nil }}); err != nil {
		t.Fatal(err)
	}
	got = h.hooks()
	if len(got["PostToolUse"]) != 1 || strings.Contains(mustJSON(got["PostToolUse"][0]), "--edited") {
		t.Errorf("reapply re-places the set's current group: %v", got)
	}
}

// T042: a separate hooks file (Codex hooks.json) is created when a set is placed.
func TestEventGroupsCreatesSeparateFile(t *testing.T) {
	_, lib := library(t, map[string]string{"log": "name: log\nbodies:\n  codex:\n    PostToolUse: [{matcher: shell, hooks: []}]\n"})
	h := &hookEnv{t: t, def: builtin(t, "codex"), dir: t.TempDir(), lib: lib}
	p := &profile.Profile{Harness: "codex", Name: "work", Share: profile.ShareNone, HookSets: []string{"log"}}
	h.sync(p)
	data, err := os.ReadFile(filepath.Join(h.dir, "hooks.json"))
	if err != nil || !strings.Contains(string(data), `"shell"`) {
		t.Fatalf("hooks.json should hold the set: %s %v", data, err)
	}
	// No set: nothing is created.
	other := &hookEnv{t: t, def: builtin(t, "codex"), dir: t.TempDir(), lib: lib}
	other.sync(&profile.Profile{Harness: "codex", Name: "x", Share: profile.ShareNone})
	if _, err := os.Stat(filepath.Join(other.dir, "hooks.json")); !os.IsNotExist(err) {
		t.Error("no hooks file is created without a set")
	}
}

// T042/FR-029: the files form links into the library and never touches real files
// or foreign links.
func TestFilesForm(t *testing.T) {
	libDir, lib := library(t, map[string]string{"jira": jiraSet})
	testutil.WriteFile(t, filepath.Join(libDir, "files", "jira", "jira.ts"), "export default {}\n")
	lib, _ = hooksets.LoadLibrary(libDir)
	h := &hookEnv{t: t, def: builtin(t, "opencode"), dir: t.TempDir(), lib: lib}
	plugins := filepath.Join(h.dir, "config", "opencode", "plugins")
	testutil.WriteFile(t, filepath.Join(plugins, "mine.ts"), "mine")
	p := &profile.Profile{Harness: "opencode", Name: "w", Share: profile.ShareContent, HookSets: []string{"jira"}}
	hp := h.sync(p)
	if len(hp.MissingScripts) != 0 {
		t.Errorf("no missing scripts: %v", hp.MissingScripts)
	}
	target, err := os.Readlink(filepath.Join(plugins, "jira.ts"))
	if err != nil || target != filepath.Join(libDir, "files", "jira", "jira.ts") {
		t.Fatalf("jira.ts should link into the library: %q %v", target, err)
	}
	if b, _ := os.ReadFile(filepath.Join(plugins, "mine.ts")); string(b) != "mine" {
		t.Error("the user's own plugin file is untouched")
	}
	// The user replaces the managed link with a real file: diverged, left alone.
	_ = os.Remove(filepath.Join(plugins, "jira.ts"))
	testutil.WriteFile(t, filepath.Join(plugins, "jira.ts"), "my version")
	hp = h.sync(p)
	if len(hp.Divergences) != 1 {
		t.Errorf("real file in place of the link is a divergence: %+v", hp.Divergences)
	}
	if b, _ := os.ReadFile(filepath.Join(plugins, "jira.ts")); string(b) != "my version" {
		t.Error("a real file is never replaced by sync")
	}
	// Detach: only links aprofile placed are removed; a foreign link stays.
	foreign := filepath.Join(t.TempDir(), "elsewhere.ts")
	testutil.WriteFile(t, foreign, "x")
	if err := os.Symlink(foreign, filepath.Join(plugins, "other.ts")); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(filepath.Join(plugins, "jira.ts"))
	h.rec = &hooksets.Record{}
	h.sync(p)
	p.HookSets = nil
	h.sync(p)
	if _, err := os.Lstat(filepath.Join(plugins, "jira.ts")); !os.IsNotExist(err) {
		t.Error("detaching removes the managed link")
	}
	if _, err := os.Stat(filepath.Join(plugins, "mine.ts")); err != nil {
		t.Error("the user's file survives detach")
	}
	if target, err := os.Readlink(filepath.Join(plugins, "other.ts")); err != nil || target != foreign {
		t.Error("a link aprofile did not place is never touched")
	}
}

// Spec Edge Cases: a referenced set deleted from the library leaves the managed
// entries unchanged and is reported.
func TestMissingSetKeepsEntries(t *testing.T) {
	libDir, lib := library(t, map[string]string{"jira": jiraSet})
	h := &hookEnv{t: t, def: builtin(t, "claude"), dir: t.TempDir(), lib: lib}
	testutil.WriteFile(t, filepath.Join(h.dir, "settings.json"), `{}`)
	p := &profile.Profile{Harness: "claude", Name: "work", Share: profile.ShareContent, HookSets: []string{"jira"}}
	h.sync(p)
	_ = os.Remove(filepath.Join(libDir, "jira.yaml"))
	h.lib, _ = hooksets.LoadLibrary(libDir)
	hp := h.sync(p)
	if len(hp.MissingSets) != 1 || hp.MissingSets[0] != "jira" {
		t.Errorf("missing set reported: %v", hp.MissingSets)
	}
	if len(hp.Steps) != 0 || matchers(h.hooks()["PostToolUse"]) != "Bash" {
		t.Errorf("managed entries stay unchanged: steps=%v hooks=%v", hp.Steps, h.hooks())
	}
	if len(hp.MissingScripts) != 0 {
		t.Error("a missing set has no scripts to check")
	}
}

func TestRecordRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "managed-hooks", "claude", "work.json")
	rec, err := hooksets.LoadRecord(path)
	if err != nil || len(rec.Entries) != 0 {
		t.Fatalf("missing record is empty: %v %v", rec, err)
	}
	rec.Entries = []hooksets.RecordEntry{{Set: "jira", Form: "event-groups", Event: "Stop", Fingerprint: "sha256:x"}}
	if changed, err := rec.Save(path); err != nil || !changed {
		t.Fatalf("save: %v %v", changed, err)
	}
	if changed, _ := rec.Save(path); changed {
		t.Error("saving an unchanged record writes nothing")
	}
	back, _ := hooksets.LoadRecord(path)
	if back.FormatVersion != 1 || len(back.Entries) != 1 {
		t.Errorf("round trip: %+v", back)
	}
	if hooksets.Fingerprint([]byte(`{"a":1,"b":2}`)) != hooksets.Fingerprint([]byte(`{ "b":2, "a":1 }`)) {
		t.Error("fingerprints ignore key order and whitespace")
	}
}

// Review fixes: deterministic results, user groups never taken over, reapply only
// replaces an unambiguous edited copy, invalid bodies are not applied, and at all
// aprofile removes what it placed into a profile's own hook file.
func TestHookPlanningSafety(t *testing.T) {
	two := "name: two\nbodies:\n  claude:\n    Stop:\n      - matcher: a\n        hooks: []\n      - matcher: b\n        hooks: []\n"
	libDir, lib := library(t, map[string]string{"two": two})
	h := &hookEnv{t: t, def: builtin(t, "claude"), dir: t.TempDir(), lib: lib}
	testutil.WriteFile(t, filepath.Join(h.dir, "settings.json"), `{}`)
	p := &profile.Profile{Harness: "claude", Name: "w", Share: profile.ShareContent, HookSets: []string{"two"}}
	h.sync(p)

	// Two diverged entries and a deleted set: repeated plans are byte-identical.
	testutil.WriteFile(t, filepath.Join(h.dir, "settings.json"), `{"hooks":{}}`)
	first := h.sync(p)
	recA, _ := json.Marshal(h.rec)
	for i := 0; i < 20; i++ {
		hp, err := hooksets.PlanHooks(h.def, p, h.lib, h.rec, h.dir, hooksets.PlanOptions{})
		if err != nil {
			t.Fatal(err)
		}
		recB, _ := json.Marshal(hp.Record)
		if len(hp.Steps) != 0 || string(recA) != string(recB) {
			t.Fatalf("plan %d is not deterministic: steps=%v", i, hp.Steps)
		}
	}
	if len(first.Divergences) != 2 {
		t.Errorf("both removed groups are diverged: %+v", first.Divergences)
	}
	_ = os.Remove(filepath.Join(libDir, "two.yaml"))
	h.lib, _ = hooksets.LoadLibrary(libDir)
	h.rec = &hooksets.Record{}
	testutil.WriteFile(t, filepath.Join(h.dir, "settings.json"), `{}`)

	// An identical group the user added is theirs: not duplicated, not recorded.
	_, lib2 := library(t, map[string]string{"one": "name: one\nbodies:\n  claude:\n    Stop: [{matcher: m, hooks: []}]\n"})
	u := &hookEnv{t: t, def: builtin(t, "claude"), dir: t.TempDir(), lib: lib2}
	testutil.WriteFile(t, filepath.Join(u.dir, "settings.json"), `{"hooks":{"Stop":[{"matcher":"m","hooks":[]}]}}`)
	pu := &profile.Profile{Harness: "claude", Name: "u", Share: profile.ShareContent, HookSets: []string{"one"}}
	u.sync(pu)
	if len(u.hooks()["Stop"]) != 1 || len(u.rec.Entries) != 0 {
		t.Errorf("the user's identical group stays theirs: hooks=%v rec=%+v", u.hooks(), u.rec.Entries)
	}
	pu.HookSets = nil
	u.sync(pu)
	if len(u.hooks()["Stop"]) != 1 {
		t.Error("detaching never removes the user's own group")
	}

	// Reapply: two user groups that both run the managed program are ambiguous and
	// nothing is replaced; a group that runs another program is the user's own and
	// stays while the managed group is re-added.
	_, lib3 := library(t, map[string]string{"run": "name: run\nbodies:\n  claude:\n    Stop: [{matcher: m, hooks: [{type: command, command: run.sh}]}]\n"})
	r := &hookEnv{t: t, def: builtin(t, "claude"), dir: t.TempDir(), lib: lib3}
	testutil.WriteFile(t, filepath.Join(r.dir, "settings.json"), `{}`)
	pr := &profile.Profile{Harness: "claude", Name: "r", Share: profile.ShareContent, HookSets: []string{"run"}}
	r.sync(pr)
	testutil.WriteFile(t, filepath.Join(r.dir, "settings.json"),
		`{"hooks":{"Stop":[{"matcher":"m","hooks":[{"type":"command","command":"run.sh --a"}]},{"matcher":"m","hooks":[{"type":"command","command":"run.sh --b"}]}]}}`)
	r.sync(pr)
	rp, _ := hooksets.PlanHooks(r.def, pr, r.lib, r.rec, r.dir, hooksets.PlanOptions{Reapply: true})
	if len(rp.Steps) != 0 || len(rp.Divergences) != 1 || !strings.Contains(rp.Divergences[0].Reason, "could be the edited copy") {
		t.Errorf("ambiguous reapply is refused: steps=%v div=%+v", rp.Steps, rp.Divergences)
	}
	testutil.WriteFile(t, filepath.Join(r.dir, "settings.json"),
		`{"hooks":{"Stop":[{"matcher":"m","hooks":[{"type":"command","command":"other.sh"}]}]}}`)
	rp, _ = hooksets.PlanHooks(r.def, pr, r.lib, r.rec, r.dir, hooksets.PlanOptions{Reapply: true})
	if _, _, err := layer.Apply(rp.Steps, layer.ApplyOptions{Yes: true, Backup: func([]string) (string, error) { return "b", nil }}); err != nil {
		t.Fatal(err)
	}
	if got := r.hooks()["Stop"]; len(got) != 2 || !strings.Contains(mustJSON(got[0]), "other.sh") {
		t.Errorf("the user's own group stays and the managed one is re-added: %v", got)
	}

	// A set that fails to load leaves its placed entries unchanged.
	brokenDir, _ := library(t, map[string]string{"run": "name: run\nbodies:\n  claude:\n    Stop: [{matcher: m, hooks: [{type: command, command: run.sh}]}]\n"})
	b := &hookEnv{t: t, def: builtin(t, "claude"), dir: t.TempDir()}
	b.lib, _ = hooksets.LoadLibrary(brokenDir)
	testutil.WriteFile(t, filepath.Join(b.dir, "settings.json"), `{}`)
	pb := &profile.Profile{Harness: "claude", Name: "b", Share: profile.ShareContent, HookSets: []string{"run"}}
	b.sync(pb)
	testutil.WriteFile(t, filepath.Join(brokenDir, "run.yaml"), "name: run\nbodies: [not, a, map]\n")
	b.lib, _ = hooksets.LoadLibrary(brokenDir)
	if hp := b.sync(pb); len(hp.Steps) != 0 || len(b.hooks()["Stop"]) != 1 || len(b.rec.Entries) != 1 {
		t.Errorf("a broken set's entries stay: steps=%v hooks=%v rec=%v", hp.Steps, b.hooks(), b.rec.Entries)
	}

	// An every-profile set whose body has the wrong form is not applied.
	_, bad := library(t, map[string]string{"bad": "name: bad\nevery_profile: true\nbodies:\n  opencode:\n    Stop: [{matcher: m}]\n"})
	hp, err := hooksets.PlanHooks(builtin(t, "opencode"), &profile.Profile{Harness: "opencode", Name: "o", Share: profile.ShareContent}, bad, nil, t.TempDir(), hooksets.PlanOptions{})
	if err != nil || len(hp.InvalidSets) != 1 || len(hp.Steps) != 0 {
		t.Errorf("an invalid every-profile body is reported, not applied: %+v %v", hp, err)
	}

	// At all, with the profile's own settings file still in place, aprofile
	// removes the entries it placed.
	a := &hookEnv{t: t, def: builtin(t, "claude"), dir: t.TempDir(), lib: lib2}
	testutil.WriteFile(t, filepath.Join(a.dir, "settings.json"), `{}`)
	pa := &profile.Profile{Harness: "claude", Name: "a", Share: profile.ShareContent, HookSets: []string{"one"}}
	a.sync(pa)
	pa.Share, pa.HookSets = profile.ShareAll, nil
	a.sync(pa)
	if len(a.hooks()) != 0 || len(a.rec.Entries) != 0 {
		t.Errorf("at all the managed entries are removed: %v %+v", a.hooks(), a.rec.Entries)
	}
}

// Re-review fix: a group the user appends after the managed ones does not make
// sync rewrite the file.
func TestUserOrderKept(t *testing.T) {
	_, lib := library(t, map[string]string{"one": "name: one\nbodies:\n  claude:\n    Stop: [{matcher: m, hooks: []}]\n"})
	h := &hookEnv{t: t, def: builtin(t, "claude"), dir: t.TempDir(), lib: lib}
	testutil.WriteFile(t, filepath.Join(h.dir, "settings.json"), `{}`)
	p := &profile.Profile{Harness: "claude", Name: "w", Share: profile.ShareContent, HookSets: []string{"one"}}
	h.sync(p)
	testutil.WriteFile(t, filepath.Join(h.dir, "settings.json"), `{"hooks":{"Stop":[{"matcher":"m","hooks":[]},{"matcher":"later","hooks":[]}]}}`)
	if hp := h.sync(p); len(hp.Steps) != 0 {
		t.Errorf("only the order differs from aprofile's layout: no rewrite, got %v", hp.Steps)
	}
}

// Round 3 fixes: a changed set body does not re-add an entry the user removed,
// and an interpreter prefix does not make an unrelated user group the edited copy.
func TestDivergedSurvivesBodyChangeAndLaunchers(t *testing.T) {
	libDir, lib := library(t, map[string]string{"gg": "name: gg\nbodies:\n  claude:\n    Stop: [{matcher: m, hooks: [{type: command, command: bash /x/guard.sh}]}]\n"})
	h := &hookEnv{t: t, def: builtin(t, "claude"), dir: t.TempDir(), lib: lib}
	testutil.WriteFile(t, filepath.Join(h.dir, "settings.json"), `{}`)
	p := &profile.Profile{Harness: "claude", Name: "w", Share: profile.ShareContent, HookSets: []string{"gg"}}
	h.sync(p)
	testutil.WriteFile(t, filepath.Join(h.dir, "settings.json"), `{"hooks":{"Stop":[{"matcher":"m","hooks":[{"type":"command","command":"bash /mine/other.sh"}]}]}}`)
	h.sync(p)
	testutil.WriteFile(t, filepath.Join(libDir, "gg.yaml"), "name: gg\nbodies:\n  claude:\n    Stop: [{matcher: m, hooks: [{type: command, command: bash /x/guard.sh --v2}]}]\n")
	h.lib, _ = hooksets.LoadLibrary(libDir)
	if hp := h.sync(p); len(hp.Steps) != 0 || len(h.hooks()["Stop"]) != 1 {
		t.Errorf("a removed entry is not re-added after its set changed: %v %v", hp.Steps, h.hooks())
	}
	rp, _ := hooksets.PlanHooks(h.def, p, h.lib, h.rec, h.dir, hooksets.PlanOptions{Reapply: true})
	if _, _, err := layer.Apply(rp.Steps, layer.ApplyOptions{Yes: true, Backup: func([]string) (string, error) { return "b", nil }}); err != nil {
		t.Fatal(err)
	}
	if got := h.hooks()["Stop"]; len(got) != 2 || !strings.Contains(mustJSON(got[0]), "/mine/other.sh") {
		t.Errorf("bash /mine/other.sh is the user's own group and stays: %v", got)
	}
}

// FR-020: a script run through an interpreter ("bash /path/x.sh") is checked too.
func TestMissingScriptsBehindLauncher(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "ok.sh")
	if err := os.WriteFile(present, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "gone.sh")
	s := &hooksets.Set{Name: "x", Bodies: map[string]hooksets.Body{"claude": {Events: map[string][]json.RawMessage{
		"PreToolUse": {
			json.RawMessage(`{"matcher":"a","hooks":[{"type":"command","command":"bash ` + missing + ` --flag"}]}`),
			json.RawMessage(`{"matcher":"b","hooks":[{"type":"command","command":"bash -e ` + present + `"}]}`),
			json.RawMessage(`{"matcher":"c","hooks":[{"type":"command","command":"rtk-edit"}]}`),
		},
	}}}}
	got := hooksets.MissingScripts(s, builtin(t, "claude"))
	if len(got) != 1 || !strings.Contains(got[0], "gone.sh") {
		t.Errorf("MissingScripts = %v, want only gone.sh", got)
	}
}
