package cli

import (
	"testing"

	"github.com/cc-deck/aprofile/internal/tui"
)

// TestUICopyFlagsCarryAuthKeyAndAdopt: the TUI copy form's auth key reference and
// adopt choice reach the copy operation as the --auth-key and --adopt flags would
// (Constitution VI, SC-008), alongside the values the command's own mapping carries.
func TestUICopyFlagsCarryAuthKeyAndAdopt(t *testing.T) {
	f := uiCopyFlags(tui.CopyValues{
		Source: "claude/work", Target: "codex/work",
		Auth: "api-key", AuthKey: "  env:OPENAI_API_KEY ", Adopt: true,
		Label: "W", Overwrite: true, Share: "none",
	})
	if f.authKey != "env:OPENAI_API_KEY" {
		t.Errorf("authKey = %q, want env:OPENAI_API_KEY", f.authKey)
	}
	if !f.adopt {
		t.Error("adopt must be carried")
	}
	if f.auth != "api-key" || f.label != "W" || !f.overwrite || f.share != "none" {
		t.Errorf("command mapping lost values: %+v", f)
	}

	f = uiCopyFlags(tui.CopyValues{Source: "claude/work", Target: "claude/other"})
	if f.authKey != "" || f.adopt {
		t.Errorf("blank form must set neither --auth-key nor --adopt: %+v", f)
	}
}
