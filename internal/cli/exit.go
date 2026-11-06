package cli

import (
	"errors"
	"fmt"
)

// Exit codes added by feature 002 (contracts/cli.md). Every other error exits 1.
const (
	ExitInvalidForLevel   = 2 // operation not valid for the profile's share level
	ExitNeedsConfirmation = 3 // confirmation needed, not on a TTY and no --yes; nothing changed
	ExitProfileRunning    = 4 // refused because the profile is running
)

// ExitError carries a specific process exit code with its error. Execute maps it to
// the exit status; any other error exits 1.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("exit status %d", e.Code)
	}
	return e.Err.Error()
}

func (e *ExitError) Unwrap() error { return e.Err }

// exitErrorf builds an ExitError with a formatted message.
func exitErrorf(code int, format string, args ...any) error {
	return &ExitError{Code: code, Err: fmt.Errorf(format, args...)}
}

// exitCode returns the process exit status for err: 0 for nil, the ExitError code
// when err wraps one, otherwise 1.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *ExitError
	if errors.As(err, &ee) && ee.Code != 0 {
		return ee.Code
	}
	return 1
}
