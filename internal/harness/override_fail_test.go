package harness_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
)

// T068/FR-022: an override of a built-in that fails to load must not leave the
// pure built-in in the set, or sync would render that harness's wrappers without
// the user's override (an audit saw `exec 'claude2'` rewritten to `exec 'claude'`).
// The other harnesses stay usable (FR-012).
func TestFailedOverrideRemovesBuiltin(t *testing.T) {
	cases := map[string]map[string]string{
		"invalid field": {
			"claude.yaml": "name: claude\nbinary: claude2\nwrapper_prefix: \"Not Valid\"\n",
		},
		"unparseable file named after the harness": {
			"claude.yaml": "name: claude\nbinary: [unclosed\n",
		},
		"duplicate override of a built-in": {
			"a.yaml": "name: claude\nbinary: claude2\n",
			"b.yaml": "name: claude\nbinary: claude3\n",
		},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			for f, content := range files {
				if err := os.WriteFile(filepath.Join(dir, f), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			set, err := harness.Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(set.Errors()) == 0 {
				t.Fatal("expected a load error")
			}
			if _, ok := set.Get("claude"); ok {
				t.Error("a failed override must not leave the built-in claude definition in the set")
			}
			if _, ok := set.Get("codex"); !ok {
				t.Error("other built-ins must stay available")
			}
		})
	}
}

// An unparseable file whose name matches no harness changes nothing else.
func TestUnparseableUnrelatedFileKeepsBuiltins(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.yaml"), []byte("[unclosed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	set, _ := harness.Load(dir)
	if len(set.Errors()) == 0 {
		t.Fatal("expected a load error")
	}
	for _, n := range []string{"claude", "codex"} {
		if _, ok := set.Get(n); !ok {
			t.Errorf("built-in %s must stay available", n)
		}
	}
	joined := ""
	for _, e := range set.Errors() {
		joined += e.Error()
	}
	if !strings.Contains(joined, "notes.yaml") {
		t.Errorf("error should name the file: %s", joined)
	}
}
