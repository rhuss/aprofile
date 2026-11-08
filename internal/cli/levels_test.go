package cli

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/cc-deck/aprofile/internal/profile"
)

// T004/FR-014: one description per level, at most 80 characters; the all
// description says logins and history stay private.
func TestLevelDescriptions(t *testing.T) {
	for _, s := range profile.Shares {
		d := levelDescription(s)
		if d == "" {
			t.Errorf("level %s has no description", s)
		}
		if n := utf8.RuneCountInString(d); n > 80 {
			t.Errorf("level %s description is %d characters (max 80): %q", s, n, d)
		}
	}
	all := levelDescription(profile.ShareAll)
	if !strings.Contains(all, "logins") || !strings.Contains(all, "history") {
		t.Errorf("all description must mention logins and history: %q", all)
	}
	if len(useCases) != 7 {
		t.Errorf("use-case table has %d rows, want 7", len(useCases))
	}
	for _, uc := range useCases {
		if !profile.Share(uc.Recommended).Valid() {
			t.Errorf("%s recommends unknown level %q", uc.ID, uc.Recommended)
		}
	}
	for _, s := range profile.Shares {
		if len(useCasesFor(s)) == 0 {
			t.Errorf("no use case recommends %s", s)
		}
	}
}

// spec 003 (FR-026, plan Design Decision 7): logins are always private; history and
// memory only in a history group.
func TestAlwaysPrivateMentionsHistoryGroups(t *testing.T) {
	if alwaysPrivate != "logins are always private; history and memory are private unless you put profiles in a history group" {
		t.Errorf("alwaysPrivate = %q", alwaysPrivate)
	}
	if len(historyUseCases) != 4 {
		t.Errorf("history use cases = %d, want 4", len(historyUseCases))
	}
}
