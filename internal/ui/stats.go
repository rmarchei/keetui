package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/rmarchei/keetui/internal/keepass"
)

// StatsModel shows database statistics.
type StatsModel struct {
	loading bool
	stats   keepass.DBStats
	width   int
	height  int
	offset  int
}

// ShowStatsMsg requests showing database stats.
type ShowStatsMsg struct{}

func NewStatsModel(stats keepass.DBStats) StatsModel {
	return StatsModel{stats: stats}
}

func (m StatsModel) Init() tea.Cmd {
	return nil
}

func (m StatsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.offset = clampOffset(m.body(), bodyHeight(m.height, m.footer()), m.offset)
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "q":
			return m, func() tea.Msg { return RequestQuitMsg{} }
		case "ctrl+c":
			return m, func() tea.Msg { return RequestQuitMsg{} }
		case "esc", "h", "left":
			return m, func() tea.Msg { return BackToBrowserMsg{} }
		case "j", "down", "k", "up", "pgdown", "pgup", "home", "end":
			_, height := viewSize(m.width, m.height)
			visible := max(1, bodyHeight(height, m.footer()))
			limit := scrollLimit(m.body(), visible)
			switch msg.String() {
			case "j", "down":
				m.offset++
			case "k", "up":
				m.offset--
			case "pgdown":
				m.offset += visible
			case "pgup":
				m.offset -= visible
			case "home":
				m.offset = 0
			case "end":
				m.offset = limit
			}
			m.offset = min(max(0, m.offset), limit)
		}
	}

	return m, nil
}

func (m StatsModel) body() string {
	var b strings.Builder
	width, _ := viewSize(m.width, m.height)

	b.WriteString(header(width, "", "Database Info"))
	b.WriteString("\n\n")
	if m.loading {
		b.WriteString(helpStyle.Render("  Analyzing passwords…"))
		return wrapContent(b.String(), width)
	}

	fmt.Fprintf(&b, "  %s %s\n",
		labelStyle.Render("File:"),
		pathLabel(m.stats.FilePath, max(1, width-15)))

	fmt.Fprintf(&b, "  %s %s\n",
		labelStyle.Render("Size:"),
		valueStyle.Render(formatSize(m.stats.FileSize)))

	fmt.Fprintf(&b, "  %s %s\n",
		labelStyle.Render("Format:"),
		valueStyle.Render(m.stats.FormatVersion))

	fmt.Fprintf(&b, "  %s %s\n",
		labelStyle.Render("Groups:"),
		valueStyle.Render(fmt.Sprintf("%d", m.stats.TotalGroups)))

	fmt.Fprintf(&b, "  %s %s\n",
		labelStyle.Render("Entries:"),
		valueStyle.Render(fmt.Sprintf("%d", m.stats.TotalEntries)))

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  Password Health:"))
	b.WriteString("\n")

	if m.stats.WeakPasswords > 0 {
		fmt.Fprintf(&b, "  %s %s\n",
			labelStyle.Render("Weak:"),
			errorMsgStyle.Render(fmt.Sprintf("%d", m.stats.WeakPasswords)))
	} else {
		fmt.Fprintf(&b, "  %s %s\n",
			labelStyle.Render("Weak:"),
			successStyle.Render("0"))
	}

	if m.stats.ReusedPasswords > 0 {
		fmt.Fprintf(&b, "  %s %s\n",
			labelStyle.Render("Reused:"),
			errorMsgStyle.Render(fmt.Sprintf("%d entries share passwords", m.stats.ReusedPasswords)))
	} else {
		fmt.Fprintf(&b, "  %s %s\n",
			labelStyle.Render("Reused:"),
			successStyle.Render("0"))
	}

	if m.stats.EmptyPasswords > 0 {
		fmt.Fprintf(&b, "  %s %s\n",
			labelStyle.Render("Empty:"),
			errorMsgStyle.Render(fmt.Sprintf("%d", m.stats.EmptyPasswords)))
	} else {
		fmt.Fprintf(&b, "  %s %s\n",
			labelStyle.Render("Empty:"),
			successStyle.Render("0"))
	}

	if m.stats.ExpiredEntries > 0 {
		fmt.Fprintf(&b, "  %s %s\n",
			labelStyle.Render("Expired:"),
			errorMsgStyle.Render(fmt.Sprintf("%d", m.stats.ExpiredEntries)))
	} else {
		fmt.Fprintf(&b, "  %s %s\n",
			labelStyle.Render("Expired:"),
			successStyle.Render("0"))
	}

	return wrapContent(b.String(), width)
}

func (m StatsModel) footer() string {
	return renderHints(m.width, "esc: back", "q: quit", "j/k: scroll", "pgup/pgdown: page", "F1: help")
}

func (m StatsModel) View() tea.View {
	return tea.NewView(screen(m.body(), m.footer(), m.width, m.height, m.offset))
}

func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
