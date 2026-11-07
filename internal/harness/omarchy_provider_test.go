package harness_test

import (
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
)

// omarchy_provider names the Omarchy accounts file the registry export writes
// (<dir>/<provider>.json), so the loader accepts only a plain name: a value with a
// path separator or ".." could make the export write outside its directory.
func TestOmarchyProviderMustBeAName(t *testing.T) {
	const base = "name: toy\nbinary: toy\nwrapper_prefix: toy\nconfig_env: { TOY_HOME: \".\" }\n"
	for _, bad := range []string{"../evil", "a/b", "/abs", "UPPER", "x", "has space", ".."} {
		set := harness.LoadFromBytes(map[string][]byte{"toy.yaml": []byte(base + "omarchy_provider: \"" + bad + "\"\n")})
		errs := set.Errors()
		if len(errs) == 0 {
			t.Errorf("omarchy_provider %q should be rejected", bad)
			continue
		}
		if !strings.Contains(errs[0].Error(), "omarchy_provider") {
			t.Errorf("omarchy_provider %q: error should name the field: %v", bad, errs[0])
		}
	}
	set := harness.LoadFromBytes(map[string][]byte{"toy.yaml": []byte(base + "omarchy_provider: anthropic\n")})
	if errs := set.Errors(); len(errs) != 0 {
		t.Errorf("a plain provider name must load: %v", errs)
	}
}
