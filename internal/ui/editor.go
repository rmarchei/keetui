package ui

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/tobischo/gokeepasslib/v3"

	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/keepass"
)

const (
	fieldTitle = iota
	fieldUserName
	fieldPassword
	fieldURL
	fieldNotes
	fieldTOTP
	fixedFieldCount // number of built-in fields
)

var fieldLabels = [fixedFieldCount]string{
	"Title",
	"Username",
	"Password",
	"URL",
	"Notes",
	"TOTP Secret",
}

// EditorModel handles creating or editing an entry.
type EditorModel struct {
	inputs       [fixedFieldCount]textinput.Model
	customInputs []customFieldInput
	focused      int
	isNew        bool
	entryUUID    gokeepasslib.UUID
	groupUUID    gokeepasslib.UUID
	width        int
	height       int
	err          error
	generated    bool // true after password was auto-generated
	cfg          config.Config
}

type customFieldInput struct {
	key       textinput.Model
	value     textinput.Model
	protected bool
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

func NewEditorModel(entry *keepass.Entry, groupUUID gokeepasslib.UUID, cfg config.Config) EditorModel {
	var inputs [fixedFieldCount]textinput.Model
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
		cfg:       cfg,
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
		for _, cf := range entry.CustomFields {
			m.customInputs = append(m.customInputs, newCustomFieldInput(cf.Key, cf.Value, cf.Protected))
		}
	}

	m.inputs[0].Focus()
	m.inputs[0].TextStyle = focusedStyle

	return m
}

func newCustomFieldInput(key, value string, protected bool) customFieldInput {
	k := textinput.New()
	k.Prompt = "  Key: "
	k.PromptStyle = labelStyle
	k.Width = 20
	k.SetValue(key)

	v := textinput.New()
	v.Prompt = "  Value: "
	v.PromptStyle = labelStyle
	v.Width = 40
	v.SetValue(value)
	if protected {
		v.EchoMode = textinput.EchoPassword
		v.EchoCharacter = '*'
	}

	return customFieldInput{key: k, value: v, protected: protected}
}

func (m EditorModel) totalFields() int {
	return fixedFieldCount + len(m.customInputs)*2
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
			return m, func() tea.Msg { return RequestQuitMsg{} }

		case "esc":
			return m, func() tea.Msg { return CancelEditMsg{} }

		case "tab", "down":
			m.focused = (m.focused + 1) % m.totalFields()
			return m, m.updateFocus()

		case "shift+tab", "up":
			m.focused = (m.focused - 1 + m.totalFields()) % m.totalFields()
			return m, m.updateFocus()

		case "ctrl+s":
			return m, m.save()

		case "ctrl+g":
			pw, err := generatePassword(m.cfg.PasswordLen())
			if err != nil {
				m.err = fmt.Errorf("password generation failed: %w", err)
				return m, nil
			}
			m.inputs[fieldPassword].SetValue(pw)
			m.generated = true
			return m, nil

		case "ctrl+a":
			// Add a new custom field
			m.customInputs = append(m.customInputs, newCustomFieldInput("", "", false))
			m.focused = fixedFieldCount + (len(m.customInputs)-1)*2
			return m, m.updateFocus()

		case "ctrl+d":
			// Delete the custom field that is currently focused
			if m.focused >= fixedFieldCount && len(m.customInputs) > 0 {
				cfIdx := (m.focused - fixedFieldCount) / 2
				if cfIdx < len(m.customInputs) {
					m.customInputs = append(m.customInputs[:cfIdx], m.customInputs[cfIdx+1:]...)
					if m.focused >= m.totalFields() {
						m.focused = m.totalFields() - 1
						if m.focused < 0 {
							m.focused = 0
						}
					}
					return m, m.updateFocus()
				}
			}
		}
	}

	// Update only the focused input
	var cmd tea.Cmd
	if m.focused < fixedFieldCount {
		m.inputs[m.focused], cmd = m.inputs[m.focused].Update(msg)
	} else {
		cfIdx := (m.focused - fixedFieldCount) / 2
		isValue := (m.focused-fixedFieldCount)%2 == 1
		if cfIdx < len(m.customInputs) {
			if isValue {
				m.customInputs[cfIdx].value, cmd = m.customInputs[cfIdx].value.Update(msg)
			} else {
				m.customInputs[cfIdx].key, cmd = m.customInputs[cfIdx].key.Update(msg)
			}
		}
	}
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
	for ci := range m.customInputs {
		keyIdx := fixedFieldCount + ci*2
		valIdx := keyIdx + 1
		if keyIdx == m.focused {
			cmds = append(cmds, m.customInputs[ci].key.Focus())
			m.customInputs[ci].key.TextStyle = focusedStyle
			m.customInputs[ci].value.Blur()
			m.customInputs[ci].value.TextStyle = blurredStyle
		} else if valIdx == m.focused {
			m.customInputs[ci].key.Blur()
			m.customInputs[ci].key.TextStyle = blurredStyle
			cmds = append(cmds, m.customInputs[ci].value.Focus())
			m.customInputs[ci].value.TextStyle = focusedStyle
		} else {
			m.customInputs[ci].key.Blur()
			m.customInputs[ci].key.TextStyle = blurredStyle
			m.customInputs[ci].value.Blur()
			m.customInputs[ci].value.TextStyle = blurredStyle
		}
	}
	return tea.Batch(cmds...)
}

func (m EditorModel) save() tea.Cmd {
	return func() tea.Msg {
		entry := keepass.Entry{
			Title:    m.inputs[fieldTitle].Value(),
			UserName: m.inputs[fieldUserName].Value(),
			Password: m.inputs[fieldPassword].Value(),
			URL:      m.inputs[fieldURL].Value(),
			Notes:    m.inputs[fieldNotes].Value(),
			TOTP:     m.inputs[fieldTOTP].Value(),
		}
		for _, cf := range m.customInputs {
			key := cf.key.Value()
			val := cf.value.Value()
			if key != "" {
				entry.CustomFields = append(entry.CustomFields, keepass.CustomField{
					Key:       key,
					Value:     val,
					Protected: cf.protected,
				})
			}
		}
		return SaveEntryMsg{
			IsNew:     m.isNew,
			EntryUUID: m.entryUUID,
			GroupUUID: m.groupUUID,
			Entry:     entry,
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

	if len(m.customInputs) > 0 {
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("  Custom Fields:"))
		b.WriteString("\n")
		for _, cf := range m.customInputs {
			b.WriteString(cf.key.View())
			b.WriteString("  ")
			b.WriteString(cf.value.View())
			b.WriteString("\n")
		}
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
	b.WriteString(helpStyle.Render("  tab/shift+tab: navigate  ctrl+g: gen password  ctrl+a: add field  ctrl+d: del field  ctrl+s: save  esc: cancel"))
	b.WriteString("\n")

	return b.String()
}

const passwordChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()-_=+"

func generatePassword(length int) (string, error) {
	result := make([]byte, length)
	charLen := big.NewInt(int64(len(passwordChars)))
	for i := range result {
		n, err := rand.Int(rand.Reader, charLen)
		if err != nil {
			return "", fmt.Errorf("random number generation failed: %w", err)
		}
		result[i] = passwordChars[n.Int64()]
	}
	return string(result), nil
}
