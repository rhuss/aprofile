package layer_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/layer"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/testutil"
)

// toyDef is a synthetic definition with one entry per category, so the planner is
// tested from definition data alone (Principle I: no harness-name branching).
const toyDef = `name: toy
binary: toy
wrapper_prefix: toy
config_env: { TOY_HOME: "." }
default_config_location: "~/.toy"
classification:
  - { entry: "auth.json", layer: credentials }
  - { entry: "history.jsonl", layer: state }
  - { entry: "sessions/", layer: state }
  - { entry: "*.sqlite", layer: state }
  - { entry: "mcp.json", layer: owned }
  - entry: "state.json"
    layer: mixed
    keys: { token: credentials, servers: owned, runs: state }
  - { entry: "NOTES.md", layer: content }
  - { entry: "skills/", layer: content }
  - { entry: "settings.json", layer: settings }
  - { entry: "plugins/", layer: plugin-store }
hooks: { location: "settings.json#hooks", form: event-groups }
skills_location: { path: "skills" }
`

// toyFilesDef has a separate hook-location entry (a directory, form files) and a
// separate hooks file is covered by codex in the built-in tests.
const toyFilesDef = `name: toyf
binary: toyf
wrapper_prefix: toyf
config_env: { TOYF_HOME: "." }
default_config_location: "~/.toyf"
classification:
  - { entry: "NOTES.md", layer: content }
hooks: { location: "ext", form: files }
`

func loadToy(t *testing.T, body string) *harness.Definition {
	t.Helper()
	set := harness.LoadFromBytes(map[string][]byte{"toy.yaml": []byte(body)})
	if errs := set.Errors(); len(errs) != 0 {
		t.Fatalf("toy definition: %v", errs)
	}
	return set.All()[0]
}

// seedToyDefault writes the toy default setup under home.
func seedToyDefault(t *testing.T, home string) string {
	t.Helper()
	d := filepath.Join(home, ".toy")
	testutil.WriteFile(t, filepath.Join(d, "auth.json"), `{"k":"default"}`)
	testutil.WriteFile(t, filepath.Join(d, "history.jsonl"), "default history\n")
	testutil.WriteFile(t, filepath.Join(d, "memories.sqlite"), "db")
	testutil.WriteFile(t, filepath.Join(d, "NOTES.md"), "# notes\n")
	testutil.WriteFile(t, filepath.Join(d, "skills", "a", "SKILL.md"), "# a\n")
	testutil.WriteFile(t, filepath.Join(d, "settings.json"),
		`{"permissions":{"allow":["Bash(ls)"]},"statusLine":"x","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[]}]}}`)
	testutil.WriteFile(t, filepath.Join(d, "plugins", "installed.json"), "{}")
	testutil.WriteFile(t, filepath.Join(d, "state.json"), `{"token":"T","servers":{"s":{}},"runs":3,"theme":"dark"}`)
	return d
}

func prof(level profile.Share) *profile.Profile {
	return &profile.Profile{Harness: "toy", Name: "p", Share: level}
}

// kinds summarizes a plan as "kind entry" lines, sorted.
func kinds(steps []layer.Step) []string {
	var out []string
	for _, s := range steps {
		out = append(out, string(s.Kind)+" "+s.Entry)
	}
	sort.Strings(out)
	return out
}

func has(steps []layer.Step, kind layer.StepKind, entry string) *layer.Step {
	for i := range steps {
		if steps[i].Kind == kind && steps[i].Entry == entry {
			return &steps[i]
		}
	}
	return nil
}

func entryFor(steps []layer.Step, entry string) []layer.Step {
	var out []layer.Step
	for _, s := range steps {
		if s.Entry == entry {
			out = append(out, s)
		}
	}
	return out
}

func apply(t *testing.T, steps []layer.Step, yes bool) []layer.Step {
	t.Helper()
	applied, _, err := layer.Apply(steps, layer.ApplyOptions{Yes: yes, Backup: func(paths []string) (string, error) {
		return "backup-dir", nil
	}})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return applied
}

// T012: a fresh profile config dir, per level, against a full default setup.
func TestPlanFreshPerLevel(t *testing.T) {
	type want struct {
		step  layer.StepKind
		entry string
	}
	cases := []struct {
		level profile.Share
		wants []want
		nots  []want
	}{
		{profile.ShareAll,
			[]want{{layer.StepMkdir, ""}, {layer.StepMkdir, "sessions"}, {layer.StepLink, "NOTES.md"}, {layer.StepLink, "skills"},
				{layer.StepLink, "settings.json"}, {layer.StepLink, "plugins"}, {layer.StepSeedMixed, "state.json"}},
			[]want{{layer.StepLink, "history.jsonl"}, {layer.StepLink, "auth.json"}, {layer.StepCopyOnce, "settings.json"}}},
		{profile.ShareContent,
			[]want{{layer.StepLink, "NOTES.md"}, {layer.StepLink, "skills"}, {layer.StepCopyOnce, "settings.json"},
				{layer.StepLink, "plugins"}, {layer.StepSeedMixed, "state.json"}},
			[]want{{layer.StepLink, "settings.json"}, {layer.StepLink, "history.jsonl"}}},
		{profile.ShareNone,
			[]want{{layer.StepCreate, "settings.json"}, {layer.StepMkdir, "plugins"}, {layer.StepMkdir, "skills"}, {layer.StepSeedMixed, "state.json"}},
			[]want{{layer.StepLink, "NOTES.md"}, {layer.StepLink, "skills"}, {layer.StepLink, "plugins"}, {layer.StepCopyOnce, "settings.json"}}},
	}
	for _, c := range cases {
		t.Run(string(c.level), func(t *testing.T) {
			h := testutil.HermeticHome(t)
			seedToyDefault(t, h.Root)
			def := loadToy(t, toyDef)
			dir := filepath.Join(h.DataHome, "p")
			steps, err := layer.Plan(def, c.level, h.Root, dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range c.wants {
				if has(steps, w.step, w.entry) == nil {
					t.Errorf("missing step %s %q in %v", w.step, w.entry, kinds(steps))
				}
			}
			for _, w := range c.nots {
				if has(steps, w.step, w.entry) != nil {
					t.Errorf("unexpected step %s %q", w.step, w.entry)
				}
			}
			for _, s := range steps {
				if s.Confirm {
					t.Errorf("a fresh profile needs no confirmation, got %s %s", s.Kind, s.Entry)
				}
			}
			if s := has(steps, layer.StepLink, "NOTES.md"); s != nil && s.Source != filepath.Join(h.Root, ".toy", "NOTES.md") {
				t.Errorf("link source = %q", s.Source)
			}
			apply(t, steps, false)

			// Idempotent: a second plan only reports.
			again, err := layer.Plan(def, c.level, h.Root, dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range again {
				if s.Kind != layer.StepReport {
					t.Errorf("second plan should only report, got %s %s", s.Kind, s.Entry)
				}
			}
			assertPrivate(t, dir)
		})
	}
}

// assertPrivate checks that no credentials or state entry is a link.
func assertPrivate(t *testing.T, dir string) {
	t.Helper()
	for _, e := range []string{"auth.json", "history.jsonl", "sessions", "memories.sqlite", "state.json", "mcp.json"} {
		if fi, err := os.Lstat(filepath.Join(dir, e)); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			t.Errorf("%s must never be a link", e)
		}
	}
}

// T012/FR-010: the content copy of settings leaves out the hook location key.
func TestPlanContentCopyExcludesHooks(t *testing.T) {
	h := testutil.HermeticHome(t)
	seedToyDefault(t, h.Root)
	def := loadToy(t, toyDef)
	dir := filepath.Join(h.DataHome, "p")
	steps, err := layer.Plan(def, profile.ShareContent, h.Root, dir)
	if err != nil {
		t.Fatal(err)
	}
	apply(t, steps, false)
	var m map[string]any
	data, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("settings copy is not JSON: %v\n%s", err, data)
	}
	if _, ok := m["hooks"]; ok {
		t.Error("content copy must not carry the default setup's hooks")
	}
	if _, ok := m["permissions"]; !ok {
		t.Error("content copy should carry permissions")
	}
}

// T012/FR-011: at none, nothing is seeded from the default setup, not even the
// owned keys of mixed files.
func TestPlanNoneSeedsNothing(t *testing.T) {
	h := testutil.HermeticHome(t)
	seedToyDefault(t, h.Root)
	def := loadToy(t, toyDef)
	dir := filepath.Join(h.DataHome, "p")
	steps, err := layer.Plan(def, profile.ShareNone, h.Root, dir)
	if err != nil {
		t.Fatal(err)
	}
	apply(t, steps, false)
	data, _ := os.ReadFile(filepath.Join(dir, "state.json"))
	if strings.TrimSpace(string(data)) != "{}" {
		t.Errorf("mixed file at none must start minimal, got %s", data)
	}
	data, _ = os.ReadFile(filepath.Join(dir, "settings.json"))
	if strings.TrimSpace(string(data)) != "{}" {
		t.Errorf("settings at none must start minimal, got %s", data)
	}
	if _, err := os.Lstat(filepath.Join(dir, "NOTES.md")); !os.IsNotExist(err) {
		t.Error("content must not be present at none")
	}
}

// T012: the default setup is missing entirely.
func TestPlanMissingDefaultSetup(t *testing.T) {
	h := testutil.HermeticHome(t)
	def := loadToy(t, toyDef)
	dir := filepath.Join(h.DataHome, "p")
	steps, err := layer.Plan(def, profile.ShareContent, h.Root, dir)
	if err != nil {
		t.Fatal(err)
	}
	if steps[0].Kind != layer.StepMkdir || steps[0].Entry != "" {
		t.Errorf("plan should start by creating the config dir, got %s %q", steps[0].Kind, steps[0].Entry)
	}
	for _, s := range steps {
		if s.Kind == layer.StepLink {
			t.Errorf("no links without a default setup, got %s", s.Entry)
		}
	}
	if has(steps, layer.StepCreate, "settings.json") == nil {
		t.Errorf("settings should be created minimal: %v", kinds(steps))
	}
	rep := has(steps, layer.StepReport, "settings.json")
	if rep == nil || !strings.Contains(rep.Note, "nothing to copy") {
		t.Errorf("expected a report that nothing was copied: %v", kinds(steps))
	}
}

// T012: level transitions over the current state, following the data-model table.
func TestPlanTransitions(t *testing.T) {
	setup := func(t *testing.T, from profile.Share) (*harness.Definition, string, string) {
		h := testutil.HermeticHome(t)
		seedToyDefault(t, h.Root)
		def := loadToy(t, toyDef)
		dir := filepath.Join(h.DataHome, "p")
		steps, err := layer.Plan(def, from, h.Root, dir)
		if err != nil {
			t.Fatal(err)
		}
		apply(t, steps, false)
		return def, h.Root, dir
	}

	t.Run("all to content", func(t *testing.T) {
		def, home, dir := setup(t, profile.ShareAll)
		steps, err := layer.Plan(def, profile.ShareContent, home, dir)
		if err != nil {
			t.Fatal(err)
		}
		if has(steps, layer.StepUnlink, "settings.json") == nil || has(steps, layer.StepCopyOnce, "settings.json") == nil {
			t.Errorf("settings: want unlink + copy-once, got %v", kinds(entryFor(steps, "settings.json")))
		}
		rep := has(steps, layer.StepReport, "settings.json")
		if rep == nil || !strings.Contains(rep.Note, "hook-sets import") || !strings.Contains(rep.Note, "PreToolUse") {
			t.Errorf("dry run must list default hooks and suggest hook-sets import: %+v", rep)
		}
		if len(entryFor(steps, "NOTES.md")) != 0 || len(entryFor(steps, "plugins")) != 0 {
			t.Error("content and plugin store links are kept")
		}
		for _, s := range steps {
			if s.Confirm {
				t.Errorf("all to content replaces no profile data, got confirm on %s", s.Entry)
			}
		}
	})

	t.Run("content to none", func(t *testing.T) {
		def, home, dir := setup(t, profile.ShareContent)
		testutil.WriteFile(t, filepath.Join(dir, "settings.json"), `{"own":true}`)
		steps, err := layer.Plan(def, profile.ShareNone, home, dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range []string{"NOTES.md", "skills", "plugins"} {
			if has(steps, layer.StepUnlink, e) == nil {
				t.Errorf("%s: want unlink, got %v", e, kinds(steps))
			}
		}
		if has(steps, layer.StepMkdir, "plugins") == nil {
			t.Errorf("plugin store becomes the profile's own dir: %v", kinds(steps))
		}
		if len(entryFor(steps, "settings.json")) != 0 {
			t.Errorf("own settings are kept: %v", kinds(entryFor(steps, "settings.json")))
		}
	})

	t.Run("none to content", func(t *testing.T) {
		def, home, dir := setup(t, profile.ShareNone)
		testutil.WriteFile(t, filepath.Join(dir, "skills", "mine", "SKILL.md"), "mine")
		steps, err := layer.Plan(def, profile.ShareContent, home, dir)
		if err != nil {
			t.Fatal(err)
		}
		if has(steps, layer.StepLink, "NOTES.md") == nil {
			t.Errorf("absent content is linked: %v", kinds(steps))
		}
		rep := has(steps, layer.StepReport, "skills")
		if rep == nil || !strings.Contains(rep.Note, "link-replaced-by-file") {
			t.Errorf("a real skills dir is kept and reported: %v", kinds(entryFor(steps, "skills")))
		}
		// The plugin store dir is still empty (created at none): it becomes the
		// link without confirmation, since nothing is lost.
		if r := has(steps, layer.StepReplaceWithLink, "plugins"); r == nil || r.Confirm {
			t.Errorf("an empty own plugin store dir becomes the link: %v", kinds(entryFor(steps, "plugins")))
		}
		apply(t, steps, false)
		if fi, _ := os.Lstat(filepath.Join(dir, "plugins")); fi.Mode()&os.ModeSymlink == 0 {
			t.Error("plugins should now be linked")
		}
		if b, _ := os.ReadFile(filepath.Join(dir, "skills", "mine", "SKILL.md")); string(b) != "mine" {
			t.Error("the profile's own skills are kept")
		}
	})

	t.Run("content to all", func(t *testing.T) {
		def, home, dir := setup(t, profile.ShareContent)
		steps, err := layer.Plan(def, profile.ShareAll, home, dir)
		if err != nil {
			t.Fatal(err)
		}
		s := has(steps, layer.StepReplaceWithLink, "settings.json")
		if s == nil || !s.Confirm {
			t.Fatalf("own settings are replaced by a link only with confirmation: %v", kinds(steps))
		}
		// Safe mode (sync) skips it; confirmed apply backs up first.
		_, skipped, err := layer.Apply(steps, layer.ApplyOptions{Safe: true})
		if err != nil || len(skipped) != 1 {
			t.Fatalf("safe apply should skip exactly the confirm step: skipped=%v err=%v", kinds(skipped), err)
		}
		if fi, _ := os.Lstat(filepath.Join(dir, "settings.json")); fi.Mode()&os.ModeSymlink != 0 {
			t.Error("safe mode must not replace profile data")
		}
		var backedUp []string
		applied, _, err := layer.Apply(steps, layer.ApplyOptions{Yes: true, Backup: func(p []string) (string, error) {
			backedUp = append(backedUp, p...)
			return "b", nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		if len(backedUp) != 1 || filepath.Base(backedUp[0]) != "settings.json" {
			t.Errorf("backup before replace: %v", backedUp)
		}
		if a := has(applied, layer.StepReplaceWithLink, "settings.json"); a == nil || a.Backup != "b" {
			t.Errorf("applied step should record the backup: %+v", a)
		}
		if fi, _ := os.Lstat(filepath.Join(dir, "settings.json")); fi.Mode()&os.ModeSymlink == 0 {
			t.Error("settings should now be a link")
		}
	})
}

// T012: current state variants for a linked entry.
func TestPlanCurrentStates(t *testing.T) {
	h := testutil.HermeticHome(t)
	seedToyDefault(t, h.Root)
	def := loadToy(t, toyDef)
	dir := filepath.Join(h.DataHome, "p")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Wrong link for content: relinked. Real entry for content: kept and reported.
	elsewhere := filepath.Join(h.Root, "elsewhere.md")
	testutil.WriteFile(t, elsewhere, "x")
	if err := os.Symlink(elsewhere, filepath.Join(dir, "NOTES.md")); err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, filepath.Join(dir, "skills", "own", "SKILL.md"), "own")
	// A leaking state link.
	if err := os.Symlink(filepath.Join(h.Root, ".toy", "history.jsonl"), filepath.Join(dir, "history.jsonl")); err != nil {
		t.Fatal(err)
	}
	// A leaking glob-classified database link.
	if err := os.Symlink(filepath.Join(h.Root, ".toy", "memories.sqlite"), filepath.Join(dir, "memories.sqlite")); err != nil {
		t.Fatal(err)
	}
	// An unclassified entry.
	testutil.WriteFile(t, filepath.Join(dir, "foo.json"), "{}")

	steps, err := layer.Plan(def, profile.ShareContent, h.Root, dir)
	if err != nil {
		t.Fatal(err)
	}
	if has(steps, layer.StepLink, "NOTES.md") == nil {
		t.Errorf("a wrong link is relinked: %v", kinds(entryFor(steps, "NOTES.md")))
	}
	if r := has(steps, layer.StepReport, "skills"); r == nil || !strings.Contains(r.Note, "link-replaced-by-file") {
		t.Errorf("a real content entry is reported: %v", kinds(entryFor(steps, "skills")))
	}
	for _, e := range []string{"history.jsonl", "memories.sqlite"} {
		if r := has(steps, layer.StepReport, e); r == nil || !strings.Contains(r.Note, "fix-links") {
			t.Errorf("%s: leaking link reported with fix-links: %v", e, kinds(entryFor(steps, e)))
		}
	}
	if r := has(steps, layer.StepReport, "foo.json"); r == nil || !strings.Contains(r.Note, "unclassified") {
		t.Errorf("unclassified entry reported: %v", kinds(entryFor(steps, "foo.json")))
	}

	// With FixLinks, leaking links become empty private entries; the target stays.
	fixed, err := layer.PlanWith(def, profile.ShareContent, h.Root, dir, layer.PlanOptions{FixLinks: true})
	if err != nil {
		t.Fatal(err)
	}
	fl := has(fixed, layer.StepFixLink, "history.jsonl")
	if fl == nil || !strings.Contains(fl.Note, "earlier data stays in the default setup") {
		t.Fatalf("fix-link step with FR-034 message expected: %+v", fl)
	}
	apply(t, fixed, false)
	if fi, err := os.Lstat(filepath.Join(dir, "history.jsonl")); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Error("history.jsonl should now be a real empty file")
	}
	if b, _ := os.ReadFile(filepath.Join(h.Root, ".toy", "history.jsonl")); string(b) != "default history\n" {
		t.Error("the link target must never change")
	}

	// With Relink, the real content entry becomes a confirmed replace.
	relink, err := layer.PlanWith(def, profile.ShareContent, h.Root, dir, layer.PlanOptions{Relink: true})
	if err != nil {
		t.Fatal(err)
	}
	if s := has(relink, layer.StepReplaceWithLink, "skills"); s == nil || !s.Confirm {
		t.Errorf("relink proposes a confirmed replace: %v", kinds(entryFor(relink, "skills")))
	}
}

// T012/FR-009a: a mixed or owned settings-like file is never linked, whatever the
// level; a separate hook-location entry is linked at all and released otherwise.
func TestPlanHookLocationEntry(t *testing.T) {
	h := testutil.HermeticHome(t)
	def := loadToy(t, toyFilesDef)
	testutil.WriteFile(t, filepath.Join(h.Root, ".toyf", "ext", "a.ts"), "x")
	dir := filepath.Join(h.DataHome, "p")
	steps, err := layer.Plan(def, profile.ShareAll, h.Root, dir)
	if err != nil {
		t.Fatal(err)
	}
	if has(steps, layer.StepLink, "ext") == nil {
		t.Fatalf("the hook directory is linked at all: %v", kinds(steps))
	}
	apply(t, steps, false)
	steps, err = layer.Plan(def, profile.ShareContent, h.Root, dir)
	if err != nil {
		t.Fatal(err)
	}
	if has(steps, layer.StepUnlink, "ext") == nil {
		t.Errorf("at content the hook directory link is removed: %v", kinds(steps))
	}
}

// T038 (shared with T013): an invalid JSON default settings file fails the plan,
// naming the file, before anything is written.
func TestPlanInvalidJSONFails(t *testing.T) {
	h := testutil.HermeticHome(t)
	def := loadToy(t, toyDef)
	testutil.WriteFile(t, filepath.Join(h.Root, ".toy", "settings.json"), "{not json")
	dir := filepath.Join(h.DataHome, "p")
	_, err := layer.Plan(def, profile.ShareContent, h.Root, dir)
	if err == nil || !strings.Contains(err.Error(), "settings.json") {
		t.Fatalf("expected an error naming settings.json, got %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("a failed plan must write nothing")
	}
}

// T014: dry runs write nothing; apply never deletes a link target.
func TestApplyDryRunAndSafety(t *testing.T) {
	h := testutil.HermeticHome(t)
	seedToyDefault(t, h.Root)
	def := loadToy(t, toyDef)
	dir := filepath.Join(h.DataHome, "p")
	steps, err := layer.Plan(def, profile.ShareAll, h.Root, dir)
	if err != nil {
		t.Fatal(err)
	}
	applied, _, err := layer.Apply(steps, layer.ApplyOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != len(steps) {
		t.Errorf("dry run lists every step: %d of %d", len(applied), len(steps))
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("dry run must write nothing")
	}
	apply(t, steps, false)
	// Moving to none unlinks; the default setup's files stay.
	steps, _ = layer.Plan(def, profile.ShareNone, h.Root, dir)
	apply(t, steps, false)
	for _, e := range []string{"NOTES.md", "skills/a/SKILL.md", "settings.json", "plugins/installed.json"} {
		if _, err := os.Stat(filepath.Join(h.Root, ".toy", e)); err != nil {
			t.Errorf("default setup entry %s must survive: %v", e, err)
		}
	}
	// A confirm step without a backup function is refused.
	_, _, err = layer.Apply([]layer.Step{{Kind: layer.StepReplaceWithLink, Path: filepath.Join(dir, "x"), Confirm: true}}, layer.ApplyOptions{Yes: true})
	if err == nil {
		t.Error("replacing data without a backup must fail")
	}
}

// T011: classification walks containers; glob matches count as classified;
// default-setup entries no rule covers are listed.
func TestUnclassifiedAndUnborrowed(t *testing.T) {
	h := testutil.HermeticHome(t)
	testutil.SeedDefaults(t, h, "claude")
	testutil.SeedDefaults(t, h, "opencode")
	set, _ := harness.LoadBuiltins()

	claude, _ := set.Get("claude")
	unb, err := layer.UnborrowedDefaultEntries(claude)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"RTK.md", "cpr.sh", "statusline.sh"}
	if strings.Join(unb, ",") != strings.Join(want, ",") {
		t.Errorf("claude unborrowed = %v, want %v", unb, want)
	}

	oc, _ := set.Get("opencode")
	dir := filepath.Join(h.DataHome, "oc")
	testutil.WriteFile(t, filepath.Join(dir, "config", "opencode", "foo.json"), "{}")
	testutil.WriteFile(t, filepath.Join(dir, "data", "opencode", "opencode.db-wal"), "")
	testutil.WriteFile(t, filepath.Join(dir, "config", "opencode", "plugins", "x.ts"), "")
	unc, err := layer.Unclassified(oc, dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(unc, ",") != "config/opencode/foo.json" {
		t.Errorf("opencode unclassified = %v", unc)
	}

	codex, _ := set.Get("codex")
	cdir := filepath.Join(h.DataHome, "cx")
	for _, e := range []string{"memories_1.sqlite", "memories_1.sqlite-wal", "hooks.json"} {
		testutil.WriteFile(t, filepath.Join(cdir, e), "")
	}
	if unc, _ := layer.Unclassified(codex, cdir); len(unc) != 0 {
		t.Errorf("glob matches, companions and the hooks file are classified: %v", unc)
	}
	if r := layer.Classify(codex, "hooks.json"); r.Layer != harness.LayerHookLocation {
		t.Errorf("codex hooks.json category = %q", r.Layer)
	}
}

// Quickstart finding: a real entry where the level expects a link, while the default
// setup has no such entry, is the profile's own and not a replaced link.
func TestTreatmentRealEntryWithoutDefault(t *testing.T) {
	h := testutil.HermeticHome(t)
	def := loadToy(t, toyDef)
	dir := filepath.Join(h.DataHome, "p")
	testutil.WriteFile(t, filepath.Join(dir, "settings.json"), "{}")
	entries, err := layer.Treatments(def, prof(profile.ShareAll), h.Root, dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.Entry == "settings.json" {
			found = true
			if e.Status != layer.StatusOK || e.Treatment != layer.TreatmentOwnCopy {
				t.Errorf("settings.json without a default: %+v", e)
			}
		}
	}
	if !found {
		t.Fatal("settings.json must be listed")
	}
	steps, _ := layer.Plan(def, profile.ShareAll, h.Root, dir)
	if len(entryFor(steps, "settings.json")) != 0 {
		t.Errorf("the planner leaves it alone too: %v", kinds(steps))
	}
}

// Review fixes: file modes, copy-once secret warnings, and redaction.
func TestPlanModesAndSecrets(t *testing.T) {
	h := testutil.HermeticHome(t)
	d := seedToyDefault(t, h.Root)
	testutil.WriteFile(t, filepath.Join(d, "settings.json"), `{"env":{"API_TOKEN":"abc"},"hooks":{}}`)
	if err := os.Chmod(filepath.Join(d, "settings.json"), 0o600); err != nil {
		t.Fatal(err)
	}
	def := loadToy(t, toyDef)
	dir := filepath.Join(h.DataHome, "p")
	steps, err := layer.Plan(def, profile.ShareContent, h.Root, dir)
	if err != nil {
		t.Fatal(err)
	}
	warn := false
	for _, s := range steps {
		if s.Kind == layer.StepReport && strings.Contains(s.Note, "env.API_TOKEN looks like a secret") {
			warn = true
			if strings.Contains(s.Note, "abc") {
				t.Error("the warning must not contain the value")
			}
		}
	}
	if !warn {
		t.Errorf("copy-once warns per secret-looking key: %v", kinds(steps))
	}
	apply(t, steps, false)
	for name, want := range map[string]os.FileMode{"settings.json": 0o600, "state.json": 0o600} {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil || fi.Mode().Perm() != want {
			t.Errorf("%s mode = %v, want %v (err %v)", name, fi.Mode().Perm(), want, err)
		}
	}
	_, removed, _, _ := layer.ReplaceSettings([]byte(`{"permissions":{"allow":["Bash(GH=ghp_secret gh pr list)"]}}`), []byte(`{"permissions":{"allow":[]}}`), nil, nil)
	if len(removed) != 1 || strings.Contains(removed[0], "ghp_") {
		t.Errorf("a secret-looking list item is hidden: %v", removed)
	}
}

// Review fix: an entry naming the profile root itself is rejected.
func TestDotEntryRejected(t *testing.T) {
	set := harness.LoadFromBytes(map[string][]byte{"x.yaml": []byte("name: toy\nbinary: toy\nwrapper_prefix: toy\nconfig_env: { T: \".\" }\nclassification: [{ entry: \"./\", layer: settings }]\n")})
	if len(set.Errors()) == 0 {
		t.Error("an entry for the profile root must be rejected")
	}
}

// ---- spec 003: history awareness (research R7) ----

// toyHistDef marks some state entries as history.
const toyHistDef = `name: toy
binary: toy
wrapper_prefix: toy
config_env: { TOY_HOME: "." }
default_config_location: "~/.toy"
classification:
  - { entry: "auth.json", layer: credentials }
  - { entry: "history.jsonl", layer: state, history: true }
  - { entry: "sessions/", layer: state, history: true, merge: per-file }
  - { entry: "todos/", layer: state, history: true, merge: per-file }
  - { entry: "logs/", layer: state }
  - { entry: "NOTES.md", layer: content }
`

// toyNoHistDef is toyHistDef without the history flags.
const toyNoHistDef = `name: toy
binary: toy
wrapper_prefix: toy
config_env: { TOY_HOME: "." }
default_config_location: "~/.toy"
classification:
  - { entry: "auth.json", layer: credentials }
  - { entry: "history.jsonl", layer: state }
  - { entry: "sessions/", layer: state }
  - { entry: "todos/", layer: state }
  - { entry: "logs/", layer: state }
  - { entry: "NOTES.md", layer: content }
`

// fakeStore is a layer.HistoryStore rooted at a directory.
type fakeStore struct {
	root string
	home bool
	name string
}

func (s fakeStore) EntryPath(rel string) string {
	return filepath.Join(s.root, filepath.FromSlash(strings.TrimSuffix(rel, "/")))
}
func (s fakeStore) IsHome() bool { return s.home }
func (s fakeStore) Name() string { return s.name }

func histOpts(st layer.HistoryStore, explicit bool) layer.PlanOptions {
	return layer.PlanOptions{HistoryStore: st, ExplicitJoin: explicit, ProfileID: "toy/p"}
}

func TestPlanHistoryMember(t *testing.T) {
	h := testutil.HermeticHome(t)
	def := loadToy(t, toyHistDef)
	work := fakeStore{root: filepath.Join(h.DataHome, "aprofile", "groups", "toy", "work"), name: "work"}
	homeSt := fakeStore{root: filepath.Join(h.Root, ".toy"), home: true, name: "home"}

	t.Run("correct links yield no step", func(t *testing.T) {
		dir := filepath.Join(h.DataHome, "p1")
		for _, e := range []string{"history.jsonl", "sessions", "todos"} {
			testutil.WriteFile(t, filepath.Join(dir, "keep"), "")
			if err := os.Symlink(work.EntryPath(e), filepath.Join(dir, e)); err != nil {
				t.Fatal(err)
			}
		}
		steps, err := layer.PlanWith(def, profile.ShareAll, h.Root, dir, histOpts(work, false))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range []string{"history.jsonl", "sessions", "todos"} {
			if got := entryFor(steps, e); len(got) != 0 {
				t.Errorf("%s: unexpected steps %v", e, kinds(got))
			}
		}
	})

	t.Run("wrong target relinks", func(t *testing.T) {
		dir := filepath.Join(h.DataHome, "p2")
		testutil.WriteFile(t, filepath.Join(dir, "keep"), "")
		if err := os.Symlink(homeSt.EntryPath("sessions"), filepath.Join(dir, "sessions")); err != nil {
			t.Fatal(err)
		}
		steps, _ := layer.PlanWith(def, profile.ShareAll, h.Root, dir, histOpts(work, false))
		s := has(steps, layer.StepRelinkHistory, "sessions")
		if s == nil || s.Source != work.EntryPath("sessions") || s.Confirm {
			t.Fatalf("want unconfirmed relink-history sessions -> work store, got %v", kinds(steps))
		}
	})

	t.Run("absent entries, data-dir store", func(t *testing.T) {
		dir := filepath.Join(h.DataHome, "p3")
		testutil.WriteFile(t, filepath.Join(work.root, "todos", "x.json"), "{}") // store side exists
		steps, _ := layer.PlanWith(def, profile.ShareAll, h.Root, dir, histOpts(work, false))
		if s := has(steps, layer.StepLinkHistory, "history.jsonl"); s == nil || s.Source != work.EntryPath("history.jsonl") {
			t.Errorf("absent file entry must be linked (dangling until written): %v", kinds(steps))
		}
		if has(steps, layer.StepLinkHistory, "todos") == nil {
			t.Errorf("todos: want link-history, got %v", kinds(steps))
		}
		if has(steps, layer.StepMkdir, work.EntryPath("todos")) != nil {
			t.Error("todos store dir exists: no mkdir expected")
		}
		if has(steps, layer.StepMkdir, work.EntryPath("sessions")) == nil || has(steps, layer.StepLinkHistory, "sessions") == nil {
			t.Errorf("sessions: want store-side mkdir and link-history, got %v", kinds(steps))
		}
		if has(steps, layer.StepMkdir, "sessions") != nil {
			t.Error("a member must not get a private sessions directory")
		}
		// Non-history state stays private.
		if has(steps, layer.StepMkdir, "logs") == nil {
			t.Errorf("logs: want private mkdir, got %v", kinds(steps))
		}
		apply(t, steps, false)
		for _, e := range []string{"history.jsonl", "sessions", "todos"} {
			target, err := os.Readlink(filepath.Join(dir, e))
			if err != nil || target != work.EntryPath(e) {
				t.Errorf("%s: link = %q, %v", e, target, err)
			}
		}
		again, _ := layer.PlanWith(def, profile.ShareAll, h.Root, dir, histOpts(work, false))
		for _, s := range again {
			if s.Mutating() {
				t.Errorf("second plan must not change anything, got %s %s", s.Kind, s.Entry)
			}
		}
	})

	t.Run("home store missing dir", func(t *testing.T) {
		dir := filepath.Join(h.DataHome, "p4")
		// sync mode: the default setup is never written.
		steps, _ := layer.PlanWith(def, profile.ShareAll, h.Root, dir, histOpts(homeSt, false))
		for _, s := range steps {
			if s.Kind == layer.StepMkdir && s.Note == "default setup" {
				t.Fatalf("sync-mode plan contains a default setup mkdir: %s", s)
			}
		}
		var rep *layer.Step
		for i := range steps {
			if steps[i].Kind == layer.StepReport && steps[i].Entry == "sessions" {
				rep = &steps[i]
			}
		}
		if rep == nil || !strings.Contains(rep.Note, "aprofile edit toy/p --group home") {
			t.Errorf("want a report naming edit --group home, got %v", kinds(steps))
		}
		if has(steps, layer.StepLinkHistory, "sessions") != nil {
			t.Error("sync mode must not link a missing home directory")
		}
		// The file entry is linked anyway.
		if has(steps, layer.StepLinkHistory, "history.jsonl") == nil {
			t.Errorf("history.jsonl: want link-history, got %v", kinds(steps))
		}
		// Explicit join: create it empty in the default location.
		steps, _ = layer.PlanWith(def, profile.ShareAll, h.Root, dir, histOpts(homeSt, true))
		mk := has(steps, layer.StepMkdir, homeSt.EntryPath("sessions"))
		if mk == nil || mk.Note != "default setup" || has(steps, layer.StepLinkHistory, "sessions") == nil {
			t.Errorf("explicit join: want default setup mkdir and link-history, got %v", kinds(steps))
		}
	})

	t.Run("real entry is reported, relink replaces after confirm", func(t *testing.T) {
		dir := filepath.Join(h.DataHome, "p5")
		testutil.WriteFile(t, filepath.Join(dir, "sessions", "s.jsonl"), "{}")
		steps, _ := layer.PlanWith(def, profile.ShareAll, h.Root, dir, histOpts(work, false))
		var rep *layer.Step
		for i := range steps {
			if steps[i].Kind == layer.StepReport && steps[i].Entry == "sessions" {
				rep = &steps[i]
			}
		}
		if rep == nil || !strings.Contains(rep.Note, "aprofile edit toy/p --group work") {
			t.Errorf("want a report naming edit --group work, got %v", kinds(steps))
		}
		opts := histOpts(work, false)
		opts.Relink = true
		steps, _ = layer.PlanWith(def, profile.ShareAll, h.Root, dir, opts)
		if s := has(steps, layer.StepReplaceWithLink, "sessions"); s == nil || !s.Confirm || s.Source != work.EntryPath("sessions") {
			t.Errorf("relink: want confirmed replace-with-link to the store, got %v", kinds(steps))
		}
	})

	t.Run("link into the store for a non-history entry is fixed", func(t *testing.T) {
		dir := filepath.Join(h.DataHome, "p6")
		testutil.WriteFile(t, filepath.Join(dir, "keep"), "")
		if err := os.Symlink(work.EntryPath("logs"), filepath.Join(dir, "logs")); err != nil {
			t.Fatal(err)
		}
		steps, _ := layer.PlanWith(def, profile.ShareAll, h.Root, dir, histOpts(work, false))
		if s := has(steps, layer.StepFixLink, "logs"); s == nil || !s.IsDir {
			t.Errorf("want fix-link logs, got %v", kinds(steps))
		}
	})
}

// With no history store, a definition's history flags change nothing (SC-002).
func TestPlanHistoryIsolatedUnchanged(t *testing.T) {
	h := testutil.HermeticHome(t)
	hist := loadToy(t, toyHistDef)
	plain := loadToy(t, toyNoHistDef)
	testutil.WriteFile(t, filepath.Join(h.Root, ".toy", "NOTES.md"), "# n\n")
	dirs := map[string]func(dir string){
		"fresh": func(string) {},
		"real entries": func(dir string) {
			testutil.WriteFile(t, filepath.Join(dir, "sessions", "s.jsonl"), "{}")
			testutil.WriteFile(t, filepath.Join(dir, "history.jsonl"), "x\n")
		},
		"leaking links": func(dir string) {
			testutil.WriteFile(t, filepath.Join(dir, "keep"), "")
			_ = os.Symlink(filepath.Join(h.Root, ".toy", "sessions"), filepath.Join(dir, "sessions"))
			_ = os.Symlink(filepath.Join(h.Root, ".toy", "history.jsonl"), filepath.Join(dir, "history.jsonl"))
		},
	}
	for name, seed := range dirs {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(h.DataHome, strings.ReplaceAll(name, " ", "-"))
			seed(dir)
			for _, opts := range []layer.PlanOptions{{}, {FixLinks: true}, {Relink: true}} {
				a, err := layer.PlanWith(hist, profile.ShareAll, h.Root, dir, opts)
				if err != nil {
					t.Fatal(err)
				}
				b, _ := layer.PlanWith(plain, profile.ShareAll, h.Root, dir, opts)
				ja, _ := json.Marshal(a)
				jb, _ := json.Marshal(b)
				if string(ja) != string(jb) {
					t.Errorf("opts %+v: plan with history flags differs for an isolated profile:\n%s\n%s", opts, ja, jb)
				}
			}
		})
	}
}

// Checkpoint 1 review: a store directory removed by hand is recreated for a correct
// link (non-home store), and a relink to a missing home directory is reported in
// sync mode instead of creating a dangling link.
func TestPlanHistoryStoreDirRecreated(t *testing.T) {
	h := testutil.HermeticHome(t)
	def := loadToy(t, toyHistDef)
	work := fakeStore{root: filepath.Join(h.DataHome, "store"), name: "work"}
	homeSt := fakeStore{root: filepath.Join(h.Root, ".toy"), home: true, name: "home"}
	dir := filepath.Join(h.DataHome, "p")
	testutil.WriteFile(t, filepath.Join(dir, "keep"), "")
	if err := os.Symlink(work.EntryPath("sessions"), filepath.Join(dir, "sessions")); err != nil {
		t.Fatal(err)
	}
	steps, _ := layer.PlanWith(def, profile.ShareAll, h.Root, dir, histOpts(work, false))
	if has(steps, layer.StepMkdir, work.EntryPath("sessions")) == nil {
		t.Errorf("missing store dir behind a correct link: want store-side mkdir, got %v", kinds(steps))
	}
	steps, _ = layer.PlanWith(def, profile.ShareAll, h.Root, dir, histOpts(homeSt, false))
	if has(steps, layer.StepRelinkHistory, "sessions") != nil {
		t.Errorf("sync mode must not relink to a missing home directory: %v", kinds(steps))
	}
	for _, s := range steps {
		if s.Kind == layer.StepMkdir && s.Note == "default setup" {
			t.Fatalf("sync-mode plan writes the default setup: %s", s)
		}
	}
}

// A non-history state entry that is a leaking link outside the store keeps the
// spec 002 handling for members too.
func TestPlanHistoryMemberOtherLeakUnchanged(t *testing.T) {
	h := testutil.HermeticHome(t)
	def := loadToy(t, toyHistDef)
	work := fakeStore{root: filepath.Join(h.DataHome, "store"), name: "work"}
	dir := filepath.Join(h.DataHome, "p")
	testutil.WriteFile(t, filepath.Join(dir, "keep"), "")
	if err := os.Symlink(filepath.Join(h.Root, ".toy", "logs"), filepath.Join(dir, "logs")); err != nil {
		t.Fatal(err)
	}
	steps, _ := layer.PlanWith(def, profile.ShareAll, h.Root, dir, histOpts(work, false))
	if has(steps, layer.StepFixLink, "logs") != nil {
		t.Error("a leak outside the store must only be reported without --fix-links")
	}
}
