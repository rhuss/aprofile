package tui

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// drain runs cmd and feeds its messages back into f, recursing into batches and
// sequences. huh moves focus through internal messages returned as commands, so a
// form can be driven without a terminal. Commands that do not return within 20ms
// (cursor blink timers) are dropped.
func drain(t *testing.T, f *huh.Form, cmd tea.Cmd, depth int) {
	t.Helper()
	if cmd == nil || depth > 30 {
		return
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-ch:
	case <-time.After(20 * time.Millisecond):
		return
	}
	if msg == nil {
		return
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			drain(t, f, c, depth+1)
		}
		return
	}
	if rv := reflect.ValueOf(msg); rv.Kind() == reflect.Slice { // tea's unexported sequenceMsg
		for i := 0; i < rv.Len(); i++ {
			if c, ok := rv.Index(i).Interface().(tea.Cmd); ok {
				drain(t, f, c, depth+1)
			}
		}
		return
	}
	_, next := f.Update(msg)
	drain(t, f, next, depth+1)
}

// start initializes f so its first field has focus.
func start(t *testing.T, f *huh.Form) {
	t.Helper()
	drain(t, f, f.Init(), 0)
}

// press sends each key to f and processes the resulting focus changes.
func press(t *testing.T, f *huh.Form, keys ...tea.KeyType) {
	t.Helper()
	for _, k := range keys {
		_, cmd := f.Update(tea.KeyMsg{Type: k})
		drain(t, f, cmd, 0)
	}
}

// focused returns the key of the field that has focus.
func focused(f *huh.Form) string {
	return f.GetFocusedField().GetKey()
}

func TestFormKeyMap(t *testing.T) {
	km := formKeyMap()
	has := func(name string, keys []string, want ...string) {
		t.Helper()
		for _, w := range want {
			if !slices.Contains(keys, w) {
				t.Errorf("%s keys %v: missing %q", name, keys, w)
			}
		}
	}
	has("Quit", km.Quit.Keys(), "esc", "ctrl+c")
	has("Input.Prev", km.Input.Prev.Keys(), "up", "shift+tab")
	has("Input.Next", km.Input.Next.Keys(), "down", "tab", "enter")
	has("Confirm.Prev", km.Confirm.Prev.Keys(), "up", "shift+tab")
	has("Confirm.Next", km.Confirm.Next.Keys(), "down", "tab", "enter")
	has("Select.Prev", km.Select.Prev.Keys(), "up", "shift+tab")
	has("Select.Next", km.Select.Next.Keys(), "down", "tab", "enter")
	if k := km.Select.Filter.Keys(); len(k) != 0 {
		t.Errorf("Select.Filter keys = %v, want none", k)
	}
	if k := km.MultiSelect.Filter.Keys(); len(k) != 0 {
		t.Errorf("MultiSelect.Filter keys = %v, want none", k)
	}
	has("MultiSelect.Up", km.MultiSelect.Up.Keys(), "up")
	if slices.Contains(km.MultiSelect.Next.Keys(), "down") {
		t.Errorf("MultiSelect.Next must not take down (it moves within the list)")
	}
}

func TestFormTheme(t *testing.T) {
	if !reflect.DeepEqual(formTheme(true).Focused, huh.ThemeBase16().Focused) {
		t.Error("formTheme(true) is not based on ThemeBase16")
	}
	if !reflect.DeepEqual(formTheme(false).Focused, huh.ThemeBase().Focused) {
		t.Error("formTheme(false) is not based on ThemeBase")
	}
	// Section titles are always-blurred notes; they must stand out, not render dim.
	for _, color := range []bool{true, false} {
		th := formTheme(color)
		if !th.Blurred.NoteTitle.GetBold() {
			t.Errorf("color=%v: section title (Blurred.NoteTitle) is not bold", color)
		}
		if got, want := th.Blurred.NoteTitle.GetForeground(), th.Focused.Title.GetForeground(); got != want {
			t.Errorf("color=%v: section title color = %v, want the focused title color %v", color, got, want)
		}
	}
	t.Setenv("NO_COLOR", "1")
	if formsColor() {
		t.Error("formsColor() = true with NO_COLOR set")
	}
}

func TestRemoveFormEscAborts(t *testing.T) {
	var c, p bool
	f := newRemoveForm("claude/work", &c, &p)
	start(t, f)
	press(t, f, tea.KeyEsc)
	if f.State != huh.StateAborted {
		t.Fatalf("state = %v, want aborted", f.State)
	}
}

func TestRemoveFormArrowsMoveRows(t *testing.T) {
	var c, p bool
	f := newRemoveForm("claude/work", &c, &p)
	start(t, f)
	if got := focused(f); got != "confirm" {
		t.Fatalf("initial focus = %q, want confirm", got)
	}
	press(t, f, tea.KeyDown)
	if got := focused(f); got != "purge" {
		t.Fatalf("after down focus = %q, want purge", got)
	}
	press(t, f, tea.KeyUp)
	if got := focused(f); got != "confirm" {
		t.Fatalf("after up focus = %q, want confirm", got)
	}
}

func TestIdentityFormInlineHarness(t *testing.T) {
	id := Identity{Harness: "claude"}
	f := newIdentityForm([]string{"claude", "codex"}, &id)
	start(t, f)
	if got := focused(f); got != "harness" {
		t.Fatalf("initial focus = %q, want harness", got)
	}
	press(t, f, tea.KeyRight)
	if id.Harness != "codex" {
		t.Errorf("after right Harness = %q, want codex", id.Harness)
	}
	if got := focused(f); got != "harness" {
		t.Fatalf("after right focus = %q, want harness", got)
	}
	press(t, f, tea.KeyDown)
	if got := focused(f); got != "name" {
		t.Fatalf("after down focus = %q, want name", got)
	}
}

// walk records the focused field, then presses k until focus stops changing,
// recording each newly focused field.
func walk(t *testing.T, f *huh.Form, k tea.KeyType) []string {
	t.Helper()
	seen := []string{focused(f)}
	for i := 0; i < 40; i++ {
		press(t, f, k)
		cur := focused(f)
		if cur == seen[len(seen)-1] {
			break
		}
		seen = append(seen, cur)
	}
	return seen
}

func fullProfileConfig() ProfileFormConfig {
	return ProfileFormConfig{
		AuthKinds:        []string{"oauth", "api-key"},
		SupportsModel:    true,
		SupportsEndpoint: true,
		ShareOptions: []ShareOption{
			{Value: "content", Description: "share content"},
			{Value: "none", Description: "share nothing"},
		},
		HookSetOptions:   []string{"a"},
		EveryProfileSets: []string{"b"},
		GroupOptions:     []GroupOption{{Value: "", Label: "isolated"}, {Value: "home", Label: "home"}},
	}
}

func TestProfileFormRowOrder(t *testing.T) {
	v := FormValues{}
	f := newProfileForm(fullProfileConfig(), &v)
	start(t, f)
	got := walk(t, f, tea.KeyDown)
	want := []string{"label", "color", "icon", "model", "endpoint", "auth_kind", "auth_key",
		"herdr_agent", "primary", "share", "hook_sets"}
	if !slices.Equal(got, want) {
		t.Fatalf("down walk = %v, want %v", got, want)
	}
	press(t, f, tea.KeyTab)
	if got := focused(f); got != "exclude_hook_sets" {
		t.Fatalf("tab from hook_sets: focus = %q, want exclude_hook_sets", got)
	}
	press(t, f, tea.KeyTab)
	if got := focused(f); got != "history_group" {
		t.Fatalf("tab from exclude_hook_sets: focus = %q, want history_group", got)
	}
	press(t, f, tea.KeyDown, tea.KeyDown)
	if got := focused(f); got != "history_group" {
		t.Fatalf("down on last row: focus = %q, want history_group", got)
	}
	if f.State != huh.StateNormal {
		t.Fatalf("down on last row changed state to %v; it must not submit", f.State)
	}
}

func TestProfileFormUpCrossesSections(t *testing.T) {
	v := FormValues{}
	f := newProfileForm(fullProfileConfig(), &v)
	start(t, f)
	press(t, f, tea.KeyDown, tea.KeyDown, tea.KeyDown)
	if got := focused(f); got != "model" {
		t.Fatalf("focus = %q, want model", got)
	}
	press(t, f, tea.KeyUp)
	if got := focused(f); got != "icon" {
		t.Fatalf("up from model: focus = %q, want icon", got)
	}
}

func TestProfileFormInlineSelect(t *testing.T) {
	cfg := fullProfileConfig()
	cfg.Values = FormValues{AuthKind: "api-key", Share: "none"}
	v := cfg.Values
	f := newProfileForm(cfg, &v)
	start(t, f)
	for i := 0; i < 10 && focused(f) != "auth_kind"; i++ {
		press(t, f, tea.KeyDown)
	}
	press(t, f, tea.KeyDown, tea.KeyUp)
	if got := focused(f); got != "auth_kind" {
		t.Fatalf("down+up: focus = %q, want auth_kind", got)
	}
	if v.AuthKind != "api-key" {
		t.Fatalf("down+up changed AuthKind to %q", v.AuthKind)
	}
	press(t, f, tea.KeyLeft)
	if v.AuthKind != "oauth" {
		t.Errorf("left: AuthKind = %q, want oauth", v.AuthKind)
	}
	if got := focused(f); got != "auth_kind" {
		t.Fatalf("left moved focus to %q", got)
	}
	for i := 0; i < 10 && focused(f) != "share"; i++ {
		press(t, f, tea.KeyDown)
	}
	if got := focused(f); got != "share" {
		t.Fatalf("never reached share; focus = %q", got)
	}
	if v.Share != "none" {
		t.Errorf("share = %q, want the pre-filled none", v.Share)
	}
}

func TestProfileFormMinimalSections(t *testing.T) {
	v := FormValues{}
	f := newProfileForm(ProfileFormConfig{}, &v)
	start(t, f)
	got := walk(t, f, tea.KeyDown)
	want := []string{"label", "color", "icon", "auth_key", "herdr_agent", "primary"}
	if !slices.Equal(got, want) {
		t.Fatalf("down walk = %v, want %v", got, want)
	}
	if strings.Contains(f.View(), "Sharing & history") {
		t.Error("empty Sharing & history section is rendered")
	}
}

func newTestCopyForm(v *CopyValues) *huh.Form {
	var hooks, excluded string
	return newCopyForm([]string{"claude/a", "claude/b", "claude/c"}, v,
		[]ShareOption{{Value: "content", Description: "share content"}, {Value: "none", Description: "share nothing"}},
		&hooks, &excluded)
}

func TestCopyFormSourceList(t *testing.T) {
	v := CopyValues{Source: "claude/a"}
	f := newTestCopyForm(&v)
	start(t, f)
	if got := focused(f); got != "source" {
		t.Fatalf("initial focus = %q, want source", got)
	}
	press(t, f, tea.KeyDown)
	if got := focused(f); got != "source" {
		t.Fatalf("down moved focus to %q; the source list keeps it", got)
	}
	if v.Source != "claude/b" {
		t.Errorf("down: Source = %q, want claude/b", v.Source)
	}
	press(t, f, tea.KeyTab)
	if got := focused(f); got != "target" {
		t.Fatalf("tab from source: focus = %q, want target", got)
	}
}

func TestCopyFormSourceHelpSaysTab(t *testing.T) {
	v := CopyValues{Source: "claude/a"}
	f := newTestCopyForm(&v)
	start(t, f)
	view := f.View()
	if strings.Contains(view, "↓ next") {
		t.Errorf("source list help offers ↓ next, but ↓ moves within the list:\n%s", view)
	}
	if !strings.Contains(view, "tab next") {
		t.Errorf("source list help is missing tab next:\n%s", view)
	}
	if strings.Contains(view, "back") {
		t.Errorf("first row offers back, which is disabled there:\n%s", view)
	}
}

func TestCopyFormRowOrder(t *testing.T) {
	v := CopyValues{Source: "claude/a"}
	f := newTestCopyForm(&v)
	start(t, f)
	press(t, f, tea.KeyTab)
	got := walk(t, f, tea.KeyDown)
	want := []string{"target", "overwrite", "adopt", "auth", "auth_key", "label", "color", "icon",
		"share", "hook_sets", "exclude_hook_sets"}
	if !slices.Equal(got, want) {
		t.Fatalf("down walk = %v, want %v", got, want)
	}
	if f.State != huh.StateNormal {
		t.Fatalf("down on last row changed state to %v; it must not submit", f.State)
	}
}

func TestCopyFormInlineShare(t *testing.T) {
	v := CopyValues{Source: "claude/a"}
	f := newTestCopyForm(&v)
	start(t, f)
	press(t, f, tea.KeyTab)
	for i := 0; i < 12 && focused(f) != "share"; i++ {
		press(t, f, tea.KeyDown)
	}
	if got := focused(f); got != "share" {
		t.Fatalf("never reached share; focus = %q", got)
	}
	if v.Share != "" {
		t.Fatalf("initial Share = %q, want \"\" (same as the source)", v.Share)
	}
	press(t, f, tea.KeyRight)
	if v.Share != "content" {
		t.Errorf("right: Share = %q, want content", v.Share)
	}
}

func TestProfileFormShowsSectionTitles(t *testing.T) {
	v := FormValues{}
	f := newProfileForm(fullProfileConfig(), &v)
	start(t, f)
	view := f.View()
	for _, title := range []string{"Appearance", "Model & auth", "Launch", "Sharing & history"} {
		if !strings.Contains(view, title) {
			t.Errorf("view is missing section title %q", title)
		}
	}
}

// focusBlocks counts runs of consecutive lines carrying the focus bar; each run is
// one field drawn as focused.
func focusBlocks(view string) int {
	n, in := 0, false
	for _, line := range strings.Split(view, "\n") {
		bar := strings.Contains(line, "┃")
		if bar && !in {
			n++
		}
		in = bar
	}
	return n
}

func TestProfileFormDrawsOneFocusedRow(t *testing.T) {
	v := FormValues{}
	f := newProfileForm(fullProfileConfig(), &v)
	start(t, f)
	for i := 0; i < 9; i++ {
		if n := focusBlocks(f.View()); n != 1 {
			t.Fatalf("at %q: %d rows drawn focused, want 1:\n%s", focused(f), n, f.View())
		}
		press(t, f, tea.KeyDown)
	}
}

func TestProfileFormUpOnFirstRowStays(t *testing.T) {
	v := FormValues{}
	f := newProfileForm(fullProfileConfig(), &v)
	start(t, f)
	press(t, f, tea.KeyUp)
	if got := focused(f); got != "label" {
		t.Fatalf("up on first row: focus = %q, want label", got)
	}
	if n := focusBlocks(f.View()); n != 1 {
		t.Fatalf("up on first row: %d rows drawn focused, want 1", n)
	}
}

// TestProfileFormIsOneGroup pins the single-group structure: huh switches groups
// asynchronously, and a burst of ↓ (a held key) across a group boundary left one
// row drawn focused while typing went into another. Within one group every move is
// synchronous. Section titles are Note rows, which focus skips.
func TestProfileFormIsOneGroup(t *testing.T) {
	v := FormValues{}
	f := newProfileForm(fullProfileConfig(), &v)
	start(t, f)
	f.NextGroup() // applies itself; on the only group, "next group" means submit
	if f.State != huh.StateCompleted {
		t.Fatalf("NextGroup left state %v with focus on %q; the form must be a single group",
			f.State, focused(f))
	}
}

func TestFormHelpShowsArrowsAndEsc(t *testing.T) {
	var c, p bool
	f := newRemoveForm("claude/work", &c, &p)
	start(t, f)
	view := f.View()
	for _, want := range []string{"esc cancel", "↓ next"} {
		if !strings.Contains(view, want) {
			t.Errorf("help footer is missing %q:\n%s", want, view)
		}
	}
}

func TestProfileFormEscAborts(t *testing.T) {
	v := FormValues{}
	f := newProfileForm(fullProfileConfig(), &v)
	start(t, f)
	press(t, f, tea.KeyDown, tea.KeyDown, tea.KeyEsc)
	if f.State != huh.StateAborted {
		t.Fatalf("state = %v, want aborted", f.State)
	}
}
