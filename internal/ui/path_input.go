package ui

import (
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// SaveAsMsg requests saving the database to a new path.
type SaveAsMsg struct {
	Path string
}

// SaveAsStartMsg is sent when the user requests "Save As".
type SaveAsStartMsg struct{}

// PathInputModel handles the "Save As" text input for a file path.
type PathInputModel struct {
	input  textinput.Model
	width  int
	height int
}

// NewPathInputModel creates a path input pre-filled with the current path.
func NewPathInputModel(currentPath string) PathInputModel {
	ti := textinput.New()
	ti.Placeholder = "/path/to/database.kdbx"
	ti.Focus()
	ti.Width = 60
	ti.Prompt = "  Path: "
	ti.PromptStyle = labelStyle
	ti.SetValue(currentPath)
	ti.CursorEnd()

	return PathInputModel{
		input: ti,
	}
}

func (m PathInputModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m PathInputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, func() tea.Msg { return RequestQuitMsg{} }
		case tea.KeyEsc:
			return m, func() tea.Msg { return CancelEditMsg{} }
		case tea.KeyEnter:
			path := strings.TrimSpace(m.input.Value())
			if path == "" {
				return m, nil
			}
			// Expand ~ to home directory
			if strings.HasPrefix(path, "~/") {
				if home, err := os.UserHomeDir(); err == nil {
					path = home + path[1:]
				}
			}
			return m, func() tea.Msg {
				return SaveAsMsg{Path: path}
			}
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m PathInputModel) View() string {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString(titleStyle.Render(" Save As "))
	b.WriteString("\n\n")
	b.WriteString(m.input.View())
	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("  enter: save  esc: cancel"))
	b.WriteString("\n")

	return b.String()
}
