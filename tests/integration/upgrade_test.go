package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/testutil"
)

// TestUS6UpgradeSliceOneProfile covers US6, FR-033, FR-034 and SC-009: a profile
// file without share and a config dir laid out by slice one's catch-all keep their
// settings and content links after sync; the leaking state link is reported; an
// entry that is now unclassified stays linked and is reported; nothing is deleted;
// the profile file is not rewritten; show reports all with a hint.
func TestUS6UpgradeSliceOneProfile(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	def := filepath.Join(e.root, ".claude")

	store := filepath.Join(e.root, ".config", "aprofile", "profiles", "claude", "old.yaml")
	body := "harness: claude\nname: old\nauth_kind: oauth-login\n"
	writeFile(t, store, body)
	dir := e.configDir("claude", "old")
	if err := os.MkdirAll(filepath.Join(dir, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"settings.json", "CLAUDE.md", "history.jsonl", "RTK.md"} {
		if err := os.Symlink(filepath.Join(def, name), filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}

	defaultsBefore := snapshotTree(t, def)
	profileBefore := entrySnapshot(t, dir)
	out := e.run(nil, "sync")
	assertTreeUnchanged(t, "default setup", defaultsBefore, snapshotTree(t, def))
	after := entrySnapshot(t, dir)
	for k, v := range profileBefore {
		if after[k] != v {
			t.Errorf("sync changed or deleted the existing entry %s", k)
		}
	}
	for _, name := range []string{"settings.json", "CLAUDE.md", "RTK.md"} {
		target, err := os.Readlink(filepath.Join(dir, name))
		if err != nil || target != filepath.Join(def, name) {
			t.Errorf("%s link must be unchanged after the upgrade: %q %v", name, target, err)
		}
	}
	if !strings.Contains(out, "history.jsonl") || !strings.Contains(out, "leaking-link") {
		t.Errorf("the leaking history.jsonl link is reported:\n%s", out)
	}
	if !strings.Contains(out, "RTK.md") || !strings.Contains(out, "unclassified link") {
		t.Errorf("RTK.md is reported as an unclassified link:\n%s", out)
	}
	if target, err := os.Readlink(filepath.Join(dir, "history.jsonl")); err != nil || target != filepath.Join(def, "history.jsonl") {
		t.Error("sync without --fix-links leaves the leaking link in place")
	}
	if readFile(t, store) != body {
		t.Errorf("the profile file must not be rewritten:\n%s", readFile(t, store))
	}
	for _, name := range []string{"settings.json", "CLAUDE.md", "history.jsonl", "RTK.md"} {
		if _, err := os.Stat(filepath.Join(def, name)); err != nil {
			t.Errorf("default setup entry %s was deleted: %v", name, err)
		}
	}
	show := e.run(nil, "show", "claude/old")
	if !strings.Contains(show, "share level:    all") || !strings.Contains(show, "--share content") {
		t.Errorf("show reports all with a hint:\n%s", show)
	}
	reg := readFile(t, filepath.Join(e.root, ".local", "share", "aprofile", "registry.json"))
	if !strings.Contains(reg, `"share": "all"`) {
		t.Errorf("the registry publishes share all:\n%s", reg)
	}
}

// TestPrimaryMoveKeepsImplicitShare covers FR-033: moving the primary flag off a
// slice-one profile saves it, but does not stamp a share level it never stored.
func TestPrimaryMoveKeepsImplicitShare(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	store := filepath.Join(e.root, ".config", "aprofile", "profiles", "claude", "old.yaml")
	writeFile(t, store, "harness: claude\nname: old\nauth_kind: oauth-login\nprimary: true\n")
	e.run(nil, "add", "claude", "new", "--primary", "--auth-kind", "oauth-login")
	got := readFile(t, store)
	if strings.Contains(got, "primary: true") {
		t.Errorf("the primary flag moved off the old profile:\n%s", got)
	}
	if strings.Contains(got, "share:") {
		t.Errorf("the old profile must not get a stored share level:\n%s", got)
	}
}

// US6, FR-033: a profile from before share levels keeps its own skills directory
// (empty or not) where share level all would link one; sync does not replace it,
// show and doctor report it as information, and a non-share edit does not store a
// level. sync --relink then shares the default setup's skills after a backup.
func TestUpgradeKeepsOwnSkills(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	if err := os.MkdirAll(filepath.Join(e.root, ".claude", "skills", "shared-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"empty", "full"} {
		writeFile(t, e.profileStorePath("claude", n), "harness: claude\nname: "+n+"\nauth_kind: oauth-login\n")
		if err := os.MkdirAll(filepath.Join(e.configDir("claude", n), "skills"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(e.configDir("claude", "full"), "skills", "mine", "SKILL.md"), "mine")

	out := e.run(nil, "sync")
	for _, n := range []string{"empty", "full"} {
		fi, err := os.Lstat(filepath.Join(e.configDir("claude", n), "skills"))
		if err != nil || fi.Mode()&os.ModeSymlink != 0 {
			t.Errorf("%s: the own skills directory must stay a real directory: %v", n, err)
		}
	}
	if !strings.Contains(out, "kept-from-upgrade") {
		t.Errorf("sync reports the kept directory:\n%s", out)
	}
	rep, _ := doctorJSON(t, e)
	for _, f := range rep.Findings {
		if strings.HasPrefix(f.Profile, "claude/") && strings.Contains(f.Message, "skills") && f.Severity == "problem" {
			t.Errorf("a kept own skills directory is not a problem: %+v", f)
		}
	}
	if show := e.run(nil, "show", "claude/full"); !strings.Contains(show, "kept-from-upgrade") {
		t.Errorf("show reports the kept directory:\n%s", show)
	}

	e.run(nil, "edit", "claude/full", "--label", "Full")
	if got := readFile(t, e.profileStorePath("claude", "full")); strings.Contains(got, "share:") {
		t.Errorf("an edit that does not set --share stores no level:\n%s", got)
	}
	e.run(nil, "rename", "claude/empty", "renamed")
	if got := readFile(t, e.profileStorePath("claude", "renamed")); strings.Contains(got, "share:") {
		t.Errorf("rename stores no level:\n%s", got)
	}

	e.run(nil, "sync", "--relink", "--yes")
	if target, err := os.Readlink(filepath.Join(e.configDir("claude", "full"), "skills")); err != nil || target != filepath.Join(e.root, ".claude", "skills") {
		t.Errorf("sync --relink shares the default skills: %q %v", target, err)
	}
	if b := backupsOf(t, e, "claude", "full", "skills"); len(b) != 1 {
		t.Errorf("the own skills are backed up first: %v", b)
	}
}

// US6: slice one's pi agent/settings.json was owned; after the upgrade an all
// profile keeps it (sync does not offer to replace it, doctor reports information).
func TestUpgradeKeepsPiOwnSettings(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "pi")
	writeFile(t, e.profileStorePath("pi", "old"), "harness: pi\nname: old\nauth_kind: login\n")
	own := filepath.Join(e.configDir("pi", "old"), "agent", "settings.json")
	writeFile(t, own, `{"mine":true}`)
	out := e.run(nil, "sync")
	if readFile(t, own) != `{"mine":true}` {
		t.Error("the own settings stay")
	}
	if strings.Contains(out, "replace-with-link") || !strings.Contains(out, "kept-from-upgrade") {
		t.Errorf("sync keeps and reports the own settings:\n%s", out)
	}
	rep, _ := doctorJSON(t, e)
	for _, f := range rep.Findings {
		if f.Profile == "pi/old" && f.Severity == "problem" && strings.Contains(f.Message, "settings.json") {
			t.Errorf("not a problem: %+v", f)
		}
	}
}
