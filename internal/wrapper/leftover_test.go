package wrapper_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/testutil"
	"github.com/cc-deck/aprofile/internal/wrapper"
)

// FR-006b: a marked wrapper is a leftover when no profile owns it under that name;
// unmarked files never are, and a harness whose definition is missing keeps its
// wrappers while a profile names it (as sync does).
func TestLeftovers(t *testing.T) {
	testutil.HermeticHome(t)
	set, err := harness.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	def, _ := set.Get("claude")
	bin := t.TempDir()
	write := func(name, h, n string) {
		p := &profile.Profile{Harness: h, Name: n, AuthKind: "oauth-login"}
		content, err := wrapper.Render(p, def, "/tmp/x")
		if err != nil {
			t.Fatal(err)
		}
		testutil.WriteFile(t, filepath.Join(bin, name), content)
	}
	write("claude-work", "claude", "work")     // owned
	write("claude-gone", "claude", "gone")     // removed profile
	write("old-prefix-work", "claude", "work") // owned profile, but under another name
	write("toy-a", "toy", "a")                 // no definition, but profile toy/b names the harness
	write("ghost-a", "ghost", "a")             // no definition, no profile
	testutil.WriteFile(t, filepath.Join(bin, "mytool"), "#!/bin/sh\necho mine\n")

	keys := []harness.ProfileKey{{Harness: "claude", Name: "work"}, {Harness: "toy", Name: "b"}}
	got, err := wrapper.LeftoversFor(bin, set, keys)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"claude-gone", "ghost-a", "old-prefix-work"}; !reflect.DeepEqual(got, want) {
		t.Errorf("leftovers = %v, want %v", got, want)
	}
	if got, err := wrapper.LeftoversFor(filepath.Join(bin, "missing"), set, keys); err != nil || got != nil {
		t.Errorf("a missing bin dir has no leftovers: %v %v", got, err)
	}
}
