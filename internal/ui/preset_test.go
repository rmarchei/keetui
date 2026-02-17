package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(PresetPickerModel)
	if m.cursor != initial+1 {
		t.Errorf("after j: cursor=%d, want %d", m.cursor, initial+1)
	}

	// Navigate up
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
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
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
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

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(PresetPickerModel)
	if m.mode != pickerModePreview {
		t.Fatalf("expected preview mode, got %d", m.mode)
	}
	words := strings.Split(m.generatedPassword, "-")
	if len(words) != 6 {
		t.Errorf("passphrase word count = %d, want 6", len(words))
	}
}

func TestPresetPickerPreviewAccept(t *testing.T) {
	cfg := config.Default()
	m := NewPresetPickerModel(cfg, "")
	m.cursor = 0 // PIN

	// Enter → preview
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(PresetPickerModel)

	pw := m.generatedPassword

	// Enter again → accept
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
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
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(PresetPickerModel)
	firstPW := m.generatedPassword

	// Press 'r' to refresh — password should change (with very high probability)
	// Try multiple times to avoid flaky test
	changed := false
	for i := 0; i < 10; i++ {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
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
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(PresetPickerModel)
	if !m.passwordVisible {
		t.Fatal("password should be visible initially")
	}

	// Press 'p' to hide
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = updated.(PresetPickerModel)
	if m.passwordVisible {
		t.Error("password should be hidden after p")
	}

	// Press 'p' again to show
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
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
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(PresetPickerModel)

	// Esc → back to list
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
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
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(PresetPickerModel)
	if m.mode != pickerModePreview {
		t.Fatalf("expected preview mode, got %d", m.mode)
	}
	if m.previewFrom != pickerModeCustom {
		t.Errorf("previewFrom = %d, want %d", m.previewFrom, pickerModeCustom)
	}

	// Esc → back to custom
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
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

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
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
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
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
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
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

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
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

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
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

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
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

	view := m.View()
	if strings.Contains(view, "secret123") {
		t.Error("masked view should not contain the actual password")
	}
	if !strings.Contains(view, "*********") {
		t.Error("masked view should contain asterisks")
	}
}

func TestPresetPickerViewPreviewVisible(t *testing.T) {
	cfg := config.Default()
	m := NewPresetPickerModel(cfg, "")
	m.mode = pickerModePreview
	m.generatedPassword = "secret123"
	m.lastPresetName = "Strong"
	m.passwordVisible = true

	view := m.View()
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
