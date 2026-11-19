package cli

import (
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/testutil"
)

// add and remove take one <harness>/<name> argument like every other profile
// command. The legacy two-argument form is a clean break: it fails with a hint
// naming the slash form.
func TestAddTakesSlashProfileRef(t *testing.T) {
	testutil.HermeticHome(t)
	out := mustCLI(t, "add", "claude/work", "--auth-kind", "oauth-login")
	if !strings.Contains(out, "created profile claude/work") {
		t.Fatalf("add claude/work: unexpected output\n%s", out)
	}
}

func TestAddLegacyTwoArgsFailsWithHint(t *testing.T) {
	testutil.HermeticHome(t)
	out, err := runCLIInput(t, "", "add", "claude", "work")
	if err == nil || !strings.Contains(err.Error(), `did you mean "aprofile add claude/work"?`) {
		t.Fatalf("add claude work: err %v\n%s", err, out)
	}
}

func TestRemoveTakesSlashProfileRef(t *testing.T) {
	testutil.HermeticHome(t)
	mustCLI(t, "add", "claude/work", "--auth-kind", "oauth-login")
	out := mustCLI(t, "remove", "claude/work")
	if !strings.Contains(out, "removed profile claude/work") {
		t.Fatalf("remove claude/work: unexpected output\n%s", out)
	}
}

func TestRemoveLegacyTwoArgsFailsWithHint(t *testing.T) {
	testutil.HermeticHome(t)
	mustCLI(t, "add", "claude/work", "--auth-kind", "oauth-login")
	out, err := runCLIInput(t, "", "remove", "claude", "work")
	if err == nil || !strings.Contains(err.Error(), `did you mean "aprofile remove claude/work"?`) {
		t.Fatalf("remove claude work: err %v\n%s", err, out)
	}
}
