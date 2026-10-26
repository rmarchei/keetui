package ui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/tobischo/gokeepasslib/v3"

	"github.com/rmarchei/keetui/internal/config"
)

// privateConfig is the defaults with privacy mode on.
func privateConfig() config.Config {
	cfg := config.Default()
	on := true
	cfg.PrivacyMode = &on
	return cfg
}

// Typing in a protected field reveals it briefly, except in privacy mode,
// where neither typing nor a generated password shows it; Ctrl+R still does.
func TestPrivacyModeKeepsEditorPasswordsHidden(t *testing.T) {
	typed := func(cfg config.Config) EditorModel {
		m := NewEditorModel(nil, gokeepasslib.UUID{}, cfg)
		m.width, m.height = 80, 24
		for _, msg := range []tea.Msg{tea.KeyPressMsg{Code: tea.KeyTab}, tea.KeyPressMsg{Code: tea.KeyTab}, tea.KeyPressMsg{Code: 'x', Text: "x"}} {
			next, _ := m.Update(msg)
			m = next.(EditorModel)
		}
		if m.focused != fieldPassword || m.inputs[fieldPassword].Value() != "x" {
			t.Fatalf("focused %d, password %q: want x typed into the password", m.focused, m.inputs[fieldPassword].Value())
		}
		return m
	}
	if m := typed(config.Default()); m.inputs[fieldPassword].EchoMode != textinput.EchoNormal {
		t.Fatal("typing should reveal the password briefly without privacy mode")
	}
	m := typed(privateConfig())
	if m.inputs[fieldPassword].EchoMode != textinput.EchoPassword {
		t.Error("typing revealed the password in privacy mode")
	}
	if cmd := m.SetGeneratedPassword("synthetic-generated"); cmd != nil || m.inputs[fieldPassword].EchoMode != textinput.EchoPassword {
		t.Error("a generated password was revealed in privacy mode")
	}
	next, _ := m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if next.(EditorModel).inputs[fieldPassword].EchoMode != textinput.EchoNormal {
		t.Error("Ctrl+R must still reveal the password")
	}
}

// The generator's preview masks the new password in privacy mode; p shows it.
func TestPrivacyModeMasksTheGeneratorPreview(t *testing.T) {
	m := NewPresetPickerModel(privateConfig(), "")
	m.width, m.height = 80, 24
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(PresetPickerModel)
	if m.mode != pickerModePreview || m.generatedPassword == "" {
		t.Fatalf("mode %d: want the preview of a generated password", m.mode)
	}
	if view := ansi.Strip(m.View().Content); strings.Contains(view, m.generatedPassword) {
		t.Error("the preview shows the password in privacy mode")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	m = next.(PresetPickerModel)
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, m.generatedPassword) {
		t.Error("p should show the password")
	}
}

// The settings screen turns privacy mode on and off.
func TestPrivacyModeSetting(t *testing.T) {
	m := NewSettingsModel(config.Default())
	if got := m.inputs[settingPrivacyMode].Value(); got != "0" {
		t.Fatalf("privacy mode shows %q, want off", got)
	}
	m.inputs[settingPrivacyMode].SetValue("1")
	cfg, errs := m.draftConfig()
	if len(errs) != 0 || !cfg.HidePasswords() {
		t.Errorf("draft: privacy %v, errors %v", cfg.HidePasswords(), errs)
	}
	if !isToggleSetting(settingPrivacyMode) {
		t.Error("privacy mode should be a toggle")
	}
}
