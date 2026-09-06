package ui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/tobischo/gokeepasslib/v3"
)

// GroupInputModel handles creating or renaming a group.
type GroupInputModel struct {
	input      textinput.Model
	parentUUID gokeepasslib.UUID
	renaming   bool
	width      int
	height     int
}

// SaveGroupMsg signals that the new group should be saved (or renamed).
type SaveGroupMsg struct {
	ParentUUID gokeepasslib.UUID
	Name       string
	Rename     bool
}

func NewGroupInputModel(parentUUID gokeepasslib.UUID, currentName string) GroupInputModel {
	ti := newTextInput()
	ti.Placeholder = "Group name"
	ti.Focus()
	ti.SetWidth(40)
	ti.Prompt = "  Name: "
	setInputPromptStyle(&ti, labelStyle)
	if currentName != "" {
		ti.SetValue(currentName)
	}

	return GroupInputModel{
		input:      ti,
		parentUUID: parentUUID,
	}
}

func (m GroupInputModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m GroupInputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			name := m.input.Value()
			if name == "" {
				return m, nil
			}
			return m, func() tea.Msg {
				return SaveGroupMsg{
					ParentUUID: m.parentUUID,
					Name:       name,
					Rename:     m.renaming,
				}
			}
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m GroupInputModel) View() tea.View {
	var b strings.Builder
	width, _ := viewSize(m.width, m.height)
	sizeInput(&m.input, width)

	b.WriteString("\n")
	title := " New Group "
	if m.renaming {
		title = " Rename Group "
	}
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n\n")
	b.WriteString(m.input.View())
	b.WriteString("\n\n")
	return tea.NewView(screen(b.String(), renderHints(width, "enter: confirm", "esc: cancel", "F1: help"), m.width, m.height, 0))
}
