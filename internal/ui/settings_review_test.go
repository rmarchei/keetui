package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/keepass"
)

func TestSettingsSaveWritesOnlyBindingOverrides(t *testing.T) {
	isolateSettingsConfig(t)
	for _, overrides := range []*config.KeyBindings{nil, {Delete: "#"}, {HIBPCheck: "5", CopyUsername: "b"}} {
		cfg := config.Default()
		cfg.KeyBindingsPtr = overrides
		m := NewSettingsModel(cfg)
		_, cmd := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
		if cmd == nil {
			t.Fatal("save failed")
		}
		data, err := os.ReadFile(config.Path())
		if err != nil {
			t.Fatal(err)
		}
		var disk config.Config
		if err := json.Unmarshal(data, &disk); err != nil {
			t.Fatal(err)
		}
		if overrides == nil {
			if disk.KeyBindingsPtr != nil || strings.Contains(string(data), "keybindings") {
				t.Fatal("pinned default shortcuts")
			}
		} else if disk.KeyBindingsPtr == nil || *disk.KeyBindingsPtr != *overrides {
			t.Fatalf("serialized merged defaults: %+v", disk.KeyBindingsPtr)
		}
	}
}

func TestSettingsSaveErrorSurvivesNavigationAndEditing(t *testing.T) {
	isolateSettingsConfig(t)
	path := config.Path()
	// Use a directory as a deterministic I/O failure, even when running as root.
	backup := filepath.Join(filepath.Dir(path), "original.json")
	if err := os.Rename(path, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	m := NewSettingsModel(config.Default())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 23})
	updated, cmd := updated.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	m = updated.(SettingsModel)
	if cmd != nil || m.saveError == "" {
		t.Fatal("missing save error")
	}
	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyTab}, {Code: tea.KeyDown}, textKey("7")} {
		updated, _ = m.Update(key)
		m = updated.(SettingsModel)
		if !strings.Contains(ansi.Strip(m.View().Content), "Settings not saved") {
			t.Fatal("save error disappeared")
		}
	}
	m.focused = settingDefaultPreset
	m.picking = true
	if !strings.Contains(ansi.Strip(m.View().Content), "Settings not saved") {
		t.Fatal("picker hid save failure")
	}
	m.picking, m.editingKeys = false, true
	if !strings.Contains(ansi.Strip(m.View().Content), "Settings not saved") {
		t.Fatal("key editor hid save failure")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	} // empty test directory only
	if err := os.Rename(backup, path); err != nil {
		t.Fatal(err)
	}
	updated, cmd = m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	m = updated.(SettingsModel)
	if cmd == nil || m.saveError != "" {
		t.Fatalf("successful retry did not clear error: %s", m.saveError)
	}
}

func TestBackspaceResetsWhileRecording(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyBackspace}, {Code: tea.KeyDelete}} {
		m := NewSettingsModel(config.Default())
		m.bindings.Delete = "#"
		for i, binding := range m.bindings.Bindings() {
			if binding.ID == "delete" {
				m.bindingIndex = i
			}
		}
		m.editingKeys, m.recording = true, true
		updated, _ := m.Update(key)
		m = updated.(SettingsModel)
		if m.recording || m.bindingError != "" || m.bindings.Delete != config.DefaultKeyBindings().Delete {
			t.Fatalf("reset key was recorded as a shortcut: %+v", m.bindings)
		}
	}
}

func TestRecordingUsableSpecialKeys(t *testing.T) {
	for _, key := range recordableNavigationKeys() {
		m := NewSettingsModel(config.Default())
		for i, binding := range m.bindings.Bindings() {
			if binding.ID == "show_settings" {
				m.bindingIndex = i
			}
		}
		m.editingKeys, m.recording = true, true
		updated, _ := m.Update(key)
		m = updated.(SettingsModel)
		if m.recording || m.bindingError != "" {
			t.Fatalf("usable %s rejected: %s", key.String(), m.bindingError)
		}
		browser := BrowserModel{keys: m.bindings}
		_, cmd := browser.Update(key)
		if cmd == nil {
			t.Fatalf("%s cannot invoke its action", key.String())
		}
		if _, ok := cmd().(ShowSettingsMsg); !ok {
			t.Fatalf("%s is shadowed", key.String())
		}
	}
}

func recordableNavigationKeys() []tea.KeyPressMsg {
	return []tea.KeyPressMsg{
		{Code: tea.KeyTab}, {Code: tea.KeySpace}, {Code: tea.KeyPgUp}, {Code: tea.KeyHome}, {Code: tea.KeyInsert},
		{Code: tea.KeyF2}, {Code: tea.KeyF3}, {Code: tea.KeyF4}, {Code: tea.KeyF5}, {Code: tea.KeyF6},
		{Code: tea.KeyF7}, {Code: tea.KeyF8}, {Code: tea.KeyF9}, {Code: tea.KeyF10}, {Code: tea.KeyF11}, {Code: tea.KeyF12},
	}
}

func TestBrowserBindingsNeverDispatchInsideSettings(t *testing.T) {
	for _, key := range recordableNavigationKeys() {
		t.Run(key.String(), func(t *testing.T) {
			m := createTestApp(t)
			m.cfg.KeyBindingsPtr = &config.KeyBindings{Delete: key.String()}
			m.browser.keys = m.cfg.Keys()
			for i, item := range m.browser.items {
				if !item.isGroup {
					m.browser.cursor = i
					break
				}
			}
			_, cmd := m.Update(key)
			if cmd == nil {
				t.Fatal("browser did not recognize its shortcut")
			}
			if _, ok := cmd().(DeleteEntryMsg); !ok {
				t.Fatal("browser shortcut did not request deletion")
			}
			for _, mode := range []string{"form", "picker", "bindings"} {
				m.state = viewSettings
				m.settings = NewSettingsModel(m.cfg)
				m.settings.focused = settingPasswordStrength
				switch mode {
				case "picker":
					m.settings.focused, m.settings.picking = settingDefaultPreset, true
				case "bindings":
					m.settings.editingKeys = true
				}
				updated, cmd := m.Update(key)
				if cmd != nil {
					// Deliver the immediate message (e.g. blink initialization),
					// but do not launch the resulting background timer chain.
					updated, _ = updated.Update(cmd())
				}
				m = updated.(AppModel)
				if m.state != viewSettings || m.dirty || m.db.EntryCount() != 2 {
					t.Fatalf("browser binding escaped %s settings", mode)
				}
				if key.Code == tea.KeyTab {
					switch mode {
					case "form":
						if m.settings.focused != settingPasswordStrength+1 {
							t.Fatal("tab no longer moves focus")
						}
					case "picker":
						if m.settings.choice != 1 {
							t.Fatal("tab no longer moves the picker")
						}
					case "bindings":
						if m.settings.bindingIndex != 1 {
							t.Fatal("tab no longer moves through bindings")
						}
					}
				}
				if key.Code == tea.KeySpace && mode == "form" && m.settings.inputs[settingPasswordStrength].Value() != "0" {
					t.Fatal("space no longer toggles settings")
				}
			}
		})
	}
}

func TestConfigSavedRefreshesBrowserKeysWithoutResettingNavigation(t *testing.T) {
	m := createTestApp(t)
	m.browser.cursor = 1
	m.browser.searchInput.SetValue("draft search")
	cfg := m.cfg
	cfg.KeyBindingsPtr = &config.KeyBindings{ShowStats: "Z"}
	updated, _ := m.Update(ConfigSavedMsg{Cfg: cfg})
	m = updated.(AppModel)
	if m.browser.cursor != 1 || m.browser.searchInput.Value() != "draft search" {
		t.Fatal("browser was reconstructed")
	}
	_, cmd := m.Update(textKey("Z"))
	if cmd == nil {
		t.Fatal("new shortcut did not take effect")
	}
	if _, ok := cmd().(ShowStatsMsg); !ok {
		t.Fatal("new shortcut invoked wrong action")
	}
	_, cmd = m.Update(textKey("i"))
	if cmd != nil {
		t.Fatal("retired shortcut is still active")
	}
}

func TestSettingsViewDoesNotRevalidateDraft(t *testing.T) {
	m := NewSettingsModel(config.Default())
	m.fieldErrors[settingClipboardClear] = "cached validation sentinel"
	if !strings.Contains(ansi.Strip(m.View().Content), "cached validation sentinel") {
		t.Fatal("View revalidated a throwaway copy")
	}
}

func TestAttachmentInputPromptKeepsTrailingSpace(t *testing.T) {
	m := NewDetailModel(keepass.Entry{}, config.Default())
	m.attachPathInput.SetValue("/tmp/synthetic.txt")
	if got := ansi.Strip(m.attachPathInput.View()); !strings.Contains(got, "File path: /tmp/synthetic.txt") {
		t.Fatalf("attachment path prompt clipped: %q", got)
	}
}

func TestSettingsSaveErrorDetailsRemainAvailableInHelp(t *testing.T) {
	m := createTestApp(t)
	m.state = viewSettings
	m.settings = NewSettingsModel(m.cfg)
	m.settings.saveError = "Settings not saved: write config: permission denied"
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyF1})
	m = updated.(AppModel)
	if m.help.section != helpMessage || m.help.message != m.settings.saveError {
		t.Fatal("help did not preserve full I/O error details")
	}
	updated, _ = m.Update(CloseHelpMsg{})
	if !strings.Contains(ansi.Strip(updated.View().Content), "Settings not saved") {
		t.Fatal("closing help dismissed the save error")
	}
}
