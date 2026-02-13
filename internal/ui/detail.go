package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rmarchei/keetui/internal/keepass"
)

// DetailModel shows a read-only view of a single entry.
type DetailModel struct {
	entry        keepass.Entry
	showPassword bool
	width        int
	height       int
	copied       string
}

// BackToBrowserMsg signals returning to the browser.
type BackToBrowserMsg struct{}

// CopyToClipboardMsg requests copying a value to clipboard.
type CopyToClipboardMsg struct {
	Field string
	Value string
}

func NewDetailModel(entry keepass.Entry) DetailModel {
	return DetailModel{
		entry: entry,
	}
}

func (m DetailModel) Init() tea.Cmd {
	return nil
}

func (m DetailModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "esc", "h", "left":
			return m, func() tea.Msg { return BackToBrowserMsg{} }

		case "p":
			m.showPassword = !m.showPassword

		case "e":
			return m, func() tea.Msg {
				return EditEntryMsg{Entry: m.entry}
			}

		case "1":
			return m, func() tea.Msg {
				return CopyToClipboardMsg{Field: "username", Value: m.entry.UserName}
			}

		case "2":
			return m, func() tea.Msg {
				return CopyToClipboardMsg{Field: "password", Value: m.entry.Password}
			}

		case "3":
			return m, func() tea.Msg {
				return CopyToClipboardMsg{Field: "url", Value: m.entry.URL}
			}
		}
	}

	return m, nil
}

func (m DetailModel) View() string {
	var b strings.Builder

	b.WriteString("\n")
	title := m.entry.Title
	if title == "" {
		title = "(untitled)"
	}
	b.WriteString(titleStyle.Render(fmt.Sprintf(" %s ", title)))
	b.WriteString("\n\n")

	b.WriteString(fmt.Sprintf("  %s %s\n",
		labelStyle.Render("Username:"),
		valueStyle.Render(m.entry.UserName)))

	pw := strings.Repeat("*", len(m.entry.Password))
	if m.showPassword {
		pw = m.entry.Password
	}
	b.WriteString(fmt.Sprintf("  %s %s\n",
		labelStyle.Render("Password:"),
		hiddenStyle.Render(pw)))

	b.WriteString(fmt.Sprintf("  %s %s\n",
		labelStyle.Render("URL:"),
		valueStyle.Render(m.entry.URL)))

	if m.entry.Notes != "" {
		b.WriteString(fmt.Sprintf("  %s\n",
			labelStyle.Render("Notes:")))
		for _, line := range strings.Split(m.entry.Notes, "\n") {
			b.WriteString(fmt.Sprintf("  %s%s\n",
				strings.Repeat(" ", 13),
				valueStyle.Render(line)))
		}
	}

	if m.copied != "" {
		b.WriteString("\n")
		b.WriteString(successStyle.Render(fmt.Sprintf("  Copied %s to clipboard!", m.copied)))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  p: toggle password  e: edit  1: copy user  2: copy pass  3: copy url  esc: back  q: quit"))
	b.WriteString("\n")

	return b.String()
}
