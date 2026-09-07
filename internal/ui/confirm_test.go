package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
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
