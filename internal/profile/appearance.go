package profile

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/rivo/uniseg"
)

// ansiColorNames are the named terminal colors FR-001 accepts (matched case
// insensitively).
var ansiColorNames = map[string]bool{
	"black": true, "red": true, "green": true, "yellow": true, "blue": true,
	"magenta": true, "cyan": true, "white": true, "gray": true, "grey": true,
	"brightblack": true, "brightred": true, "brightgreen": true, "brightyellow": true,
	"brightblue": true, "brightmagenta": true, "brightcyan": true, "brightwhite": true,
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
	if !ansiColorNames[strings.ToLower(c)] {
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
