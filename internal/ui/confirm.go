package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// ConfirmModel shows a yes/no confirmation prompt.
type ConfirmModel struct {
	message   string
	onConfirm tea.Cmd
	selected  bool // false = No, true = Yes
	width     int
	height    int
}

func NewConfirmModel(message string, onConfirm tea.Cmd) ConfirmModel {
	return ConfirmModel{
		message:   message,
		onConfirm: onConfirm,
		selected:  false,
	}
}

func (m ConfirmModel) Init() tea.Cmd {
	return nil
}

func (m ConfirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			// Buttons are on line 5 (0-indexed): newline, title, newline, message, newline, buttons
			if msg.Y == 5 {
				// No button roughly at x < 14, Yes button after
				if msg.X < 14 {
					m.selected = false
					return m, func() tea.Msg { return CancelEditMsg{} }
				}
				m.selected = true
				return m, m.onConfirm
			}
		}
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			// Route through RequestQuitMsg so the app can warn about
			// unsaved changes, like every other post-unlock view.
			return m, func() tea.Msg { return RequestQuitMsg{} }
		case "y", "Y":
			return m, m.onConfirm
		case "n", "N", "esc":
			return m, func() tea.Msg { return CancelEditMsg{} }
		case "left", "right", "tab", "h", "l":
			m.selected = !m.selected
		case "enter":
			if m.selected {
				return m, m.onConfirm
			}
			return m, func() tea.Msg { return CancelEditMsg{} }
		}
	}

	return m, nil
}

func (m ConfirmModel) View() tea.View {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString(titleStyle.Render(" Confirm "))
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "  %s\n\n", sanitizeDisplay(m.message))

	var no, yes string
	if m.selected {
		yes = selectedItemStyle.Render("  [>Yes ]")
		no = helpStyle.Render("  [ No ]")
	} else {
		no = selectedItemStyle.Render("  [>No ]")
		yes = helpStyle.Render("  [ Yes ]")
	}
	fmt.Fprintf(&b, "  %s  %s\n", no, yes)

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  y: yes  n/esc: no  tab: toggle  enter: confirm"))
	b.WriteString("\n")

	return tea.NewView(b.String())
}
