package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/tobischo/gokeepasslib/v3"
)

// GroupInputModel handles creating a new group name.
type GroupInputModel struct {
	input      textinput.Model
	parentUUID gokeepasslib.UUID
	width      int
	height     int
}

// SaveGroupMsg signals that the new group should be saved.
type SaveGroupMsg struct {
	ParentUUID gokeepasslib.UUID
	Name       string
}

func NewGroupInputModel(parentUUID gokeepasslib.UUID) GroupInputModel {
	ti := textinput.New()
	ti.Placeholder = "Group name"
	ti.Focus()
	ti.Width = 40
	ti.Prompt = "  Name: "
	ti.PromptStyle = labelStyle

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
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyEsc:
			return m, func() tea.Msg { return CancelEditMsg{} }
		case tea.KeyEnter:
			name := m.input.Value()
			if name == "" {
				return m, nil
			}
			return m, func() tea.Msg {
				return SaveGroupMsg{
					ParentUUID: m.parentUUID,
					Name:       name,
				}
			}
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m GroupInputModel) View() string {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString(titleStyle.Render(" New Group "))
	b.WriteString("\n\n")
	b.WriteString(m.input.View())
	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render(fmt.Sprintf("  enter: create  esc: cancel")))
	b.WriteString("\n")

	return b.String()
}
