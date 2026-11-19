package tui

import (
	"reflect"
	"slices"
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
	if !reflect.DeepEqual(formTheme(true), huh.ThemeBase16()) {
		t.Error("formTheme(true) is not ThemeBase16")
	}
	if !reflect.DeepEqual(formTheme(false), huh.ThemeBase()) {
		t.Error("formTheme(false) is not ThemeBase")
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
