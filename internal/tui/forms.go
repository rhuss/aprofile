package tui

import (
	"errors"
	"io"
	"strings"

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
	// Feature 002.
	Share           string
	HookSets        []string
	ExcludeHookSets []string
}

// ShareOption is one share level offered by the form, with the one-line
// description and the use cases it fits (US2 AS2). The CLI supplies the text from
// its single source (cli/levels.go).
type ShareOption struct {
	Value       string
	Description string
	UseCases    []string
}

// ProfileFormConfig configures a profile add/edit form from harness-definition data
// (Principle I): the auth kinds to offer and whether this harness carries a model and
// endpoint. Values pre-fills the fields (used by edit).
type ProfileFormConfig struct {
	AuthKinds        []string
	SupportsModel    bool
	SupportsEndpoint bool
	Values           FormValues
	// ShareOptions offers the share levels; empty hides the choice.
	ShareOptions []ShareOption
	// HookSetOptions lists the library's sets with a body for this harness; empty
	// hides the hook-set choices. EveryProfileSets are the ones that can be excluded.
	HookSetOptions   []string
	EveryProfileSets []string
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
	// Feature 002: share level ("" keeps the source's) and hook sets.
	Share           string
	HookSets        []string
	ExcludeHookSets []string
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
	if len(cfg.ShareOptions) > 0 {
		fields = append(fields, shareSelect(cfg.ShareOptions, &v.Share))
	}
	fields = append(fields, hookSetFields(cfg.HookSetOptions, cfg.EveryProfileSets, &v.HookSets, &v.ExcludeHookSets)...)

	form := huh.NewForm(huh.NewGroup(fields...))
	ok, err := runForm(form, in, out)
	return v, ok, err
}

// shareSelect builds the share level choice: each option shows its one-line
// description, and the description area lists the use cases it fits.
func shareSelect(opts []ShareOption, value *string) huh.Field {
	var options []huh.Option[string]
	byValue := map[string]ShareOption{}
	for _, o := range opts {
		options = append(options, huh.NewOption(o.Value+": "+o.Description, o.Value))
		byValue[o.Value] = o
	}
	if *value == "" && len(opts) > 0 {
		*value = opts[0].Value
	}
	return huh.NewSelect[string]().
		Title("Share level (what this profile uses from your default setup)").
		DescriptionFunc(func() string {
			o := byValue[*value]
			if len(o.UseCases) == 0 {
				return ""
			}
			return "Fits: " + strings.Join(o.UseCases, "; ")
		}, value).
		Options(options...).
		Value(value)
}

// hookSetFields builds the multi-selects for named and excluded hook sets.
func hookSetFields(sets, everyProfile []string, named, excluded *[]string) []huh.Field {
	var fields []huh.Field
	if len(sets) > 0 {
		fields = append(fields, huh.NewMultiSelect[string]().
			Title("Hook sets to attach (not at share level all)").
			Options(huh.NewOptions(sets...)...).
			Value(named))
	}
	if len(everyProfile) > 0 {
		fields = append(fields, huh.NewMultiSelect[string]().
			Title("Every-profile hook sets to skip (share level content)").
			Options(huh.NewOptions(everyProfile...)...).
			Value(excluded))
	}
	return fields
}

// RunCopyForm collects a copy's source, target and overrides. sources lists the
// existing profile references offered for the source; initialSource pre-selects one.
// shareOpts gives the levels with their one-line descriptions (FR-014).
func RunCopyForm(in io.Reader, out io.Writer, sources []string, initial CopyValues, shareOpts []ShareOption) (CopyValues, bool, error) {
	v := initial
	levels := []huh.Option[string]{huh.NewOption("same as the source", "")}
	for _, o := range shareOpts {
		levels = append(levels, huh.NewOption(o.Value+": "+o.Description, o.Value))
	}
	hookSets := strings.Join(v.HookSets, ",")
	excluded := strings.Join(v.ExcludeHookSets, ",")
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title("Source profile").Options(huh.NewOptions(sources...)...).Value(&v.Source),
		huh.NewInput().Title("Target (<harness>/<name>)").Value(&v.Target),
		huh.NewInput().Title("Auth kind for a cross-harness target (optional)").Value(&v.Auth),
		huh.NewInput().Title("Label override (optional)").Value(&v.Label),
		huh.NewInput().Title("Color override (optional)").Value(&v.Color),
		huh.NewInput().Title("Icon override (optional)").Value(&v.Icon),
		huh.NewConfirm().Title("Overwrite the target if it exists?").Value(&v.Overwrite),
		huh.NewSelect[string]().Title("Share level of the target").
			Options(levels...).
			Value(&v.Share),
		huh.NewInput().Title("Hook sets of the target (comma-separated; blank keeps the source's)").Value(&hookSets),
		huh.NewInput().Title("Every-profile hook sets the target skips (comma-separated; blank keeps the source's)").Value(&excluded),
	))
	ok, err := runForm(form, in, out)
	v.HookSets = splitList(hookSets)
	v.ExcludeHookSets = splitList(excluded)
	return v, ok, err
}

// splitList parses a comma-separated list, dropping blanks.
func splitList(text string) []string {
	var out []string
	for _, s := range strings.Split(text, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
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
