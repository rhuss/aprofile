package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

var sampleRows = []ProfileRow{
	{Harness: "claude", Name: "work", Label: "Work", Color: "#ff0000", Icon: "W", Primary: true, WrapperStatus: "synced"},
	{Harness: "claude", Name: "private", Label: "Private", Color: "#00ff00", Icon: "P", WrapperStatus: "stale"},
	{Harness: "codex", Name: "work", Label: "Codex Work", WrapperStatus: "missing"},
}

// renderSnapshot renders the browse view without a cursor, as the interactive view
// shows it before the first keypress.
func renderSnapshot(rows []ProfileRow, color bool) string {
	return renderBrowse(sortRows(rows), -1, color)
}

// TestRenderBrowsePlain covers FR-042 degradation: with color disabled (stdout is not a
// TTY or NO_COLOR is set) the browse view is plain text with no ANSI escape codes, while
// still showing icons, the primary marker, the wrapper status and the harness grouping.
func TestRenderBrowsePlain(t *testing.T) {
	out := renderSnapshot(sampleRows, false)

	if strings.Contains(out, "\x1b[") {
		t.Errorf("plain render must contain no ANSI escapes:\n%q", out)
	}
	for _, want := range []string{
		"claude:", "codex:", // grouped by harness
		"W Work", "P Private", // icon + label
		"(primary)",                        // primary marker
		"[synced]", "[stale]", "[missing]", // wrapper status
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plain render missing %q:\n%s", want, out)
		}
	}
}

// TestRenderBrowseColored covers FR-042: with color enabled and a terminal color
// profile, a profile with a color is rendered with ANSI escapes. The color profile is
// forced so the test is deterministic regardless of where it runs.
func TestRenderBrowseColored(t *testing.T) {
	saved := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(saved) })

	out := renderSnapshot(sampleRows, true)
	if !strings.Contains(out, "\x1b[") {
		t.Errorf("colored render should contain ANSI escapes:\n%q", out)
	}
}

// TestRenderBrowseEmpty shows the empty-state hint rather than a blank screen.
func TestRenderBrowseEmpty(t *testing.T) {
	out := renderSnapshot(nil, false)
	if !strings.Contains(out, "no profiles yet") {
		t.Errorf("empty render should hint how to add a profile, got:\n%s", out)
	}
}

// TestRenderBrowseCursor marks the row under the cursor and no other.
func TestRenderBrowseCursor(t *testing.T) {
	out := renderBrowse(sortRows(sampleRows), 0, false)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	markers := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "> ") {
			markers++
		}
	}
	if markers != 1 {
		t.Errorf("exactly one row should carry the cursor marker, got %d:\n%s", markers, out)
	}
}
