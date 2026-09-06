package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestPathInputSaveAs(t *testing.T) {
	m := NewPathInputModel("/tmp/test.kdbx", "KDBX 3.1 (AES)", false)
	m.input.SetValue("/tmp/new.kdbx")

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
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
	m := NewPathInputModel("", "KDBX 3.1 (AES)", false)
	m.input.SetValue("")

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Error("expected no command for empty path")
	}
}

func TestPathInputCancel(t *testing.T) {
	m := NewPathInputModel("/tmp/test.kdbx", "KDBX 3.1 (AES)", false)

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected command on Esc")
	}
	msg := cmd()
	if _, ok := msg.(CancelEditMsg); !ok {
		t.Fatalf("expected CancelEditMsg, got %T", msg)
	}
}

func TestPathInputPrefilledPath(t *testing.T) {
	m := NewPathInputModel("/home/user/passwords.kdbx", "KDBX 4 (Argon2)", false)
	if got := m.input.Value(); got != "/home/user/passwords.kdbx" {
		t.Errorf("pre-filled value = %q, want /home/user/passwords.kdbx", got)
	}
}

func TestPathInputView(t *testing.T) {
	m := NewPathInputModel("/tmp/test.kdbx", "KDBX 3.1 (AES)", false)
	view := m.View().Content
	if !strings.Contains(view, "Save As") {
		t.Error("view should contain 'Save As' title")
	}
}

func TestPathInputUpgradeSelect(t *testing.T) {
	m := NewPathInputModel("/tmp/test.kdbx", "KDBX 3.1 (AES)", true)
	m.input.SetValue("/tmp/test.kdbx")

	// Tab to format field
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(PathInputModel)
	if m.focused != focusFormat {
		t.Fatal("expected focus on format after tab")
	}

	// Right arrow to select KDBX 4
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'l', Text: string('l')})
	m = updated.(PathInputModel)

	// View should show KDBX 4 selected
	view := m.View().Content
	if !strings.Contains(view, "KDBX 4 (Argon2)") {
		t.Error("view should show KDBX 4 option")
	}

	// Save should include Upgrade=true
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command on Enter")
	}
	msg := cmd()
	saveAs, ok := msg.(SaveAsMsg)
	if !ok {
		t.Fatalf("expected SaveAsMsg, got %T", msg)
	}
	if !saveAs.Upgrade {
		t.Error("Upgrade should be true after selecting KDBX 4")
	}
}

func TestPathInputUpgradeNotAvailable(t *testing.T) {
	m := NewPathInputModel("/tmp/test.kdbx", "KDBX 4 (Argon2)", false)
	m.input.SetValue("/tmp/test.kdbx")

	// Tab should not switch focus when canUpgrade is false
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(PathInputModel)
	if m.focused != focusPath {
		t.Error("focus should stay on path when canUpgrade is false")
	}

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msg := cmd()
	saveAs := msg.(SaveAsMsg)
	if saveAs.Upgrade {
		t.Error("Upgrade should be false when canUpgrade is false")
	}
}

func TestPathInputViewShowsFormat(t *testing.T) {
	m := NewPathInputModel("/tmp/test.kdbx", "KDBX 3.1 (AES)", true)
	view := m.View().Content
	if !strings.Contains(view, "Format") {
		t.Error("view should contain Format label")
	}
	if !strings.Contains(view, "Upgrade to KDBX 4 (Argon2)") {
		t.Error("view should show upgrade option when canUpgrade is true")
	}
}

func TestPathInputTabBackToPath(t *testing.T) {
	m := NewPathInputModel("/tmp/test.kdbx", "KDBX 3.1 (AES)", true)

	// Tab to format
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(PathInputModel)
	if m.focused != focusFormat {
		t.Fatal("expected focus on format")
	}

	// Tab back to path
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(PathInputModel)
	if m.focused != focusPath {
		t.Fatal("expected focus back on path")
	}
}

func TestPathInputExpandsTilde(t *testing.T) {
	m := NewPathInputModel("", "KDBX 3.1 (AES)", false)
	m.input.SetValue("~/test.kdbx")

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
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
