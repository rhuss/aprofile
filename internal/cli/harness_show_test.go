package cli

import (
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/testutil"
)

// TestHarnessShowLoginAndClears: the text form of `harness show` prints the
// login_command (as the command line it runs) and auth_clears, not only --json.
func TestHarnessShowLoginAndClears(t *testing.T) {
	testutil.HermeticHome(t)

	out, err := runCLI(t, "harness", "show", "claude")
	if err != nil {
		t.Fatalf("harness show claude: %v\n%s", err, out)
	}
	for _, want := range []string{
		"login_command:", "claude auth login",
		"auth_clears:", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_BEDROCK",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("harness show claude missing %q:\n%s", want, out)
		}
	}

	// pi declares an empty args list and a note: the bare binary plus the note.
	out, err = runCLI(t, "harness", "show", "pi")
	if err != nil {
		t.Fatalf("harness show pi: %v\n%s", err, out)
	}
	for _, want := range []string{"login_command:", "login_note:", "/login"} {
		if !strings.Contains(out, want) {
			t.Errorf("harness show pi missing %q:\n%s", want, out)
		}
	}
}

// spec 003 T033: harness show lists the history entries with their merge mode and
// the history note, or says the harness does not support history groups.
func TestHarnessShowHistoryEntries(t *testing.T) {
	testutil.HermeticHome(t)
	out, err := runCLI(t, "harness", "show", "claude")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"history entries", "projects/", "per-file", "history.jsonl", "none"} {
		if !strings.Contains(out, want) {
			t.Errorf("harness show claude missing %q:\n%s", want, out)
		}
	}
	out, _ = runCLI(t, "harness", "show", "pi")
	if !strings.Contains(out, "agent/sessions/") || !strings.Contains(out, "migrates session format") {
		t.Errorf("harness show pi lacks history entries or note:\n%s", out)
	}
	out, _ = runCLI(t, "harness", "show", "opencode")
	if !strings.Contains(out, "history groups not supported") {
		t.Errorf("harness show opencode:\n%s", out)
	}
}
