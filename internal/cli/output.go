package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/secret"
	"github.com/cc-deck/aprofile/internal/tui"
)

func cmdErr() io.Writer { return os.Stderr }

// isTTY reports whether w is a terminal. Used to decide colored vs plain output
// (FR-042) and whether an interactive form may open (FR-043).
func isTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	// A character device is not enough: /dev/null is one, and a command run with
	// its input redirected from it must count as non-interactive (exit 3 instead of
	// a prompt nobody can answer).
	return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
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
	return colorize(text, color)
}

// colorize renders text in a profile color. Named ANSI colors are mapped to their
// codes first (tui.TerminalColor): lipgloss renders a bare name as no color (T080).
func colorize(text, color string) string {
	if color == "" {
		return text
	}
	return lipgloss.NewStyle().Foreground(tui.TerminalColor(color)).Render(text)
}

// writeJSON encodes v as indented JSON to w.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// validateColor enforces FR-001 (an ANSI color name or #rrggbb; empty means no
// color). It delegates to profile.ValidateColor so the store, CLI and TUI share one
// rule.
func validateColor(c string) error { return profile.ValidateColor(c) }

// validateIcon enforces FR-001 (a single Unicode grapheme; empty means no icon) via
// profile.ValidateIcon.
func validateIcon(icon string) error { return profile.ValidateIcon(icon) }

// validEnvName reports whether name is a valid environment variable name. It
// delegates to secret.ValidEnvVarName so the store, loader and CLI share one rule.
func validEnvName(name string) bool { return secret.ValidEnvVarName(name) }

// parseProfileRef parses a "<harness>/<name>" reference. Both parts must be valid
// names (validateProfileID), so a reference never smuggles a path into a join.
func parseProfileRef(s string) (harness, name string, err error) {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("profile reference %q must be <harness>/<name>", s)
	}
	if err := validateProfileID(parts[0], parts[1]); err != nil {
		return "", "", fmt.Errorf("profile reference %q: %w", s, err)
	}
	return parts[0], parts[1], nil
}

func sortStrings(s []string) { sort.Strings(s) }
