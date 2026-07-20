package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// UnlockModel handles the password entry screen.
type UnlockModel struct {
	filePath    string
	keyFilePath string
	input       textinput.Model
	err         error
	unlocking   bool // true while the database is being opened in the background
	width       int
	height      int
}

// DatabaseOpenedMsg is sent when the database is successfully opened.
type DatabaseOpenedMsg struct {
	FilePath    string
	Password    string
	KeyFilePath string
}

func NewUnlockModel(filePath, keyFilePath string) UnlockModel {
	ti := textinput.New()
	ti.Placeholder = "Master password"
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '*'
	ti.Focus()
	ti.Width = 40

	return UnlockModel{
		filePath:    filePath,
		keyFilePath: keyFilePath,
		input:       ti,
	}
}

func (m UnlockModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m UnlockModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		// Ignore input while unlocking (except quit): a second enter
		// would start a duplicate key-derivation run.
		if m.unlocking {
			if msg.Type == tea.KeyCtrlC {
				return m, tea.Quit
			}
			return m, nil
		}
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyEnter:
			// Keyfile-only databases have no password: allow an empty
			// password when a key file was supplied.
			if m.input.Value() == "" && m.keyFilePath == "" {
				m.err = fmt.Errorf("password cannot be empty")
				return m, nil
			}
			m.err = nil
			return m, func() tea.Msg {
				return DatabaseOpenedMsg{
					FilePath:    m.filePath,
					Password:    m.input.Value(),
					KeyFilePath: m.keyFilePath,
				}
			}
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m UnlockModel) View() string {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString(titleStyle.Render(" keetui "))
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "  File: %s\n", m.filePath)
	if m.keyFilePath != "" {
		fmt.Fprintf(&b, "  Key:  %s\n", m.keyFilePath)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "  %s\n", m.input.View())

	if m.unlocking {
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("  Unlocking…"))
		b.WriteString("\n")
	} else if m.err != nil {
		b.WriteString("\n")
		b.WriteString(errorMsgStyle.Render(fmt.Sprintf("  Error: %s", m.err)))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  enter: unlock  ctrl+c: quit"))
	b.WriteString("\n")

	return b.String()
}
