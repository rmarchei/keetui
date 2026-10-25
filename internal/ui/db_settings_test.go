package ui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/keepass"
)

// openWithSettings unlocks a synthetic database that stores settings.
func openWithSettings(t *testing.T, settings string) (AppModel, tea.Cmd) {
	t.Helper()
	isolateSettingsConfig(t)
	path := filepath.Join(t.TempDir(), "synthetic.kdbx")
	db, err := keepass.Create(path, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	db.SetCustomData(config.OverridesKey, settings)
	m := NewApp(path, "", config.Default())
	m.clipboard = &testClipboard{}
	updated, cmd := m.Update(databaseOpenResultMsg{db: db})
	return updated.(AppModel), cmd
}

// quickMsgs runs cmd and the commands it batches, keeping the messages that
// arrive at once; timers are left running.
func quickMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		if batch, ok := msg.(tea.BatchMsg); ok {
			var msgs []tea.Msg
			for _, c := range batch {
				msgs = append(msgs, quickMsgs(c)...)
			}
			return msgs
		}
		return []tea.Msg{msg}
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}

func quickStatus(cmd tea.Cmd) string {
	for _, msg := range quickMsgs(cmd) {
		if status, ok := msg.(StatusMsg); ok {
			return status.Text
		}
	}
	return ""
}

func TestDatabaseSettingsApplyWhileItIsOpen(t *testing.T) {
	m, _ := openWithSettings(t, `{"auto_lock_minutes":10,"clipboard_clear_seconds":30,"backup_on_save":true}`)
	if m.cfg.AutoLockMinutes != 10 || m.cfg.ClipboardClearSeconds != 30 || !m.db.BackupOnSave {
		t.Fatalf("database settings not in effect: %+v, backup %v", m.cfg, m.db.BackupOnSave)
	}
	if m.globalCfg.AutoLockMinutes != 5 || m.globalCfg.ClipboardClearSeconds != 15 {
		t.Fatal("database settings leaked into the global ones")
	}

	m.lastActivity = time.Now().Add(-time.Hour)
	updated, _ := m.Update(autoLockMsg{gen: m.autoLockGen})
	m = updated.(AppModel)
	if m.state != viewUnlock {
		t.Fatal("the database did not lock")
	}
	if m.cfg.AutoLockMinutes != 5 || m.cfg.ClipboardClearSeconds != 15 || !m.dbOverrides.IsZero() {
		t.Errorf("locking kept the database's settings: %+v", m.cfg)
	}
}

func TestUnreadableDatabaseSettingsFallBackToGlobal(t *testing.T) {
	m, cmd := openWithSettings(t, `{"auto_lock_minutes":`)
	if m.cfg.AutoLockMinutes != 5 || !m.dbOverrides.IsZero() {
		t.Errorf("unreadable settings changed the configuration: %+v", m.cfg)
	}
	if status := quickStatus(cmd); !strings.Contains(status, "settings are unreadable") {
		t.Errorf("status = %q, want a warning", status)
	}
}

// databaseEditor opens the settings and their "This database" editor.
func databaseEditor(t *testing.T, o config.Overrides, readOnly bool) SettingsModel {
	t.Helper()
	isolateSettingsConfig(t)
	m := NewSettingsModel(config.Default())
	m.setDatabase(o, readOnly)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = updated.(SettingsModel)
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	m = updated.(SettingsModel)
	if m.focused != settingDatabase {
		t.Fatalf("focused = %d, want the database row", m.focused)
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(SettingsModel)
	if !m.db.editing {
		t.Fatal("enter did not open the database editor")
	}
	return m
}

func typeInto(m SettingsModel, text string) SettingsModel {
	for _, r := range text {
		updated, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = updated.(SettingsModel)
	}
	return m
}

func TestSettingsSaveTheDatabaseSettingsSeparately(t *testing.T) {
	m := databaseEditor(t, config.Overrides{}, false)
	m = typeInto(m, "30")                                     // clipboard clear
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab}) // terminal clipboard
	m = updated.(SettingsModel)
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	m = updated.(SettingsModel)
	if got := m.db.values[settingClipboardOSC52].Value(); got != config.OSC52Modes[0] {
		t.Fatalf("space cycled terminal clipboard to %q, want %q", got, config.OSC52Modes[0])
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m = updated.(SettingsModel)
	if m.db.values[settingClipboardOSC52].Value() != "" {
		t.Fatal("backspace did not reset to inherit")
	}

	_, cmd := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+s did not save")
	}
	saved, ok := cmd().(ConfigSavedMsg)
	if !ok || saved.Overrides == nil {
		t.Fatalf("save sent %#v, want database settings", saved)
	}
	if got := saved.Overrides; got.ClipboardClearSeconds == nil || *got.ClipboardClearSeconds != 30 || got.ClipboardOSC52 != nil {
		t.Errorf("database settings = %s", got.Encode())
	}
	if global, err := config.Load(); err != nil || global.ClipboardClearSeconds != 15 {
		t.Errorf("the config file got the database's setting: %+v, %v", global, err)
	}

	// The global editor notes what the database sets.
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(SettingsModel)
	if body, _ := m.body(); !strings.Contains(ansi.Strip(body), "This database: 30") {
		t.Error("the global editor does not show the database's clipboard setting")
	}
}

func TestDatabaseSettingsAreValidated(t *testing.T) {
	m := databaseEditor(t, config.Overrides{}, false)
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(SettingsModel)
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab}) // auto-lock
	m = updated.(SettingsModel)
	m = typeInto(m, "2")
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	m = updated.(SettingsModel)
	if cmd != nil {
		t.Fatal("an invalid database setting was saved")
	}
	if !m.db.editing || databaseSettingList[m.db.focus] != settingAutoLock || !m.isError {
		t.Errorf("the problem is not shown at the setting: editing %v, focus %d, status %q", m.db.editing, m.db.focus, m.status)
	}
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Must be at least 5") {
		t.Errorf("the error is not drawn:\n%s", view)
	}
}

func TestReadOnlyDatabaseSettingsCannotChange(t *testing.T) {
	thirty := 30
	m := databaseEditor(t, config.Overrides{ClipboardClearSeconds: &thirty}, true)
	m = typeInto(m, "5")
	if m.db.values[settingClipboardClear].Value() != "30" || !m.isError {
		t.Errorf("a read-only database's setting changed to %q", m.db.values[settingClipboardClear].Value())
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if saved, ok := cmd().(ConfigSavedMsg); !ok || saved.Overrides != nil {
		t.Errorf("saving sent database settings for a read-only database: %#v", saved)
	}
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "read-only") {
		t.Error("the editor does not say the database is read-only")
	}
}

func TestSavedDatabaseSettingsGoIntoTheDatabase(t *testing.T) {
	m := createTestApp(t)
	isolateSettingsConfig(t)
	ten := 10
	updated, _ := m.Update(ConfigSavedMsg{Cfg: config.Default(), Overrides: &config.Overrides{AutoLockMinutes: &ten}})
	m = updated.(AppModel)
	if got := m.db.CustomData(config.OverridesKey); got != `{"auto_lock_minutes":10}` {
		t.Errorf("stored settings = %q", got)
	}
	if !m.dirty || m.cfg.AutoLockMinutes != 10 {
		t.Errorf("dirty %v, auto-lock %d: the change is not pending or not in effect", m.dirty, m.cfg.AutoLockMinutes)
	}

	// A later global save keeps the database's settings on top.
	global := config.Default()
	global.ClipboardClearSeconds = 20
	updated, _ = m.Update(ConfigSavedMsg{Cfg: global})
	m = updated.(AppModel)
	if m.cfg.AutoLockMinutes != 10 || m.cfg.ClipboardClearSeconds != 20 {
		t.Errorf("after a global save: auto-lock %d, clipboard %d", m.cfg.AutoLockMinutes, m.cfg.ClipboardClearSeconds)
	}
}

func TestDefaultPresetChangesWhereItIsSet(t *testing.T) {
	m := createTestApp(t)
	isolateSettingsConfig(t)
	updated, cmd := m.Update(SetDefaultPresetMsg{Name: "Passphrase"})
	m = updated.(AppModel)
	if global, err := config.Load(); err != nil || global.DefaultPreset != "Passphrase" || m.cfg.Preset() != "Passphrase" {
		t.Errorf("global default not saved: file %q, in effect %q", global.DefaultPreset, m.cfg.Preset())
	}
	if m.dirty || quickStatus(cmd) != "Default preset updated" {
		t.Error("a global default touched the database")
	}

	pin := "PIN"
	m.setOverrides(config.Overrides{DefaultPreset: &pin})
	updated, _ = m.Update(SetDefaultPresetMsg{Name: "Strong"})
	m = updated.(AppModel)
	if !strings.Contains(m.db.CustomData(config.OverridesKey), `"default_preset":"Strong"`) || !m.dirty || m.cfg.Preset() != "Strong" {
		t.Errorf("database default not changed: %q, dirty %v", m.db.CustomData(config.OverridesKey), m.dirty)
	}
	if global, _ := config.Load(); global.DefaultPreset != "Passphrase" {
		t.Error("a database default changed the config file")
	}
}
