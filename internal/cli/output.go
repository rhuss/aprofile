package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/rivo/uniseg"
	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/secret"
)

func cmdErr() io.Writer { return os.Stderr }

// isTTY reports whether w is a terminal. Used to decide colored vs plain output
// (FR-042) and whether an interactive form may open (FR-043).
func isTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// colorEnabled reports whether colored output should be used for w: only on a TTY
// and only when NO_COLOR is unset (FR-042).
func colorEnabled(w io.Writer) bool {
	if _, noColor := os.LookupEnv("NO_COLOR"); noColor {
		return false
	}
	return isTTY(w)
}

// bothTTY reports whether both stdin and the command's stdout are terminals, the only
// condition under which add/edit may open an interactive form (FR-043). When either is
// not a terminal, the command stays non-interactive and errors naming the missing
// flags instead of blocking on a form.
func bothTTY(cmd *cobra.Command) bool {
	return isTTY(os.Stdin) && isTTY(cmd.OutOrStdout())
}

// styleLabel renders a profile label in its color and icon, degrading to plain
// text when color is disabled (FR-042).
func styleLabel(w io.Writer, label, color, icon string) string {
	text := label
	if icon != "" {
		text = icon + " " + label
	}
	if !colorEnabled(w) || color == "" {
		return text
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(text)
}

// writeJSON encodes v as indented JSON to w.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// ansiColorNames are the named terminal colors FR-001 accepts.
var ansiColorNames = map[string]bool{
	"black": true, "red": true, "green": true, "yellow": true, "blue": true,
	"magenta": true, "cyan": true, "white": true, "gray": true, "grey": true,
	"brightblack": true, "brightred": true, "brightgreen": true, "brightyellow": true,
	"brightblue": true, "brightmagenta": true, "brightcyan": true, "brightwhite": true,
}

var hexColorRE = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// validateColor enforces FR-001: an ANSI color name or #rrggbb. Empty is allowed
// (no color).
func validateColor(c string) error {
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

// validateIcon enforces FR-001: a single Unicode grapheme. Empty is allowed.
func validateIcon(icon string) error {
	if icon == "" {
		return nil
	}
	if uniseg.GraphemeClusterCount(icon) != 1 {
		return fmt.Errorf("icon %q must be a single Unicode grapheme", icon)
	}
	return nil
}

// validEnvName reports whether name is a valid environment variable name. It
// delegates to secret.ValidEnvVarName so the store, loader and CLI share one rule.
func validEnvName(name string) bool { return secret.ValidEnvVarName(name) }

// parseProfileRef parses a "<harness>/<name>" reference.
func parseProfileRef(s string) (harness, name string, err error) {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("profile reference %q must be <harness>/<name>", s)
	}
	return parts[0], parts[1], nil
}
