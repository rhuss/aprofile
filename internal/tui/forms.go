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
	return newForm(section{"", []huh.Field{
		huh.NewSelect[string]().Key("harness").Title("Harness").
			Options(huh.NewOptions(harnesses...)...).Inline(true).Value(&id.Harness),
		huh.NewInput().Key("name").Title("Profile name").Value(&id.Name),
	}})
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

	return newForm(
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
	)
}

// section is one titled group of form fields; an empty title draws no header.
type section struct {
	title  string
	fields []huh.Field
}

// newForm builds a form from sections, skipping any without fields, and applies the
// shared look and keys: Esc cancels, the arrow keys move between rows, and the help
// line lists the cancel key.
//
// All sections go into ONE huh group, each headed by a Note row (focus skips notes).
// huh switches groups asynchronously, so with one group per section a held ↓ across
// a section boundary left one row drawn focused while typing went into another. One
// group also scrolls to keep the focused row visible on a short terminal.
func newForm(secs ...section) *huh.Form {
	var fields []huh.Field
	for _, s := range secs {
		if len(s.fields) == 0 {
			continue
		}
		if s.title != "" {
			fields = append(fields, huh.NewNote().Title(s.title))
		}
		fields = append(fields, s.fields...)
	}
	km := formKeyMap()
	g := huh.NewGroup(fields...)
	return huh.NewForm(g).
		WithKeyMap(km).
		WithTheme(formTheme(formsColor())).
		WithShowHelp(true).
		WithLayout(&helpLayout{group: g, quit: km.Quit})
}

// helpLayout draws the form's single group as huh does, with the cancel key added
// to the help line (huh's help lists only the focused field's keys).
type helpLayout struct {
	group *huh.Group
	quit  key.Binding
}

func (l *helpLayout) View(f *huh.Form) string {
	view := l.group.View()
	if len(f.Errors()) > 0 {
		return view // the footer holds error lines; leave it as huh draws it
	}
	// huh ends the view with a blank line and the one-line help footer, but sizes
	// the group for the footer alone, so the view is one line taller than the
	// terminal and the renderer drops the top line. The help replaces both lines.
	lines := strings.Split(view, "\n")
	if l.group.Footer() == "" || len(lines) < 2 {
		return view
	}
	help := f.Help().ShortHelpView(append(f.KeyBinds(), l.quit))
	return strings.Join(append(lines[:len(lines)-2], help), "\n")
}

func (l *helpLayout) GroupWidth(_ *huh.Form, _ *huh.Group, w int) int { return w }

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
	hookSets := strings.Join(v.HookSets, ",")
	excluded := strings.Join(v.ExcludeHookSets, ",")
	ok, err := runForm(newCopyForm(sources, &v, shareOpts, &hookSets, &excluded), in, out)
	v.HookSets = splitList(hookSets)
	v.ExcludeHookSets = splitList(excluded)
	return v, ok, err
}

// newCopyForm builds the copy form, writing into v and the comma-separated hook-set
// texts. The source is inline like every other select: ←/→ change it, and holding ↑
// to get back to the top can never change it by accident.
func newCopyForm(sources []string, v *CopyValues, shareOpts []ShareOption, hookSets, excluded *string) *huh.Form {
	levels := []huh.Option[string]{huh.NewOption("same as the source", "")}
	for _, o := range shareOpts {
		levels = append(levels, huh.NewOption(o.Value+": "+o.Description, o.Value))
	}
	return newForm(
		section{"Source & target", []huh.Field{
			huh.NewSelect[string]().Key("source").Title("Source profile").
				Options(huh.NewOptions(sources...)...).Inline(true).Value(&v.Source),
			huh.NewInput().Key("target").Title("Target (<harness>/<name>)").Value(&v.Target),
			huh.NewConfirm().Key("overwrite").Title("Overwrite the target if it exists?").Value(&v.Overwrite),
			huh.NewConfirm().Key("adopt").Title("Adopt an existing config dir left at the target's location?").Value(&v.Adopt),
		}},
		section{"Auth", []huh.Field{
			huh.NewInput().Key("auth").Title("Auth kind for a cross-harness target (optional)").Value(&v.Auth),
			huh.NewInput().Key("auth_key").Title("Auth key reference (env:NAME or file:/abs/path; optional)").Value(&v.AuthKey),
		}},
		section{"Overrides", []huh.Field{
			huh.NewInput().Key("label").Title("Label override (optional)").Value(&v.Label),
			huh.NewInput().Key("color").Title("Color override (optional)").Value(&v.Color),
			huh.NewInput().Key("icon").Title("Icon override (optional)").Value(&v.Icon),
		}},
		section{"Sharing", []huh.Field{
			huh.NewSelect[string]().Key("share").Title("Share level of the target").
				Options(levels...).Inline(true).Value(&v.Share),
			huh.NewInput().Key("hook_sets").Title("Hook sets of the target (comma-separated; blank keeps the source's)").Value(hookSets),
			huh.NewInput().Key("exclude_hook_sets").Title("Every-profile hook sets the target skips (comma-separated; blank keeps the source's)").Value(excluded),
		}},
	)
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
	return newForm(section{"", []huh.Field{
		huh.NewConfirm().Key("confirm").Title("Remove profile "+ref+"?").Value(confirmed),
		huh.NewConfirm().Key("purge").Title("Also purge its config dir (credentials and state)?").Value(purge),
	}})
}

// formKeyMap is huh's default keymap with Esc as cancel and ↑/↓ as previous/next
// row for inputs, confirms and selects. Single selects are inline, so huh leaves
// ↑/↓ free for row movement there; multi-selects keep ↑/↓ for their own items.
// Filtering gets no key at all, so Esc never means "clear filter" (huh re-enables a
// disabled filter binding on every update, so disabling it would not stick).
func formKeyMap() *huh.KeyMap {
	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys("ctrl+c", "esc"), key.WithHelp("esc", "cancel"))
	km.Input.Prev = withArrow(km.Input.Prev, "up", "↑", "back")
	km.Input.Next = withArrow(km.Input.Next, "down", "↓", "next")
	km.Confirm.Prev = withArrow(km.Confirm.Prev, "up", "↑", "back")
	km.Confirm.Next = withArrow(km.Confirm.Next, "down", "↓", "next")
	km.Select.Prev = withArrow(km.Select.Prev, "up", "↑", "back")
	km.Select.Next = withArrow(km.Select.Next, "down", "↓", "next")
	km.Select.Filter = key.NewBinding()
	km.MultiSelect.Filter = key.NewBinding()
	return km
}

// withArrow returns b with an arrow key added, shown in the help line as glyph.
func withArrow(b key.Binding, arrow, glyph, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(append(b.Keys(), arrow)...), key.WithHelp(glyph, desc))
}

// formTheme picks the form theme: the terminal's own 16 colors, or no color.
// Section titles are notes, which never take focus, so huh would draw them in the
// dim blurred style; they are drawn bold in the focused title color instead.
func formTheme(color bool) *huh.Theme {
	t := huh.ThemeBase()
	if color {
		t = huh.ThemeBase16()
	}
	t.Blurred.NoteTitle = t.Blurred.NoteTitle.Bold(true).Foreground(t.Focused.Title.GetForeground())
	return t
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
