package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/keepass"
)

func testEntry() keepass.Entry {
	return keepass.Entry{
		Title:    "Test Entry",
		UserName: "testuser",
		Password: "testpassword123",
		URL:      "https://example.com",
		Notes:    "Some notes",
	}
}

func TestDetailPasswordToggle(t *testing.T) {
	entry := testEntry()
	m := NewDetailModel(entry, config.Default())
	m.SetSize(80, 24)

	if m.showPassword {
		t.Error("password should be hidden initially")
	}

	// Toggle password
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(DetailModel)
	if !m.showPassword {
		t.Error("password should be visible after toggle")
	}

	// Toggle back
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(DetailModel)
	if m.showPassword {
		t.Error("password should be hidden after second toggle")
	}
}

func TestDetailCopyCommands(t *testing.T) {
	entry := testEntry()
	m := NewDetailModel(entry, config.Default())
	m.SetSize(80, 24)

	tests := []struct {
		key   rune
		field string
		value string
	}{
		{'1', "username", "testuser"},
		{'2', "password", "testpassword123"},
		{'3', "url", "https://example.com"},
	}

	for _, tt := range tests {
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{tt.key}})
		m = updated.(DetailModel)
		if cmd == nil {
			t.Errorf("key %c should produce a command", tt.key)
			continue
		}
		msg := cmd()
		copyMsg, ok := msg.(CopyToClipboardMsg)
		if !ok {
			t.Errorf("key %c: expected CopyToClipboardMsg, got %T", tt.key, msg)
			continue
		}
		if copyMsg.Field != tt.field {
			t.Errorf("key %c: field = %q, want %q", tt.key, copyMsg.Field, tt.field)
		}
		if copyMsg.Value != tt.value {
			t.Errorf("key %c: value = %q, want %q", tt.key, copyMsg.Value, tt.value)
		}
	}
}

func TestDetailEditMsg(t *testing.T) {
	entry := testEntry()
	m := NewDetailModel(entry, config.Default())
	m.SetSize(80, 24)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if cmd == nil {
		t.Fatal("e key should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(EditEntryMsg); !ok {
		t.Errorf("expected EditEntryMsg, got %T", msg)
	}
}

func TestDetailQRToggle(t *testing.T) {
	entry := testEntry()
	m := NewDetailModel(entry, config.Default())
	m.SetSize(80, 24)

	// Show QR
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Q'}})
	m = updated.(DetailModel)
	if !m.showQR {
		t.Error("QR should be shown after Q")
	}

	// Close QR
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(DetailModel)
	if m.showQR {
		t.Error("QR should be hidden after esc")
	}
}

func TestDetailHIBPTrigger(t *testing.T) {
	entry := testEntry()
	m := NewDetailModel(entry, config.Default())
	m.SetSize(80, 24)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	m = updated.(DetailModel)
	if cmd == nil {
		t.Fatal("5 key should produce a command for HIBP check")
	}
	if !m.hibpChecking {
		t.Error("hibpChecking should be true after pressing 5")
	}
}

func TestDetailBackMsg(t *testing.T) {
	entry := testEntry()
	m := NewDetailModel(entry, config.Default())
	m.SetSize(80, 24)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(BackToBrowserMsg); !ok {
		t.Errorf("expected BackToBrowserMsg, got %T", msg)
	}
}

func TestDetailAttachmentMode(t *testing.T) {
	entry := testEntry()
	m := NewDetailModel(entry, config.Default())
	m.SetSize(80, 24)

	// Set attachments
	m.attachments = []keepass.Attachment{
		{Name: "file.txt", Size: 100},
		{Name: "doc.pdf", Size: 2048},
	}
	m.RefreshContent()

	// Enter attachment mode
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = updated.(DetailModel)
	if !m.attachMode {
		t.Error("should be in attachment mode after 'a'")
	}

	// Navigate
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(DetailModel)
	if m.attachIdx != 1 {
		t.Errorf("attachIdx = %d, want 1", m.attachIdx)
	}

	// Exit attachment mode
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(DetailModel)
	if m.attachMode {
		t.Error("should exit attachment mode on esc")
	}
}

func TestDetailView(t *testing.T) {
	entry := testEntry()
	m := NewDetailModel(entry, config.Default())
	m.SetSize(80, 24)

	view := m.View()
	if view == "" {
		t.Error("View() should return non-empty string")
	}
	if !strings.Contains(view, "Test Entry") {
		t.Error("View() should contain entry title")
	}
}
