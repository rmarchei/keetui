package ui

import (
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/tobischo/gokeepasslib/v3"

	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/keepass"
)

func TestEditorFieldNavigation(t *testing.T) {
	m := NewEditorModel(nil, gokeepasslib.UUID{}, config.Default())
	m.width, m.height = 80, 24

	if m.focused != 0 {
		t.Errorf("initial focus = %d, want 0", m.focused)
	}

	// Tab moves to next field
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(EditorModel)
	if m.focused != 1 {
		t.Errorf("focus after tab = %d, want 1", m.focused)
	}

	// Shift+tab moves back
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = updated.(EditorModel)
	if m.focused != 0 {
		t.Errorf("focus after shift+tab = %d, want 0", m.focused)
	}
}

func TestEditorSave(t *testing.T) {
	m := NewEditorModel(nil, gokeepasslib.UUID{}, config.Default())
	m.width, m.height = 80, 24

	// Type a title
	m.inputs[fieldTitle].SetValue("My Entry")
	m.inputs[fieldUserName].SetValue("myuser")
	m.inputs[fieldPassword].SetValue("mypass")

	// Save
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("ctrl+s should produce a save command")
	}
	msg := cmd()
	saveMsg, ok := msg.(SaveEntryMsg)
	if !ok {
		t.Fatalf("expected SaveEntryMsg, got %T", msg)
	}
	if !saveMsg.IsNew {
		t.Error("should be a new entry")
	}
	if saveMsg.Entry.Title != "My Entry" {
		t.Errorf("title = %q, want %q", saveMsg.Entry.Title, "My Entry")
	}
	if saveMsg.Entry.UserName != "myuser" {
		t.Errorf("username = %q, want %q", saveMsg.Entry.UserName, "myuser")
	}
}

func TestEditorPasswordGenerationShowsPicker(t *testing.T) {
	m := NewEditorModel(nil, gokeepasslib.UUID{}, config.Default())
	m.width, m.height = 80, 24

	m.inputs[fieldPassword].SetValue("existing")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	if cmd == nil {
		t.Fatal("ctrl+g should produce a command")
	}
	msg := cmd()
	pickerMsg, ok := msg.(ShowPresetPickerMsg)
	if !ok {
		t.Fatalf("expected ShowPresetPickerMsg, got %T", msg)
	}
	if pickerMsg.CurrentPassword != "existing" {
		t.Errorf("CurrentPassword = %q, want %q", pickerMsg.CurrentPassword, "existing")
	}
}

func TestEditorSetGeneratedPassword(t *testing.T) {
	m := NewEditorModel(nil, gokeepasslib.UUID{}, config.Default())
	m.width, m.height = 80, 24

	m.SetGeneratedPassword("newpass123")
	if m.inputs[fieldPassword].Value() != "newpass123" {
		t.Errorf("password = %q, want %q", m.inputs[fieldPassword].Value(), "newpass123")
	}
	if !m.generated {
		t.Error("generated flag should be true")
	}
}

func TestEditorCustomFields(t *testing.T) {
	m := NewEditorModel(nil, gokeepasslib.UUID{}, config.Default())
	m.width, m.height = 80, 24

	if len(m.customInputs) != 0 {
		t.Errorf("initial custom fields = %d, want 0", len(m.customInputs))
	}

	// Add custom field
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	m = updated.(EditorModel)
	if len(m.customInputs) != 1 {
		t.Errorf("custom fields after ctrl+a = %d, want 1", len(m.customInputs))
	}

	// Toggle protected (focus is on the custom field key)
	if m.customInputs[0].protected {
		t.Error("new custom field should not be protected initially")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	m = updated.(EditorModel)
	if !m.customInputs[0].protected {
		t.Error("custom field should be protected after ctrl+t")
	}

	// Toggle back
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	m = updated.(EditorModel)
	if m.customInputs[0].protected {
		t.Error("custom field should not be protected after second ctrl+t")
	}

	// Delete custom field (focus should be on the new field)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	m = updated.(EditorModel)
	if len(m.customInputs) != 0 {
		t.Errorf("custom fields after ctrl+d = %d, want 0", len(m.customInputs))
	}
}

func TestEditorCancel(t *testing.T) {
	m := NewEditorModel(nil, gokeepasslib.UUID{}, config.Default())
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

func TestEditorEditExisting(t *testing.T) {
	entry := keepass.Entry{
		Title:    "Existing",
		UserName: "user",
		Password: "pass",
		URL:      "https://example.com",
		Notes:    "notes",
		TOTP:     "JBSWY3DPEHPK3PXP",
		CustomFields: []keepass.CustomField{
			{Key: "recovery", Value: "key123", Protected: true},
		},
	}

	m := NewEditorModel(&entry, gokeepasslib.UUID{}, config.Default())
	m.width, m.height = 80, 24

	if m.isNew {
		t.Error("should not be new when editing existing entry")
	}
	if m.inputs[fieldTitle].Value() != "Existing" {
		t.Errorf("title = %q, want %q", m.inputs[fieldTitle].Value(), "Existing")
	}
	if len(m.customInputs) != 1 {
		t.Errorf("custom fields = %d, want 1", len(m.customInputs))
	}
	if !m.customInputs[0].protected {
		t.Error("loaded protected custom field should retain protected flag")
	}
	if m.customInputs[0].value.EchoMode != textinput.EchoPassword {
		t.Error("loaded protected custom field should have EchoPassword mode")
	}
}

func TestEditorToggleProtectedOnFixedField(t *testing.T) {
	m := NewEditorModel(nil, gokeepasslib.UUID{}, config.Default())
	m.width, m.height = 80, 24

	// Focus is on Title (a fixed field); ctrl+t should be a no-op
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	m = updated.(EditorModel)
	// No crash, no custom fields created
	if len(m.customInputs) != 0 {
		t.Error("ctrl+t on fixed field should not create custom fields")
	}
}

func TestEditorToggleProtectedSetsEchoMode(t *testing.T) {
	m := NewEditorModel(nil, gokeepasslib.UUID{}, config.Default())
	m.width, m.height = 80, 24

	// Add a custom field
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	m = updated.(EditorModel)

	if m.customInputs[0].value.EchoMode != textinput.EchoNormal {
		t.Error("new custom field value should have EchoNormal")
	}

	// Toggle protected on
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	m = updated.(EditorModel)
	if m.customInputs[0].value.EchoMode != textinput.EchoPassword {
		t.Error("protected custom field value should have EchoPassword")
	}

	// Toggle protected off
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	m = updated.(EditorModel)
	if m.customInputs[0].value.EchoMode != textinput.EchoNormal {
		t.Error("unprotected custom field value should have EchoNormal")
	}
}

func TestEditorRejectsEmptyTitle(t *testing.T) {
	m := NewEditorModel(nil, gokeepasslib.UUID{}, config.Default())
	m.width, m.height = 80, 24

	// Leave title empty, try to save
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(EditorModel)
	if cmd != nil {
		t.Error("save with empty title should not produce a command")
	}
	if m.err == nil {
		t.Fatal("save with empty title should set an error")
	}
	if m.err.Error() != "title cannot be empty" {
		t.Errorf("error = %q, want %q", m.err.Error(), "title cannot be empty")
	}
}

func TestEditorRejectsWhitespaceTitle(t *testing.T) {
	m := NewEditorModel(nil, gokeepasslib.UUID{}, config.Default())
	m.width, m.height = 80, 24

	m.inputs[fieldTitle].SetValue("   ")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(EditorModel)
	if cmd != nil {
		t.Error("save with whitespace title should not produce a command")
	}
	if m.err == nil {
		t.Fatal("save with whitespace title should set an error")
	}
}

func TestEditorAcceptsNonEmptyTitle(t *testing.T) {
	m := NewEditorModel(nil, gokeepasslib.UUID{}, config.Default())
	m.width, m.height = 80, 24

	m.inputs[fieldTitle].SetValue("Valid Title")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("save with valid title should produce a command")
	}
	msg := cmd()
	saveMsg, ok := msg.(SaveEntryMsg)
	if !ok {
		t.Fatalf("expected SaveEntryMsg, got %T", msg)
	}
	if saveMsg.Entry.Title != "Valid Title" {
		t.Errorf("title = %q, want %q", saveMsg.Entry.Title, "Valid Title")
	}
}

func TestEditorView(t *testing.T) {
	m := NewEditorModel(nil, gokeepasslib.UUID{}, config.Default())
	m.width, m.height = 80, 24

	view := m.View()
	if view == "" {
		t.Error("View() should return non-empty string")
	}
}
