package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/testutil"
)

// M13: when the exec hook's env operation fails, list shows the wrapper status as
// unknown and says why once per harness on stderr, instead of once per profile or
// not at all.
func TestListExplainsUnknownWrapperStatusOncePerHarness(t *testing.T) {
	h := testutil.HermeticHome(t)
	t.Setenv("PATH", "/usr/bin:/bin")
	for _, n := range []string{"one", "two"} {
		if out, err := runCLI(t, "add", "claude", n, "--auth-kind", "oauth-login"); err != nil {
			t.Fatalf("add %s: %v\n%s", n, err, out)
		}
	}
	if out, err := runCLI(t, "sync"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}

	testutil.WriteFile(t, filepath.Join(h.ConfigHome, "aprofile", "harnesses.d", "claude.yaml"),
		"name: claude\nexec_hook:\n  operations: [env]\n")
	installCLIHook(t, "claude", "#!/bin/sh\ncat >/dev/null\necho 'hook broke' >&2\nexit 3\n")

	for _, args := range [][]string{{"list"}, {"list", "--json"}} {
		out, err := runCLI(t, args...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		if n := strings.Count(out, "wrapper status of claude profiles is unknown"); n != 1 {
			t.Errorf("%v: the reason should be printed once per harness, got %d times:\n%s", args, n, out)
		}
		if !strings.Contains(out, "hook broke") || !strings.Contains(out, "env") {
			t.Errorf("%v: the reason should name the hook's error:\n%s", args, out)
		}
	}
	out, _ := runCLI(t, "list")
	if n := strings.Count(out, "[unknown]"); n != 2 {
		t.Errorf("both profiles should show [unknown], got %d:\n%s", n, out)
	}
}
