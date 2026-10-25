package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestUnlockRejectsEmpty(t *testing.T) {
	m := NewUnlockModel("test.kdbx", "")
	m.width, m.height = 80, 24

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
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
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
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

	view := m.View().Content
	if !strings.Contains(view, "key.key") {
		t.Error("View() should contain key file path")
	}
}

func TestUnlockHidesKeyFileWhenEmpty(t *testing.T) {
	m := NewUnlockModel("test.kdbx", "")
	m.width, m.height = 80, 24

	view := m.View().Content
	if strings.Contains(view, "Key:") {
		t.Error("View() should not contain Key: line when no key file")
	}
}

func TestUnlockView(t *testing.T) {
	m := NewUnlockModel("test.kdbx", "")
	m.width, m.height = 80, 24

	view := m.View().Content
	if view == "" {
		t.Error("View() should return non-empty string")
	}
	if !strings.Contains(view, "test.kdbx") {
		t.Error("View() should contain file path")
	}
}

func TestUnlockCardIsCenteredUnlessTheErrorNeedsScrolling(t *testing.T) {
	m := NewUnlockModel("/vaults/synthetic.kdbx", "")
	m.width, m.height = 100, 30
	view := m.View().Content
	assertFrame(t, view, m.width, m.height)
	card, fits := m.card()
	if !fits {
		t.Fatal("the card should fit 100x30")
	}
	if col := column(t, view, "keetui"); col < 20 {
		t.Errorf("card starts at column %d, want it centered", col)
	}
	if !strings.Contains(ansi.Strip(card), "synthetic.kdbx") || strings.Contains(ansi.Strip(view), "pgup/pgdown") {
		t.Error("card hides the file, or offers to scroll an error it shows whole")
	}

	m.err = errors.New(strings.Repeat("Synthetic diagnosis detail. ", 60))
	if _, fits := m.card(); fits {
		t.Fatal("an error taller than the screen must not use the card")
	}
	if plain := ansi.Strip(m.View().Content); !strings.Contains(plain, "pgup/pgdown error") || !strings.Contains(plain, "synthetic.kdbx") {
		t.Errorf("long error lost the scrolling hint or the file:\n%s", plain)
	}
}
