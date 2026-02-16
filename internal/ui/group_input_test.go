package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tobischo/gokeepasslib/v3"
)

func TestGroupInputNew(t *testing.T) {
	uuid := gokeepasslib.UUID{1, 2, 3}
	m := NewGroupInputModel(uuid, "")
	m.width, m.height = 80, 24

	m.input.SetValue("My Group")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter with name should produce a command")
	}
	msg := cmd()
	saveMsg, ok := msg.(SaveGroupMsg)
	if !ok {
		t.Fatalf("expected SaveGroupMsg, got %T", msg)
	}
	if saveMsg.Name != "My Group" {
		t.Errorf("Name = %q, want %q", saveMsg.Name, "My Group")
	}
	if saveMsg.ParentUUID != uuid {
		t.Error("ParentUUID mismatch")
	}
	if saveMsg.Rename {
		t.Error("Rename should be false for new group")
	}
}

func TestGroupInputRename(t *testing.T) {
	uuid := gokeepasslib.UUID{1, 2, 3}
	m := NewGroupInputModel(uuid, "Old Name")
	m.renaming = true
	m.width, m.height = 80, 24

	m.input.SetValue("New Name")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter with name should produce a command")
	}
	msg := cmd()
	saveMsg, ok := msg.(SaveGroupMsg)
	if !ok {
		t.Fatalf("expected SaveGroupMsg, got %T", msg)
	}
	if saveMsg.Name != "New Name" {
		t.Errorf("Name = %q, want %q", saveMsg.Name, "New Name")
	}
	if !saveMsg.Rename {
		t.Error("Rename should be true")
	}
}

func TestGroupInputRejectsEmpty(t *testing.T) {
	m := NewGroupInputModel(gokeepasslib.UUID{}, "")
	m.width, m.height = 80, 24

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Error("enter with empty name should not produce a command")
	}
}

func TestGroupInputCancel(t *testing.T) {
	m := NewGroupInputModel(gokeepasslib.UUID{}, "")
	m.width, m.height = 80, 24

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(CancelEditMsg); !ok {
		t.Errorf("expected CancelEditMsg, got %T", msg)
	}
}

func TestGroupInputViewNew(t *testing.T) {
	m := NewGroupInputModel(gokeepasslib.UUID{}, "")
	m.width, m.height = 80, 24

	view := m.View()
	if !strings.Contains(view, "New Group") {
		t.Error("View() should contain 'New Group' for new group")
	}
}

func TestGroupInputViewRename(t *testing.T) {
	m := NewGroupInputModel(gokeepasslib.UUID{}, "Old")
	m.renaming = true
	m.width, m.height = 80, 24

	view := m.View()
	if !strings.Contains(view, "Rename Group") {
		t.Error("View() should contain 'Rename Group' for rename")
	}
}
