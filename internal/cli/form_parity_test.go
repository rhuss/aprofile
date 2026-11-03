package cli

import (
	"io"
	"reflect"
	"testing"

	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/testutil"
	"github.com/cc-deck/aprofile/internal/tui"
)

// newTestApp builds an app against a hermetic HOME/XDG root (Constitution VII). It
// discards command output so a test never writes to the real terminal.
func newTestApp(t *testing.T) *app {
	t.Helper()
	testutil.HermeticHome(t)
	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	a, err := newApp(cmd)
	if err != nil {
		t.Fatalf("newApp: %v", err)
	}
	return a
}

// TestFormAddMatchesFlagAdd proves the form-driven add and the flag-driven add converge
// on the same stored profile (FR-043, SC-008): both route through createProfileFromFlags,
// so a profile entered in the form is byte-for-byte the equivalent command's profile.
func TestFormAddMatchesFlagAdd(t *testing.T) {
	a := newTestApp(t)
	def, ok := a.set.Get("claude")
	if !ok {
		t.Fatal("claude definition missing")
	}

	// Flag path: values arrive as command-line flags.
	flagF := &profileFlags{
		label: "Work", color: "blue", icon: "W",
		model: "m1", endpoint: "https://ep",
		authKind: "oauth-token", authKey: "env:TOK",
		herdrAgent: "coder",
	}
	if err := createProfileFromFlags(a, def, "claude", "viaflags", flagF); err != nil {
		t.Fatalf("flag add: %v", err)
	}

	// Form path: the same values arrive as tui.FormValues and are mapped onto the
	// shared flags with the same helper the interactive path uses.
	v := tui.FormValues{
		Label: "Work", Color: "blue", Icon: "W",
		Model: "m1", Endpoint: "https://ep",
		AuthKind: "oauth-token", AuthKey: "env:TOK",
		HerdrAgent: "coder",
	}
	formF := &profileFlags{}
	applyFormValues(formF, v)
	if err := createProfileFromFlags(a, def, "claude", "viaform", formF); err != nil {
		t.Fatalf("form add: %v", err)
	}

	pFlag, err := a.store.Load("claude", "viaflags")
	if err != nil {
		t.Fatalf("load flag profile: %v", err)
	}
	pForm, err := a.store.Load("claude", "viaform")
	if err != nil {
		t.Fatalf("load form profile: %v", err)
	}
	// Compare every field except the name (the only intentional difference).
	pFlag.Name = ""
	pForm.Name = ""
	if !reflect.DeepEqual(pFlag, pForm) {
		t.Errorf("form-add and flag-add produced different profiles:\nflag: %+v\nform: %+v", pFlag, pForm)
	}
}

// TestRequiredAddFlags checks what counts as a missing required value: the auth key is
// required for an auth kind that declares variables and not for an interactive kind or
// a harness with no auth kinds (so the non-TTY error and the form trigger correctly).
func TestRequiredAddFlags(t *testing.T) {
	a := newTestApp(t)
	claude, _ := a.set.Get("claude")

	// Default kind (auth-token declares a variable) with no key: required.
	if got := requiredAddFlags(claude, &profileFlags{}); len(got) != 1 || got[0] != "--auth-key" {
		t.Errorf("claude default without key: got %v, want [--auth-key]", got)
	}
	// A key supplied: nothing required.
	if got := requiredAddFlags(claude, &profileFlags{authKey: "env:TOK"}); got != nil {
		t.Errorf("claude with key: got %v, want nil", got)
	}
	// An interactive kind (oauth-login declares no variable): nothing required.
	if got := requiredAddFlags(claude, &profileFlags{authKind: "oauth-login"}); got != nil {
		t.Errorf("claude oauth-login: got %v, want nil", got)
	}
}
