package ui

import (
	"testing"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
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
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(EditorModel)
	if m.focused != 1 {
		t.Errorf("focus after tab = %d, want 1", m.focused)
	}

	// Shift+tab moves back
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
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
	_, cmd := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
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
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
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
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	m = updated.(EditorModel)
	if len(m.customInputs) != 1 {
		t.Errorf("custom fields after ctrl+a = %d, want 1", len(m.customInputs))
	}

	// Toggle protected (focus is on the custom field key)
	if m.customInputs[0].protected {
		t.Error("new custom field should not be protected initially")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	m = updated.(EditorModel)
	if !m.customInputs[0].protected {
		t.Error("custom field should be protected after ctrl+t")
	}

	// Toggle back
	updated, _ = m.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	m = updated.(EditorModel)
	if m.customInputs[0].protected {
		t.Error("custom field should not be protected after second ctrl+t")
	}

	// Delete custom field (focus should be on the new field)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	m = updated.(EditorModel)
	if len(m.customInputs) != 0 {
		t.Errorf("custom fields after ctrl+d = %d, want 0", len(m.customInputs))
	}
}

func TestEditorCancel(t *testing.T) {
	m := NewEditorModel(nil, gokeepasslib.UUID{}, config.Default())
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

func TestEditorEditExisting(t *testing.T) {
	entry := keepass.Entry{
		Title:    "Existing",
		UserName: "user",
		Password: "pass",
		URL:      "https://example.com",
		Notes:    "notes",
		TOTP:     "JBSWY3DPEHPK3PXP",
		Tags:     "favorite;work",
		CustomFields: []keepass.CustomField{
			{Key: "recovery", Value: "key123", Protected: true},
		},
	}

	m := NewEditorModel(&entry, gokeepasslib.UUID{}, config.Default())
	m.width, m.height = 80, 24

	if m.isNew {
		t.Error("should not be new when editing existing entry")
	}
	if m.tags != "favorite;work" {
		t.Errorf("tags = %q, want %q", m.tags, "favorite;work")
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

func TestEditorSavePreservesTags(t *testing.T) {
	entry := keepass.Entry{
		Title: "Existing",
		Tags:  "favorite;work",
	}

	m := NewEditorModel(&entry, gokeepasslib.UUID{}, config.Default())
	m.width, m.height = 80, 24

	_, cmd := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+s should produce a save command")
	}
	saveMsg, ok := cmd().(SaveEntryMsg)
	if !ok {
		t.Fatalf("expected SaveEntryMsg, got %T", cmd())
	}
	if saveMsg.Entry.Tags != "favorite;work" {
		t.Errorf("tags = %q, want %q (tags must round-trip through an edit)", saveMsg.Entry.Tags, "favorite;work")
	}
}

func TestEditorExpiryRoundTripsInLocalTime(t *testing.T) {
	entry := keepass.Entry{
		Title:      "Existing",
		Expires:    true,
		ExpiryTime: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
	}

	m := NewEditorModel(&entry, gokeepasslib.UUID{}, config.Default())
	m.width, m.height = 80, 24
	displayed := m.inputs[fieldExpiry].Value()

	// Saving without touching the field must keep the displayed date.
	_, cmd := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+s should produce a save command")
	}
	saveMsg, ok := cmd().(SaveEntryMsg)
	if !ok {
		t.Fatalf("expected SaveEntryMsg, got %T", cmd())
	}
	got := saveMsg.Entry.ExpiryTime.Local().Format("2006-01-02")
	if got != displayed {
		t.Errorf("expiry after save = %s, displayed %s (date must not drift across an edit)", got, displayed)
	}
}

func TestEditorNotesArrowNavigation(t *testing.T) {
	m := NewEditorModel(nil, gokeepasslib.UUID{}, config.Default())
	m.width, m.height = 80, 24
	m.notesInput.SetValue("line1\nline2\nline3")
	m.focused = fieldNotes
	m.updateFocus()

	// Cursor starts on the last line; "up" must move within the textarea,
	// not jump to another field.
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = updated.(EditorModel)
	if m.focused != fieldNotes {
		t.Fatalf("focus = %d, want fieldNotes (up must move the cursor inside notes)", m.focused)
	}
	if m.notesInput.Line() != 1 {
		t.Errorf("cursor line = %d, want 1", m.notesInput.Line())
	}

	// From the first line, "up" leaves the field.
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = updated.(EditorModel)
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = updated.(EditorModel)
	if m.focused == fieldNotes {
		t.Error("up on the first notes line should move focus to the previous field")
	}
}

func TestEditorPresetPickerRemasksPassword(t *testing.T) {
	m := NewEditorModel(nil, gokeepasslib.UUID{}, config.Default())
	m.width, m.height = 80, 24
	m.focused = fieldPassword
	m.updateFocus()

	// Typing auto-reveals the password field.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'x', Text: string('x')})
	m = updated.(EditorModel)
	if m.inputs[fieldPassword].EchoMode != textinput.EchoNormal {
		t.Fatal("typing should auto-reveal the password field")
	}

	// Opening the preset picker before the 1s re-mask tick fires must not
	// leave the password revealed.
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
	m = updated.(EditorModel)
	if m.inputs[fieldPassword].EchoMode != textinput.EchoPassword {
		t.Error("password must be re-masked when leaving for the preset picker")
	}
}

func TestEditorToggleProtectedOnFixedField(t *testing.T) {
	m := NewEditorModel(nil, gokeepasslib.UUID{}, config.Default())
	m.width, m.height = 80, 24

	// Focus is on Title (a fixed field); ctrl+t should be a no-op
	updated, _ := m.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
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
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	m = updated.(EditorModel)

	if m.customInputs[0].value.EchoMode != textinput.EchoNormal {
		t.Error("new custom field value should have EchoNormal")
	}

	// Toggle protected on
	updated, _ = m.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	m = updated.(EditorModel)
	if m.customInputs[0].value.EchoMode != textinput.EchoPassword {
		t.Error("protected custom field value should have EchoPassword")
	}

	// Toggle protected off
	updated, _ = m.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	m = updated.(EditorModel)
	if m.customInputs[0].value.EchoMode != textinput.EchoNormal {
		t.Error("unprotected custom field value should have EchoNormal")
	}
}

func TestEditorRejectsEmptyTitle(t *testing.T) {
	m := NewEditorModel(nil, gokeepasslib.UUID{}, config.Default())
	m.width, m.height = 80, 24

	// Leave title empty, try to save
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
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
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
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
	_, cmd := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
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

	view := m.View().Content
	if view == "" {
		t.Error("View() should return non-empty string")
	}
}
