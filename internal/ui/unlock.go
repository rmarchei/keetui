package ui

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// UnlockModel handles the password entry screen.
type UnlockModel struct {
	filePath    string
	keyFilePath string
	input       textinput.Model
	err         error
	notice      string // why the screen shows, when that is no error ("Locked.")
	unlocking   bool   // true while the database is being opened in the background
	missing     string // what is wrong with the files' paths (see fileProblem); "" when they are there
	width       int
	height      int
	errorOffset int
}

// fileCheckInterval is how often the unlock screen looks at its files again,
// so that its warning appears and goes without a key press: a volume
// unmounted while keetui was locked, then mounted again.
var fileCheckInterval = 2 * time.Second

// unlockFilesMsg reports what checkUnlockFiles found.
type unlockFilesMsg struct {
	gen     int    // AppModel.fileCheckGen when the check was started
	missing string // fileProblem's answer
}

// checkUnlockFiles looks at the database and key file paths after wait. It
// runs as a command: a stat on an unresponsive mount can block.
func checkUnlockFiles(gen int, dbPath, keyPath string, wait time.Duration) tea.Cmd {
	check := func(time.Time) tea.Msg {
		return unlockFilesMsg{gen: gen, missing: fileProblem(dbPath, keyPath)}
	}
	if wait <= 0 {
		return func() tea.Msg { return check(time.Time{}) }
	}
	return tea.Tick(wait, check)
}

// fileProblem describes a database or key file path with nothing to open,
// "" when both are there. Other errors (permissions) are left to the open,
// which reports them.
func fileProblem(dbPath, keyPath string) string {
	for _, f := range []struct{ path, name string }{{dbPath, "Database file"}, {keyPath, "Key file"}} {
		if f.path == "" {
			continue
		}
		switch info, err := os.Stat(f.path); {
		case errors.Is(err, fs.ErrNotExist):
			return f.name + " not found"
		case err == nil && info.IsDir():
			return f.name + " path is a folder"
		}
	}
	return ""
}

// DatabaseOpenedMsg is sent when the database is successfully opened.
type DatabaseOpenedMsg struct {
	FilePath    string
	Password    string
	KeyFilePath string
}

func NewUnlockModel(filePath, keyFilePath string) UnlockModel {
	ti := newTextInput()
	ti.Placeholder = "Master password"
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '*'
	ti.Focus()
	ti.SetWidth(40)

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
		sizeInput(&m.input, max(1, msg.Width-2))
		return m, nil

	case tea.KeyPressMsg:
		if quitsUnlock(msg) {
			return m, tea.Quit
		}
		// Ignore other input while unlocking: a second enter would start a
		// duplicate key-derivation run.
		if m.unlocking {
			return m, nil
		}
		switch msg.String() {
		case "pgdown", "pgup":
			if m.err != nil {
				_, height := viewSize(m.width, m.height)
				step := max(1, height-strings.Count(m.header(), "\n")-lipgloss.Height(m.footer()))
				if msg.String() == "pgdown" {
					m.errorOffset += step
				} else {
					m.errorOffset -= step
				}
				m.errorOffset = min(max(0, m.errorOffset), max(0, lipgloss.Height(m.errorContent())-step))
			}
			return m, nil
		case "enter":
			m.errorOffset = 0
			// Keyfile-only databases have no password: allow an empty
			// password when a key file was supplied.
			if m.input.Value() == "" && m.keyFilePath == "" {
				m.err = fmt.Errorf("password cannot be empty")
				return m, nil
			}
			m.err, m.notice = nil, ""
			// Unlocking from now on, not from when the app gets the open
			// request: an enter queued behind this one is ignored.
			m.unlocking = true
			return m, func() tea.Msg {
				return DatabaseOpenedMsg{
					FilePath:    m.filePath,
					Password:    m.input.Value(),
					KeyFilePath: m.keyFilePath,
				}
			}
		}
	}

	if m.unlocking {
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m UnlockModel) header() string {
	var b strings.Builder
	width, _ := viewSize(m.width, m.height)

	b.WriteString(header(width, "", "Unlock"))
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "  File: %s\n", pathLabel(m.filePath, width-8))
	if m.keyFilePath != "" {
		fmt.Fprintf(&b, "  Key:  %s\n", pathLabel(m.keyFilePath, width-8))
	}
	if m.missing != "" {
		fmt.Fprintf(&b, " %s\n", warnStyle.Render(m.missing))
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "  %s\n", m.input.View())
	return b.String()
}

func (m UnlockModel) errorContent() string {
	if m.err == nil {
		return ""
	}
	width, _ := viewSize(m.width, m.height)
	return wrapContent(errorMsgStyle.Render("Error: "+sanitizeDisplay(m.err.Error())), width)
}

func (m UnlockModel) footer() string {
	hints := []string{"enter: unlock"}
	if _, fits := m.card(); m.err != nil && !fits {
		// Scrolling is the only way to read a long error: keep it ahead of quit.
		hints = append(hints, "pgup/pgdown: error")
	}
	return alignRight(renderHints(m.width, append(hints, "ctrl+o: settings", "ctrl+c/esc: quit")...), Version, m.width)
}

// quitsUnlock reports a key that quits from the unlock screen: nothing is
// open there that leaving could lose, and settings opened from it return
// to it on esc, so a second esc leaves keetui.
func quitsUnlock(msg tea.KeyPressMsg) bool {
	s := msg.String()
	return s == "ctrl+c" || s == "esc"
}

// card lays the form out as a box centered above the footer, its border in
// the error color after a failed attempt. fits is false when the box, error
// included, does not fit the screen whole: the plain layout, whose error
// scrolls, takes over.
func (m UnlockModel) card() (card string, fits bool) {
	width, height := viewSize(m.width, m.height)
	inner := min(56, width-12)
	if inner < 30 {
		return "", false
	}
	sizeInput(&m.input, inner)
	label := func(text string) string { return labelStyle.Width(5).Render(text) }
	lines := []string{titleStyle.Render("keetui"), "", label("File") + " " + pathLabel(m.filePath, inner-6)}
	if m.keyFilePath != "" {
		lines = append(lines, label("Key")+" "+pathLabel(m.keyFilePath, inner-6))
	}
	if m.missing != "" {
		lines = append(lines, warnStyle.Padding(0).Render(m.missing))
	}
	lines = append(lines, "", m.input.View())
	border := primaryColor
	switch {
	case m.unlocking:
		lines = append(lines, "", helpStyle.Render("Unlocking…"))
	case m.err != nil:
		border = errorColor
		lines = append(lines, "", errorMsgStyle.Padding(0).Render(wrapContent("Error: "+sanitizeDisplay(m.err.Error()), inner)))
	case m.notice != "":
		lines = append(lines, "", helpStyle.Render(m.notice))
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(1, 3).
		Render(lipgloss.NewStyle().Width(inner).Render(strings.Join(lines, "\n")))
	available := height - 1 // the footer's row
	if lipgloss.Height(box) > available {
		return "", false
	}
	pad := strings.Repeat(" ", (width-lipgloss.Width(box))/2)
	top := strings.Repeat("\n", (available-lipgloss.Height(box))/2)
	return top + pad + strings.ReplaceAll(box, "\n", "\n"+pad), true
}

func (m UnlockModel) View() tea.View {
	width, height := viewSize(m.width, m.height)
	if card, fits := m.card(); fits {
		return tea.NewView(screen(card, m.footer(), width, height, 0))
	}
	sizeInput(&m.input, max(1, width-2))
	header := m.header()
	body := header
	if m.unlocking {
		body += "\n" + helpStyle.Render("  Unlocking…")
	} else if m.err == nil && m.notice != "" {
		body += "\n" + helpStyle.Render("  "+m.notice)
	} else if m.err != nil {
		available := max(1, height-strings.Count(header, "\n")-lipgloss.Height(m.footer()))
		body += contentWindow(m.errorContent(), width, available, m.errorOffset)
	}
	return tea.NewView(screen(body, m.footer(), width, height, 0))
}
