package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ansiCodes maps every ANSI color name FR-001 accepts to its terminal color code.
// lipgloss understands codes ("0".."15") and #rrggbb but not names: a bare name
// like "red" renders with no color at all (T080).
var ansiCodes = map[string]string{
	"black": "0", "red": "1", "green": "2", "yellow": "3",
	"blue": "4", "magenta": "5", "cyan": "6", "white": "7",
	"brightblack": "8", "brightred": "9", "brightgreen": "10", "brightyellow": "11",
	"brightblue": "12", "brightmagenta": "13", "brightcyan": "14", "brightwhite": "15",
	// gray/grey is bright black, the conventional terminal gray.
	"gray": "8", "grey": "8",
}

// TerminalColor converts a profile color (an ANSI color name, matched case
// insensitively, or #rrggbb) into a lipgloss color. An empty or unknown value yields
// the empty color, which lipgloss renders as no color. The CLI and the browse view
// both render profile colors through it (FR-042).
func TerminalColor(c string) lipgloss.Color {
	if strings.HasPrefix(c, "#") {
		return lipgloss.Color(c)
	}
	return lipgloss.Color(ansiCodes[strings.ToLower(c)])
}
