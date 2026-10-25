package ui

import (
	"os"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rmarchei/keetui/internal/config"
)

func TestSettingsEnterAdvancesNumericFieldsWithoutSaving(t *testing.T) {
	for _, scope := range []string{"global", "database", "read-only database"} {
		for _, field := range []int{settingClipboardClear, settingAutoLock, settingPasswordLength} {
			t.Run(scope+"/"+settingLabels[field], func(t *testing.T) {
				isolateSettingsConfig(t)
				before, err := os.ReadFile(config.Path())
				if err != nil {
					t.Fatal(err)
				}
				m := NewSettingsModel(config.Default())
				m.width = 160
				m.focused = field
				footer := m.footer()
				if scope != "global" {
					m.setDatabase(config.Overrides{}, scope == "read-only database")
					m.db.editing = true
					m.db.focus = slices.Index(databaseSettingList, field)
					footer = m.databaseFooter()
				}
				if !strings.Contains(ansi.Strip(footer), "enter next") {
					t.Fatal("numeric field does not advertise Enter to advance")
				}
				updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				next := updated.(SettingsModel)
				if scope == "global" && next.focused != field+1 || scope != "global" && next.db.focus != m.db.focus+1 {
					t.Fatal("Enter did not advance to the next setting")
				}
				for _, msg := range quickMsgs(cmd) {
					if _, saved := msg.(ConfigSavedMsg); saved {
						t.Fatal("Enter saved settings")
					}
				}
				after, err := os.ReadFile(config.Path())
				if err != nil || string(before) != string(after) {
					t.Fatal("Enter wrote to the config file")
				}
				_, cmd = next.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
				if cmd == nil {
					t.Fatal("Ctrl+S did not save settings")
				}
				if _, ok := cmd().(ConfigSavedMsg); !ok {
					t.Fatal("Ctrl+S did not report saved settings")
				}
			})
		}
	}
}

func TestSettingsEnterChangesControlsAndTabOnlyNavigates(t *testing.T) {
	for _, database := range []bool{false, true} {
		for _, field := range []int{settingPWUppercase, settingClipboardOSC52, settingDefaultPreset} {
			m := NewSettingsModel(config.Default())
			m.width = 160
			m.focused = field
			before, footer := m.inputs[field].Value(), m.footer()
			if database {
				m.setDatabase(config.Overrides{}, false)
				m.db.editing, m.db.focus = true, slices.Index(databaseSettingList, field)
				before, footer = m.db.values[field].Value(), m.databaseFooter()
			}
			if !strings.Contains(ansi.Strip(footer), "space/enter change") {
				t.Fatal("control does not advertise Enter to change")
			}
			updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			next := updated.(SettingsModel)
			if cmd != nil {
				t.Fatal("changing a control must not save")
			}
			if database {
				if next.db.values[field].Value() == before || next.db.focus != m.db.focus {
					t.Fatal("Enter did not cycle the database control in place")
				}
			} else if isToggleSetting(field) {
				if next.inputs[field].Value() == before || next.focused != field {
					t.Fatal("Enter did not toggle in place")
				}
			} else if !next.picking {
				t.Fatal("Enter did not open the choice picker")
			}
			for _, mod := range []tea.KeyMod{0, tea.ModShift} {
				updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: mod})
				next = updated.(SettingsModel)
				if database {
					if next.db.focus == m.db.focus || next.db.values[field].Value() != before {
						t.Fatal("Tab must navigate without changing a database setting")
					}
				} else if next.focused == field || next.inputs[field].Value() != before || next.picking {
					t.Fatal("Tab must navigate without changing a global setting")
				}
			}
		}
	}
}
