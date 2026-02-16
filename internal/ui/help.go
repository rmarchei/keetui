package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/rmarchei/keetui/internal/config"
)

// ShowHelpMsg requests showing the help overlay.
type ShowHelpMsg struct{}

// HelpModel displays a scrollable keybinding cheat sheet.
type HelpModel struct {
	keys     config.KeyBindings
	viewport viewport.Model
	width    int
	height   int
	ready    bool
}

// NewHelpModel creates a help model with the given keybindings and dimensions.
func NewHelpModel(keys config.KeyBindings, width, height int) HelpModel {
	m := HelpModel{
		keys:   keys,
		width:  width,
		height: height,
	}
	footerHeight := 1
	vpHeight := height - footerHeight
	if vpHeight < 1 {
		vpHeight = 1
	}
	m.viewport = viewport.New(width, vpHeight)
	m.viewport.SetContent(m.buildContent())
	m.ready = true
	return m
}

func (m HelpModel) buildContent() string {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString(titleStyle.Render(" Keyboard Shortcuts "))
	b.WriteString("\n\n")

	section := func(title string, bindings []struct{ key, desc string }) {
		b.WriteString(fmt.Sprintf("  %s\n", labelStyle.Render(title)))
		for _, bind := range bindings {
			b.WriteString(fmt.Sprintf("    %-16s %s\n",
				selectedItemStyle.Render(bind.key),
				helpStyle.Render(bind.desc)))
		}
		b.WriteString("\n")
	}

	k := m.keys

	section("Browser", []struct{ key, desc string }{
		{k.NavDown + "/" + k.NavUp, "Navigate down/up"},
		{"enter/" + k.Open, "Open entry or group"},
		{"esc/" + k.Back, "Go back"},
		{k.Search, "Search all entries"},
		{k.NewEntry, "New entry"},
		{k.NewGroup, "New group"},
		{k.RenameGroup, "Rename group"},
		{k.Delete, "Delete entry/group"},
		{k.Duplicate, "Duplicate entry"},
		{k.Favorite, "Toggle favorite"},
		{k.CycleSort, "Cycle sort (name/modified/created)"},
		{k.FilterTag, "Filter by tag"},
		{k.Cut, "Cut entry/group"},
		{k.Paste, "Paste entry/group"},
		{k.Save + "/ctrl+s", "Save database (edit path to save as)"},
		{k.ShowStats, "Database info"},
		{k.ShowAudit, "Security audit"},
		{k.ShowSettings, "Settings"},
		{k.ChangePassword, "Change master password"},
		{"ctrl+z", "Undo last action"},
		{"?", "This help screen"},
		{k.Quit, "Quit"},
	})

	section("Entry Detail", []struct{ key, desc string }{
		{"j/k", "Select field"},
		{"enter", "Copy selected field"},
		{k.TogglePassword, "Toggle password visibility"},
		{k.ShowQR, "Show QR code"},
		{k.Edit, "Edit entry"},
		{k.OpenURL, "Open URL in browser"},
		{k.Attachments, "Manage attachments"},
		{k.CopyUsername, "Copy username"},
		{k.CopyPassword, "Copy password"},
		{k.CopyURL, "Copy URL"},
		{k.CopyTOTP, "Copy TOTP code"},
		{k.HIBPCheck, "Check password breaches"},
		{k.ShowHistory, "View entry history"},
		{"?", "This help screen"},
		{"esc/" + k.Back, "Back to browser"},
	})

	section("Editor", []struct{ key, desc string }{
		{"tab/shift+tab", "Navigate fields"},
		{"ctrl+s", "Save entry"},
		{"ctrl+g", "Generate password"},
		{"ctrl+p", "Generate passphrase"},
		{"ctrl+a", "Add custom field"},
		{"ctrl+d", "Delete custom field"},
		{"esc", "Cancel"},
	})

	section("Recycle Bin", []struct{ key, desc string }{
		{k.Delete, "Permanently delete"},
		{k.Restore, "Restore to root group"},
		{k.EmptyRecycleBin, "Empty recycle bin"},
	})

	return b.String()
}

func (m HelpModel) Init() tea.Cmd {
	return nil
}

func (m HelpModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		footerHeight := 1
		vpHeight := m.height - footerHeight
		if vpHeight < 1 {
			vpHeight = 1
		}
		m.viewport.Width = m.width
		m.viewport.Height = vpHeight
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "?":
			return m, func() tea.Msg { return BackToBrowserMsg{} }
		case "q":
			return m, func() tea.Msg { return RequestQuitMsg{} }
		case "ctrl+c":
			return m, func() tea.Msg { return RequestQuitMsg{} }
		}
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m HelpModel) View() string {
	if !m.ready {
		return ""
	}
	footer := helpStyle.Render("  esc/?: close  j/k/up/down: scroll  q: quit")
	return fmt.Sprintf("%s\n%s", m.viewport.View(), footer)
}
