package profile_test

import (
	"os"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/profile"
)

func TestValidateColor(t *testing.T) {
	for _, ok := range []string{"", "blue", "Blue", "brightmagenta", "grey", "#c05ae2", "#C05AE2"} {
		if err := profile.ValidateColor(ok); err != nil {
			t.Errorf("ValidateColor(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"#fff", "#gggggg", "#c05ae2ff", "purple-ish", "12", "c05ae2"} {
		if err := profile.ValidateColor(bad); err == nil {
			t.Errorf("ValidateColor(%q) = nil, want error", bad)
		}
	}
}

// TestANSIColorCode covers T088/FR-042: profile keeps the one table of ANSI color
// names and codes. ValidateColor accepts a name exactly when the table has a code
// for it, so validation and rendering cannot drift apart.
func TestANSIColorCode(t *testing.T) {
	codes := map[string]string{
		"black": "0", "red": "1", "green": "2", "yellow": "3",
		"blue": "4", "magenta": "5", "cyan": "6", "white": "7",
		"brightblack": "8", "brightred": "9", "brightgreen": "10", "brightyellow": "11",
		"brightblue": "12", "brightmagenta": "13", "brightcyan": "14", "brightwhite": "15",
		"gray": "8", "grey": "8",
		"Red": "1", "BRIGHTCYAN": "14", "Grey": "8", // case insensitive
	}
	for name, want := range codes {
		got, ok := profile.ANSIColorCode(name)
		if !ok || got != want {
			t.Errorf("ANSIColorCode(%q) = %q, %v; want %q, true", name, got, ok, want)
		}
		if err := profile.ValidateColor(name); err != nil {
			t.Errorf("ValidateColor(%q) = %v, want nil for a name with a code", name, err)
		}
	}
	for _, none := range []string{"", "purple", "#ff0000", "12", "bright"} {
		if code, ok := profile.ANSIColorCode(none); ok {
			t.Errorf("ANSIColorCode(%q) = %q, true; want no code", none, code)
		}
		if !strings.HasPrefix(none, "#") && none != "" && profile.ValidateColor(none) == nil {
			t.Errorf("ValidateColor(%q) = nil for a name without a code", none)
		}
	}
}

func TestValidateIcon(t *testing.T) {
	for _, ok := range []string{"", "W", "☁", "\U0001F680", "\U0001F468‍\U0001F4BB", "é"} {
		if err := profile.ValidateIcon(ok); err != nil {
			t.Errorf("ValidateIcon(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"WX", "\U0001F680\U0001F680", "ab"} {
		if err := profile.ValidateIcon(bad); err == nil {
			t.Errorf("ValidateIcon(%q) = nil, want error", bad)
		}
	}
}

// FR-001: Validate checks color and icon, so save and load both reject them.
func TestValidateRejectsBadColorAndIcon(t *testing.T) {
	store := newStore(t)
	for _, p := range []*profile.Profile{
		{Harness: "claude", Name: "work", Color: "notacolor"},
		{Harness: "claude", Name: "work", Icon: "two"},
	} {
		err := store.Save(p)
		if err == nil {
			t.Fatalf("Save(%+v) should fail", p)
		}
		if !strings.Contains(err.Error(), "claude/work") {
			t.Errorf("error should name the profile: %v", err)
		}
	}
}

// A color or icon that fails validation on load (a file written before the check
// existed, or a hand edit) is ignored with a warning instead of failing the load, so
// the profile still lists, syncs and can be repaired with edit. Save stays strict
// and drops the ignored value.
func TestHandEditedBadAppearanceIgnoredOnLoad(t *testing.T) {
	store := newStore(t)
	var warned []string
	store.Warn = func(msg string) { warned = append(warned, msg) }
	if err := store.Save(&profile.Profile{Harness: "claude", Name: "work", Color: "blue", Icon: "W"}); err != nil {
		t.Fatal(err)
	}
	path := store.Path("claude", "work")
	data, _ := os.ReadFile(path)
	data = []byte(strings.Replace(string(data), "color: blue", "color: '#12345'", 1))
	data = []byte(strings.Replace(string(data), "icon: W", "icon: TooLong", 1))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := store.Load("claude", "work")
	if err != nil {
		t.Fatalf("Load must not fail on a bad color/icon: %v", err)
	}
	if p.Color != "" || p.Icon != "" {
		t.Errorf("invalid color/icon should be cleared, got %q / %q", p.Color, p.Icon)
	}
	all := strings.Join(p.LoadWarnings, "\n")
	for _, want := range []string{"claude/work", `"#12345"`, `"TooLong"`, "aprofile edit claude/work"} {
		if !strings.Contains(all, want) {
			t.Errorf("load warnings should mention %s: %q", want, all)
		}
	}

	// List loads it too, and the store warns once per profile and problem.
	profiles, failures, err := store.List()
	if err != nil || len(failures) != 0 || len(profiles) != 1 {
		t.Fatalf("List = %v, failures %v, err %v", profiles, failures, err)
	}
	if len(warned) != 2 {
		t.Errorf("store warnings = %q, want one per bad field across loads", warned)
	}

	// The next save writes the repaired profile.
	if err := store.Save(p); err != nil {
		t.Fatalf("Save after a repaired load: %v", err)
	}
	saved, _ := os.ReadFile(path)
	if strings.Contains(string(saved), "12345") || strings.Contains(string(saved), "TooLong") {
		t.Errorf("save should drop the ignored values:\n%s", saved)
	}
}
