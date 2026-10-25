package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestConfirmYesKey(t *testing.T) {
	confirmed := false
	m := NewConfirmModel(viewBrowser, "Delete?", func() tea.Msg {
		confirmed = true
		return nil
	})
	m.width, m.height = 80, 24

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: string('y')})
	if cmd == nil {
		t.Fatal("y key should produce a command")
	}
	cmd()
	if !confirmed {
		t.Error("onConfirm should have been called")
	}
}

func TestConfirmNoKey(t *testing.T) {
	m := NewConfirmModel(viewBrowser, "Delete?", func() tea.Msg { return nil })
	m.width, m.height = 80, 24

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'n', Text: string('n')})
	if cmd == nil {
		t.Fatal("n key should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(CancelEditMsg); !ok {
		t.Errorf("expected CancelEditMsg, got %T", msg)
	}
}

func TestConfirmToggle(t *testing.T) {
	m := NewConfirmModel(viewBrowser, "Delete?", func() tea.Msg { return nil })
	m.width, m.height = 80, 24

	if m.selected {
		t.Error("should default to No")
	}

	// Toggle
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(ConfirmModel)
	if !m.selected {
		t.Error("should be Yes after tab")
	}

	// Toggle back
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(ConfirmModel)
	if m.selected {
		t.Error("should be No after second tab")
	}
}

func TestConfirmEnter(t *testing.T) {
	// Enter with No selected
	m := NewConfirmModel(viewBrowser, "Delete?", func() tea.Msg { return nil })
	m.width, m.height = 80, 24

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(CancelEditMsg); !ok {
		t.Errorf("expected CancelEditMsg when No selected, got %T", msg)
	}

	// Enter with Yes selected
	m = NewConfirmModel(viewBrowser, "Delete?", func() tea.Msg { return nil })
	m.selected = true
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should produce a command")
	}
}

func TestConfirmEsc(t *testing.T) {
	m := NewConfirmModel(viewBrowser, "Delete?", func() tea.Msg { return nil })
	m.width, m.height = 80, 24

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(CancelEditMsg); !ok {
		t.Errorf("expected CancelEditMsg, got %T", msg)
	}
}

func TestConfirmView(t *testing.T) {
	m := NewConfirmModel(viewBrowser, "Are you sure?", func() tea.Msg { return nil })
	m.width, m.height = 80, 24

	view := m.View().Content
	if view == "" {
		t.Error("View() should return non-empty string")
	}
}

func TestConfirmDialogIsCenteredAndClickable(t *testing.T) {
	confirmed := false
	m := NewConfirmModel(viewBrowser, "Delete entry 'GitHub'?", func() tea.Msg {
		confirmed = true
		return nil
	})
	m.width, m.height = 100, 29
	d := m.dialog()
	if boxW := lipgloss.Width(d.box); d.x <= 0 || d.x+boxW >= m.width || (m.width-boxW)/2 != d.x {
		t.Fatalf("box at x=%d, %d wide, not centered in %d", d.x, boxW, m.width)
	}
	view := ansi.Strip(m.View().Content)
	assertFrame(t, m.View().Content, m.width, m.height)
	rows := strings.Split(view, "\n")
	if got := ansi.Cut(rows[d.no.y], d.no.x, d.no.x+d.no.w); strings.TrimSpace(got) != "No" {
		t.Fatalf("No button area holds %q", got)
	}
	if got := ansi.Cut(rows[d.yes.y], d.yes.x, d.yes.x+d.yes.w); strings.TrimSpace(got) != "Yes" {
		t.Fatalf("Yes button area holds %q", got)
	}

	_, cmd := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: d.no.x + 1, Y: d.no.y})
	if cmd == nil {
		t.Fatal("clicking No did nothing")
	}
	if _, ok := cmd().(CancelEditMsg); !ok {
		t.Fatal("clicking No did not cancel")
	}
	_, cmd = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: d.yes.x + 1, Y: d.yes.y})
	if cmd == nil {
		t.Fatal("clicking Yes did nothing")
	}
	cmd()
	if !confirmed {
		t.Fatal("clicking Yes did not confirm")
	}
	if _, cmd := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: d.x + 1, Y: d.y + 1}); cmd != nil {
		t.Fatal("a click inside the box but off the buttons acted")
	}
}

func TestDestructiveConfirmFillsYesInTheErrorColor(t *testing.T) {
	m := NewConfirmModel(viewBrowser, "Delete?", nil).Destructive()
	m.width, m.height = 80, 24
	m.selected = true
	danger := titleStyle.Padding(0, 2).Underline(true).Background(errorColor).Render("Yes")
	if !strings.Contains(m.dialog().box, danger) {
		t.Error("focused destructive Yes is not filled in the error color")
	}
	m = NewConfirmModel(viewBrowser, "Quit?", nil)
	m.selected = true
	if strings.Contains(m.dialog().box, danger) {
		t.Error("a harmless Yes is drawn as destructive")
	}
}

func TestConfirmShowsTheScreenItReturnsTo(t *testing.T) {
	m := createTestApp(t)
	m.width, m.height = 100, 30
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updated.(AppModel)
	m.confirm = NewConfirmModel(viewBrowser, "Delete entry 'Root Entry'?", nil).Destructive()
	m.confirm.width, m.confirm.height = m.width, m.viewHeight()
	m.state = viewConfirm
	view := m.View().Content
	assertFrame(t, view, m.width, m.height)
	plain := ansi.Strip(view)
	for _, want := range []string{"Email", "Delete entry 'Root Entry'?"} {
		if !strings.Contains(plain, want) {
			t.Errorf("confirm screen lacks %q:\n%s", want, plain)
		}
	}
	// The screen behind is dimmed: drawn in the muted color.
	muted := strings.TrimSuffix(strings.TrimPrefix(ansi.Style{}.ForegroundColor(mutedColor).String(), "\x1b["), "m")
	if row := rowWith(t, view, "Email"); !strings.Contains(row, muted) {
		t.Errorf("background row is not dimmed: %q", row)
	}
}
