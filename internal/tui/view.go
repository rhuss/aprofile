// Package tui is the interactive layer: a bubbletea/lipgloss browse view and huh
// forms for add/edit/copy/remove. It is a thin layer over the same operations the
// CLI commands use (Constitution VI): it reads display data the CLI hands it and
// returns the values a user entered; it never touches the profile store, renders a
// wrapper, or runs a sync itself. Every action it offers has a non-interactive
// command equivalent (SC-008).
package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ProfileRow is the display data for one profile in the browse view (FR-043). The
// CLI builds it from the profile store and the on-disk wrapper state and hands it to
// the TUI; the TUI never reads the store itself (Constitution VI).
type ProfileRow struct {
	Harness       string
	Name          string
	Label         string
	Color         string
	Icon          string
	Primary       bool
	WrapperStatus string // synced | stale | missing | unknown
}

// sortRows returns a copy of rows in display order: by harness, then name, matching
// the CLI list command. The browse model sorts once when it receives its rows, so
// rendering and selection on every keypress need no re-sort.
func sortRows(rows []ProfileRow) []ProfileRow {
	ordered := append([]ProfileRow(nil), rows...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Harness != ordered[j].Harness {
			return ordered[i].Harness < ordered[j].Harness
		}
		return ordered[i].Name < ordered[j].Name
	})
	return ordered
}

// renderBrowse groups rows by harness and renders each profile with its icon, label
// (in its color when color is enabled), a primary marker and its wrapper sync status
// (FR-043, FR-042). ordered must already be in display order (sortRows). A non-negative
// cursor highlights that row (used by the interactive browse model); the color
// argument follows FR-042 (false when stdout is not a TTY or NO_COLOR is set gives
// plain text with no escape codes). It is a pure function of its inputs so it is
// table-testable.
func renderBrowse(ordered []ProfileRow, cursor int, color bool) string {
	if len(ordered) == 0 {
		return "no profiles yet (add one with 'a' or: aprofile add <harness> <name>)\n"
	}

	var b strings.Builder
	lastHarness := ""
	for idx, r := range ordered {
		if r.Harness != lastHarness {
			if lastHarness != "" {
				b.WriteString("\n")
			}
			b.WriteString(r.Harness + ":\n")
			lastHarness = r.Harness
		}

		marker := "  "
		if idx == cursor {
			marker = "> "
		}

		label := r.Label
		if label == "" {
			label = r.Name
		}
		text := label
		if r.Icon != "" {
			text = r.Icon + " " + label
		}
		if color && r.Color != "" {
			text = lipgloss.NewStyle().Foreground(TerminalColor(r.Color)).Render(text)
		}

		primary := ""
		if r.Primary {
			primary = " (primary)"
		}
		b.WriteString(fmt.Sprintf("%s%s%s  [%s]\n", marker, text, primary, statusText(r.WrapperStatus)))
	}
	return b.String()
}

// statusText normalizes an empty wrapper status to "unknown" for display.
func statusText(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
