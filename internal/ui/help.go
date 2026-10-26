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
	readOnly      bool
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
	writable := func(bindings []helpBinding, actions ...helpBinding) []helpBinding {
		if m.readOnly {
			return bindings
		}
		return append(bindings, actions...)
	}
	browser := []helpBinding{
		{k.NavDown + "/" + k.NavUp, "Navigate"}, {"enter/" + k.Open, "Open entry/group"},
		{"esc/" + k.Back, "Back"}, {k.Search, "Search (literal)"},
		{"tab (search)", "Toggle literal/fuzzy"},
	}
	browser = writable(browser, helpBinding{k.NewEntry, "New entry"}, helpBinding{k.NewGroup, "New group"},
		helpBinding{k.RenameGroup, "Rename group"}, helpBinding{k.Delete, "Delete"},
		helpBinding{k.Duplicate, "Duplicate"}, helpBinding{k.Favorite, "Favorite"})
	browser = append(browser, helpBinding{k.CycleSort, "Sort"}, helpBinding{k.FilterTag, "Filter by tag"})
	browser = writable(browser, helpBinding{k.Cut, "Move: cut entry/group"}, helpBinding{k.Paste, "Move: paste into selected group"},
		helpBinding{k.PasteHere, "Move: paste here"}, helpBinding{k.Save + "/ctrl+s", "Save / save as"})
	browser = append(browser, helpBinding{k.ShowStats, "Database info"}, helpBinding{k.ShowAudit, "Password audit"},
		helpBinding{k.ShowSettings + "/ctrl+o", "Settings"})
	browser = writable(browser, helpBinding{k.ChangePassword, "Master password"}, helpBinding{"ctrl+z", "Undo"})
	browser = append(browser, helpBinding{k.Lock, "Lock (also in entries, stats, audit)"},
		helpBinding{"?/F1", "Help"}, helpBinding{quitHelpKeys(k.Quit), "Quit"})

	entry := []helpBinding{
		{k.NavDown + "/" + k.NavUp, "Select field"}, {"enter", "Copy selected field"},
		{"pgup/pgdown", "Scroll a page"},
		{k.CopyUsername, "Copy username"}, {k.CopyPassword, "Copy password"},
		{k.CopyURL, "Copy URL"}, {k.CopyTOTP, "Copy TOTP"},
		{k.HIBPCheck, "Check breaches (HIBP)"}, {k.TogglePassword, "Reveal / hide"}, {k.ShowQR, "Show QR"},
	}
	entry = writable(entry, helpBinding{k.Edit, "Edit entry"})
	entry = append(entry, helpBinding{k.OpenURL, "Open URL"})
	entry = writable(entry, helpBinding{k.Attachments, "Attachments"})
	entry = append(entry, helpBinding{k.ShowHistory, "Password history"}, helpBinding{k.Lock, "Lock"},
		helpBinding{"esc/" + k.Back, "Back"}, helpBinding{"?/F1", "Help"}, helpBinding{quitHelpKeys(k.Quit), "Quit"})

	bin := writable(nil, helpBinding{k.Delete, "Delete permanently"}, helpBinding{k.Restore, "Restore to root"},
		helpBinding{k.EmptyRecycleBin, "Empty bin"}, helpBinding{k.Save, "Save database"})
	bin = append(bin, helpBinding{k.NavDown + "/" + k.NavUp, "Navigate"}, helpBinding{"enter/" + k.Open, "Open entry/group"},
		helpBinding{"esc/" + k.Back, "Back"}, helpBinding{k.Lock, "Lock"}, helpBinding{quitHelpKeys(k.Quit), "Quit"})
	result := []helpSection{
		{"Browser", browser}, {"Entry", entry},
		{"Editor", []helpBinding{
			{"tab/shift+tab", "Navigate fields"}, {"ctrl+s", "Save entry"},
			{"ctrl+r", "Reveal / hide"}, {"ctrl+g", "Generate password"},
			{"ctrl+a", "Add custom field"}, {"ctrl+t", "Protect custom field"},
			{"ctrl+d", "Delete custom field"}, {"esc", "Cancel"},
			{"F1", "Help (keeps edits)"}, {"ctrl+c", "Quit"},
		}},
		settingsHelp("Back to browser"),
		{"Bin", bin},
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

func settingsHelp(back string) helpSection {
	return helpSection{"Settings", []helpBinding{
		{"tab/shift+tab", "Navigate fields"}, {"space", "Toggle / open picker"},
		{"ctrl+s", "Save settings"}, {"enter", "Next / change / open"},
		{"esc", back}, {"↑/↓", "Navigate fields"}, {"ctrl+c", "Quit"}, {"F1", "Help / full status"},
	}}
}

func quitHelpKeys(configured string) string {
	if configured == "ctrl+c" {
		return configured
	}
	return configured + "/ctrl+c"
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
