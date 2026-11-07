package wrapper_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/wrapper"
)

// preDef is a data-only harness whose binary echoes a marker and whose exec hook
// declares pre-launch.
func preDef(t *testing.T, ops string) *harness.Definition {
	t.Helper()
	src := "name: prel\nbinary: prelbin\nwrapper_prefix: prel\nconfig_env:\n  PREL_DIR: \".\"\nexec_hook:\n  operations: [" + ops + "]\n"
	set := harness.LoadFromBytes(map[string][]byte{"prel.yaml": []byte(src)})
	d, ok := set.Get("prel")
	if !ok {
		t.Fatalf("load: %v", set.Errors())
	}
	return d
}

// runPrel renders and runs the wrapper with a scripts dir first on PATH; hook is
// the pre-launch hook script ("" installs none). It returns output and exit error.
func runPrel(t *testing.T, def *harness.Definition, hook string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "cfg")
	scripts := filepath.Join(dir, "scripts")
	for _, d := range []string{cfg, scripts} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(scripts, "prelbin"), []byte("#!/bin/sh\necho PRELBIN_RAN\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if hook != "" {
		if err := os.WriteFile(filepath.Join(scripts, "aprofile-harness-prel"), []byte(hook), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	p := &profile.Profile{Harness: "prel", Name: "demo"}
	s, err := wrapper.Render(p, def, cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "w")
	if err := os.WriteFile(path, []byte(s), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", path)
	cmd.Env = []string{"PATH=" + scripts + ":/usr/bin:/bin"}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// T076: a declared pre-launch hook whose executable is missing stops the wrapper
// before the harness starts, naming the hook (FR-011).
func TestPreLaunchMissingHookStopsWrapper(t *testing.T) {
	out, err := runPrel(t, preDef(t, "pre-launch"), "")
	if err == nil {
		t.Fatalf("the wrapper must fail when the declared pre-launch hook is missing:\n%s", out)
	}
	if strings.Contains(out, "PRELBIN_RAN") {
		t.Errorf("the harness must not start:\n%s", out)
	}
	if !strings.Contains(out, "aprofile-harness-prel") || !strings.Contains(out, "pre-launch") {
		t.Errorf("the error should name the hook and the operation:\n%s", out)
	}
}

func TestPreLaunchHookOutcomes(t *testing.T) {
	cases := []struct {
		name, hook string
		runs       bool
	}{
		{"ok", "#!/bin/sh\ncat >/dev/null\nexit 0\n", true},
		{"unsupported", "#!/bin/sh\nexit 64\n", true},
		{"fails", "#!/bin/sh\nexit 3\n", false},
	}
	for _, c := range cases {
		out, err := runPrel(t, preDef(t, "pre-launch"), c.hook)
		if got := strings.Contains(out, "PRELBIN_RAN"); got != c.runs || (err == nil) != c.runs {
			t.Errorf("%s: runs=%v err=%v\n%s", c.name, got, err, out)
		}
	}
}

// A definition whose hook does not declare pre-launch never looks for the hook.
func TestPreLaunchUndeclaredNeedsNoHook(t *testing.T) {
	out, err := runPrel(t, preDef(t, "auth-status"), "")
	if err != nil || !strings.Contains(out, "PRELBIN_RAN") {
		t.Errorf("undeclared pre-launch must not need the hook: %v\n%s", err, out)
	}
}
