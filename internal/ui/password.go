package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
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
	ni := newTextInput()
	ni.Placeholder = "New master password"
	ni.EchoMode = textinput.EchoPassword
	ni.EchoCharacter = '*'
	ni.Prompt = "  New password:     "
	setInputPromptStyle(&ni, labelStyle.UnsetWidth())
	ni.SetWidth(40)
	ni.Focus()
	setInputTextStyle(&ni, focusedStyle)

	ci := newTextInput()
	ci.Placeholder = "Confirm password"
	ci.EchoMode = textinput.EchoPassword
	ci.EchoCharacter = '*'
	ci.Prompt = "  Confirm password: "
	setInputPromptStyle(&ci, labelStyle.UnsetWidth())
	ci.SetWidth(40)

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
		sizeInput(&m.newInput, msg.Width)
		sizeInput(&m.confirmInput, msg.Width)
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, func() tea.Msg { return RequestQuitMsg{} }
		case "esc":
			return m, func() tea.Msg { return CancelEditMsg{} }
		case "tab", "down", "shift+tab", "up":
			if m.focused == 0 {
				m.focused = 1
				m.newInput.Blur()
				setInputTextStyle(&m.newInput, blurredStyle)
				m.confirmInput.Focus()
				setInputTextStyle(&m.confirmInput, focusedStyle)
			} else {
				m.focused = 0
				m.confirmInput.Blur()
				setInputTextStyle(&m.confirmInput, blurredStyle)
				m.newInput.Focus()
				setInputTextStyle(&m.newInput, focusedStyle)
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

func (m PasswordModel) View() tea.View {
	var b strings.Builder
	width, height := viewSize(m.width, m.height)
	sizeInput(&m.newInput, width)
	sizeInput(&m.confirmInput, width)

	b.WriteString("\n")
	b.WriteString(titleStyle.Render(" Change Master Password "))
	b.WriteString("\n\n")

	start := strings.Count(wrapContent(b.String(), width), "\n")
	newView := wrapContent(m.newInput.View(), width)
	confirmView := wrapContent(m.confirmInput.View(), width)
	field := fieldRows{start, start + strings.Count(newView, "\n")}
	if m.focused == 1 {
		field.start = field.end + 1
		field.end = field.start + strings.Count(confirmView, "\n")
	}
	b.WriteString(newView)
	b.WriteString("\n")
	b.WriteString(confirmView)
	b.WriteString("\n")

	if m.err != nil {
		b.WriteString("\n")
		b.WriteString(errorMsgStyle.Render(fmt.Sprintf("  Error: %s", m.err)))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	footer := renderHints(width, "enter: confirm", "esc: cancel", "tab: switch field", "F1: help")
	offset := followField(0, field, bodyHeight(height, footer))
	return tea.NewView(screen(wrapContent(b.String(), width), footer, width, height, offset))
}
