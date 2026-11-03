package tui

import (
	"errors"
	"io"

	"github.com/charmbracelet/huh"
)

// FormValues are the profile fields a form collects. They mirror the CLI value flags
// exactly so the CLI can feed them through the same apply path its flag parsing uses
// (Constitution VI): the form never builds a profile or writes the store itself.
type FormValues struct {
	Label      string
	Color      string
	Icon       string
	Model      string
	Endpoint   string
	AuthKind   string
	AuthKey    string
	HerdrAgent string
	Primary    bool
}

// ProfileFormConfig configures a profile add/edit form from harness-definition data
// (Principle I): the auth kinds to offer and whether this harness carries a model and
// endpoint. Values pre-fills the fields (used by edit).
type ProfileFormConfig struct {
	AuthKinds        []string
	SupportsModel    bool
	SupportsEndpoint bool
	Values           FormValues
}

// Identity is the (harness, name) pair the add flow collects before the field form.
type Identity struct {
	Harness string
	Name    string
}

// CopyValues are the fields the copy form collects. Source and Target are
// "<harness>/<name>" references; the CLI runs the same copy operation its command uses.
type CopyValues struct {
	Source    string
	Target    string
	Auth      string
	Label     string
	Color     string
	Icon      string
	Overwrite bool
}

// RunIdentityForm collects the harness and name for a new profile. harnesses lists the
// selectable harness names.
func RunIdentityForm(in io.Reader, out io.Writer, harnesses []string) (Identity, bool, error) {
	var id Identity
	if len(harnesses) > 0 {
		id.Harness = harnesses[0]
	}
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title("Harness").Options(huh.NewOptions(harnesses...)...).Value(&id.Harness),
		huh.NewInput().Title("Profile name").Value(&id.Name),
	))
	ok, err := runForm(form, in, out)
	return id, ok, err
}

// RunProfileForm collects a profile's fields. It returns ok=false when the user aborts
// (Constitution VI: the TUI only gathers input; the CLI applies it).
func RunProfileForm(in io.Reader, out io.Writer, cfg ProfileFormConfig) (FormValues, bool, error) {
	v := cfg.Values
	fields := []huh.Field{
		huh.NewInput().Title("Label").Value(&v.Label),
		huh.NewInput().Title("Color (ANSI name or #rrggbb)").Value(&v.Color),
		huh.NewInput().Title("Icon (single grapheme)").Value(&v.Icon),
	}
	if cfg.SupportsModel {
		fields = append(fields, huh.NewInput().Title("Model").Value(&v.Model))
	}
	if cfg.SupportsEndpoint {
		fields = append(fields, huh.NewInput().Title("Endpoint (API base URL)").Value(&v.Endpoint))
	}
	if len(cfg.AuthKinds) > 0 {
		fields = append(fields, huh.NewSelect[string]().
			Title("Auth kind").
			Options(huh.NewOptions(cfg.AuthKinds...)...).
			Value(&v.AuthKind))
	}
	fields = append(fields,
		huh.NewInput().
			Title("Auth key (env:NAME or file:/abs/path; blank for interactive login)").
			Value(&v.AuthKey),
		huh.NewInput().Title("HERDR_AGENT hint (optional)").Value(&v.HerdrAgent),
		huh.NewConfirm().Title("Make this the primary profile for its harness?").Value(&v.Primary),
	)

	form := huh.NewForm(huh.NewGroup(fields...))
	ok, err := runForm(form, in, out)
	return v, ok, err
}

// RunCopyForm collects a copy's source, target and overrides. sources lists the
// existing profile references offered for the source; initialSource pre-selects one.
func RunCopyForm(in io.Reader, out io.Writer, sources []string, initial CopyValues) (CopyValues, bool, error) {
	v := initial
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title("Source profile").Options(huh.NewOptions(sources...)...).Value(&v.Source),
		huh.NewInput().Title("Target (<harness>/<name>)").Value(&v.Target),
		huh.NewInput().Title("Auth kind for a cross-harness target (optional)").Value(&v.Auth),
		huh.NewInput().Title("Label override (optional)").Value(&v.Label),
		huh.NewInput().Title("Color override (optional)").Value(&v.Color),
		huh.NewInput().Title("Icon override (optional)").Value(&v.Icon),
		huh.NewConfirm().Title("Overwrite the target if it exists?").Value(&v.Overwrite),
	))
	ok, err := runForm(form, in, out)
	return v, ok, err
}

// RunRemoveForm confirms a removal and whether to purge the config dir. It returns
// confirmed=false when the user declines or aborts.
func RunRemoveForm(in io.Reader, out io.Writer, ref string) (confirmed, purge bool, err error) {
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title("Remove profile "+ref+"?").Value(&confirmed),
		huh.NewConfirm().Title("Also purge its config dir (credentials and state)?").Value(&purge),
	))
	ok, ferr := runForm(form, in, out)
	if ferr != nil || !ok {
		return false, false, ferr
	}
	return confirmed, purge, nil
}

// runForm runs a huh form against the given streams and maps a user abort to ok=false
// rather than an error, so the CLI can treat "cancelled" as a clean no-op.
func runForm(form *huh.Form, in io.Reader, out io.Writer) (bool, error) {
	if in != nil {
		form = form.WithInput(in)
	}
	if out != nil {
		form = form.WithOutput(out)
	}
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
