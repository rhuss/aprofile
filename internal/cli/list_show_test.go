package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/layer"
	"github.com/cc-deck/aprofile/internal/testutil"
)

// T034/T035 (FR-015, SC-005): show lists every entry with one treatment and a
// reason, per level, in a stable JSON schema.
func TestShowTreatmentsPerLevel(t *testing.T) {
	h := testutil.HermeticHome(t)
	testutil.SeedDefaults(t, h, "claude")
	type want struct{ entry, treatment string }
	cases := map[string][]want{
		"all":     {{"projects", layer.TreatmentPrivate}, {"CLAUDE.md", layer.TreatmentLinked}, {"settings.json", layer.TreatmentLinked}, {"settings.json#hooks", layer.TreatmentLinked}},
		"content": {{"projects", layer.TreatmentPrivate}, {"CLAUDE.md", layer.TreatmentLinked}, {"settings.json", layer.TreatmentOwnCopy}, {"settings.json#hooks", layer.TreatmentManaged}, {"plugins", layer.TreatmentLinked}},
		"none":    {{"projects", layer.TreatmentPrivate}, {"settings.json", layer.TreatmentOwnCopy}, {"plugins", layer.TreatmentOwnCopy}, {"skills", layer.TreatmentOwnCopy}},
	}
	for level, wants := range cases {
		if _, err := runCLI(t, "add", "claude", "p-"+level, "--auth-kind", "oauth-login", "--share", level); err != nil {
			t.Fatal(err)
		}
		if out, err := runCLI(t, "sync"); err != nil {
			t.Fatalf("sync: %v\n%s", err, out)
		}
		out, err := runCLI(t, "show", "claude/p-"+level, "--json")
		if err != nil {
			t.Fatalf("show: %v\n%s", err, out)
		}
		var sj struct {
			Share            string                 `json:"share"`
			ShareDescription string                 `json:"share_description"`
			Entries          []layer.EntryTreatment `json:"entries"`
			HomeEscapes      []any                  `json:"home_escapes"`
			Unborrowed       []string               `json:"unborrowed_default_entries"`
			SettingsDiffer   bool                   `json:"settings_differ"`
		}
		if err := json.Unmarshal([]byte(out), &sj); err != nil {
			t.Fatalf("show --json: %v\n%s", err, out)
		}
		if sj.Share != level || sj.ShareDescription == "" {
			t.Errorf("%s: share %q description %q", level, sj.Share, sj.ShareDescription)
		}
		got := map[string]layer.EntryTreatment{}
		for _, e := range sj.Entries {
			if e.Reason == "" || e.Treatment == "" || e.Status == "" {
				t.Errorf("%s: entry without treatment/reason/status: %+v", level, e)
			}
			got[e.Entry] = e
		}
		for _, w := range wants {
			if got[w.entry].Treatment != w.treatment {
				t.Errorf("%s: %s treatment = %q, want %q", level, w.entry, got[w.entry].Treatment, w.treatment)
			}
		}
		if got["projects"].Reason != "runtime state: always private" {
			t.Errorf("%s: reason = %q", level, got["history.jsonl"].Reason)
		}
		if level != "none" && len(sj.Unborrowed) == 0 {
			t.Errorf("%s: unborrowed default entries (RTK.md, ...) should be listed", level)
		}
		if level == "content" && sj.SettingsDiffer {
			t.Error("a fresh content copy does not differ from the default setup")
		}
		// Every existing entry of the config dir is covered (SC-005).
		dir := filepath.Join(h.DataHome, "aprofile", "profiles", "claude", "p-"+level)
		// Ground truth from the filesystem, not from the function under test.
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, de := range entries {
			if et, ok := got[de.Name()]; !ok || et.Treatment == "" || et.Reason == "" {
				t.Errorf("%s: config dir entry %s has no treatment in show", level, de.Name())
			}
		}
	}
}

// currentApp builds an app for the hermetic home the test already set up.
func currentApp(t *testing.T) *app {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	a, err := newApp(cmd)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// Walkthrough observations (RUNBOOK Steps 7 and 18): an entry the level would link
// but the default setup lacks is not listed as linked, and a copy-once entry with
// nothing to copy says it started minimal; hooks/ is borrowed as content.
func TestShowAbsentAndMinimalEntries(t *testing.T) {
	h := testutil.HermeticHome(t)
	testutil.WriteFile(t, filepath.Join(h.Root, ".claude", "CLAUDE.md"), "# default\n")
	testutil.WriteFile(t, filepath.Join(h.Root, ".claude", "hooks", "rtk.sh"), "#!/bin/sh\n")
	testutil.WriteFile(t, filepath.Join(h.Root, ".claude", "settings.json"), `{"permissions":{}}`)
	if _, err := runCLI(t, "add", "claude", "pc", "--auth-kind", "oauth-login", "--share", "content"); err != nil {
		t.Fatal(err)
	}
	if out, err := runCLI(t, "sync"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}
	out, err := runCLI(t, "show", "claude/pc")
	if err != nil {
		t.Fatalf("show: %v\n%s", err, out)
	}
	linked := section(out, "  linked to the default setup:")
	absent := section(out, "  not in the default setup (linked once it is):")
	for _, e := range []string{"agents", "commands", "themes"} {
		if strings.Contains(linked, e) || !strings.Contains(absent, e) {
			t.Errorf("%s belongs under the absent group, not linked:\nlinked:\n%s\nabsent:\n%s", e, linked, absent)
		}
	}
	if strings.Contains(out, "[missing-link-target]") {
		t.Errorf("the absent group needs no status suffix:\n%s", out)
	}
	if !strings.Contains(linked, "CLAUDE.md") || !strings.Contains(linked, "hooks") {
		t.Errorf("CLAUDE.md and hooks are linked:\n%s", linked)
	}
	if !strings.Contains(out, "settings.local.json          settings: the profile's own, started minimal; the default setup has none to copy") {
		t.Errorf("settings.local.json had nothing to copy:\n%s", out)
	}
	if !strings.Contains(out, "settings.json                settings: the profile's own copy, taken from the default setup once") {
		t.Errorf("settings.json was copied:\n%s", out)
	}
	// JSON keeps the documented treatment and status (data-model.md).
	js, _ := runCLI(t, "show", "claude/pc", "--json")
	if !strings.Contains(js, `"entry": "agents"`) || !strings.Contains(js, `"status": "missing-link-target"`) {
		t.Errorf("show --json keeps linked + missing-link-target:\n%s", js)
	}
}

// section returns the indented lines after a heading line of show's output.
func section(out, heading string) string {
	var b strings.Builder
	in := false
	for _, l := range strings.Split(out, "\n") {
		switch {
		case l == heading:
			in = true
		case in && strings.HasPrefix(l, "    "):
			b.WriteString(l + "\n")
		case in:
			in = false
		}
	}
	return b.String()
}
