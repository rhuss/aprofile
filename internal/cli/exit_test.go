package cli

import (
	"errors"
	"fmt"
	"testing"
)

func TestExitCode(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{nil, 0},
		{errors.New("plain"), 1},
		{exitErrorf(ExitInvalidForLevel, "bad level"), 2},
		{exitErrorf(ExitNeedsConfirmation, "needs --yes"), 3},
		{fmt.Errorf("wrapped: %w", exitErrorf(ExitProfileRunning, "running")), 4},
	}
	for _, c := range cases {
		if got := exitCode(c.err); got != c.want {
			t.Errorf("exitCode(%v) = %d, want %d", c.err, got, c.want)
		}
	}
	if (&ExitError{Code: 3, Err: errors.New("x")}).Error() != "x" {
		t.Error("ExitError should print its wrapped error")
	}
}
