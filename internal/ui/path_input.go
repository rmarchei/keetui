package ui

import (
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// SaveAsMsg requests saving the database to a new path.
type SaveAsMsg struct {
	Path    string
	Upgrade bool // when true, upgrade KDBX 3.1 → 4 before saving
}

// SaveAsStartMsg is sent when the user requests "Save As".
type SaveAsStartMsg struct{}

const (
	focusPath   = 0
	focusFormat = 1
)

// PathInputModel handles the "Save As" text input for a file path.
type PathInputModel struct {
	input      textinput.Model
	format     string // current format description (e.g. "KDBX 3.1 (AES)")
	canUpgrade bool   // true if database can be upgraded to KDBX 4
	upgrade    bool   // user selected KDBX 4
	focused    int    // focusPath or focusFormat
	width      int
	height     int
}

// NewPathInputModel creates a path input pre-filled with the current path.
func NewPathInputModel(currentPath, format string, canUpgrade bool) PathInputModel {
	ti := newTextInput()
	ti.Placeholder = "/path/to/database.kdbx"
	ti.Focus()
	ti.SetWidth(60)
	ti.Prompt = "  Path:    "
	setInputPromptStyle(&ti, labelStyle)
	ti.SetValue(currentPath)
	ti.CursorEnd()

	return PathInputModel{
		input:      ti,
		format:     format,
		canUpgrade: canUpgrade,
		focused:    focusPath,
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
		sizeInput(&m.input, msg.Width)
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, func() tea.Msg { return RequestQuitMsg{} }
		case "esc":
			return m, func() tea.Msg { return CancelEditMsg{} }
		case "enter":
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
			path = filepath.Clean(path)
			upgrade := m.canUpgrade && m.upgrade
			return m, func() tea.Msg {
				return SaveAsMsg{Path: path, Upgrade: upgrade}
			}
		case "tab", "down":
			if m.canUpgrade {
				return m.toggleFocus(), nil
			}
		case "shift+tab", "up":
			if m.canUpgrade {
				return m.toggleFocus(), nil
			}
		}

		// When focused on format, left/right/space toggle the selection
		if m.focused == focusFormat && m.canUpgrade {
			switch msg.String() {
			case "left", "right", "space", "h", "l":
				m.upgrade = !m.upgrade
				return m, nil
			}
		}
	}

	// Only pass key events to text input when it's focused
	if m.focused == focusPath {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m PathInputModel) toggleFocus() PathInputModel {
	if m.focused == focusPath {
		m.focused = focusFormat
		m.input.Blur()
	} else {
		m.focused = focusPath
		m.input.Focus()
	}
	return m
}

func (m PathInputModel) View() tea.View {
	var b strings.Builder
	width, _ := viewSize(m.width, m.height)
	sizeInput(&m.input, width)

	b.WriteString("\n")
	b.WriteString(titleStyle.Render(" Save As "))
	b.WriteString("\n\n")
	b.WriteString(m.input.View())
	b.WriteString("\n")

	// Format line
	if m.canUpgrade {
		var formatStr string
		if m.upgrade {
			formatStr = m.formatOption("Upgrade to KDBX 4 (Argon2)", true) + "  " + m.formatOption(m.format, false)
		} else {
			formatStr = m.formatOption(m.format, true) + "  " + m.formatOption("Upgrade to KDBX 4 (Argon2)", false)
		}
		b.WriteString("  " + labelStyle.Render("Format:") + " " + formatStr + "\n")
	} else {
		b.WriteString("  " + labelStyle.Render("Format:") + " " + valueStyle.Render(m.format) + "\n")
	}

	b.WriteString("\n")
	help := "  enter: save  esc: cancel"
	if m.canUpgrade {
		help = "  tab: switch field  enter: save  esc: cancel"
	}
	footer := renderHints(width, append(strings.Split(strings.TrimSpace(help), "  "), "F1: help")...)
	return tea.NewView(screen(wrapContent(b.String(), width), footer, m.width, m.height, 0))
}

// formatOption renders a format choice, highlighted when selected.
func (m PathInputModel) formatOption(label string, selected bool) string {
	if selected {
		if m.focused == focusFormat {
			return focusedStyle.Bold(true).Render("[" + label + "]")
		}
		return valueStyle.Render("[" + label + "]")
	}
	return helpStyle.Render(" " + label + " ")
}
