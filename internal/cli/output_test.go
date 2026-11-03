package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// TestStyleLabelPlainWhenNotTTY covers FR-042: styling a label to a non-terminal writer
// yields plain text (icon + label) with no ANSI escapes.
func TestStyleLabelPlainWhenNotTTY(t *testing.T) {
	var buf bytes.Buffer // not an *os.File, so not a TTY
	got := styleLabel(&buf, "Work", "blue", "W")
	if strings.Contains(got, "\x1b[") {
		t.Errorf("non-TTY output must be plain, got %q", got)
	}
	if got != "W Work" {
		t.Errorf("non-TTY label = %q, want %q", got, "W Work")
	}
}

// TestColorEnabledNoColorEnv covers FR-042: NO_COLOR disables color even for a terminal.
func TestColorEnabledNoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if colorEnabled(os.Stdout) {
		t.Error("colorEnabled must be false when NO_COLOR is set")
	}
}

// TestColorEnabledNonTTY covers FR-042: a non-terminal writer never gets color. A
// bytes.Buffer is never a TTY, so the result is false regardless of NO_COLOR; no env
// mutation is needed (and none is left behind).
func TestColorEnabledNonTTY(t *testing.T) {
	var buf bytes.Buffer
	if colorEnabled(&buf) {
		t.Error("colorEnabled must be false for a non-terminal writer")
	}
}

// TestStyleLabelNoIcon shows a label without an icon renders just the label.
func TestStyleLabelNoIcon(t *testing.T) {
	var buf bytes.Buffer
	if got := styleLabel(&buf, "Private", "", ""); got != "Private" {
		t.Errorf("label without icon/color = %q, want %q", got, "Private")
	}
}
