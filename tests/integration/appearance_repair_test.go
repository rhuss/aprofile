package integration

import (
	"os"
	"strings"
	"testing"
)

// A profile file with a color or icon that fails the FR-001 checks (written before
// the checks existed, or hand edited) still loads: list and sync work, stderr warns
// once, and edit repairs it.
func TestInvalidAppearanceOnLoadWarnsAndIsRepairable(t *testing.T) {
	e := newEnv(t)
	e.run(nil, "add", "claude", "work", "--auth-kind", "oauth-login", "--color", "blue")
	path := e.profileStorePath("claude", "work")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "color: blue", "color: not-a-color", 1))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := e.tryRunSplit(nil, "list")
	if err != nil {
		t.Fatalf("list must not fail on an invalid color: %v\n%s%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "work") {
		t.Errorf("list should show the profile:\n%s", stdout)
	}
	if strings.Count(stderr, "not-a-color") != 1 || !strings.Contains(stderr, "aprofile edit claude/work --color") {
		t.Errorf("stderr should warn once with the repair command:\n%s", stderr)
	}
	if _, stderr, err := e.tryRunSplit(nil, "sync"); err != nil {
		t.Fatalf("sync must not fail on an invalid color: %v\n%s", err, stderr)
	}

	e.run(nil, "edit", "claude/work", "--color", "red")
	if got := readFile(t, path); !strings.Contains(got, "color: red") {
		t.Errorf("edit should repair the color:\n%s", got)
	}
	if _, stderr, _ := e.tryRunSplit(nil, "list"); strings.Contains(stderr, "warning") {
		t.Errorf("no warning after the repair:\n%s", stderr)
	}
}
