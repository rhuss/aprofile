package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/testutil"
)

func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(""))
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

// T041/FR-001a: harness classify writes one keyed rule into harnesses.d, replaces
// it on a second run, and the rule merges with the built-in list.
func TestHarnessClassify(t *testing.T) {
	h := testutil.HermeticHome(t)
	dir := filepath.Join(h.ConfigHome, "aprofile", "harnesses.d")
	file := filepath.Join(dir, "claude.yaml")

	out, err := runCLI(t, "harness", "classify", "claude", "RTK.md", "--as", "content", "--dry-run")
	if err != nil || !strings.Contains(out, "RTK.md") {
		t.Fatalf("dry run: %v\n%s", err, out)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("dry run must not write the file")
	}
	if _, err := runCLI(t, "harness", "classify", "claude", "RTK.md", "--as", "content"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "harness", "classify", "claude", "cpr.sh", "--as", "content"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "harness", "classify", "claude", "RTK.md", "--as", "state"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(file)
	if strings.Count(string(data), "RTK.md") != 1 {
		t.Errorf("the rule is upserted by entry:\n%s", data)
	}
	set, _ := harness.Load(dir)
	d, _ := set.Get("claude")
	var rtk, creds *harness.EntryRule
	for i := range d.Classification {
		switch d.Classification[i].Entry {
		case "RTK.md":
			rtk = &d.Classification[i]
		case ".credentials.json":
			creds = &d.Classification[i]
		}
	}
	if rtk == nil || rtk.Layer != harness.LayerState || rtk.Origin != file {
		t.Errorf("RTK.md rule = %+v", rtk)
	}
	if creds == nil {
		t.Error("built-in rules are kept (merge by entry)")
	}
	if _, err := runCLI(t, "harness", "classify", "claude", "x", "--as", "mixed"); err == nil {
		t.Error("an unsupported category is rejected")
	}
	if _, err := runCLI(t, "harness", "classify", "nope", "x", "--as", "content"); err == nil {
		t.Error("an unknown harness is rejected")
	}
}
