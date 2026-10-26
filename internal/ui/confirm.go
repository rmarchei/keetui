package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ConfirmModel shows a yes/no confirmation prompt.
type ConfirmModel struct {
	returnState viewState // owned by this dialog, including when a quit prompt replaces it
	message     string
	onConfirm   tea.Cmd
	selected    bool // false = No, true = Yes
	destructive bool // Yes loses data: drawn in the error color
	width       int
	height      int
}

// Destructive marks a dialog whose Yes deletes or overwrites something.
func (m ConfirmModel) Destructive() ConfirmModel {
	m.destructive = true
	return m
}

// The return target is required: creating a dialog cannot accidentally reuse
// a previous dialog's target or default to the retired unlock screen.
func NewConfirmModel(returnState viewState, message string, onConfirm tea.Cmd) ConfirmModel {
	return ConfirmModel{
		returnState: returnState,
		message:     message,
		onConfirm:   onConfirm,
		selected:    false,
	}
}

func (m ConfirmModel) Init() tea.Cmd {
	return nil
}

func (m ConfirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			d := m.dialog()
			switch {
			case d.no.contains(msg.X, msg.Y):
				m.selected = false
				return m, func() tea.Msg { return CancelEditMsg{} }
			case d.yes.contains(msg.X, msg.Y):
				m.selected = true
				return m, m.onConfirm
			}
		}
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			// Route through RequestQuitMsg so the app can warn about
			// unsaved changes, like every other post-unlock view.
			return m, func() tea.Msg { return RequestQuitMsg{} }
		case "y", "Y":
			return m, m.onConfirm
		case "n", "N", "esc":
			return m, func() tea.Msg { return CancelEditMsg{} }
		case "left", "right", "tab", "shift+tab", "h", "l":
			m.selected = !m.selected
		case "enter":
			if m.selected {
				return m, m.onConfirm
			}
			return m, func() tea.Msg { return CancelEditMsg{} }
		}
	}

	return m, nil
}

func (m ConfirmModel) View() tea.View {
	width, height := viewSize(m.width, m.height)
	d := m.dialog()
	return tea.NewView(overlay(fitFrame("", width, height), d.box, d.x, d.y))
}

// cells is a rectangle of terminal cells.
type cells struct{ x, y, w, h int }

func (c cells) contains(x, y int) bool {
	return x >= c.x && x < c.x+c.w && y >= c.y && y < c.y+c.h
}

// confirmDialog is the rendered dialog box, its top-left corner, and where
// its buttons are, all in the view's cells.
type confirmDialog struct {
	box     string
	x, y    int
	no, yes cells
}

// dialog lays the box out centered in the view: the message, the buttons
// (the focused one filled; Yes in the error color when it destroys data),
// then the shortcuts.
func (m ConfirmModel) dialog() confirmDialog {
	width, height := viewSize(m.width, m.height)
	const padX, padY = 3, 1
	inner := min(max(lipgloss.Width(sanitizeDisplay(m.message)), 36), max(1, width-2-2*padX-2))

	button := func(label string, focused, danger bool) string {
		style := secondaryStyle.Padding(0, 2)
		if focused {
			style = titleStyle.Padding(0, 2).Underline(true)
			if danger {
				style = style.Background(errorColor)
			}
		}
		return style.Render(label)
	}
	no := button("No", !m.selected, false)
	yes := button("Yes", m.selected, m.destructive)
	message := valueStyle.Render(wrapContent(sanitizeDisplay(m.message), inner))
	hints := ansi.Truncate(strings.TrimSpace(renderHints(inner+1, "y: yes", "n/esc: no", "tab: switch")), inner, "…")
	body := message + "\n\n" + no + "  " + yes + "\n\n" + hints

	border := primaryColor
	if m.destructive {
		border = errorColor
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(padY, padX).
		Render(lipgloss.NewStyle().Width(inner).Render(body))

	d := confirmDialog{box: box}
	d.x = max(0, (width-lipgloss.Width(box))/2)
	d.y = max(0, (height-lipgloss.Height(box))/3)
	row := d.y + 1 + padY + lipgloss.Height(message) + 1
	d.no = cells{d.x + 1 + padX, row, lipgloss.Width(no), 1}
	d.yes = cells{d.no.x + d.no.w + 2, row, lipgloss.Width(yes), 1}
	return d
}
