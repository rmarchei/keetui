package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// ChangePasswordMsg signals that the master password should be changed.
type ChangePasswordMsg struct {
	NewPassword string
}

// PasswordModel handles the master password change form.
type PasswordModel struct {
	newInput     textinput.Model
	confirmInput textinput.Model
	focused      int // 0 = new, 1 = confirm
	err          error
	width        int
	height       int
}

func NewPasswordModel() PasswordModel {
	ni := textinput.New()
	ni.Placeholder = "New master password"
	ni.EchoMode = textinput.EchoPassword
	ni.EchoCharacter = '*'
	ni.Prompt = "  New password:     "
	ni.PromptStyle = labelStyle
	ni.Width = 40
	ni.Focus()
	ni.TextStyle = focusedStyle

	ci := textinput.New()
	ci.Placeholder = "Confirm password"
	ci.EchoMode = textinput.EchoPassword
	ci.EchoCharacter = '*'
	ci.Prompt = "  Confirm password: "
	ci.PromptStyle = labelStyle
	ci.Width = 40

	return PasswordModel{
		newInput:     ni,
		confirmInput: ci,
	}
}

func (m PasswordModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m PasswordModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, func() tea.Msg { return RequestQuitMsg{} }
		case "esc":
			return m, func() tea.Msg { return CancelEditMsg{} }
		case "tab", "down", "shift+tab", "up":
			if m.focused == 0 {
				m.focused = 1
				m.newInput.Blur()
				m.newInput.TextStyle = blurredStyle
				m.confirmInput.Focus()
				m.confirmInput.TextStyle = focusedStyle
			} else {
				m.focused = 0
				m.confirmInput.Blur()
				m.confirmInput.TextStyle = blurredStyle
				m.newInput.Focus()
				m.newInput.TextStyle = focusedStyle
			}
			return m, textinput.Blink
		case "enter":
			newPw := m.newInput.Value()
			confirm := m.confirmInput.Value()
			if newPw == "" {
				m.err = fmt.Errorf("password cannot be empty")
				return m, nil
			}
			if newPw != confirm {
				m.err = fmt.Errorf("passwords do not match")
				return m, nil
			}
			return m, func() tea.Msg {
				return ChangePasswordMsg{NewPassword: newPw}
			}
		}
	}

	var cmd tea.Cmd
	if m.focused == 0 {
		m.newInput, cmd = m.newInput.Update(msg)
	} else {
		m.confirmInput, cmd = m.confirmInput.Update(msg)
	}
	return m, cmd
}

func (m PasswordModel) View() string {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString(titleStyle.Render(" Change Master Password "))
	b.WriteString("\n\n")

	b.WriteString(m.newInput.View())
	b.WriteString("\n")
	b.WriteString(m.confirmInput.View())
	b.WriteString("\n")

	if m.err != nil {
		b.WriteString("\n")
		b.WriteString(errorMsgStyle.Render(fmt.Sprintf("  Error: %s", m.err)))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  tab: switch field  enter: confirm  esc: cancel"))
	b.WriteString("\n")

	return b.String()
}
