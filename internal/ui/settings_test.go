package ui

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rmarchei/keetui/internal/config"
)

func isolateSettingsConfig(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	// An existing XDG config prevents the legacy macOS fallback from selecting
	// the user's real config, without changing their HOME environment variable.
	dir := filepath.Join(tmp, "keetui")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestSettingsSavePreservesKeyBindings(t *testing.T) {
	isolateSettingsConfig(t)

	cfg := config.Default()
	kb := config.KeyBindings{Delete: "#"}
	cfg.KeyBindingsPtr = &kb

	m := NewSettingsModel(cfg)
	updated, cmd := m.save()
	sm := updated.(SettingsModel)
	if sm.isError {
		t.Fatalf("save failed: %s", sm.status)
	}
	if cmd == nil {
		t.Fatal("expected a ConfigSavedMsg command")
	}
	msg, ok := cmd().(ConfigSavedMsg)
	if !ok {
		t.Fatalf("expected ConfigSavedMsg, got %T", cmd())
	}
	if msg.Cfg.KeyBindingsPtr == nil || msg.Cfg.KeyBindingsPtr.Delete != "#" {
		t.Error("keybindings must survive saving settings in-session")
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.KeyBindingsPtr == nil || loaded.KeyBindingsPtr.Delete != "#" {
		t.Error("keybindings were erased from the config file by saving settings")
	}
}

func TestSettingsNavigation(t *testing.T) {
	m := NewSettingsModel(config.Default())
	m.width, m.height = 80, 24

	if m.focused != 0 {
		t.Errorf("initial focus = %d, want 0", m.focused)
	}

	// Tab moves forward
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(SettingsModel)
	if m.focused != 1 {
		t.Errorf("focus after tab = %d, want 1", m.focused)
	}

	// Shift+Tab moves back
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	m = updated.(SettingsModel)
	if m.focused != 0 {
		t.Errorf("focus after shift+tab = %d, want 0", m.focused)
	}

	// Shift+Tab wraps around to last field
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	m = updated.(SettingsModel)
	if m.focused != settingCount-1 {
		t.Errorf("focus after wrap = %d, want %d", m.focused, settingCount-1)
	}

	// Tab wraps around to first field
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(SettingsModel)
	if m.focused != 0 {
		t.Errorf("focus after forward wrap = %d, want 0", m.focused)
	}
}

func TestSettingsConfirmQuitDefaultAndPersistence(t *testing.T) {
	isolateSettingsConfig(t)
	m := NewSettingsModel(config.Default())
	if got := m.inputs[settingConfirmQuit].Value(); got != "1" {
		t.Fatalf("Confirm on quit defaults to %q, want 1", got)
	}
	for _, value := range []string{"0", "1"} {
		m.inputs[settingConfirmQuit].SetValue(value)
		updated, cmd := m.save()
		m = updated.(SettingsModel)
		if m.isError || cmd == nil {
			t.Fatalf("saving Confirm on quit=%s failed: %s", value, m.status)
		}
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.ConfirmOnQuit() != (value == "1") {
			t.Fatalf("Confirm on quit=%s did not persist", value)
		}
		m = NewSettingsModel(cfg)
		if got := m.inputs[settingConfirmQuit].Value(); got != value {
			t.Fatalf("reopened setting = %q, want %q", got, value)
		}
	}
}

func TestSettingsInitialValues(t *testing.T) {
	cfg := config.Default()
	m := NewSettingsModel(cfg)
	m.width, m.height = 80, 24

	if v := m.inputs[settingClipboardClear].Value(); v != "15" {
		t.Errorf("clipboard clear = %q, want %q", v, "15")
	}
	if v := m.inputs[settingAutoLock].Value(); v != "5" {
		t.Errorf("auto lock = %q, want %q", v, "5")
	}
	if v := m.inputs[settingPasswordLength].Value(); v != "20" {
		t.Errorf("password length = %q, want %q", v, "20")
	}
	if v := m.inputs[settingPasswordStrength].Value(); v != "1" {
		t.Errorf("password strength = %q, want %q", v, "1")
	}
	if v := m.inputs[settingTheme].Value(); v != "dark" {
		t.Errorf("theme = %q, want %q", v, "dark")
	}
}

func TestSettingsValidationClipboard(t *testing.T) {
	isolateSettingsConfig(t)
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"valid 0", "0", false},
		{"valid 15", "15", false},
		{"valid 30", "30", false},
		{"too low", "5", true},
		{"negative", "-1", true},
		{"non-numeric", "abc", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewSettingsModel(config.Default())
			m.width, m.height = 80, 24
			m.inputs[settingClipboardClear].SetValue(tt.value)
			updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = updated.(SettingsModel)
			if tt.wantErr {
				if !m.isError {
					t.Error("expected validation error")
				}
				if cmd != nil {
					t.Error("should not produce command on validation error")
				}
			} else {
				if m.isError {
					t.Errorf("unexpected error: %s", m.status)
				}
			}
		})
	}
}

func TestSettingsValidationAutoLock(t *testing.T) {
	isolateSettingsConfig(t)
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"valid 0", "0", false},
		{"valid 5", "5", false},
		{"valid 10", "10", false},
		{"too low", "3", true},
		{"negative", "-1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewSettingsModel(config.Default())
			m.width, m.height = 80, 24
			m.inputs[settingAutoLock].SetValue(tt.value)
			updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = updated.(SettingsModel)
			if tt.wantErr {
				if !m.isError {
					t.Error("expected validation error")
				}
				if cmd != nil {
					t.Error("should not produce command on validation error")
				}
			} else {
				if m.isError {
					t.Errorf("unexpected error: %s", m.status)
				}
			}
		})
	}
}

func TestSettingsValidationPasswordLength(t *testing.T) {
	isolateSettingsConfig(t)
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"valid 8", "8", false},
		{"valid 32", "32", false},
		{"too short", "4", true},
		{"non-numeric", "abc", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewSettingsModel(config.Default())
			m.width, m.height = 80, 24
			m.inputs[settingPasswordLength].SetValue(tt.value)
			updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = updated.(SettingsModel)
			if tt.wantErr {
				if !m.isError {
					t.Error("expected validation error")
				}
				if cmd != nil {
					t.Error("should not produce command on validation error")
				}
			} else {
				if m.isError {
					t.Errorf("unexpected error: %s", m.status)
				}
			}
		})
	}
}

func TestSettingsValidationBoolFields(t *testing.T) {
	boolFields := []struct {
		idx  int
		name string
	}{
		{settingPasswordStrength, "password strength"},
		{settingPWUppercase, "uppercase"},
		{settingPWLowercase, "lowercase"},
		{settingPWDigits, "digits"},
		{settingPWSymbols, "symbols"},
	}
	for _, bf := range boolFields {
		t.Run(bf.name+" invalid", func(t *testing.T) {
			m := NewSettingsModel(config.Default())
			m.width, m.height = 80, 24
			m.inputs[bf.idx].SetValue("2")
			updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = updated.(SettingsModel)
			if !m.isError {
				t.Errorf("expected error for %s=2", bf.name)
			}
			if cmd != nil {
				t.Error("should not produce command on error")
			}
		})
	}
}

func TestSettingsValidationTheme(t *testing.T) {
	isolateSettingsConfig(t)
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"dark", "dark", false},
		{"light", "light", false},
		{"high-contrast", "high-contrast", false},
		{"invalid", "neon", true},
		{"empty", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewSettingsModel(config.Default())
			m.width, m.height = 80, 24
			m.inputs[settingTheme].SetValue(tt.value)
			updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = updated.(SettingsModel)
			if tt.wantErr {
				if !m.isError {
					t.Error("expected validation error")
				}
				if cmd != nil {
					t.Error("should not produce command on error")
				}
			} else {
				if m.isError {
					t.Errorf("unexpected error: %s", m.status)
				}
			}
		})
	}
}

func TestSettingsValidationAllCharsetsDisabled(t *testing.T) {
	m := NewSettingsModel(config.Default())
	m.width, m.height = 80, 24
	m.inputs[settingPWUppercase].SetValue("0")
	m.inputs[settingPWLowercase].SetValue("0")
	m.inputs[settingPWDigits].SetValue("0")
	m.inputs[settingPWSymbols].SetValue("0")

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(SettingsModel)
	if !m.isError {
		t.Error("expected error when all charsets disabled")
	}
	if cmd != nil {
		t.Error("should not produce command on error")
	}
}

func TestSettingsCancel(t *testing.T) {
	m := NewSettingsModel(config.Default())
	m.width, m.height = 80, 24

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(BackToBrowserMsg); !ok {
		t.Errorf("expected BackToBrowserMsg, got %T", msg)
	}
}

func TestSettingsView(t *testing.T) {
	m := NewSettingsModel(config.Default())
	m.width, m.height = 80, 24

	view := m.View().Content
	if view == "" {
		t.Error("View() should return non-empty string")
	}
}
