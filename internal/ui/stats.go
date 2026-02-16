package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rmarchei/keetui/internal/keepass"
)

// StatsModel shows database statistics.
type StatsModel struct {
	stats  keepass.DBStats
	width  int
	height int
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
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q":
			return m, func() tea.Msg { return RequestQuitMsg{} }
		case "ctrl+c":
			return m, func() tea.Msg { return RequestQuitMsg{} }
		case "esc", "h", "left":
			return m, func() tea.Msg { return BackToBrowserMsg{} }
		}
	}

	return m, nil
}

func (m StatsModel) View() string {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString(titleStyle.Render(" Database Info "))
	b.WriteString("\n\n")

	b.WriteString(fmt.Sprintf("  %s %s\n",
		labelStyle.Render("File:"),
		valueStyle.Render(m.stats.FilePath)))

	b.WriteString(fmt.Sprintf("  %s %s\n",
		labelStyle.Render("Size:"),
		valueStyle.Render(formatSize(m.stats.FileSize))))

	b.WriteString(fmt.Sprintf("  %s %s\n",
		labelStyle.Render("Format:"),
		valueStyle.Render(m.stats.FormatVersion)))

	b.WriteString(fmt.Sprintf("  %s %s\n",
		labelStyle.Render("Groups:"),
		valueStyle.Render(fmt.Sprintf("%d", m.stats.TotalGroups))))

	b.WriteString(fmt.Sprintf("  %s %s\n",
		labelStyle.Render("Entries:"),
		valueStyle.Render(fmt.Sprintf("%d", m.stats.TotalEntries))))

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  Password Health:"))
	b.WriteString("\n")

	if m.stats.WeakPasswords > 0 {
		b.WriteString(fmt.Sprintf("  %s %s\n",
			labelStyle.Render("Weak:"),
			errorMsgStyle.Render(fmt.Sprintf("%d", m.stats.WeakPasswords))))
	} else {
		b.WriteString(fmt.Sprintf("  %s %s\n",
			labelStyle.Render("Weak:"),
			successStyle.Render("0")))
	}

	if m.stats.ReusedPasswords > 0 {
		b.WriteString(fmt.Sprintf("  %s %s\n",
			labelStyle.Render("Reused:"),
			errorMsgStyle.Render(fmt.Sprintf("%d entries share passwords", m.stats.ReusedPasswords))))
	} else {
		b.WriteString(fmt.Sprintf("  %s %s\n",
			labelStyle.Render("Reused:"),
			successStyle.Render("0")))
	}

	if m.stats.EmptyPasswords > 0 {
		b.WriteString(fmt.Sprintf("  %s %s\n",
			labelStyle.Render("Empty:"),
			errorMsgStyle.Render(fmt.Sprintf("%d", m.stats.EmptyPasswords))))
	} else {
		b.WriteString(fmt.Sprintf("  %s %s\n",
			labelStyle.Render("Empty:"),
			successStyle.Render("0")))
	}

	if m.stats.ExpiredEntries > 0 {
		b.WriteString(fmt.Sprintf("  %s %s\n",
			labelStyle.Render("Expired:"),
			errorMsgStyle.Render(fmt.Sprintf("%d", m.stats.ExpiredEntries))))
	} else {
		b.WriteString(fmt.Sprintf("  %s %s\n",
			labelStyle.Render("Expired:"),
			successStyle.Render("0")))
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  esc: back  q: quit"))
	b.WriteString("\n")

	return b.String()
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
