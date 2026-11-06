package profile_test

import (
	"os"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/profile"
)

// T009/FR-033: a profile file without `share` loads as all, and loading never
// rewrites the file.
func TestProfileWithoutShareLoadsAsAll(t *testing.T) {
	store := newStore(t)
	path := store.Path("claude", "old")
	body := "harness: claude\nname: old\nauth_kind: oauth-login\n"
	if err := os.MkdirAll(dirOf(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := store.Load("claude", "old")
	if err != nil {
		t.Fatal(err)
	}
	if p.ShareLevel() != profile.ShareAll {
		t.Errorf("ShareLevel = %q, want all", p.ShareLevel())
	}
	if !p.ShareImplicit {
		t.Error("a profile without a stored level should be marked implicit")
	}
	after, _ := os.ReadFile(path)
	if string(after) != body {
		t.Errorf("Load rewrote the profile file:\n%s", after)
	}
}

func TestShareRoundTripAndValidation(t *testing.T) {
	store := newStore(t)
	p := &profile.Profile{Harness: "claude", Name: "work", Share: profile.ShareContent,
		HookSets: []string{"rtk", "jira"}, ExcludeHookSets: []string{"zz", "aa"}}
	if err := store.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load("claude", "work")
	if err != nil {
		t.Fatal(err)
	}
	if got.Share != profile.ShareContent || got.ShareImplicit {
		t.Errorf("share = %q implicit=%v", got.Share, got.ShareImplicit)
	}
	if strings.Join(got.HookSets, ",") != "jira,rtk" || strings.Join(got.ExcludeHookSets, ",") != "aa,zz" {
		t.Errorf("hook sets not stored sorted: %v %v", got.HookSets, got.ExcludeHookSets)
	}

	bad := &profile.Profile{Harness: "claude", Name: "bad", Share: "most"}
	if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), "share") {
		t.Errorf("unknown share level should be rejected, got %v", err)
	}
	for _, s := range []string{"all", "content", "none"} {
		if !profile.Share(s).Valid() {
			t.Errorf("%s should be valid", s)
		}
	}
}

func dirOf(p string) string {
	i := strings.LastIndex(p, "/")
	return p[:i]
}
