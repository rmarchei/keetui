package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/keepass"
)

// ShowAuditMsg requests showing the password audit view.
type ShowAuditMsg struct{}

// auditLine is one physical row of the scrollable audit list. Keeping a
// strict one-line-per-element invariant (blank separators included) is
// what makes the offset/window math correct.
type auditLine struct {
	isHeader bool
	blank    bool // spacer row before a section header
	header   string
	entry    keepass.AuditEntry
}

// AuditModel shows a comprehensive password audit.
type AuditModel struct {
	keys    config.KeyBindings
	loading bool
	result  keepass.AuditResult
	lines   []auditLine
	cursor  int
	offset  int
	width   int
	height  int
}

func NewAuditModel(result keepass.AuditResult, keys ...config.KeyBindings) AuditModel {
	kb := config.DefaultKeyBindings()
	if len(keys) > 0 {
		kb = keys[0]
	}
	m := AuditModel{result: result, keys: kb}
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
		if len(m.lines) > 0 {
			m.lines = append(m.lines, auditLine{isHeader: true, blank: true})
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
	// Fixed chrome: 5 rows above the list (blank, title, blank, issue
	// count, blank) and 4 below (blank, position counter, blank, help).
	_, height := viewSize(m.width, m.height)
	return max(1, height-7-lipgloss.Height(m.footer()))
}

func (m AuditModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := m.update(msg)
	next := updated.(AuditModel)
	next.cursor, next.offset = followList(next.cursor, next.offset, len(next.lines), next.visibleHeight())
	return next, cmd
}

func (m AuditModel) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case m.keys.Quit:
			return m, func() tea.Msg { return RequestQuitMsg{} }
		case "ctrl+c":
			return m, func() tea.Msg { return RequestQuitMsg{} }
		case "esc", m.keys.Back, "left":
			return m, func() tea.Msg { return BackToBrowserMsg{} }

		case m.keys.NavDown, "down":
			m.moveDown()

		case m.keys.NavUp, "up":
			m.moveUp()

		case "enter", m.keys.Open, "right":
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
			return
		}
	}
}

func (m *AuditModel) moveUp() {
	for i := m.cursor - 1; i >= 0; i-- {
		if !m.lines[i].isHeader {
			m.cursor = i
			return
		}
	}
}

func (m AuditModel) View() tea.View {
	if m.loading {
		body := header(m.width, "", "Password Audit") + "\n\n" + helpStyle.Render("  Analyzing passwords…")
		return tea.NewView(screen(wrapContent(body, m.width), m.footer(), m.width, m.height, 0))
	}
	var b strings.Builder

	b.WriteString(header(m.width, "", "Password Audit"))
	b.WriteString("\n\n")

	total := m.result.TotalIssues()
	if total == 0 {
		b.WriteString(successStyle.Render("  No issues found!"))
		b.WriteString("\n")
	} else {
		fmt.Fprintf(&b, "  %s\n\n",
			warnStyle.Render(fmt.Sprintf("%d issues found", total)))

		vis := m.visibleHeight()
		end := min(m.offset+vis, len(m.lines))
		titleWidth := 0
		for _, line := range m.lines {
			if !line.blank && !line.isHeader {
				titleWidth = max(titleWidth, lipgloss.Width(auditTitle(line)))
			}
		}
		titleWidth = min(titleWidth, max(12, m.width*2/5))
		for i := m.offset; i < end; i++ {
			line := m.lines[i]
			if line.blank {
				b.WriteString("\n")
			} else if line.isHeader {
				fmt.Fprintf(&b, "  %s\n", helpStyle.Render(line.header))
			} else {
				cursor, style := "  ", entryStyle
				if i == m.cursor {
					cursor, style = selectionCursor, selectedItemStyle
				}
				row := "  " + cursor + style.Render(padCell(auditTitle(line), titleWidth)) +
					"  " + hiddenStyle.Render(sanitizeDisplay(line.entry.GroupPath))
				if i == m.cursor {
					row = selectedRow(row, m.width)
				}
				b.WriteString(row + "\n")
			}
		}

		if len(m.lines) > vis {
			fmt.Fprintf(&b, "\n%s\n",
				helpStyle.Render(fmt.Sprintf("  %d/%d", m.cursor+1, len(m.lines))))
		}
	}

	return tea.NewView(screen(b.String(), m.footer(), m.width, m.height, 0))
}

func (m AuditModel) footer() string {
	return renderHints(m.width, "esc: back", "F1: help", m.keys.NavDown+"/"+m.keys.NavUp+": navigate", "enter: view entry", m.keys.Quit+": quit")
}

func auditTitle(line auditLine) string {
	if title := sanitizeDisplay(line.entry.Entry.Title); title != "" {
		return title
	}
	return "(untitled)"
}
