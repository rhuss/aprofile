package secret

import (
	"bufio"
	"fmt"
	"os"
)

// Resolve reads a reference's value in Go (as opposed to the shell renderer used
// by wrappers). It is fail-closed: an unset/empty variable or a missing,
// unreadable or empty file is an error (FR-028a). File references return the first
// line only.
func Resolve(r Reference) (string, error) {
	switch r.Kind {
	case KindEnv:
		v := os.Getenv(r.Value)
		if v == "" {
			return "", fmt.Errorf("reference %s is unset or empty", r.Raw)
		}
		return v, nil
	case KindFile:
		f, err := os.Open(r.Value)
		if err != nil {
			return "", fmt.Errorf("reference %s is missing or unreadable: %w", r.Raw, err)
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		if !sc.Scan() {
			return "", fmt.Errorf("reference %s is empty", r.Raw)
		}
		line := sc.Text()
		if line == "" {
			return "", fmt.Errorf("reference %s is empty", r.Raw)
		}
		return line, nil
	default:
		return "", fmt.Errorf("unknown reference kind %q", r.Kind)
	}
}
