package ui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rmarchei/keetui/internal/config"
)

var ctrlO = tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl}

// feed sends msg and then the message its command produces at once, as the
// program loop would.
func feed(t *testing.T, m AppModel, msg tea.Msg) AppModel {
	t.Helper()
	updated, cmd := m.Update(msg)
	m = updated.(AppModel)
	for _, next := range quickMsgs(cmd) {
		switch next.(type) {
		case ShowSettingsMsg, BackToBrowserMsg, ConfigSavedMsg:
			updated, _ = m.Update(next)
			m = updated.(AppModel)
		}
	}
	return m
}

func lockedApp(t *testing.T) AppModel {
	t.Helper()
	m := NewApp(filepath.Join(t.TempDir(), "synthetic.kdbx"), "", config.Default())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return updated.(AppModel)
}

func TestCtrlOOpensSettingsFromTheUnlockScreen(t *testing.T) {
	m := lockedApp(t)
	for _, r := range "synthetic" {
		updated, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = updated.(AppModel)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "ctrl+o settings") {
		t.Error("the unlock footer does not offer the settings")
	}

	m = feed(t, m, ctrlO)
	if m.state != viewSettings {
		t.Fatalf("state = %d, want the settings", m.state)
	}
	if m.unlock.input.Value() != "" {
		t.Error("the password typed before opening the settings was kept")
	}
	if m.settings.db.open || m.settings.db.summary() != "(no database open)" {
		t.Error("the database settings are offered with no database open")
	}
	view := ansi.Strip(m.View().Content)
	assertFrame(t, m.View().Content, m.width, m.height)
	if strings.Contains(view, "0 entries") {
		t.Error("the status bar counts entries with no database open")
	}

	m = feed(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.state != viewUnlock || m.db != nil {
		t.Fatalf("esc went to state %d, want the unlock screen", m.state)
	}
}

func TestSettingsSavedFromTheUnlockScreenApplyThere(t *testing.T) {
	t.Cleanup(func() { ApplyTheme(GetTheme("dark")) })
	m := feed(t, lockedApp(t), ctrlO)
	m.settings.inputs[settingTheme].SetValue("light")
	m = feed(t, m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if m.state != viewUnlock {
		t.Fatalf("saving went to state %d, want the unlock screen", m.state)
	}
	if m.globalCfg.Theme() != "light" || primaryColor != lipgloss.Color(GetTheme("light").Primary) {
		t.Error("the saved theme is not in effect on the unlock screen")
	}
	if saved, err := config.Load(); err != nil || saved.Theme() != "light" {
		t.Errorf("config file theme = %q, %v", saved.Theme(), err)
	}
}

func TestCtrlOWaitsWhileUnlocking(t *testing.T) {
	m := lockedApp(t)
	m.unlock.unlocking = true
	updated, cmd := m.Update(ctrlO)
	if updated.(AppModel).state != viewUnlock || cmd != nil {
		t.Error("ctrl+o acted during key derivation")
	}
}

func TestCtrlOOpensSettingsFromTheBrowser(t *testing.T) {
	m := createTestApp(t)
	m = feed(t, m, ctrlO)
	if m.state != viewSettings || !m.settings.db.open {
		t.Errorf("state = %d, database settings open %v; want the settings with the database's", m.state, m.settings.db.open)
	}
}

// Esc in settings opened from the unlock screen goes back to it; only a
// second esc, on the unlock screen, quits.
func TestEscFromUnlockSettingsReturnsBeforeQuitting(t *testing.T) {
	esc := tea.KeyPressMsg{Code: tea.KeyEscape}
	quits := func(cmd tea.Cmd) bool {
		for _, msg := range quickMsgs(cmd) {
			if _, ok := msg.(tea.QuitMsg); ok {
				return true
			}
		}
		return false
	}
	m := feed(t, lockedApp(t), ctrlO)
	updated, cmd := m.Update(esc)
	m = updated.(AppModel)
	for _, msg := range quickMsgs(cmd) {
		if _, ok := msg.(tea.QuitMsg); ok {
			t.Fatal("esc in the settings quit keetui")
		}
		if _, ok := msg.(BackToBrowserMsg); ok {
			updated, cmd = m.Update(msg)
			m = updated.(AppModel)
			if quits(cmd) {
				t.Fatal("leaving the settings quit keetui")
			}
		}
	}
	if m.state != viewUnlock {
		t.Fatalf("esc went to state %d, want the unlock screen", m.state)
	}
	if _, cmd = m.Update(esc); !quits(cmd) {
		t.Error("esc on the unlock screen did not quit")
	}
}
