package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPathInputSaveAs(t *testing.T) {
	m := NewPathInputModel("/tmp/test.kdbx")
	m.input.SetValue("/tmp/new.kdbx")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_ = updated

	if cmd == nil {
		t.Fatal("expected command on Enter")
	}
	msg := cmd()
	saveAs, ok := msg.(SaveAsMsg)
	if !ok {
		t.Fatalf("expected SaveAsMsg, got %T", msg)
	}
	if saveAs.Path != "/tmp/new.kdbx" {
		t.Errorf("Path = %q, want /tmp/new.kdbx", saveAs.Path)
	}
}

func TestPathInputRejectsEmpty(t *testing.T) {
	m := NewPathInputModel("")
	m.input.SetValue("")

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Error("expected no command for empty path")
	}
}

func TestPathInputCancel(t *testing.T) {
	m := NewPathInputModel("/tmp/test.kdbx")

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected command on Esc")
	}
	msg := cmd()
	if _, ok := msg.(CancelEditMsg); !ok {
		t.Fatalf("expected CancelEditMsg, got %T", msg)
	}
}

func TestPathInputPrefilledPath(t *testing.T) {
	m := NewPathInputModel("/home/user/passwords.kdbx")
	if got := m.input.Value(); got != "/home/user/passwords.kdbx" {
		t.Errorf("pre-filled value = %q, want /home/user/passwords.kdbx", got)
	}
}

func TestPathInputView(t *testing.T) {
	m := NewPathInputModel("/tmp/test.kdbx")
	view := m.View()
	if !strings.Contains(view, "Save As") {
		t.Error("view should contain 'Save As' title")
	}
}

func TestPathInputExpandsTilde(t *testing.T) {
	m := NewPathInputModel("")
	m.input.SetValue("~/test.kdbx")

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command on Enter")
	}
	msg := cmd()
	saveAs, ok := msg.(SaveAsMsg)
	if !ok {
		t.Fatalf("expected SaveAsMsg, got %T", msg)
	}
	if strings.HasPrefix(saveAs.Path, "~") {
		t.Errorf("tilde should be expanded, got %q", saveAs.Path)
	}
	if !strings.HasSuffix(saveAs.Path, "/test.kdbx") {
		t.Errorf("path should end with /test.kdbx, got %q", saveAs.Path)
	}
}
