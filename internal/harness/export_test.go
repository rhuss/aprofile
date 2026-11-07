package harness

import (
	"errors"
	"time"
)

// ParseBuiltinForTest parses data with the built-in validation rules, so tests can
// check what a built-in definition may and may not contain.
func ParseBuiltinForTest(data []byte) (*Definition, error) {
	d, lerr := parseDefinitionOpts(data, "builtin-test.yaml", true)
	if lerr != nil {
		return nil, errors.New(lerr.Error())
	}
	return d, nil
}

// NewHookForTest returns a hook client that runs binary instead of resolving the
// executable on PATH, with timeout bounding each call (0 means DefaultHookTimeout).
func NewHookForTest(harnessName, binary string, timeout time.Duration) *Hook {
	return &Hook{Harness: harnessName, binary: binary, timeout: timeout}
}
