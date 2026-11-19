# Form UI: keyboard navigation and canonical huh layout

**Date:** 2026-11-19
**Size:** small change
**Status:** proposed

## Intent

The interactive `aprofile edit` form is awkward to drive. Esc does nothing, ↑/↓ inside a select only moves within that select (so the arrows feel "trapped"), and every select renders as a fully expanded list instead of something that reads like a dropdown. The goal is a form that behaves like a settings screen: arrows move between rows, choice fields take one line, Esc leaves.

## Goals

- Esc cancels any form without saving (same result as Ctrl+C: `edit cancelled`, no store write).
- ↑/↓ move between rows for text inputs, yes/no confirms and single selects.
- Single selects render inline as one row (`Auth kind  ◀ oauth ▶`); ←/→ cycle the value.
- Fields are grouped into titled sections, all visible on one screen.
- A consistent built-in theme and the key help footer.

## Non-goals

- Leaving `huh`. Checked alternatives: tview has true popup dropdowns but means a second, full-screen framework next to Bubble Tea; boba is pre-production; pui is minimal. None justify a switch.
- Upgrading to huh v2 / Bubble Tea v2 (import path and dependency migration, no new form widgets). Separate work.
- Changing what the forms collect or how the CLI applies the values (Constitution VI stays intact: the TUI only gathers input).

## Approaches

1. **Navigation only:** keymap changes plus inline selects. Smallest diff, but leaves a long unsectioned field list.
2. **Navigation plus canonical huh layout (chosen):** approach 1, plus titled groups shown together, a theme and the help footer. Uses only stock huh features, so it stays idiomatic.
3. **tview rewrite:** real collapsed dropdowns, rejected for the framework split and full-screen takeover described above.

## Design

All changes live in `internal/tui/forms.go`, so `add`, `edit`, `copy`, `remove` and the browse-launched forms all get them.

**Keymap** (built once, applied in `runForm` via `form.WithKeyMap`):
- `Quit`: `ctrl+c`, `esc`.
- `Input.Prev`/`Next`, `Confirm.Prev`/`Next`, `Select.Prev`/`Next`: add `up` / `down` to the existing `shift+tab` / `tab`+`enter` bindings.
- Multi-selects keep ↑/↓ for their own items (huh checks `Up`/`Down` before `Prev`/`Next`); Tab/Shift+Tab leave them. The help footer makes this visible.

**Inline selects:** `.Inline(true)` on single selects. huh disables a select's `Up`/`Down` in inline mode, so the added arrow bindings reach `Prev`/`Next` without conflict. Filtering stays off on inline selects so Esc is never captured by a filter.

**Sections** (all fields in ONE huh group, each section headed by a `huh.Note` row that focus skips; empty sections are omitted when the harness hides their fields). Built as one group per section with `huh.LayoutStack` first, this showed two problems in a real terminal: LayoutStack draws no group titles, and huh switches groups asynchronously, so holding ↓ across a section boundary left one row drawn focused while typing went into another. One group has neither problem and also scrolls to keep the focused row visible on a short terminal. Section titles are drawn bold in the focused title color (notes never take focus, so huh would draw them dim). A small layout adds `esc cancel` to huh's help line, and on a field that uses ↑/↓ itself (the copy source list) the help says `tab next` instead of `↓ next`:
- Profile form: *Appearance* (label, color, icon), *Model & auth* (model, endpoint, auth kind, auth key), *Launch* (primary, HERDR_AGENT hint), *Sharing & history* (share level, hook sets, excluded hook sets, history group).
- Copy form: *Source & target* (source, target, overwrite, adopt), *Auth* (auth kind, auth key), *Overrides* (label, color, icon), *Sharing* (share level, hook sets, excluded hook sets).
- Identity and remove forms are short; they get the keymap, theme and inline harness select but no sections.

**Theme:** a built-in huh theme applied in `runForm`, with the key help shown (`WithShowHelp(true)`).

## Calls made by Claude (correct if wrong)

- **Theme:** `huh.ThemeBase16`, because it uses the terminal's own 16 colors and stays readable on light and dark backgrounds. Fall back to `huh.ThemeBase` when color is disabled, matching the browse view's color rule (FR-042).
- **Copy form source select:** inline like every other select. (First built as a scrollable list; review found that holding ↑ to get back to the top moved past Target into the list and silently changed, even wrapped, the source the browse view had already chosen. Cost: picking among many sources means repeated ←/→.)
- **Section names and field grouping** as listed above.

## Left to the builder

- Exact `Height` for the copy source list and multi-selects.
- How to test: unit tests can drive the forms through `WithInput` with scripted key sequences (Esc aborts with ok=false, ↓ advances focus); a final manual check runs `aprofile edit` in a real terminal.
