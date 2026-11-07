package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestColorizeNamedAndHex covers T080/FR-042: a label in a named ANSI color (normal,
// bright, gray/grey) renders with the matching SGR escape, and a hex color with a
// 24-bit escape. Before the mapping, lipgloss rendered a bare name like "red" with no
// color at all.
func TestColorizeNamedAndHex(t *testing.T) {
	saved := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(saved) })

	for color, sgr := range map[string]string{
		"red":         "\x1b[31m",
		"Blue":        "\x1b[34m",
		"brightgreen": "\x1b[92m",
		"grey":        "\x1b[90m",
		"#00ff00":     "\x1b[38;2;0;255;0m",
	} {
		got := colorize("W Work", color)
		if !strings.Contains(got, sgr) || !strings.Contains(got, "W Work") {
			t.Errorf("colorize(%q) = %q, want it to contain %q and the text", color, got, sgr)
		}
	}
	if got := colorize("W Work", ""); got != "W Work" {
		t.Errorf("no color must leave the text unchanged, got %q", got)
	}
}

// TestValidateColorDelegates: the CLI's color/icon validation is the profile
// package's (one rule for store, CLI and TUI).
func TestValidateColorDelegates(t *testing.T) {
	for _, c := range []string{"", "red", "BrightWhite", "gray", "#a1b2c3"} {
		if err := validateColor(c); err != nil {
			t.Errorf("validateColor(%q) = %v, want nil", c, err)
		}
	}
	for _, c := range []string{"#abc", "purple", "bright-red"} {
		if validateColor(c) == nil {
			t.Errorf("validateColor(%q) = nil, want an error", c)
		}
	}
	if validateIcon("ab") == nil || validateIcon("W") != nil || validateIcon("") != nil {
		t.Error("validateIcon must accept one grapheme or empty and reject more")
	}
}
