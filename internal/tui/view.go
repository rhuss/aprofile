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

// RenderBrowse renders the browse view without a cursor, suitable for a snapshot or a
// non-interactive dump. See renderBrowse for the behavior; the color argument follows
// FR-042 (the caller passes false when stdout is not a TTY or NO_COLOR is set, and the
// output is then plain text with no escape codes).
func RenderBrowse(rows []ProfileRow, color bool) string {
	return renderBrowse(rows, -1, color)
}

// renderBrowse groups rows by harness and renders each profile with its icon, label
// (in its color when color is enabled), a primary marker and its wrapper sync status
// (FR-043, FR-042). A non-negative cursor highlights that row (used by the interactive
// browse model). It is a pure function of its inputs so it is table-testable.
func renderBrowse(rows []ProfileRow, cursor int, color bool) string {
	if len(rows) == 0 {
		return "no profiles yet (add one with 'a' or: aprofile add <harness> <name>)\n"
	}

	// Stable order: by harness, then name, matching the CLI list command.
	ordered := append([]ProfileRow(nil), rows...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Harness != ordered[j].Harness {
			return ordered[i].Harness < ordered[j].Harness
		}
		return ordered[i].Name < ordered[j].Name
	})

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
			text = lipgloss.NewStyle().Foreground(lipgloss.Color(r.Color)).Render(text)
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
