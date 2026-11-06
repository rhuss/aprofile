package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/testutil"
)

type doctorReport struct {
	Findings []struct {
		Profile  string `json:"profile"`
		Severity string `json:"severity"`
		Message  string `json:"message"`
		Fix      string `json:"fix"`
	} `json:"findings"`
}

func doctorJSON(t *testing.T, e *env) (doctorReport, error) {
	t.Helper()
	out, _, err := e.tryRunSplit(nil, "doctor", "--json")
	var r doctorReport
	if jerr := json.Unmarshal([]byte(out), &r); jerr != nil {
		t.Fatalf("doctor --json: %v\n%s", jerr, out)
	}
	return r, err
}

// TestUS5LinkReplacedByFile covers US5 scenario 1, FR-020 and FR-021: doctor reports
// a linked entry that became a real file; sync keeps it; sync --relink backs it up
// and relinks after confirmation.
func TestUS5LinkReplacedByFile(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "claude")
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login", "--share", "all")
	e.run(nil, "sync")
	settings := filepath.Join(e.configDir("claude", "work"), "settings.json")
	_ = os.Remove(settings)
	writeFile(t, settings, `{"rewritten":"by the harness"}`)

	rep, err := doctorJSON(t, e)
	found := false
	for _, f := range rep.Findings {
		if f.Profile == "claude/work" && strings.Contains(f.Message, "stopped sharing settings.json") {
			found = true
			if f.Severity != "problem" || !strings.Contains(f.Fix, "--relink") {
				t.Errorf("finding = %+v", f)
			}
		}
	}
	if !found || err == nil {
		t.Errorf("doctor should report the replaced link as a problem (err=%v): %+v", err, rep.Findings)
	}

	out := e.run(nil, "sync")
	if !strings.Contains(out, "replaces profile data") || !strings.Contains(out, "--relink") {
		t.Errorf("sync reports the entry and names --relink:\n%s", out)
	}
	if fi, _ := os.Lstat(settings); fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("sync must never relink a real file by itself")
	}
	if out, err := e.tryRun(nil, "sync", "--relink"); exitCodeOf(err) != 3 {
		t.Fatalf("relink off a TTY without --yes: exit %d, want 3\n%s", exitCodeOf(err), out)
	}
	out = e.run(nil, "sync", "--relink", "--yes")
	if !strings.Contains(out, "replace-with-link settings.json") || !strings.Contains(out, "backed up to") {
		t.Errorf("relink shows the change and the backup:\n%s", out)
	}
	if fi, _ := os.Lstat(settings); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("settings.json should be relinked")
	}
	backups, _ := filepath.Glob(filepath.Join(e.root, ".local", "share", "aprofile", "backups", "claude", "work", "*", "settings.json"))
	if len(backups) != 1 || !strings.Contains(readFile(t, backups[0]), "rewritten") {
		t.Errorf("the real file is backed up first: %v", backups)
	}
}

// TestUS5HomeEscapesAndInfo covers US5 scenarios 2 and 4: $HOME escapes and
// differing settings are information, listed by show and doctor, and do not make
// doctor fail.
func TestUS5HomeEscapesAndInfo(t *testing.T) {
	e := newEnv(t)
	testutil.SeedDefaultsAt(t, e.root, "opencode")
	testutil.SeedDefaultsAt(t, e.root, "claude")
	e.run(nil, "add", "opencode", "client", "--auth-kind", "login", "--share", "none")
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login", "--share", "content")
	e.run(nil, "sync")
	writeFile(t, filepath.Join(e.configDir("claude", "work"), "settings.json"), `{"mine":true}`)

	show := e.run(nil, "show", "opencode/client")
	for _, want := range []string{"~/.agents/skills", "~/.claude/skills", "no share level can separate this"} {
		if !strings.Contains(show, want) {
			t.Errorf("show should list the $HOME escapes (%q):\n%s", want, show)
		}
	}
	rep, _ := doctorJSON(t, e)
	for _, f := range rep.Findings {
		if (f.Profile == "opencode/client" || f.Profile == "claude/work") && f.Severity == "problem" &&
			(strings.Contains(f.Message, "skills") || strings.Contains(f.Message, "settings differ")) {
			t.Errorf("escapes and differing settings are information, not problems: %+v", f)
		}
	}
	var escapes, differ int
	for _, f := range rep.Findings {
		if f.Profile == "opencode/client" && strings.Contains(f.Message, "~/.agents/skills") && f.Severity == "info" {
			escapes++
		}
		if f.Profile == "claude/work" && strings.Contains(f.Message, "settings differ") && f.Severity == "info" && strings.Contains(f.Fix, "settings pull") {
			differ++
		}
		if f.Severity == "info" && f.Profile != "" && !strings.HasPrefix(f.Profile, "opencode/") && !strings.HasPrefix(f.Profile, "claude/") {
			t.Errorf("unexpected info finding %+v", f)
		}
	}
	if escapes != 1 || differ != 1 {
		t.Errorf("doctor info findings: escapes=%d differ=%d\n%+v", escapes, differ, rep.Findings)
	}
}
