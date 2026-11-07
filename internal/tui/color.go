package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/cc-deck/aprofile/internal/profile"
)

// TerminalColor converts a profile color (an ANSI color name, matched case
// insensitively, or #rrggbb) into a lipgloss color. lipgloss understands codes
// ("0".."15") and #rrggbb but not names: a bare name like "red" renders with no
// color at all (T080), so a name is mapped to its code from profile's color table
// (T088). An empty or unknown value yields the empty color, which lipgloss renders
// as no color. The CLI and the browse view both render profile colors through it
// (FR-042).
func TerminalColor(c string) lipgloss.Color {
	if strings.HasPrefix(c, "#") {
		return lipgloss.Color(c)
	}
	code, _ := profile.ANSIColorCode(c)
	return lipgloss.Color(code)
}
