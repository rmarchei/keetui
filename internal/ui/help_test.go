package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rmarchei/keetui/internal/config"
)

func TestHelpClose(t *testing.T) {
	m := NewHelpModel(config.DefaultKeyBindings(), 80, 24)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(BackToBrowserMsg); !ok {
		t.Errorf("expected BackToBrowserMsg, got %T", msg)
	}
}

func TestHelpCloseQuestion(t *testing.T) {
	m := NewHelpModel(config.DefaultKeyBindings(), 80, 24)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if cmd == nil {
		t.Fatal("? should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(BackToBrowserMsg); !ok {
		t.Errorf("expected BackToBrowserMsg, got %T", msg)
	}
}

func TestHelpQuit(t *testing.T) {
	m := NewHelpModel(config.DefaultKeyBindings(), 80, 24)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("q should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(RequestQuitMsg); !ok {
		t.Errorf("expected RequestQuitMsg, got %T", msg)
	}
}

func TestHelpView(t *testing.T) {
	m := NewHelpModel(config.DefaultKeyBindings(), 80, 24)

	view := m.View()
	if view == "" {
		t.Error("View() should return non-empty string")
	}
	if !strings.Contains(view, "Keyboard Shortcuts") {
		t.Error("View() should contain 'Keyboard Shortcuts'")
	}
}
