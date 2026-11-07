package cli

import (
	"fmt"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/profile"
)

// wrapperClash returns the ID ("<harness>/<name>") of another stored profile whose
// wrapper name equals the one the profile (harnessName, name) would get, or "" when
// there is none (FR-002). except names a profile to ignore, such as the old identity
// of a profile being renamed ("" for none); the profile (harnessName, name) itself
// is always ignored, so an overwrite of an existing profile never clashes with
// itself. Store files that fail to load still count: their (harness, name) is known
// from the file path.
func wrapperClash(a *app, harnessName, name, except string) (otherID string, err error) {
	keys, err := storedProfileKeys(a.store)
	if err != nil {
		return "", err
	}
	if other, ok := a.set.WrapperClash(harnessName, name, keys, except); ok {
		return other.ID(), nil
	}
	return "", nil
}

// checkWrapperClash is wrapperClash as an error: it names the clashing profile and
// the shared wrapper name, and returns nil when there is no clash.
func checkWrapperClash(a *app, harnessName, name, except string) error {
	other, err := wrapperClash(a, harnessName, name, except)
	if err != nil || other == "" {
		return err
	}
	wrapper, _ := a.set.WrapperName(harnessName, name)
	return fmt.Errorf("wrapper name %q is already used by profile %s; choose another name", wrapper, other)
}

// storedProfileKeys lists the (harness, name) of every stored profile, including
// those whose file fails to load.
func storedProfileKeys(store *profile.Store) ([]harness.ProfileKey, error) {
	profiles, failures, err := store.List()
	if err != nil {
		return nil, err
	}
	keys := make([]harness.ProfileKey, 0, len(profiles)+len(failures))
	for _, p := range profiles {
		keys = append(keys, harness.ProfileKey{Harness: p.Harness, Name: p.Name})
	}
	for _, f := range failures {
		keys = append(keys, harness.ProfileKey{Harness: f.Harness, Name: f.Name})
	}
	return keys, nil
}
