package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/cc-deck/aprofile/internal/profile"
)

// forceTrueColor makes lipgloss emit escapes regardless of where the test runs.
func forceTrueColor(t *testing.T) {
	t.Helper()
	saved := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(saved) })
}

// TestTerminalColorNames covers T080/FR-042: every ANSI color name FR-001 accepts maps
// to its ANSI code (lipgloss renders a bare name like "red" as no color at all).
func TestTerminalColorNames(t *testing.T) {
	cases := map[string]string{
		"black": "0", "red": "1", "green": "2", "yellow": "3",
		"blue": "4", "magenta": "5", "cyan": "6", "white": "7",
		"brightblack": "8", "brightred": "9", "brightgreen": "10", "brightyellow": "11",
		"brightblue": "12", "brightmagenta": "13", "brightcyan": "14", "brightwhite": "15",
		"gray": "8", "grey": "8",
		"Red": "1", "BRIGHTCYAN": "14", "Grey": "8", // case insensitive like validation
		"#ff8800": "#ff8800", "#00FF00": "#00FF00", // hex passes through
		"": "", "notacolor": "", // no color
	}
	for in, want := range cases {
		if got := string(TerminalColor(in)); got != want {
			t.Errorf("TerminalColor(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestTerminalColorCoversValidation keeps the mapping and FR-001 validation in step:
// every name validation accepts maps to a code, and every mapped name validates.
func TestTerminalColorCoversValidation(t *testing.T) {
	for name := range ansiCodes {
		if err := profile.ValidateColor(name); err != nil {
			t.Errorf("mapped name %q fails validation: %v", name, err)
		}
	}
	for _, name := range []string{
		"black", "red", "green", "yellow", "blue", "magenta", "cyan", "white", "gray", "grey",
		"brightblack", "brightred", "brightgreen", "brightyellow", "brightblue",
		"brightmagenta", "brightcyan", "brightwhite",
	} {
		if profile.ValidateColor(name) != nil {
			t.Fatalf("validation no longer accepts %q; update this list", name)
		}
		if TerminalColor(name) == "" {
			t.Errorf("valid color %q has no ANSI code", name)
		}
	}
}

// TestRenderBrowseNamedColors: a profile with a named color is rendered with the
// matching ANSI SGR escape (normal and bright), and a hex color with a 24-bit escape.
func TestRenderBrowseNamedColors(t *testing.T) {
	forceTrueColor(t)
	for color, sgr := range map[string]string{
		"red":       "\x1b[31m",
		"brightred": "\x1b[91m",
		"gray":      "\x1b[90m",
		"#ff0000":   "\x1b[38;2;255;0;0m",
	} {
		rows := []ProfileRow{{Harness: "claude", Name: "work", Label: "Work", Color: color}}
		out := renderSnapshot(rows, true)
		if !strings.Contains(out, sgr) {
			t.Errorf("color %q: render should contain %q:\n%q", color, sgr, out)
		}
	}
}
