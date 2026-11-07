package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func prefixErrors(s *Set) []LoadError {
	var out []LoadError
	for _, e := range s.Errors() {
		if e.Field == "wrapper_prefix" && strings.Contains(e.Msg, "more than one harness") {
			out = append(out, e)
		}
	}
	return out
}

// T069, FR-002: a wrapper_prefix declared by two definitions is a load error that
// names both harnesses; the definitions stay usable.
func TestDuplicateWrapperPrefixIsLoadError(t *testing.T) {
	set := LoadFromBytes(map[string][]byte{
		"a.yaml": []byte("name: alpha\nbinary: a\nwrapper_prefix: same\nconfig_env:\n  A_DIR: \".\"\n"),
		"b.yaml": []byte("name: beta\nbinary: b\nwrapper_prefix: same\nconfig_env:\n  B_DIR: \".\"\n"),
		"c.yaml": []byte("name: gamma\nbinary: c\nwrapper_prefix: other\nconfig_env:\n  C_DIR: \".\"\n"),
	})
	errs := prefixErrors(set)
	if len(errs) != 1 {
		t.Fatalf("want one prefix clash error, got %v (all: %v)", errs, set.Errors())
	}
	if !strings.Contains(errs[0].Msg, "alpha") || !strings.Contains(errs[0].Msg, "beta") || !strings.Contains(errs[0].Msg, `"same"`) {
		t.Errorf("error should name the prefix and both harnesses: %s", errs[0].Error())
	}
	if _, ok := set.Get("alpha"); !ok {
		t.Errorf("clashing definitions stay in the set")
	}
}

// An override in harnesses.d that reuses a built-in's prefix is reported against
// the user file.
func TestOverrideDuplicatePrefixAgainstBuiltin(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mine.yaml"),
		[]byte("name: mine\nbinary: mine\nwrapper_prefix: codex\nconfig_env:\n  M_DIR: \".\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	set, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	errs := prefixErrors(set)
	if len(errs) != 1 || !strings.Contains(errs[0].File, "mine.yaml") {
		t.Fatalf("want one clash error naming mine.yaml, got %v", set.Errors())
	}
	clean, _ := Load(t.TempDir())
	if errs := prefixErrors(clean); len(errs) != 0 {
		t.Fatalf("built-ins must not clash: %v", errs)
	}
}

func TestWrapperClash(t *testing.T) {
	set := LoadFromBytes(map[string][]byte{
		"a.yaml": []byte("name: claude\nbinary: a\nwrapper_prefix: claude\nconfig_env:\n  A_DIR: \".\"\n"),
		"b.yaml": []byte("name: cx\nbinary: b\nwrapper_prefix: claude-x\nconfig_env:\n  B_DIR: \".\"\n"),
	})
	others := []ProfileKey{{"claude", "x-a"}, {"cx", "b"}, {"ghost", "a"}}
	if got, ok := set.WrapperClash("cx", "a", others, ""); !ok || got.ID() != "claude/x-a" {
		t.Errorf("cx/a should clash with claude/x-a, got %v %v", got, ok)
	}
	if _, ok := set.WrapperClash("cx", "a", others, "claude/x-a"); ok {
		t.Errorf("the excepted profile must not be reported")
	}
	if _, ok := set.WrapperClash("claude", "x-a", others, ""); ok {
		t.Errorf("a profile never clashes with itself")
	}
	if _, ok := set.WrapperClash("cx", "c", others, ""); ok {
		t.Errorf("no clash expected for cx/c")
	}
	if name, ok := set.WrapperName("cx", "a"); !ok || name != "claude-x-a" {
		t.Errorf("WrapperName = %q %v", name, ok)
	}
}
