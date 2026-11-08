package integration

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestScenario9UIParity covers US7 / FR-043 / Scenario 9: a form-driven add produces
// the same stored profile as the equivalent flag command, and add/edit without the
// required values on a non-TTY fail naming the missing flags and never block on a form.
//
// The form path is exercised through its shared apply path in the cli package's
// white-box tests (it cannot be driven without a real terminal); here we assert the
// behavior observable from the built binary: the flag command stores exactly the values
// a form would collect, and the non-interactive failure modes.

// TestAddMissingAuthKeyNonTTYFails: add for an env-var auth kind with no --auth-key, on
// a non-TTY, errors naming --auth-key and never opens a form (it returns promptly).
func TestAddMissingAuthKeyNonTTYFails(t *testing.T) {
	e := newEnv(t)
	out, err := e.tryRun(nil, "add", "claude", "work")
	if err == nil {
		t.Fatalf("add without --auth-key on a non-TTY should fail, not open a form:\n%s", out)
	}
	if !strings.Contains(out, "--auth-key") {
		t.Errorf("error should name the missing --auth-key flag:\n%s", out)
	}
}

// TestAddInteractiveKindNonTTYSucceeds: an interactive auth kind needs no key, so add
// still succeeds on a non-TTY (no required value is missing).
func TestAddInteractiveKindNonTTYSucceeds(t *testing.T) {
	e := newEnv(t)
	out, err := e.tryRun(nil, "add", "claude", "login", "--auth-kind", "oauth-login")
	if err != nil {
		t.Fatalf("add with an interactive auth kind should succeed on a non-TTY:\n%s\n%v", out, err)
	}
}

// TestEditNothingNonTTYFails: a bare edit (no field flags) on a non-TTY errors naming
// the flags instead of opening a form.
func TestEditNothingNonTTYFails(t *testing.T) {
	e := newEnv(t)
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-token", "--auth-key", "env:TOK")
	out, err := e.tryRun(nil, "edit", "claude/work")
	if err == nil {
		t.Fatalf("bare edit on a non-TTY should fail, not open a form:\n%s", out)
	}
	if !strings.Contains(out, "nothing to change") || !strings.Contains(out, "--model") {
		t.Errorf("error should say nothing to change and name the flags:\n%s", out)
	}
}

// TestEditWithFlagNonTTYSucceeds: an edit that sets a flag applies non-interactively.
func TestEditWithFlagNonTTYSucceeds(t *testing.T) {
	e := newEnv(t)
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-token", "--auth-key", "env:TOK")
	if out, err := e.tryRun(nil, "edit", "claude/work", "--label", "Renamed"); err != nil {
		t.Fatalf("edit with a flag should succeed on a non-TTY:\n%s\n%v", out, err)
	}
	js := e.run(nil, "show", "claude/work", "--json")
	if !strings.Contains(js, `"label": "Renamed"`) {
		t.Errorf("edit did not apply the label:\n%s", js)
	}
}

// TestUIRequiresTerminal: aprofile ui off a terminal errors cleanly rather than hanging.
func TestUIRequiresTerminal(t *testing.T) {
	e := newEnv(t)
	out, err := e.tryRun(nil, "ui")
	if err == nil {
		t.Fatalf("ui should require a terminal:\n%s", out)
	}
	if !strings.Contains(out, "terminal") {
		t.Errorf("ui error should mention a terminal:\n%s", out)
	}
}

// TestFlagAddStoresFormValues: the values a form collects, fed as the equivalent flags,
// store exactly as entered. Combined with the cli package's shared-path parity test,
// this proves form-add equals the flag command (SC-008).
func TestFlagAddStoresFormValues(t *testing.T) {
	e := newEnv(t)
	e.run(nil, "add", "claude", "work",
		"--label", "Work", "--color", "blue", "--icon", "W",
		"--model", "m1", "--endpoint", "https://ep",
		"--auth-kind", "oauth-token", "--auth-key", "env:TOK", "--herdr-agent", "coder")

	js := e.run(nil, "show", "claude/work", "--json")
	var p struct {
		Label      string            `json:"label"`
		Color      string            `json:"color"`
		Icon       string            `json:"icon"`
		Model      string            `json:"model"`
		Endpoint   string            `json:"endpoint"`
		AuthKind   string            `json:"auth_kind"`
		AuthRefs   map[string]string `json:"auth_refs"`
		HerdrAgent string            `json:"herdr_agent"`
	}
	if err := json.Unmarshal([]byte(js), &p); err != nil {
		t.Fatalf("show --json not parseable: %v\n%s", err, js)
	}
	if p.Label != "Work" || p.Color != "blue" || p.Icon != "W" || p.Model != "m1" ||
		p.Endpoint != "https://ep" || p.AuthKind != "oauth-token" || p.HerdrAgent != "coder" {
		t.Errorf("stored profile does not match the entered values: %+v", p)
	}
	if p.AuthRefs["CLAUDE_CODE_OAUTH_TOKEN"] != "env:TOK" {
		t.Errorf("auth ref not stored as a reference: %+v", p.AuthRefs)
	}
}

// spec 003 T034: the group a form collects is stored exactly as --group stores it.
func TestAddGroupFlagStoresGroup(t *testing.T) {
	e := newEnv(t)
	writeFile(t, filepath.Join(e.root, ".claude", "projects", "p", "s.jsonl"), "{}")
	e.run(nil, "add", "claude", "grp", "--auth-kind", "oauth-login", "--group", "home")
	if !strings.Contains(readFile(t, e.profileStorePath("claude", "grp")), "history_group: home") {
		t.Error("add --group home did not store history_group")
	}
}
