package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/tobischo/gokeepasslib/v3"

	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/keepass"
)

// ShowHistoryMsg requests showing password history for an entry.
type ShowHistoryMsg struct {
	EntryTitle string
	EntryUUID  gokeepasslib.UUID
}

// RestoreHistoryMsg asks to make an earlier version of an entry its
// current one; Index counts as the history lists them, most recent first.
type RestoreHistoryMsg struct {
	EntryUUID gokeepasslib.UUID
	Index     int
	When      time.Time // the version's modification time, for the question
}

// HistoryModel shows the earlier versions of an entry: a list, and one
// version at a time with what differs from the current entry.
type HistoryModel struct {
	entryTitle string
	history    []keepass.HistoryEntry
	cursor     int
	offset     int
	showPw     bool
	width      int
	height     int
	keys       config.KeyBindings
	readOnly   bool

	entryUUID          gokeepasslib.UUID
	current            keepass.Entry // what a version is compared with
	currentAttachments []string
	previewing         bool
	previewOffset      int
}

func NewHistoryModel(entryTitle string, history []keepass.HistoryEntry, keys ...config.KeyBindings) HistoryModel {
	kb := config.DefaultKeyBindings()
	if len(keys) > 0 {
		kb = keys[0]
	}
	return HistoryModel{
		entryTitle: entryTitle,
		history:    history,
		keys:       kb,
	}
}

func (m HistoryModel) Init() tea.Cmd {
	return nil
}

func (m HistoryModel) visibleHeight() int {
	_, height := viewSize(m.width, m.height)
	return max(1, height-5-lipgloss.Height(m.footer()))
}

func (m HistoryModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := m.update(msg)
	next := updated.(HistoryModel)
	next.cursor, next.offset = followList(next.cursor, next.offset, len(next.history), next.visibleHeight())
	return next, cmd
}

func (m HistoryModel) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyPressMsg:
		if m.previewing {
			return m.updatePreview(msg)
		}
		switch msg.String() {
		case "enter", "right", m.keys.Open:
			if len(m.history) > 0 {
				m.previewing, m.previewOffset = true, 0
			}
		case m.keys.Restore:
			return m, m.restore()
		case m.keys.Quit:
			return m, func() tea.Msg { return RequestQuitMsg{} }
		case "ctrl+c":
			return m, func() tea.Msg { return RequestQuitMsg{} }
		case "esc", m.keys.Back, "left":
			return m, func() tea.Msg { return BackToDetailMsg{} }

		case m.keys.NavDown, "down":
			if m.cursor < len(m.history)-1 {
				m.cursor++
			}

		case m.keys.NavUp, "up":
			if m.cursor > 0 {
				m.cursor--
			}

		case m.keys.TogglePassword:
			m.showPw = !m.showPw

		case m.keys.CopyPassword:
			if len(m.history) > 0 {
				pw := m.history[m.cursor].Password
				return m, func() tea.Msg {
					return CopyToClipboardMsg{Field: "history password", Value: pw}
				}
			}
		}
	}

	return m, nil
}

func (m HistoryModel) updatePreview(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case m.keys.Quit, "ctrl+c":
		return m, func() tea.Msg { return RequestQuitMsg{} }
	case "esc", m.keys.Back, "left":
		m.previewing = false
	case m.keys.NavDown, "down":
		m.previewOffset = min(m.previewOffset+1, scrollLimit(m.previewBody(), m.previewHeight()))
	case m.keys.NavUp, "up":
		m.previewOffset = max(m.previewOffset-1, 0)
	case m.keys.TogglePassword:
		m.showPw = !m.showPw
	case m.keys.CopyPassword:
		pw := m.history[m.cursor].Password
		return m, func() tea.Msg { return CopyToClipboardMsg{Field: "history password", Value: pw} }
	case m.keys.Restore:
		return m, m.restore()
	}
	return m, nil
}

// restore asks the app to restore the selected version, which confirms.
func (m HistoryModel) restore() tea.Cmd {
	if len(m.history) == 0 {
		return nil
	}
	msg := RestoreHistoryMsg{EntryUUID: m.entryUUID, Index: m.cursor, When: m.history[m.cursor].ModificationTime}
	return func() tea.Msg { return msg }
}

// versionLabel names a version by when it was saved.
func versionLabel(when time.Time) string {
	if when.IsZero() {
		return "an undated version"
	}
	return when.Local().Format("2006-01-02 15:04:05")
}

func (m HistoryModel) previewHeight() int {
	_, height := viewSize(m.width, m.height)
	return max(1, height-2-lipgloss.Height(m.footer()))
}

// previewBody lays the selected version out field by field, marking what
// differs from the current entry. Protected values stay masked until shown.
func (m HistoryModel) previewBody() string {
	h := m.history[m.cursor]
	v, cur := h.Entry, m.current
	var b strings.Builder
	changed := focusedStyle.Render("  changed")
	row := func(label, value string, protected, differs bool) {
		if protected && value != "" && !m.showPw {
			value = maskedValue
		}
		line := "  " + labelStyle.Render(padCell(label, 12)) + " " + valueStyle.Render(sanitizeDisplay(value))
		if differs {
			line += changed
		}
		b.WriteString(line + "\n")
	}
	row("Title", v.Title, false, v.Title != cur.Title)
	row("Username", v.UserName, false, v.UserName != cur.UserName)
	row("Password", v.Password, true, v.Password != cur.Password)
	row("URL", v.URL, false, v.URL != cur.URL)
	if v.TOTP != "" || cur.TOTP != "" {
		row("TOTP", v.TOTP, true, v.TOTP != cur.TOTP)
	}
	if v.Tags != "" || cur.Tags != "" {
		row("Tags", v.Tags, false, v.Tags != cur.Tags)
	}
	for _, cf := range v.CustomFields {
		i := slices.IndexFunc(cur.CustomFields, func(c keepass.CustomField) bool { return c.Key == cf.Key })
		row(cf.Key, cf.Value, cf.Protected, i < 0 || cur.CustomFields[i] != cf)
	}
	for _, cf := range cur.CustomFields {
		if !slices.ContainsFunc(v.CustomFields, func(c keepass.CustomField) bool { return c.Key == cf.Key }) {
			row(cf.Key, "(not in this version)", false, true)
		}
	}
	if len(h.Attachments) > 0 || len(m.currentAttachments) > 0 {
		row("Attachments", strings.Join(h.Attachments, ", "), false, !slices.Equal(h.Attachments, m.currentAttachments))
	}
	if v.Notes != "" || cur.Notes != "" {
		b.WriteString("  " + labelStyle.Render("Notes"))
		if v.Notes != cur.Notes {
			b.WriteString(changed)
		}
		b.WriteString("\n")
		for _, line := range sanitizeNoteLines(v.Notes) {
			b.WriteString("    " + valueStyle.Render(line) + "\n")
		}
	}
	return b.String()
}

func (m HistoryModel) viewPreview() tea.View {
	width, height := viewSize(m.width, m.height)
	when := versionLabel(m.history[m.cursor].ModificationTime)
	top := header(width, "", m.entryTitle, "History", when) + "\n\n"
	body := contentWindow(m.previewBody(), width, m.previewHeight(), m.previewOffset)
	return tea.NewView(screen(top+body, m.footer(), width, height, 0))
}

func (m HistoryModel) View() tea.View {
	if m.previewing && len(m.history) > 0 {
		return m.viewPreview()
	}
	var b strings.Builder

	b.WriteString(header(m.width, "", m.entryTitle, "History"))
	b.WriteString("\n\n")

	if len(m.history) == 0 {
		b.WriteString(itemStyle.Render(hiddenStyle.Render("(no history)")))
		b.WriteString("\n")
	} else {
		vis := m.visibleHeight()
		end := min(m.offset+vis, len(m.history))
		for i := m.offset; i < end; i++ {
			h := m.history[i]
			cursor := "  "
			if i == m.cursor {
				cursor = selectionCursor
			}

			dateStr := ""
			if !h.ModificationTime.IsZero() {
				dateStr = h.ModificationTime.Local().Format("2006-01-02 15:04:05")
			}

			pw := maskedValue
			if m.showPw {
				pw = sanitizeDisplay(h.Password)
			}

			style := entryStyle
			if i == m.cursor {
				style = selectedItemStyle
			}

			line := fmt.Sprintf("%s%s  %s",
				cursor,
				style.Render(dateStr),
				hiddenStyle.Render(pw))
			if i == m.cursor {
				line = selectedRow(line, m.width)
			}
			b.WriteString(line)
			b.WriteString("\n")
		}

		if len(m.history) > vis {
			fmt.Fprintf(&b, "\n%s",
				helpStyle.Render(fmt.Sprintf("  %d/%d entries", m.cursor+1, len(m.history))))
			b.WriteString("\n")
		}
	}

	return tea.NewView(screen(b.String(), m.footer(), m.width, m.height, 0))
}

func (m HistoryModel) footer() string {
	var restore []string
	if !m.readOnly {
		restore = []string{m.keys.Restore + ": restore"}
	}
	if m.previewing {
		hints := append([]string{"esc: back", "F1: help"}, restore...)
		return renderHints(m.width, append(hints, m.keys.TogglePassword+": show/hide",
			m.keys.CopyPassword+": copy password", m.keys.NavDown+"/"+m.keys.NavUp+": scroll", m.keys.Quit+": quit")...)
	}
	hints := append([]string{"esc: back", "enter: view", "F1: help"}, restore...)
	return renderHints(m.width, append(hints, m.keys.NavDown+"/"+m.keys.NavUp+": navigate",
		m.keys.TogglePassword+": toggle passwords", m.keys.CopyPassword+": copy password", m.keys.Quit+": quit")...)
}
