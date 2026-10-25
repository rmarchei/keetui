package ui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/rmarchei/keetui/internal/config"
)

type ShowHelpMsg struct{}
type CloseHelpMsg struct{}

// Version is keetui's version, shown in the help header; main sets it.
var Version = "dev"

const (
	helpBrowser = iota
	helpDetail
	helpEditor
	helpSettings
	helpRecycleBin
	helpMessage
)

type helpBinding struct{ key, description string }
type helpSection struct {
	title    string
	bindings []helpBinding
}

// HelpModel shows the current context first, with tabs for other contexts.
type HelpModel struct {
	keys          config.KeyBindings
	viewport      viewport.Model
	width, height int
	ready         bool
	message       string
	section       int
	context       *helpSection
	contextIndex  int
}

func NewHelpModel(keys config.KeyBindings, width, height int) HelpModel {
	m := HelpModel{keys: keys, width: width, height: height, ready: true}
	m.viewport = viewport.New()
	m.viewport.FillHeight = true
	m.viewport.KeyMap = viewport.KeyMap{
		Up:       key.NewBinding(key.WithKeys("up", keys.NavUp)),
		Down:     key.NewBinding(key.WithKeys("down", keys.NavDown)),
		PageUp:   key.NewBinding(key.WithKeys("pgup")),
		PageDown: key.NewBinding(key.WithKeys("pgdown")),
	}
	m.resize()
	return m
}

func (m HelpModel) sections() []helpSection {
	k := m.keys
	result := []helpSection{
		{"Browser", []helpBinding{
			{k.NavDown + "/" + k.NavUp, "Navigate"}, {"enter/" + k.Open, "Open entry/group"},
			{"esc/" + k.Back, "Back"}, {k.Search, "Search (literal)"},
			{"tab (search)", "Toggle literal/fuzzy"},
			{k.NewEntry, "New entry"}, {k.NewGroup, "New group"},
			{k.RenameGroup, "Rename group"}, {k.Delete, "Delete"},
			{k.Duplicate, "Duplicate"}, {k.Favorite, "Favorite"},
			{k.CycleSort, "Sort"}, {k.FilterTag, "Filter by tag"},
			{k.Cut, "Cut"}, {k.Paste, "Paste"},
			{k.Save + "/ctrl+s", "Save / save as"}, {k.ShowStats, "Database info"},
			{k.ShowAudit, "Password audit"}, {k.ShowSettings + "/ctrl+o", "Settings"},
			{k.ChangePassword, "Master password"}, {"ctrl+z", "Undo"},
			{"?/F1", "Help"}, {k.Quit + "/ctrl+c", "Quit"},
		}},
		{"Entry", []helpBinding{
			{k.NavDown + "/" + k.NavUp, "Select field"}, {"enter", "Copy selected field"},
			{k.CopyUsername, "Copy username"}, {k.CopyPassword, "Copy password"},
			{k.CopyURL, "Copy URL"}, {k.CopyTOTP, "Copy TOTP"},
			{k.HIBPCheck, "Check breaches (HIBP)"},
			{k.TogglePassword, "Reveal / hide"}, {k.ShowQR, "Show QR"},
			{k.Edit, "Edit entry"}, {k.OpenURL, "Open URL"},
			{k.Attachments, "Attachments"}, {k.ShowHistory, "Password history"},
			{"esc/" + k.Back, "Back"}, {"?/F1", "Help"}, {k.Quit + "/ctrl+c", "Quit"},
		}},
		{"Editor", []helpBinding{
			{"tab/shift+tab", "Navigate fields"}, {"ctrl+s", "Save entry"},
			{"ctrl+r", "Reveal / hide"}, {"ctrl+g", "Generate password"},
			{"ctrl+a", "Add custom field"}, {"ctrl+t", "Protect custom field"},
			{"ctrl+d", "Delete custom field"}, {"esc", "Cancel"},
			{"F1", "Help (keeps edits)"}, {"ctrl+c", "Quit"},
		}},
		{"Settings", []helpBinding{
			{"tab/shift+tab", "Navigate fields"}, {"space", "Toggle / open picker"},
			{"ctrl+s", "Save settings"}, {"enter", "Next / change / open"},
			{"esc", "Back to browser"}, {"↑/↓", "Navigate fields"},
			{"ctrl+c", "Quit"}, {"F1", "Help / full status"},
		}},
		{"Bin", []helpBinding{
			{k.Delete, "Delete permanently"}, {k.Restore, "Restore to root"},
			{k.EmptyRecycleBin, "Empty bin"}, {"esc/" + k.Back, "Back"}, {k.Save, "Save database"},
		}},
	}
	// Replace the matching overview tab with the current screen's controls.
	// This keeps contextual help visible without adding a long row of tabs.
	if m.context != nil {
		result[m.contextIndex] = *m.context
	}
	if m.message != "" {
		result = append(result, helpSection{title: "Message"})
	}
	return result
}

func (m HelpModel) header() string {
	var tabs []string
	for i, section := range m.sections() {
		label := helpStyle.Render(" " + section.title + " ")
		if i == m.section {
			label = selectedRowStyle.Bold(true).Render(" " + section.title + " ")
		}
		tabs = append(tabs, label)
	}
	width, _ := viewSize(m.width, m.height)
	return header(width, Version, "Keyboard Shortcuts") + "\n\n" + wrapContent(strings.Join(tabs, " "), width) + "\n\n"
}

func (m HelpModel) buildContent() string {
	width, _ := viewSize(m.width, m.height)
	if m.section == helpMessage {
		return wrapContent(m.message, width)
	}
	bindings := m.sections()[m.section].bindings
	column := func(items []helpBinding, width int) string {
		var lines []string
		for _, item := range items {
			keyWidth := min(14, max(8, width/3))
			key := focusedStyle.Width(keyWidth).Render(sanitizeDisplay(item.key))
			desc := wrapContent(item.description, max(1, width-keyWidth-1))
			lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top, key, " ", desc))
		}
		return strings.Join(lines, "\n")
	}
	if width >= 76 && len(bindings) > 10 {
		split := (len(bindings) + 1) / 2
		colWidth := (width - 2) / 2
		left := lipgloss.NewStyle().Width(colWidth).Render(column(bindings[:split], colWidth))
		right := column(bindings[split:], width-colWidth-2)
		return lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)
	}
	return column(bindings, width)
}

func (m HelpModel) Init() tea.Cmd { return nil }

func (m HelpModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "?", "f1":
			return m, func() tea.Msg { return CloseHelpMsg{} }
		case m.keys.Quit, "ctrl+c":
			return m, func() tea.Msg { return RequestQuitMsg{} }
		case "tab", "right", "shift+tab", "left":
			delta := 1
			if msg.String() == "left" || msg.String() == "shift+tab" {
				delta = -1
			}
			m.section = (m.section + delta + len(m.sections())) % len(m.sections())
			m.viewport.GotoTop()
			m.resize()
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m HelpModel) View() tea.View {
	if !m.ready {
		return tea.NewView("")
	}
	body := m.header() + m.viewport.View()
	return tea.NewView(screen(body, m.footer(), m.width, m.height, 0))
}

func (m HelpModel) footer() string {
	return renderHints(m.width, "esc/F1: close", "←/→/tab: section", m.keys.NavDown+"/"+m.keys.NavUp+": scroll", "pgup/pgdown: page", m.keys.Quit+": quit")
}

func (m *HelpModel) resize() {
	width, height := viewSize(m.width, m.height)
	m.section = min(max(0, m.section), len(m.sections())-1)
	m.viewport.SetWidth(width)
	m.viewport.SetHeight(max(1, height-lipgloss.Height(m.footer())-strings.Count(m.header(), "\n")))
	m.viewport.SetContent(m.buildContent())
}
