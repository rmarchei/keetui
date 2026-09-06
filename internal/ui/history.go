package ui

import (
	"fmt"
	"strings"

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

// HistoryModel shows the password history for an entry.
type HistoryModel struct {
	entryTitle string
	history    []keepass.HistoryEntry
	cursor     int
	offset     int
	showPw     bool
	width      int
	height     int
	keys       config.KeyBindings
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
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.offset = followField(m.offset, fieldRows{m.cursor, m.cursor}, m.visibleHeight())
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case m.keys.Quit:
			return m, func() tea.Msg { return RequestQuitMsg{} }
		case "ctrl+c":
			return m, func() tea.Msg { return RequestQuitMsg{} }
		case "esc", "h", "left":
			return m, func() tea.Msg { return BackToDetailMsg{} }

		case "j", "down":
			if m.cursor < len(m.history)-1 {
				m.cursor++
				vis := m.visibleHeight()
				if m.cursor >= m.offset+vis {
					m.offset = m.cursor - vis + 1
				}
			}

		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < m.offset {
					m.offset = m.cursor
				}
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

func (m HistoryModel) View() tea.View {
	var b strings.Builder
	m.offset = followField(m.offset, fieldRows{m.cursor, m.cursor}, m.visibleHeight())

	b.WriteString("\n")
	b.WriteString(titleStyle.Render(fmt.Sprintf(" History: %s ", sanitizeDisplay(m.entryTitle))))
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
	return renderHints(m.width, "esc: back", "F1: help", "j/k: navigate", m.keys.TogglePassword+": toggle passwords", m.keys.CopyPassword+": copy password", m.keys.Quit+": quit")
}
