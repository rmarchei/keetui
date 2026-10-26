package ui

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/keepass"
)

func enterBrowsingInput(m AppModel, kind string) AppModel {
	if kind == "search" {
		m.state = viewBrowser
		m.browser = NewBrowserModel(m.db, m.cfg.Keys())
		m.browser.searching = true
		m.browser.searchInput.Focus()
		m.browser.searchInput.SetValue("synthetic draft")
	} else {
		m.state = viewDetail
		m.detail = NewDetailModel(m.db.RootGroup().Entries[0], m.cfg)
		m.detail.SetSize(m.width, m.viewHeight())
		m.detail.attachMode = true
		m.detail.addingAttachment = kind == "add path"
		m.detail.extractingAttachment = kind == "extract path"
		m.detail.attachPathInput.Focus()
		m.detail.attachPathInput.SetValue("/tmp/synthetic-draft")
	}
	return m
}

func browsingInputValue(m AppModel, kind string) string {
	if kind == "search" {
		return m.browser.searchInput.Value()
	}
	return m.detail.attachPathInput.Value()
}

func TestLockShortcutPreservesBrowsingTextInputs(t *testing.T) {
	for _, kind := range []string{"search", "add path", "extract path"} {
		for _, key := range []string{"ctrl+l", "L"} {
			for _, help := range []bool{false, true} {
				name := kind + "/" + key
				if help {
					name += "/help"
				}
				t.Run(name, func(t *testing.T) {
					m := createTestApp(t)
					if key == "L" {
						m.cfg.KeyBindingsPtr = &config.KeyBindings{Lock: "L"}
					}
					if err := m.cfg.Keys().Validate(); err != nil {
						t.Fatal(err)
					}
					m = enterBrowsingInput(m, kind)
					before := browsingInputValue(m, kind)
					if help {
						updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyF1})
						m = updated.(AppModel)
					}
					state := m.state
					press := ctrlL
					if key == "L" {
						press = textKey("L")
					}
					updated, _ := m.Update(press)
					m = updated.(AppModel)
					if m.state != state || m.db == nil {
						t.Fatal("a shortcut closed an active text input")
					}
					want := before
					if key == "L" && !help {
						want += "L"
					}
					if value := browsingInputValue(m, kind); value != want {
						t.Fatalf("input %q, want %q", value, want)
					}
					if help {
						updated, _ = m.Update(CloseHelpMsg{})
						m = updated.(AppModel)
						if m.state == viewHelp || browsingInputValue(m, kind) != before {
							t.Fatal("closing help lost the input")
						}
					}
				})
			}
		}
	}
}

func TestUndoDoesNotChangeDatabaseWhileTypingInBrowsingInputs(t *testing.T) {
	for _, kind := range []string{"search", "add path", "extract path"} {
		for _, readOnly := range []bool{false, true} {
			name := kind
			if readOnly {
				name += "/read-only"
			}
			t.Run(name, func(t *testing.T) {
				m := createTestApp(t)
				updated, _ := m.Update(SaveGroupMsg{ParentUUID: m.db.RootGroup().UUID, Name: "Synthetic undo target"})
				m = enterBrowsingInput(updated.(AppModel), kind)
				m.readOnly = readOnly
				before := m.db.RootGroup()
				state, input, undoCount := m.state, browsingInputValue(m, kind), m.undoStack.length()
				updated, cmd := m.Update(tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
				m = updated.(AppModel)
				if !reflect.DeepEqual(before, m.db.RootGroup()) || m.undoStack.length() != undoCount || m.state != state || browsingInputValue(m, kind) != input {
					t.Fatal("Ctrl+Z acted on the database or dismissed the input")
				}
				if status := quickStatus(cmd); status != "" {
					t.Fatalf("Ctrl+Z in an input produced a database status: %s", status)
				}
				// Leaving the input restores the normal database Undo action.
				m.readOnly = false
				updated, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
				m = updated.(AppModel)
				for _, msg := range quickMsgs(cmd) {
					if _, ok := msg.(BackToBrowserMsg); ok {
						updated, _ = m.Update(msg)
						m = updated.(AppModel)
					}
				}
				updated, _ = m.Update(tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
				m = updated.(AppModel)
				if m.undoStack.length() != undoCount-1 || len(m.db.RootGroup().Groups) != len(before.Groups)-1 {
					t.Fatal("Undo did not resume after leaving the input")
				}
			})
		}
	}
}

func TestQuitCtrlCAliasFreesQAndQuitsBrowsingScreens(t *testing.T) {
	cfg := config.Default()
	cfg.KeyBindingsPtr = &config.KeyBindings{Quit: "ctrl+c", CopyPassword: "q"}
	if err := cfg.Keys().Validate(); err != nil {
		t.Fatal(err)
	}
	keys := cfg.Keys()
	for name, model := range map[string]tea.Model{
		"browser": NewBrowserModel(createTestUIDB(t), keys),
		"detail":  NewDetailModel(testEntry(), cfg),
		"history": NewHistoryModel("Synthetic", nil, keys),
		"stats":   NewStatsModel(keepass.DBStats{}, keys),
		"audit":   NewAuditModel(keepass.AuditResult{}, keys),
		"help":    NewHelpModel(keys, 80, 24),
	} {
		t.Run(name, func(t *testing.T) {
			_, cmd := model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
			requireQuitCommand(t, cmd)
		})
	}
	for name, model := range map[string]tea.Model{
		"detail":  NewDetailModel(testEntry(), cfg),
		"history": NewHistoryModel("Synthetic", []keepass.HistoryEntry{{Password: "synthetic history password"}}, keys),
	} {
		t.Run(name+"/copy with q", func(t *testing.T) {
			_, cmd := model.Update(textKey("q"))
			if cmd == nil {
				t.Fatal("freed q did not trigger its configured action")
			}
			if msg, ok := cmd().(CopyToClipboardMsg); !ok || msg.Value == "" {
				t.Fatal("freed q quit or ignored its configured copy action")
			}
		})
	}
}
