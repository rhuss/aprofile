package cli

import "fmt"

// wrapperClash returns the ID ("<harness>/<name>") of another stored profile whose
// wrapper name equals the one the profile (harnessName, name) would get, or "" when
// there is none (FR-002). except names a profile to ignore, such as the old identity
// of a profile being renamed ("" for none); the profile (harnessName, name) itself
// is always ignored, so an overwrite of an existing profile never clashes with
// itself. Store files that fail to load still count: their (harness, name) is known
// from the file path.
func wrapperClash(a *app, harnessName, name, except string) (otherID string, err error) {
	keys, err := a.store.Keys()
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
