package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tobischo/gokeepasslib/v3"

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
}

func NewHistoryModel(entryTitle string, history []keepass.HistoryEntry) HistoryModel {
	return HistoryModel{
		entryTitle: entryTitle,
		history:    history,
	}
}

func (m HistoryModel) Init() tea.Cmd {
	return nil
}

func (m HistoryModel) visibleHeight() int {
	h := m.height - 8
	if h < 5 {
		h = 5
	}
	return h
}

func (m HistoryModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q":
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

		case "p":
			m.showPw = !m.showPw

		case "1":
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

func (m HistoryModel) View() string {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString(titleStyle.Render(fmt.Sprintf(" History: %s ", m.entryTitle)))
	b.WriteString("\n\n")

	if len(m.history) == 0 {
		b.WriteString(itemStyle.Render(hiddenStyle.Render("(no history)")))
		b.WriteString("\n")
	} else {
		vis := m.visibleHeight()
		end := m.offset + vis
		if end > len(m.history) {
			end = len(m.history)
		}
		for i := m.offset; i < end; i++ {
			h := m.history[i]
			cursor := "  "
			if i == m.cursor {
				cursor = "> "
			}

			dateStr := ""
			if !h.ModificationTime.IsZero() {
				dateStr = h.ModificationTime.Local().Format("2006-01-02 15:04:05")
			}

			pw := strings.Repeat("*", len(h.Password))
			if m.showPw {
				pw = h.Password
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
			b.WriteString(fmt.Sprintf("\n%s",
				helpStyle.Render(fmt.Sprintf("  %d/%d entries", m.cursor+1, len(m.history)))))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  j/k: navigate  p: toggle passwords  1: copy selected password  esc: back  q: quit"))
	b.WriteString("\n")

	return b.String()
}
