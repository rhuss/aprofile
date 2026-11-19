package tui

import (
	"errors"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/key"
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
	// Feature 003: the history group ("" for isolated).
	HistoryGroup string
}

// GroupOption is one history group choice of the form: Value is the group name
// ("" for isolated), Label the text shown.
type GroupOption struct {
	Value string
	Label string
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
	// GroupOptions offers the history group choice (isolated, home, the harness's
	// own groups); empty hides it (a harness without history entries).
	GroupOptions []GroupOption
}

// Identity is the (harness, name) pair the add flow collects before the field form.
type Identity struct {
	Harness string
	Name    string
}

// CopyValues are the fields the copy form collects. Source and Target are
// "<harness>/<name>" references; the CLI runs the same copy operation its command uses.
type CopyValues struct {
	Source string
	Target string
	Auth   string
	// AuthKey is the secret reference for a cross-harness target's auth kind
	// (env:NAME or file:/abs/path), the --auth-key flag; blank sets none.
	AuthKey   string
	Label     string
	Color     string
	Icon      string
	Overwrite bool
	// Adopt reuses a leftover config dir for a new target (FR-004), the --adopt flag.
	Adopt bool
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
	ok, err := runForm(newIdentityForm(harnesses, &id), in, out)
	return id, ok, err
}

// newIdentityForm builds the harness and name form, writing into id.
func newIdentityForm(harnesses []string, id *Identity) *huh.Form {
	return styleForm(huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Key("harness").Title("Harness").
			Options(huh.NewOptions(harnesses...)...).Inline(true).Value(&id.Harness),
		huh.NewInput().Key("name").Title("Profile name").Value(&id.Name),
	)))
}

// RunProfileForm collects a profile's fields. It returns ok=false when the user aborts
// (Constitution VI: the TUI only gathers input; the CLI applies it).
func RunProfileForm(in io.Reader, out io.Writer, cfg ProfileFormConfig) (FormValues, bool, error) {
	v := cfg.Values
	ok, err := runForm(newProfileForm(cfg, &v), in, out)
	return v, ok, err
}

// newProfileForm builds the profile form, writing into v. Fields are grouped into
// titled sections shown together; a section whose fields the harness hides is left
// out.
func newProfileForm(cfg ProfileFormConfig, v *FormValues) *huh.Form {
	var auth []huh.Field
	if cfg.SupportsModel {
		auth = append(auth, huh.NewInput().Key("model").Title("Model").Value(&v.Model))
	}
	if cfg.SupportsEndpoint {
		auth = append(auth, huh.NewInput().Key("endpoint").Title("Endpoint (API base URL)").Value(&v.Endpoint))
	}
	if len(cfg.AuthKinds) > 0 {
		auth = append(auth, huh.NewSelect[string]().Key("auth_kind").
			Title("Auth kind").
			Options(huh.NewOptions(cfg.AuthKinds...)...).
			Inline(true).
			Value(&v.AuthKind))
	}
	auth = append(auth, huh.NewInput().Key("auth_key").
		Title("Auth key (env:NAME or file:/abs/path; blank for interactive login)").
		Value(&v.AuthKey))

	var sharing []huh.Field
	if len(cfg.ShareOptions) > 0 {
		sharing = append(sharing, shareSelect(cfg.ShareOptions, &v.Share))
	}
	sharing = append(sharing, hookSetFields(cfg.HookSetOptions, cfg.EveryProfileSets, &v.HookSets, &v.ExcludeHookSets)...)
	if len(cfg.GroupOptions) > 0 {
		sharing = append(sharing, groupSelect(cfg.GroupOptions, &v.HistoryGroup))
	}

	return styleForm(huh.NewForm(sections(
		section{"Appearance", []huh.Field{
			huh.NewInput().Key("label").Title("Label").Value(&v.Label),
			huh.NewInput().Key("color").Title("Color (ANSI name or #rrggbb)").Value(&v.Color),
			huh.NewInput().Key("icon").Title("Icon (single grapheme)").Value(&v.Icon),
		}},
		section{"Model & auth", auth},
		section{"Launch", []huh.Field{
			huh.NewInput().Key("herdr_agent").Title("HERDR_AGENT hint (optional)").Value(&v.HerdrAgent),
			huh.NewConfirm().Key("primary").Title("Make this the primary profile for its harness?").Value(&v.Primary),
		}},
		section{"Sharing & history", sharing},
	)...))
}

// section is one titled group of form fields.
type section struct {
	title  string
	fields []huh.Field
}

// sections turns sections into huh groups, skipping any without fields.
func sections(secs ...section) []*huh.Group {
	var groups []*huh.Group
	for _, s := range secs {
		if len(s.fields) > 0 {
			groups = append(groups, huh.NewGroup(s.fields...).Title(s.title))
		}
	}
	return groups
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
	return huh.NewSelect[string]().Key("share").
		Title("Share level (what this profile uses from your default setup)").
		DescriptionFunc(func() string {
			o := byValue[*value]
			if len(o.UseCases) == 0 {
				return ""
			}
			return "Fits: " + strings.Join(o.UseCases, "; ")
		}, value).
		Options(options...).
		Inline(true).
		Value(value)
}

// groupSelect builds the history group choice, with the same rules as --group:
// isolated, home, or a group made with aprofile group create.
func groupSelect(opts []GroupOption, value *string) huh.Field {
	var options []huh.Option[string]
	for _, o := range opts {
		options = append(options, huh.NewOption(o.Label, o.Value))
	}
	return huh.NewSelect[string]().Key("history_group").
		Title("History group (which profiles share this profile's conversation history)").
		Description("Logins stay private in every group.").
		Options(options...).
		Inline(true).
		Value(value)
}

// hookSetFields builds the multi-selects for named and excluded hook sets.
func hookSetFields(sets, everyProfile []string, named, excluded *[]string) []huh.Field {
	var fields []huh.Field
	if len(sets) > 0 {
		fields = append(fields, huh.NewMultiSelect[string]().Key("hook_sets").
			Title("Hook sets to attach (not at share level all)").
			Options(huh.NewOptions(sets...)...).
			Value(named))
	}
	if len(everyProfile) > 0 {
		fields = append(fields, huh.NewMultiSelect[string]().Key("exclude_hook_sets").
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
		huh.NewInput().Title("Auth key reference (env:NAME or file:/abs/path; optional)").Value(&v.AuthKey),
		huh.NewInput().Title("Label override (optional)").Value(&v.Label),
		huh.NewInput().Title("Color override (optional)").Value(&v.Color),
		huh.NewInput().Title("Icon override (optional)").Value(&v.Icon),
		huh.NewConfirm().Title("Overwrite the target if it exists?").Value(&v.Overwrite),
		huh.NewConfirm().Title("Adopt an existing config dir left at the target's location?").Value(&v.Adopt),
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
	ok, ferr := runForm(newRemoveForm(ref, &confirmed, &purge), in, out)
	if ferr != nil || !ok {
		return false, false, ferr
	}
	return confirmed, purge, nil
}

// newRemoveForm builds the removal confirmation form, writing into confirmed and purge.
func newRemoveForm(ref string, confirmed, purge *bool) *huh.Form {
	return styleForm(huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Key("confirm").Title("Remove profile "+ref+"?").Value(confirmed),
		huh.NewConfirm().Key("purge").Title("Also purge its config dir (credentials and state)?").Value(purge),
	)))
}

// styleForm applies the shared look and keys to a form: Esc cancels, the arrow keys
// move between rows, sections stack on one screen, and the key help is shown.
func styleForm(f *huh.Form) *huh.Form {
	return f.WithKeyMap(formKeyMap()).
		WithTheme(formTheme(formsColor())).
		WithShowHelp(true).
		WithLayout(huh.LayoutStack)
}

// formKeyMap is huh's default keymap with Esc as cancel and ↑/↓ as previous/next
// row for inputs, confirms and selects. Single selects are inline, so huh leaves
// ↑/↓ free for row movement there; multi-selects keep ↑/↓ for their own items.
// Filtering gets no key at all, so Esc never means "clear filter" (huh re-enables a
// disabled filter binding on every update, so disabling it would not stick).
func formKeyMap() *huh.KeyMap {
	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys("ctrl+c", "esc"), key.WithHelp("esc", "cancel"))
	km.Input.Prev = withKeys(km.Input.Prev, "up")
	km.Input.Next = withKeys(km.Input.Next, "down")
	km.Confirm.Prev = withKeys(km.Confirm.Prev, "up")
	km.Confirm.Next = withKeys(km.Confirm.Next, "down")
	km.Select.Prev = withKeys(km.Select.Prev, "up")
	km.Select.Next = withKeys(km.Select.Next, "down")
	km.Select.Filter = key.NewBinding()
	km.MultiSelect.Filter = key.NewBinding()
	return km
}

// withKeys returns b with extra keys added, keeping its help text.
func withKeys(b key.Binding, extra ...string) key.Binding {
	h := b.Help()
	return key.NewBinding(key.WithKeys(append(b.Keys(), extra...)...), key.WithHelp(h.Key, h.Desc))
}

// formTheme picks the form theme: the terminal's own 16 colors, or no color.
func formTheme(color bool) *huh.Theme {
	if color {
		return huh.ThemeBase16()
	}
	return huh.ThemeBase()
}

// formsColor reports whether forms use color. Forms only open on a terminal, so
// NO_COLOR is the one remaining condition of the CLI's color rule (FR-042).
func formsColor() bool {
	_, noColor := os.LookupEnv("NO_COLOR")
	return !noColor
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
