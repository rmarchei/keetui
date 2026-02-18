package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rmarchei/keetui/internal/keepass"
)

// ShowAuditMsg requests showing the password audit view.
type ShowAuditMsg struct{}

type auditLine struct {
	isHeader bool
	header   string
	entry    keepass.AuditEntry
}

// AuditModel shows a comprehensive password audit.
type AuditModel struct {
	result keepass.AuditResult
	lines  []auditLine
	cursor int
	offset int
	width  int
	height int
}

func NewAuditModel(result keepass.AuditResult) AuditModel {
	m := AuditModel{result: result}
	m.buildLines()
	// Move cursor to first selectable line
	for i, l := range m.lines {
		if !l.isHeader {
			m.cursor = i
			break
		}
	}
	return m
}

func (m *AuditModel) buildLines() {
	m.lines = nil

	sections := []struct {
		header  string
		entries []keepass.AuditEntry
	}{
		{"Weak Passwords", m.result.WeakEntries},
		{"Reused Passwords", m.result.ReusedEntries},
		{"Empty Passwords", m.result.EmptyEntries},
		{"Expired Entries", m.result.ExpiredEntries},
	}

	for _, s := range sections {
		if len(s.entries) == 0 {
			continue
		}
		m.lines = append(m.lines, auditLine{isHeader: true, header: fmt.Sprintf("%s (%d)", s.header, len(s.entries))})
		for _, e := range s.entries {
			m.lines = append(m.lines, auditLine{entry: e})
		}
	}
}

func (m AuditModel) Init() tea.Cmd {
	return nil
}

func (m AuditModel) visibleHeight() int {
	h := max(m.height-8, 5)
	return h
}

func (m AuditModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			return m, func() tea.Msg { return BackToBrowserMsg{} }

		case "j", "down":
			m.moveDown()

		case "k", "up":
			m.moveUp()

		case "enter", "l", "right":
			if len(m.lines) > 0 && !m.lines[m.cursor].isHeader {
				entry := m.lines[m.cursor].entry.Entry
				return m, func() tea.Msg {
					return ViewEntryMsg{Entry: entry}
				}
			}
		}
	}

	return m, nil
}

func (m *AuditModel) moveDown() {
	for i := m.cursor + 1; i < len(m.lines); i++ {
		if !m.lines[i].isHeader {
			m.cursor = i
			vis := m.visibleHeight()
			if m.cursor >= m.offset+vis {
				m.offset = m.cursor - vis + 1
			}
			return
		}
	}
}

func (m *AuditModel) moveUp() {
	for i := m.cursor - 1; i >= 0; i-- {
		if !m.lines[i].isHeader {
			m.cursor = i
			if m.cursor < m.offset {
				m.offset = m.cursor
			}
			return
		}
	}
}

func (m AuditModel) View() string {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString(titleStyle.Render(" Password Audit "))
	b.WriteString("\n\n")

	total := m.result.TotalIssues()
	if total == 0 {
		b.WriteString(successStyle.Render("  No issues found!"))
		b.WriteString("\n")
	} else {
		b.WriteString(fmt.Sprintf("  %s\n\n",
			warnStyle.Render(fmt.Sprintf("%d issues found", total))))

		vis := m.visibleHeight()
		end := min(m.offset+vis, len(m.lines))
		for i := m.offset; i < end; i++ {
			line := m.lines[i]
			if line.isHeader {
				b.WriteString(fmt.Sprintf("\n  %s\n", helpStyle.Render(line.header)))
			} else {
				cursor := "  "
				if i == m.cursor {
					cursor = "> "
				}
				title := line.entry.Entry.Title
				if title == "" {
					title = "(untitled)"
				}
				path := hiddenStyle.Render(fmt.Sprintf("  (%s)", line.entry.GroupPath))
				if i == m.cursor {
					b.WriteString(fmt.Sprintf("  %s%s%s\n", cursor, selectedItemStyle.Render(title), path))
				} else {
					b.WriteString(fmt.Sprintf("  %s%s%s\n", cursor, entryStyle.Render(title), path))
				}
			}
		}

		if len(m.lines) > vis {
			b.WriteString(fmt.Sprintf("\n%s\n",
				helpStyle.Render(fmt.Sprintf("  %d/%d", m.cursor+1, len(m.lines)))))
		}
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  j/k: navigate  enter: view entry  esc: back  q: quit"))
	b.WriteString("\n")

	return b.String()
}
