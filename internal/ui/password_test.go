package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestPasswordNavigation(t *testing.T) {
	m := NewPasswordModel()
	m.width, m.height = 80, 24

	if m.focused != 0 {
		t.Errorf("initial focus = %d, want 0", m.focused)
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(PasswordModel)
	if m.focused != 1 {
		t.Errorf("focus after tab = %d, want 1", m.focused)
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(PasswordModel)
	if m.focused != 0 {
		t.Errorf("focus after second tab = %d, want 0", m.focused)
	}
}

func TestPasswordRejectsEmpty(t *testing.T) {
	m := NewPasswordModel()
	m.width, m.height = 80, 24

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(PasswordModel)
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

func TestPasswordRejectsMismatch(t *testing.T) {
	m := NewPasswordModel()
	m.width, m.height = 80, 24

	m.newInput.SetValue("password1")
	m.confirmInput.SetValue("password2")

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(PasswordModel)
	if cmd != nil {
		t.Error("mismatched passwords should not produce a command")
	}
	if m.err == nil {
		t.Fatal("expected error for mismatched passwords")
	}
	if m.err.Error() != "passwords do not match" {
		t.Errorf("error = %q, want %q", m.err.Error(), "passwords do not match")
	}
}

func TestPasswordAcceptsMatch(t *testing.T) {
	m := NewPasswordModel()
	m.width, m.height = 80, 24

	m.newInput.SetValue("newpass123")
	m.confirmInput.SetValue("newpass123")

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("matching passwords should produce a command")
	}
	msg := cmd()
	cpMsg, ok := msg.(ChangePasswordMsg)
	if !ok {
		t.Fatalf("expected ChangePasswordMsg, got %T", msg)
	}
	if cpMsg.NewPassword != "newpass123" {
		t.Errorf("NewPassword = %q, want %q", cpMsg.NewPassword, "newpass123")
	}
}

func TestPasswordCancel(t *testing.T) {
	m := NewPasswordModel()
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

func TestPasswordView(t *testing.T) {
	m := NewPasswordModel()
	m.width, m.height = 80, 24

	view := m.View().Content
	if view == "" {
		t.Error("View() should return non-empty string")
	}
}
