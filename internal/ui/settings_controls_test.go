package ui

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rmarchei/keetui/internal/config"
)

func TestSettingsSpaceTogglesWithoutTyping(t *testing.T) {
	m := NewSettingsModel(config.Default())
	for _, field := range []int{settingPasswordStrength, settingPWUppercase, settingPWLowercase, settingPWDigits, settingPWSymbols, settingConfirmQuit, settingBackupOnSave} {
		m.focused = field
		before := m.inputs[field].Value()
		updated, _ := m.Update(textKey(" "))
		m = updated.(SettingsModel)
		if m.inputs[field].Value() == before {
			t.Fatal("space did not toggle")
		}
		updated, _ = m.Update(tea.PasteMsg{Content: "garbage"})
		m = updated.(SettingsModel)
		if m.inputs[field].Value() != "0" && m.inputs[field].Value() != "1" {
			t.Fatal("toggle accepted free text")
		}
		if strings.Contains(ansi.Strip(m.View().Content), "1: on") {
			t.Fatal("raw boolean hint remains")
		}
	}
}

func TestSettingsChoicePickerCommitAndCancel(t *testing.T) {
	for _, field := range []int{settingDefaultPreset, settingTheme} {
		m := NewSettingsModel(config.Default())
		m.focused, m.width, m.height = field, 40, 11
		original := m.inputs[field].Value()
		updated, _ := m.Update(textKey(" "))
		m = updated.(SettingsModel)
		if !m.picking {
			t.Fatal("picker did not open")
		}
		assertFrame(t, m.View().Content, 40, 11)
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		m = updated.(SettingsModel)
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
		m = updated.(SettingsModel)
		if m.inputs[field].Value() != original || m.picking {
			t.Fatal("cancel changed selection")
		}
		updated, _ = m.Update(textKey(" "))
		m = updated.(SettingsModel)
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		m = updated.(SettingsModel)
		want := m.choices()[m.choice]
		updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = updated.(SettingsModel)
		if m.inputs[field].Value() != want || m.picking || cmd != nil {
			t.Fatal("picker must select without saving")
		}
	}
}

func TestSettingsLiveValidationDoesNotWrite(t *testing.T) {
	isolateSettingsConfig(t)
	before, err := os.ReadFile(config.Path())
	if err != nil {
		t.Fatal(err)
	}
	m := NewSettingsModel(config.Default())
	m.inputs[settingClipboardClear].SetValue("")
	updated, _ := m.Update(textKey("5"))
	m = updated.(SettingsModel)
	if m.fieldErrors[settingClipboardClear] == "" || !strings.Contains(ansi.Strip(m.View().Content), "Must be at least 15") {
		t.Fatal("missing live validation")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Fatal("invalid draft was saved")
	}
	after, err := os.ReadFile(config.Path())
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("invalid draft touched config")
	}
	m.inputs[settingClipboardClear].SetValue("15")
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 23})
	m = updated.(SettingsModel)
	if m.fieldErrors[settingClipboardClear] != "" {
		t.Fatal("stale field error")
	}
}

func TestSettingsRecordKeyRejectsConflictAndPersists(t *testing.T) {
	isolateSettingsConfig(t)
	m := NewSettingsModel(config.Default())
	m.editingKeys, m.recording = true, true
	for i, binding := range m.bindings.Bindings() {
		if binding.ID == "delete" {
			m.bindingIndex = i
		}
	}
	updated, _ := m.Update(textKey("n"))
	m = updated.(SettingsModel)
	if m.bindingError == "" || !m.recording || m.bindings.Delete != "d" {
		t.Fatal("duplicate shortcut accepted")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyF1})
	m = updated.(SettingsModel)
	if m.bindingError == "" || m.bindings.Delete != "d" {
		t.Fatal("reserved shortcut accepted")
	}
	updated, _ = m.Update(textKey("#"))
	m = updated.(SettingsModel)
	if m.recording || m.bindings.Delete != "#" {
		t.Fatal("recorded key was lost")
	}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	m = updated.(SettingsModel)
	if m.isError || cmd == nil {
		t.Fatalf("save failed: %s", m.status)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Keys().Delete != "#" {
		t.Fatal("recorded key did not persist")
	}
}

func TestSettingsCaptureBypassesGlobalHelp(t *testing.T) {
	m := createTestApp(t)
	updated, _ := m.Update(ShowSettingsMsg{})
	m = updated.(AppModel)
	m.settings.editingKeys, m.settings.recording = true, true
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyF1})
	m = updated.(AppModel)
	if m.state != viewSettings || m.settings.bindingError == "" {
		t.Fatal("F1 escaped key recording")
	}
}
