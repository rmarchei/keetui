package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/rmarchei/keetui/internal/keepass"
)

func TestHistoryNavigation(t *testing.T) {
	history := []keepass.HistoryEntry{
		{Password: "pass1", ModificationTime: time.Now()},
		{Password: "pass2", ModificationTime: time.Now()},
		{Password: "pass3", ModificationTime: time.Now()},
	}
	m := NewHistoryModel("Test", history)
	m.width, m.height = 80, 24

	if m.cursor != 0 {
		t.Errorf("initial cursor = %d, want 0", m.cursor)
	}

	// Move down
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j', Text: string('j')})
	m = updated.(HistoryModel)
	if m.cursor != 1 {
		t.Errorf("cursor after j = %d, want 1", m.cursor)
	}

	// Move up
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'k', Text: string('k')})
	m = updated.(HistoryModel)
	if m.cursor != 0 {
		t.Errorf("cursor after k = %d, want 0", m.cursor)
	}
}

func TestHistoryTogglePassword(t *testing.T) {
	history := []keepass.HistoryEntry{
		{Password: "secret", ModificationTime: time.Now()},
	}
	m := NewHistoryModel("Test", history)
	m.width, m.height = 80, 24

	if m.showPw {
		t.Error("showPw should be false initially")
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'p', Text: string('p')})
	m = updated.(HistoryModel)
	if !m.showPw {
		t.Error("showPw should be true after p")
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'p', Text: string('p')})
	m = updated.(HistoryModel)
	if m.showPw {
		t.Error("showPw should be false after second p")
	}
}

func TestHistoryBack(t *testing.T) {
	m := NewHistoryModel("Test", nil)
	m.width, m.height = 80, 24

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(BackToDetailMsg); !ok {
		t.Errorf("expected BackToDetailMsg, got %T", msg)
	}
}

func TestHistoryEmpty(t *testing.T) {
	m := NewHistoryModel("Test", nil)
	m.width, m.height = 80, 24

	view := m.View().Content
	if !strings.Contains(view, "(no history)") {
		t.Error("View() should contain '(no history)' for empty history")
	}
}

func TestHistoryView(t *testing.T) {
	history := []keepass.HistoryEntry{
		{Password: "oldpass", ModificationTime: time.Now()},
	}
	m := NewHistoryModel("Test Entry", history)
	m.width, m.height = 80, 24

	view := m.View().Content
	if view == "" {
		t.Error("View() should return non-empty string")
	}
	if !strings.Contains(view, "Test Entry") {
		t.Error("View() should contain entry title")
	}
}
