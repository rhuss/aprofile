package cli

import (
	"fmt"

	"github.com/cc-deck/aprofile/internal/harness"
)

// validateProfileID checks a harness and profile name taken from the command line
// against the shared name pattern (FR-002) before either reaches a path join, so a
// value such as "../x" can never address a file or directory outside aprofile's
// per-harness locations (remove --purge deletes the profile's config dir).
func validateProfileID(h, n string) error {
	if !harness.NameValid(h) {
		return fmt.Errorf("harness name %q must match ^[a-z][a-z0-9-]{0,62}[a-z0-9]$", h)
	}
	if !harness.NameValid(n) {
		return fmt.Errorf("profile name %q must match ^[a-z][a-z0-9-]{0,62}[a-z0-9]$", n)
	}
	return nil
}
