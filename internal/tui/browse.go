package tui

import (
	"io"
	"sort"

	tea "github.com/charmbracelet/bubbletea"
)

// Action is what the user chose in the browse view. Each one maps to a CLI command
// (SC-008): ActionAdd->add, ActionEdit->edit, ActionCopy->copy, ActionRemove->remove.
type Action int

const (
	ActionQuit Action = iota
	ActionAdd
	ActionEdit
	ActionCopy
	ActionRemove
)

// BrowseResult is what RunBrowse returns: the chosen action and, for edit/copy/remove,
// the highlighted profile.
type BrowseResult struct {
	Action   Action
	Selected *ProfileRow
}

const browseHelp = "\n↑/↓ move · a add · e edit · c copy · d remove · q quit\n"

// browseModel is the bubbletea model for the browse view. It only moves a cursor and
// records the chosen action; the CLI performs the action through the same operations
// its commands use (Constitution VI).
type browseModel struct {
	rows   []ProfileRow
	color  bool
	cursor int
	result BrowseResult
}

func (m browseModel) Init() tea.Cmd { return nil }

func (m browseModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "ctrl+c", "q", "esc":
		m.result = BrowseResult{Action: ActionQuit}
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.rows)-1 {
			m.cursor++
		}
	case "a":
		m.result = BrowseResult{Action: ActionAdd}
		return m, tea.Quit
	case "e":
		m.result = BrowseResult{Action: ActionEdit, Selected: m.selected()}
		return m, tea.Quit
	case "c":
		m.result = BrowseResult{Action: ActionCopy, Selected: m.selected()}
		return m, tea.Quit
	case "d", "r":
		m.result = BrowseResult{Action: ActionRemove, Selected: m.selected()}
		return m, tea.Quit
	}
	return m, nil
}

func (m browseModel) View() string {
	return renderBrowse(m.rows, m.cursor, m.color) + browseHelp
}

// selected returns a copy of the row under the cursor in the same sorted order the
// view renders, or nil when there are no rows.
func (m browseModel) selected() *ProfileRow {
	if len(m.rows) == 0 {
		return nil
	}
	ordered := append([]ProfileRow(nil), m.rows...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Harness != ordered[j].Harness {
			return ordered[i].Harness < ordered[j].Harness
		}
		return ordered[i].Name < ordered[j].Name
	})
	if m.cursor < 0 || m.cursor >= len(ordered) {
		return nil
	}
	r := ordered[m.cursor]
	return &r
}

// RunBrowse runs the interactive browse view against the given terminal streams and
// returns the user's chosen action. The caller (the CLI) supplies the already-built
// display rows and whether color is enabled (FR-042); it then performs the chosen
// action through its own operations.
func RunBrowse(in io.Reader, out io.Writer, rows []ProfileRow, color bool) (BrowseResult, error) {
	m := browseModel{rows: rows, color: color}
	p := tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(out))
	final, err := p.Run()
	if err != nil {
		return BrowseResult{}, err
	}
	return final.(browseModel).result, nil
}
