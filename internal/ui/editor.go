package ui

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/tobischo/gokeepasslib/v3"

	"github.com/rmarchei/keetui/internal/keepass"
)

const (
	fieldTitle = iota
	fieldUserName
	fieldPassword
	fieldURL
	fieldNotes
	fieldTOTP
	fieldCount
)

var fieldLabels = [fieldCount]string{
	"Title",
	"Username",
	"Password",
	"URL",
	"Notes",
	"TOTP Secret",
}

// EditorModel handles creating or editing an entry.
type EditorModel struct {
	inputs    [fieldCount]textinput.Model
	focused   int
	isNew     bool
	entryUUID gokeepasslib.UUID
	groupUUID gokeepasslib.UUID
	width     int
	height    int
	err       error
	generated bool // true after password was auto-generated
}

// SaveEntryMsg signals that the entry should be saved.
type SaveEntryMsg struct {
	IsNew     bool
	EntryUUID gokeepasslib.UUID
	GroupUUID gokeepasslib.UUID
	Entry     keepass.Entry
}

// CancelEditMsg signals cancellation of editing.
type CancelEditMsg struct{}

func NewEditorModel(entry *keepass.Entry, groupUUID gokeepasslib.UUID) EditorModel {
	var inputs [fieldCount]textinput.Model
	for i := range inputs {
		t := textinput.New()
		t.Prompt = fmt.Sprintf("  %s: ", fieldLabels[i])
		t.PromptStyle = labelStyle
		t.Width = 50
		inputs[i] = t
	}

	inputs[fieldPassword].EchoMode = textinput.EchoPassword
	inputs[fieldPassword].EchoCharacter = '*'
	inputs[fieldTOTP].EchoMode = textinput.EchoPassword
	inputs[fieldTOTP].EchoCharacter = '*'
	inputs[fieldTOTP].Placeholder = "base32 secret or otpauth:// URI"

	m := EditorModel{
		inputs:    inputs,
		groupUUID: groupUUID,
		isNew:     entry == nil,
	}

	if entry != nil {
		m.entryUUID = entry.UUID
		inputs[fieldTitle].SetValue(entry.Title)
		inputs[fieldUserName].SetValue(entry.UserName)
		inputs[fieldPassword].SetValue(entry.Password)
		inputs[fieldURL].SetValue(entry.URL)
		inputs[fieldNotes].SetValue(entry.Notes)
		inputs[fieldTOTP].SetValue(entry.TOTP)
		m.inputs = inputs
	}

	m.inputs[0].Focus()
	m.inputs[0].TextStyle = focusedStyle

	return m
}

func (m EditorModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m EditorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit

		case "esc":
			return m, func() tea.Msg { return CancelEditMsg{} }

		case "tab", "down":
			m.focused = (m.focused + 1) % fieldCount
			return m, m.updateFocus()

		case "shift+tab", "up":
			m.focused = (m.focused - 1 + fieldCount) % fieldCount
			return m, m.updateFocus()

		case "ctrl+s":
			return m, m.save()

		case "ctrl+g":
			pw := generatePassword(20)
			m.inputs[fieldPassword].SetValue(pw)
			m.generated = true
			return m, nil
		}
	}

	// Update only the focused input
	var cmd tea.Cmd
	m.inputs[m.focused], cmd = m.inputs[m.focused].Update(msg)
	return m, cmd
}

func (m *EditorModel) updateFocus() tea.Cmd {
	var cmds []tea.Cmd
	for i := range m.inputs {
		if i == m.focused {
			cmds = append(cmds, m.inputs[i].Focus())
			m.inputs[i].TextStyle = focusedStyle
		} else {
			m.inputs[i].Blur()
			m.inputs[i].TextStyle = blurredStyle
		}
	}
	return tea.Batch(cmds...)
}

func (m EditorModel) save() tea.Cmd {
	return func() tea.Msg {
		return SaveEntryMsg{
			IsNew:     m.isNew,
			EntryUUID: m.entryUUID,
			GroupUUID: m.groupUUID,
			Entry: keepass.Entry{
				Title:    m.inputs[fieldTitle].Value(),
				UserName: m.inputs[fieldUserName].Value(),
				Password: m.inputs[fieldPassword].Value(),
				URL:      m.inputs[fieldURL].Value(),
				Notes:    m.inputs[fieldNotes].Value(),
				TOTP:     m.inputs[fieldTOTP].Value(),
			},
		}
	}
}

func (m EditorModel) View() string {
	var b strings.Builder

	b.WriteString("\n")
	action := "Edit Entry"
	if m.isNew {
		action = "New Entry"
	}
	b.WriteString(titleStyle.Render(fmt.Sprintf(" %s ", action)))
	b.WriteString("\n\n")

	for i := range m.inputs {
		b.WriteString(m.inputs[i].View())
		b.WriteString("\n")
	}

	if m.generated {
		b.WriteString("\n")
		b.WriteString(successStyle.Render("  Password generated!"))
		b.WriteString("\n")
	}

	if m.err != nil {
		b.WriteString("\n")
		b.WriteString(errorMsgStyle.Render(fmt.Sprintf("  Error: %s", m.err)))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  tab/shift+tab: navigate  ctrl+g: generate password  ctrl+s: save  esc: cancel"))
	b.WriteString("\n")

	return b.String()
}

const passwordChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()-_=+"

func generatePassword(length int) string {
	result := make([]byte, length)
	charLen := big.NewInt(int64(len(passwordChars)))
	for i := range result {
		n, err := rand.Int(rand.Reader, charLen)
		if err != nil {
			result[i] = 'x'
			continue
		}
		result[i] = passwordChars[n.Int64()]
	}
	return string(result)
}
