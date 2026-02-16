package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestUnlockRejectsEmpty(t *testing.T) {
	m := NewUnlockModel("test.kdbx", "")
	m.width, m.height = 80, 24

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(UnlockModel)
	if cmd != nil {
		t.Error("enter with empty password should not produce a command")
	}
	if m.err == nil {
		t.Fatal("expected error for empty password")
	}
	if m.err.Error() != "password cannot be empty" {
		t.Errorf("error = %q, want %q", m.err.Error(), "password cannot be empty")
	}
}

func TestUnlockAcceptsPassword(t *testing.T) {
	m := NewUnlockModel("test.kdbx", "key.key")
	m.width, m.height = 80, 24

	m.input.SetValue("mypassword")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter with password should produce a command")
	}
	msg := cmd()
	dbMsg, ok := msg.(DatabaseOpenedMsg)
	if !ok {
		t.Fatalf("expected DatabaseOpenedMsg, got %T", msg)
	}
	if dbMsg.FilePath != "test.kdbx" {
		t.Errorf("FilePath = %q, want %q", dbMsg.FilePath, "test.kdbx")
	}
	if dbMsg.Password != "mypassword" {
		t.Errorf("Password = %q, want %q", dbMsg.Password, "mypassword")
	}
	if dbMsg.KeyFilePath != "key.key" {
		t.Errorf("KeyFilePath = %q, want %q", dbMsg.KeyFilePath, "key.key")
	}
}

func TestUnlockShowsKeyFilePath(t *testing.T) {
	m := NewUnlockModel("test.kdbx", "key.key")
	m.width, m.height = 80, 24

	view := m.View()
	if !strings.Contains(view, "key.key") {
		t.Error("View() should contain key file path")
	}
}

func TestUnlockHidesKeyFileWhenEmpty(t *testing.T) {
	m := NewUnlockModel("test.kdbx", "")
	m.width, m.height = 80, 24

	view := m.View()
	if strings.Contains(view, "Key:") {
		t.Error("View() should not contain Key: line when no key file")
	}
}

func TestUnlockView(t *testing.T) {
	m := NewUnlockModel("test.kdbx", "")
	m.width, m.height = 80, 24

	view := m.View()
	if view == "" {
		t.Error("View() should return non-empty string")
	}
	if !strings.Contains(view, "test.kdbx") {
		t.Error("View() should contain file path")
	}
}
