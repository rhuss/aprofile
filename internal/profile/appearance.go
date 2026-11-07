package profile

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/rivo/uniseg"
)

// ansiColorCodes is the one table of the named terminal colors FR-001 accepts
// (matched case insensitively), with the ANSI code each renders as (FR-042).
// Renderers such as lipgloss understand codes ("0".."15") and #rrggbb but not
// names, so they read the code from here (ANSIColorCode).
var ansiColorCodes = map[string]string{
	"black": "0", "red": "1", "green": "2", "yellow": "3",
	"blue": "4", "magenta": "5", "cyan": "6", "white": "7",
	"brightblack": "8", "brightred": "9", "brightgreen": "10", "brightyellow": "11",
	"brightblue": "12", "brightmagenta": "13", "brightcyan": "14", "brightwhite": "15",
	// gray/grey is bright black, the conventional terminal gray.
	"gray": "8", "grey": "8",
}

// ANSIColorCode returns the ANSI code ("0".."15") of a color name, matched case
// insensitively, and false when name is not an ANSI color name FR-001 accepts.
func ANSIColorCode(name string) (string, bool) {
	code, ok := ansiColorCodes[strings.ToLower(name)]
	return code, ok
}

var hexColorRE = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// ValidateColor enforces FR-001: a color is an ANSI color name or #rrggbb. Empty
// means no color and is allowed.
func ValidateColor(c string) error {
	if c == "" {
		return nil
	}
	if strings.HasPrefix(c, "#") {
		if !hexColorRE.MatchString(c) {
			return fmt.Errorf("color %q must be #rrggbb", c)
		}
		return nil
	}
	if _, ok := ANSIColorCode(c); !ok {
		return fmt.Errorf("color %q is not an ANSI color name or #rrggbb", c)
	}
	return nil
}

// ValidateIcon enforces FR-001: an icon is a single Unicode grapheme. Empty means
// no icon and is allowed.
func ValidateIcon(icon string) error {
	if icon == "" {
		return nil
	}
	if uniseg.GraphemeClusterCount(icon) != 1 {
		return fmt.Errorf("icon %q must be a single Unicode grapheme", icon)
	}
	return nil
}
