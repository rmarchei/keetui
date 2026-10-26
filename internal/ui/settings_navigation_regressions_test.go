package ui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rmarchei/keetui/internal/config"
)

func focusedDatabaseSetting(t *testing.T, field int, value string, readOnly bool) SettingsModel {
	t.Helper()
	m := NewSettingsModel(config.Default())
	m.width, m.height = 180, 30
	m.setDatabase(config.Overrides{}, readOnly)
	m.db.editing = true
	m.db.values[databaseSettingList[m.db.focus]].Blur()
	m.db.focus = slices.Index(databaseSettingList, field)
	m.db.values[field].SetValue(value)
	m.db.values[field].Focus()
	m.db.values[field].CursorEnd()
	return m
}

func TestDatabaseSettingInheritHintsPreserveTextEditing(t *testing.T) {
	for _, tc := range []struct {
		field                 int
		value, afterBackspace string
	}{
		{settingAutoLock, "60", "6"},
		{settingPassphraseSeparator, "---", "--"},
	} {
		t.Run(settingLabels[tc.field], func(t *testing.T) {
			m := focusedDatabaseSetting(t, tc.field, tc.value, false)
			footer := ansi.Strip(m.databaseFooter())
			if !strings.Contains(footer, "Empty value inherits") || strings.Contains(footer, "backspace inherit") {
				t.Fatalf("text field reset hint does not describe ordinary editing: %q", footer)
			}
			updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDelete})
			if updated.(SettingsModel).db.values[tc.field].Value() != tc.value {
				t.Fatal("Delete at the end reset the whole override")
			}
			updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
			m = updated.(SettingsModel)
			if m.db.values[tc.field].Value() != tc.afterBackspace {
				t.Fatal("Backspace no longer edits one character")
			}
			for range len(tc.afterBackspace) {
				updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
				m = updated.(SettingsModel)
			}
			global, _ := m.draftConfig()
			overrides, problems := m.draftOverrides(global)
			if len(problems) != 0 || !overrides.IsZero() {
				t.Fatalf("clearing the field did not inherit: overrides=%+v, problems=%v", overrides, problems)
			}
		})
	}
}

func TestDatabaseSettingChoicesAdvertiseWorkingResetKeys(t *testing.T) {
	for _, tc := range []struct {
		field int
		value string
	}{
		{settingClipboardOSC52, config.OSC52Always},
		{settingConfirmQuit, "0"},
	} {
		for _, key := range []tea.KeyPressMsg{{Code: tea.KeyBackspace}, {Code: tea.KeyDelete}} {
			m := focusedDatabaseSetting(t, tc.field, tc.value, false)
			footer := ansi.Strip(m.databaseFooter())
			if !strings.Contains(footer, "backspace inherit") || !strings.Contains(footer, "space/enter change") {
				t.Fatalf("choice reset controls missing: %q", footer)
			}
			updated, _ := m.Update(key)
			if updated.(SettingsModel).db.values[tc.field].Value() != "" {
				t.Fatalf("%s did not reset %s to inherit", key.String(), settingLabels[tc.field])
			}
		}
	}
}

func TestReadOnlyDatabaseSettingFooterShowsAvailableControls(t *testing.T) {
	for _, tc := range []struct {
		field int
		value string
	}{
		{settingAutoLock, "60"},
		{settingPassphraseSeparator, "---"},
		{settingClipboardOSC52, config.OSC52Always},
		{settingConfirmQuit, "0"},
	} {
		t.Run(settingLabels[tc.field], func(t *testing.T) {
			m := focusedDatabaseSetting(t, tc.field, tc.value, true)
			footer := ansi.Strip(m.databaseFooter())
			for _, unavailable := range []string{"space/enter change", "backspace", "inherit"} {
				if strings.Contains(footer, unavailable) {
					t.Fatalf("read-only footer advertises %s: %q", unavailable, footer)
				}
			}
			for _, available := range []string{"esc back", "F1 help", "ctrl+s save global settings", "tab/shift+tab navigate"} {
				if !strings.Contains(footer, available) {
					t.Fatalf("read-only footer omits %s: %q", available, footer)
				}
			}
			for _, key := range []tea.KeyPressMsg{{Code: tea.KeyBackspace}, {Code: tea.KeySpace}} {
				updated, _ := m.Update(key)
				if updated.(SettingsModel).db.values[tc.field].Value() != tc.value {
					t.Fatal("read-only input changed the database override")
				}
			}
			updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
			if updated.(SettingsModel).db.focus != (m.db.focus+1)%len(databaseSettingList) {
				t.Fatal("read-only navigation stopped working")
			}
			if databaseChoices(tc.field) == nil && !strings.Contains(footer, "enter next") {
				t.Fatal("read-only text field omits its Enter navigation")
			}
		})
	}
}

func TestConfirmationShiftTabNavigatesWithoutChoosing(t *testing.T) {
	for _, initial := range []bool{false, true} {
		confirmed := false
		m := NewConfirmModel(viewBrowser, "Synthetic confirmation", func() tea.Msg {
			confirmed = true
			return nil
		})
		m.selected = initial
		updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
		m = updated.(ConfirmModel)
		if m.selected == initial || cmd != nil || confirmed {
			t.Fatal("Shift+Tab did not navigate without choosing a button")
		}
		_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if cmd == nil {
			t.Fatal("Enter did not choose the button selected with Shift+Tab")
		}
		msg := cmd()
		if m.selected {
			if !confirmed {
				t.Fatal("Enter did not confirm the selected Yes button")
			}
		} else if _, ok := msg.(CancelEditMsg); !ok || confirmed {
			t.Fatal("Enter did not cancel with No selected")
		}
	}
}
