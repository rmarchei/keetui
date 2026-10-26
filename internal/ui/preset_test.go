package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rmarchei/keetui/internal/config"
)

func TestNewPresetPickerDefaultCursor(t *testing.T) {
	cfg := config.Default()
	m := NewPresetPickerModel(cfg, "")
	// Default preset is "Strong", should be index 2 (PIN=0, Alphanumeric=1, Strong=2)
	if m.presets[m.cursor].name != "Strong" {
		t.Errorf("cursor on %q, want %q", m.presets[m.cursor].name, "Strong")
	}
}

func TestNewPresetPickerCustomDefault(t *testing.T) {
	cfg := config.Default()
	cfg.DefaultPreset = "PIN"
	m := NewPresetPickerModel(cfg, "")
	if m.presets[m.cursor].name != "PIN" {
		t.Errorf("cursor on %q, want %q", m.presets[m.cursor].name, "PIN")
	}
}

func TestPresetPickerLikeCurrentShown(t *testing.T) {
	cfg := config.Default()
	m := NewPresetPickerModel(cfg, "MyP@ssw0rd")
	found := false
	for _, p := range m.presets {
		if p.name == "Like current" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Like current preset not shown when password is non-empty")
	}
}

func TestPresetPickerLikeCurrentHidden(t *testing.T) {
	cfg := config.Default()
	m := NewPresetPickerModel(cfg, "")
	for _, p := range m.presets {
		if p.name == "Like current" {
			t.Error("Like current preset should not appear when password is empty")
		}
	}
}

func TestPresetPickerNavigation(t *testing.T) {
	cfg := config.Default()
	m := NewPresetPickerModel(cfg, "")
	initial := m.cursor

	// Navigate down
	updated, _ := m.Update(textKey("j"))
	m = updated.(PresetPickerModel)
	if m.cursor != initial+1 {
		t.Errorf("after j: cursor=%d, want %d", m.cursor, initial+1)
	}

	// Navigate up
	updated, _ = m.Update(textKey("k"))
	m = updated.(PresetPickerModel)
	if m.cursor != initial {
		t.Errorf("after k: cursor=%d, want %d", m.cursor, initial)
	}
}

func TestPresetPickerSelectPINPreview(t *testing.T) {
	cfg := config.Default()
	m := NewPresetPickerModel(cfg, "")
	m.cursor = 0 // PIN

	// Enter transitions to preview mode
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(PresetPickerModel)
	if m.mode != pickerModePreview {
		t.Fatalf("expected preview mode, got %d", m.mode)
	}
	if m.lastPresetName != "PIN" {
		t.Errorf("lastPresetName = %q, want %q", m.lastPresetName, "PIN")
	}
	if len(m.generatedPassword) != 6 {
		t.Errorf("PIN length = %d, want 6", len(m.generatedPassword))
	}
	for _, r := range m.generatedPassword {
		if r < '0' || r > '9' {
			t.Errorf("PIN contains non-digit: %c", r)
		}
	}
	if !m.passwordVisible {
		t.Error("password should be visible by default in preview")
	}
}

func TestPresetPickerSelectPassphrasePreview(t *testing.T) {
	cfg := config.Default()
	m := NewPresetPickerModel(cfg, "")
	for i, p := range m.presets {
		if p.name == "Passphrase" {
			m.cursor = i
			break
		}
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(PresetPickerModel)
	if m.mode != pickerModePreview {
		t.Fatalf("expected preview mode, got %d", m.mode)
	}
	words := strings.Split(m.generatedPassword, "-")
	if len(words) != 8 {
		t.Errorf("passphrase word count = %d, want the default 8", len(words))
	}
}

// PIN length, passphrase words and separator come from the settings, and
// the list describes what they make.
func TestPresetPickerUsesGeneratorSettings(t *testing.T) {
	cfg := config.Default()
	cfg.PINLength, cfg.PassphraseWords, cfg.PassphraseSeparator = 10, 5, "."
	m := NewPresetPickerModel(cfg, "")
	descriptions := map[string]string{}
	for _, p := range m.presets {
		descriptions[p.name] = p.desc
	}
	if descriptions["PIN"] != "10 digits" || descriptions["Passphrase"] != "5 words, separated by ." {
		t.Errorf("descriptions = %q", descriptions)
	}
	pin, err := m.generateForPreset("PIN")
	if err != nil || len(pin) != 10 || strings.Trim(pin, "0123456789") != "" {
		t.Errorf("PIN = %q, %v", pin, err)
	}
	phrase, err := m.generateForPreset("Passphrase")
	if err != nil || len(strings.Split(phrase, ".")) != 5 {
		t.Errorf("passphrase = %q, %v", phrase, err)
	}
}

func TestPresetPickerPreviewAccept(t *testing.T) {
	cfg := config.Default()
	m := NewPresetPickerModel(cfg, "")
	m.cursor = 0 // PIN

	// Enter → preview
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(PresetPickerModel)

	pw := m.generatedPassword

	// Enter again → accept
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command from Enter in preview")
	}
	msg := cmd()
	gen, ok := msg.(GeneratedPasswordMsg)
	if !ok {
		t.Fatalf("expected GeneratedPasswordMsg, got %T", msg)
	}
	if gen.Password != pw {
		t.Errorf("accepted password = %q, want %q", gen.Password, pw)
	}
}

func TestPresetPickerPreviewRefresh(t *testing.T) {
	cfg := config.Default()
	m := NewPresetPickerModel(cfg, "")
	m.cursor = 0 // PIN

	// Enter → preview
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(PresetPickerModel)
	firstPW := m.generatedPassword

	// Press 'r' to refresh — password should change (with very high probability)
	// Try multiple times to avoid flaky test
	changed := false
	for range 10 {
		updated, _ = m.Update(textKey("r"))
		m = updated.(PresetPickerModel)
		if m.generatedPassword != firstPW {
			changed = true
			break
		}
	}
	if !changed {
		t.Error("refresh did not change the password after 10 attempts")
	}
	if m.mode != pickerModePreview {
		t.Error("should still be in preview mode after refresh")
	}
}

func TestPresetPickerPreviewToggleVisibility(t *testing.T) {
	cfg := config.Default()
	m := NewPresetPickerModel(cfg, "")
	m.cursor = 0

	// Enter → preview (visible by default)
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(PresetPickerModel)
	if !m.passwordVisible {
		t.Fatal("password should be visible initially")
	}

	// Press 'p' to hide
	updated, _ = m.Update(textKey("p"))
	m = updated.(PresetPickerModel)
	if m.passwordVisible {
		t.Error("password should be hidden after p")
	}

	// Press 'p' again to show
	updated, _ = m.Update(textKey("p"))
	m = updated.(PresetPickerModel)
	if !m.passwordVisible {
		t.Error("password should be visible after second p")
	}
}

func TestPresetPickerPreviewEscBackToList(t *testing.T) {
	cfg := config.Default()
	m := NewPresetPickerModel(cfg, "")
	m.cursor = 0

	// Enter → preview
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(PresetPickerModel)

	// Esc → back to list
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = updated.(PresetPickerModel)
	if m.mode != pickerModeList {
		t.Errorf("expected list mode, got %d", m.mode)
	}
}

func TestPresetPickerPreviewEscBackToCustom(t *testing.T) {
	cfg := config.Default()
	m := NewPresetPickerModel(cfg, "")
	m.mode = pickerModeCustom
	m.lengthInput.SetValue("12")
	m.toggles = [4]bool{false, true, false, false} // lowercase only

	// Enter in custom → preview
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(PresetPickerModel)
	if m.mode != pickerModePreview {
		t.Fatalf("expected preview mode, got %d", m.mode)
	}
	if m.previewFrom != pickerModeCustom {
		t.Errorf("previewFrom = %d, want %d", m.previewFrom, pickerModeCustom)
	}

	// Esc → back to custom
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = updated.(PresetPickerModel)
	if m.mode != pickerModeCustom {
		t.Errorf("expected custom mode, got %d", m.mode)
	}
}

func TestPresetPickerSelectCustomOpensForm(t *testing.T) {
	cfg := config.Default()
	m := NewPresetPickerModel(cfg, "")
	for i, p := range m.presets {
		if p.name == "Custom" {
			m.cursor = i
			break
		}
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(PresetPickerModel)
	if m.mode != pickerModeCustom {
		t.Error("selecting Custom should switch to custom mode")
	}
}

func TestPresetPickerCustomGeneratePreview(t *testing.T) {
	cfg := config.Default()
	m := NewPresetPickerModel(cfg, "")
	m.mode = pickerModeCustom
	m.lengthInput.SetValue("12")
	m.toggles = [4]bool{false, true, false, false} // lowercase only

	// Enter in custom → preview
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(PresetPickerModel)
	if m.mode != pickerModePreview {
		t.Fatalf("expected preview mode, got %d", m.mode)
	}
	if len(m.generatedPassword) != 12 {
		t.Errorf("custom password length = %d, want 12", len(m.generatedPassword))
	}
	if m.lastPresetName != "Custom" {
		t.Errorf("lastPresetName = %q, want %q", m.lastPresetName, "Custom")
	}

	// Enter in preview → accept
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command from Enter in preview")
	}
	msg := cmd()
	gen, ok := msg.(GeneratedPasswordMsg)
	if !ok {
		t.Fatalf("expected GeneratedPasswordMsg, got %T", msg)
	}
	if len(gen.Password) != 12 {
		t.Errorf("accepted password length = %d, want 12", len(gen.Password))
	}
}

func TestPresetPickerCustomNoCharsetError(t *testing.T) {
	cfg := config.Default()
	m := NewPresetPickerModel(cfg, "")
	m.mode = pickerModeCustom
	m.lengthInput.SetValue("12")
	m.toggles = [4]bool{false, false, false, false} // all off

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(PresetPickerModel)
	if m.err == nil {
		t.Error("expected error when all character classes disabled")
	}
	if m.mode != pickerModeCustom {
		t.Error("should stay in custom mode on error")
	}
}

func TestPresetPickerEscCancels(t *testing.T) {
	cfg := config.Default()
	m := NewPresetPickerModel(cfg, "")

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected command from Esc")
	}
	msg := cmd()
	if _, ok := msg.(CancelPresetMsg); !ok {
		t.Errorf("expected CancelPresetMsg, got %T", msg)
	}
}

func TestPresetPickerCustomEscReturnsToList(t *testing.T) {
	cfg := config.Default()
	m := NewPresetPickerModel(cfg, "")
	m.mode = pickerModeCustom

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = updated.(PresetPickerModel)
	if m.mode != pickerModeList {
		t.Error("Esc in custom mode should return to list mode")
	}
}

func TestPresetPickerViewPreviewMasked(t *testing.T) {
	cfg := config.Default()
	m := NewPresetPickerModel(cfg, "")
	m.mode = pickerModePreview
	m.generatedPassword = "secret123"
	m.lastPresetName = "Strong"
	m.passwordVisible = false

	view := m.View().Content
	if strings.Contains(view, "secret123") {
		t.Error("masked view should not contain the actual password")
	}
	if !strings.Contains(view, maskedValue) {
		t.Error("masked view should contain the mask")
	}
	if strings.Contains(view, strings.Repeat("*", len("secret123"))) {
		t.Error("mask must be constant-width, not length-proportional")
	}
}

func TestPresetPickerViewPreviewVisible(t *testing.T) {
	cfg := config.Default()
	m := NewPresetPickerModel(cfg, "")
	m.mode = pickerModePreview
	m.generatedPassword = "secret123"
	m.lastPresetName = "Strong"
	m.passwordVisible = true

	view := m.View().Content
	if !strings.Contains(view, "secret123") {
		t.Error("visible view should contain the actual password")
	}
}

func TestDescribePassword(t *testing.T) {
	tests := []struct {
		pw   string
		want []string
	}{
		{"abc", []string{"3 chars", "a-z"}},
		{"ABC123", []string{"6 chars", "A-Z", "0-9"}},
		{"Hello!1", []string{"7 chars", "A-Z", "a-z", "0-9", "!@#"}},
	}
	for _, tt := range tests {
		desc := describePassword(tt.pw)
		for _, w := range tt.want {
			if !strings.Contains(desc, w) {
				t.Errorf("describePassword(%q) = %q, missing %q", tt.pw, desc, w)
			}
		}
	}
}

func TestGenerateLikeCurrent(t *testing.T) {
	pw, err := generateLikeCurrent("Hello123!")
	if err != nil {
		t.Fatalf("generateLikeCurrent failed: %v", err)
	}
	if len(pw) != 9 {
		t.Errorf("generated password length = %d, want 9", len(pw))
	}
}

func TestGenerateLikeCurrentEmpty(t *testing.T) {
	_, err := generateLikeCurrent("")
	if err == nil {
		t.Error("expected error for empty password")
	}
}

func TestPresetNames(t *testing.T) {
	expected := map[string]bool{
		"PIN": true, "Alphanumeric": true, "Strong": true,
		"Passphrase": true, "Custom": true,
	}
	for _, name := range PresetNames {
		if !expected[name] {
			t.Errorf("unexpected preset name: %q", name)
		}
	}
	if len(PresetNames) != len(expected) {
		t.Errorf("PresetNames length = %d, want %d", len(PresetNames), len(expected))
	}
}

// A default preset named in another case, or not at all a preset, selects
// Strong, never the list's first entry (PIN).
func TestPresetPickerFallsBackToStrong(t *testing.T) {
	for _, name := range []string{"strong", "bogus"} {
		cfg := config.Default()
		cfg.DefaultPreset = name
		m := NewPresetPickerModel(cfg, "")
		if got := m.presets[m.cursor].name; got != "Strong" {
			t.Errorf("default_preset %q selects %q, want Strong", name, got)
		}
	}
}

// The settings editors check the generator settings' limits.
func TestGeneratorSettingsValidation(t *testing.T) {
	m := NewSettingsModel(config.Default())
	m.inputs[settingPINLength].SetValue("17")
	m.inputs[settingPassphraseWords].SetValue("3")
	m.inputs[settingPassphraseSeparator].SetValue(" ")
	_, errs := m.draftConfig()
	for _, i := range []int{settingPINLength, settingPassphraseWords, settingPassphraseSeparator} {
		if errs[i] == "" {
			t.Errorf("%s accepted an invalid value", settingLabels[i])
		}
	}
	m.inputs[settingPINLength].SetValue("8")
	m.inputs[settingPassphraseWords].SetValue("10")
	m.inputs[settingPassphraseSeparator].SetValue("_")
	cfg, errs := m.draftConfig()
	if len(errs) != 0 || cfg.PINLen() != 8 || cfg.PassphraseWordCount() != 10 || cfg.PassphraseSep() != "_" {
		t.Errorf("draft = %d, %d, %q; errors %v", cfg.PINLen(), cfg.PassphraseWordCount(), cfg.PassphraseSep(), errs)
	}

	m.setDatabase(config.Overrides{}, false)
	m.db.values[settingPINLength].SetValue("99")
	m.db.values[settingPassphraseSeparator].SetValue(".")
	o, errs := m.draftOverrides(cfg)
	if errs[settingPINLength] == "" || o.PINLength != nil || o.PassphraseSeparator == nil || *o.PassphraseSeparator != "." {
		t.Errorf("database draft = %+v, errors %v", o, errs)
	}
}
