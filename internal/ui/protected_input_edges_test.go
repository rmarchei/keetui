package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rmarchei/keetui/internal/keepass"
)

func TestPrivacyModePastingAndExplicitRevealOfProtectedFields(t *testing.T) {
	for _, field := range []struct {
		name  string
		index int
	}{{"TOTP", fieldTOTP}, {"protected custom value", fixedFieldCount + 1}} {
		t.Run(field.name, func(t *testing.T) {
			entry := testEntry()
			entry.TOTP = "JBSWY3DPEHPK3PXP"
			entry.CustomFields = []keepass.CustomField{{Key: "Access secret", Value: "before", Protected: true}}
			m := NewEditorModel(&entry, entry.UUID, privateConfig())
			updated, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 24})
			m = updated.(EditorModel)
			for range field.index {
				updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
				m = updated.(EditorModel)
			}
			const pasted = "PASTED-SYNTHETIC-PRIVATE-VALUE"
			updated, _ = m.Update(tea.PasteMsg{Content: pasted})
			m = updated.(EditorModel)
			if strings.Contains(ansi.Strip(m.View().Content), pasted) {
				t.Fatal("pasting revealed a protected field in privacy mode")
			}
			updated, _ = m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
			m = updated.(EditorModel)
			if !strings.Contains(ansi.Strip(m.View().Content), pasted) {
				t.Fatal("explicit reveal did not show the protected field")
			}
			updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
			m = updated.(EditorModel)
			if strings.Contains(ansi.Strip(m.View().Content), pasted) {
				t.Fatal("leaving the field failed to re-mask its value")
			}
			_, cmd := m.save()
			if cmd == nil {
				t.Fatal("the protected value could not be saved")
			}
			saved := cmd().(SaveEntryMsg).Entry
			value := saved.TOTP
			if field.index >= fixedFieldCount {
				value = saved.CustomFields[0].Value
				if !saved.CustomFields[0].Protected {
					t.Fatal("revealing a field removed its protection")
				}
			}
			if !strings.Contains(value, pasted) {
				t.Fatal("masking altered the protected value being saved")
			}
		})
	}
}

func TestCustomGeneratorControlsProduceTheRequestedCharacterSet(t *testing.T) {
	m := NewPresetPickerModel(privateConfig(), "")
	m.cursor = len(m.presets) - 1
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(PresetPickerModel)
	m.lengthInput.SetValue("8")
	// Disable uppercase, digits and symbols through the displayed controls.
	for _, msg := range []tea.Msg{
		tea.KeyPressMsg{Code: tea.KeyTab}, tea.KeyPressMsg{Code: tea.KeySpace},
		tea.KeyPressMsg{Code: tea.KeyTab}, tea.KeyPressMsg{Code: tea.KeyTab}, tea.KeyPressMsg{Code: tea.KeySpace},
		tea.KeyPressMsg{Code: tea.KeyTab}, tea.KeyPressMsg{Code: tea.KeySpace},
		tea.KeyPressMsg{Code: tea.KeyTab}, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift},
		tea.KeyPressMsg{Code: tea.KeyEnter},
	} {
		updated, _ = m.Update(msg)
		m = updated.(PresetPickerModel)
	}
	if m.mode != pickerModePreview || len(m.generatedPassword) != 8 || strings.Trim(m.generatedPassword, "abcdefghijklmnopqrstuvwxyz") != "" {
		t.Fatalf("custom controls generated %q in mode %d, error %v", m.generatedPassword, m.mode, m.err)
	}
	if strings.Contains(ansi.Strip(m.View().Content), m.generatedPassword) {
		t.Fatal("custom generation exposed the password in privacy mode")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = updated.(PresetPickerModel)
	if m.mode != pickerModeCustom || m.customFocus != 0 || !m.lengthInput.Focused() || m.lengthInput.Value() != "8" {
		t.Fatal("returning from preview lost the custom settings or length focus")
	}
}

func TestCustomGeneratorRejectsInvalidLengthWithoutLeavingTheForm(t *testing.T) {
	for _, length := range []string{"", "text", "0", "3"} {
		m := NewPresetPickerModel(privateConfig(), "")
		m.cursor = len(m.presets) - 1
		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = updated.(PresetPickerModel)
		m.lengthInput.SetValue(length)
		updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = updated.(PresetPickerModel)
		if m.err == nil || m.mode != pickerModeCustom || m.generatedPassword != "" || cmd != nil {
			t.Fatalf("invalid length %q generated a password or left the form", length)
		}
	}
}
