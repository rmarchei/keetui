package ui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rmarchei/keetui/internal/config"
)

func TestDetailUsesRemappedFieldKeysAndExplicitPagerKeys(t *testing.T) {
	cfg := config.Default()
	cfg.KeyBindingsPtr = &config.KeyBindings{NavDown: "J", NavUp: "K"}
	entry := testEntry()
	entry.Notes = strings.Repeat("synthetic long note\n", 50)
	m := NewDetailModel(entry, cfg)
	m.notesExpanded = true
	m.SetSize(80, 12)
	m.viewport.SetYOffset(6)
	for _, oldAlias := range []string{"j", "k", "f", "u", "d", " "} {
		updated, _ := m.Update(textKey(oldAlias))
		m = updated.(DetailModel)
		if m.viewport.YOffset() != 6 || m.selectedField != 0 {
			t.Fatalf("undocumented pager alias %q is still active", oldAlias)
		}
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	m = updated.(DetailModel)
	if m.viewport.YOffset() != 6+m.viewport.Height() {
		t.Fatal("PageDown did not scroll one page")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	m = updated.(DetailModel)
	if m.viewport.YOffset() != 6 {
		t.Fatal("PageUp did not return to the previous position")
	}
	updated, _ = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	m = updated.(DetailModel)
	if m.viewport.YOffset() <= 6 {
		t.Fatal("explicit pager bindings disabled mouse scrolling")
	}
	for _, key := range []tea.KeyPressMsg{textKey("J"), {Code: tea.KeyDown}, textKey("K")} {
		updated, _ = m.Update(key)
		m = updated.(DetailModel)
	}
	if m.selectedField != 1 {
		t.Fatal("remapped keys and arrows no longer select fields")
	}
}

func helpBindingWithDescription(bindings []helpBinding, description string) (helpBinding, bool) {
	index := slices.IndexFunc(bindings, func(binding helpBinding) bool { return binding.description == description })
	if index < 0 {
		return helpBinding{}, false
	}
	return bindings[index], true
}

func TestContextHelpListsLockOnlyForBrowsingContexts(t *testing.T) {
	for _, tc := range []struct {
		name     string
		state    viewState
		prepare  func(*AppModel)
		wantLock bool
	}{
		{"history", viewHistory, nil, true},
		{"history version", viewHistory, func(m *AppModel) { m.history.previewing = true }, true},
		{"stats", viewStats, nil, true}, {"audit", viewAudit, nil, true},
		{"attachments", viewDetail, func(m *AppModel) { m.detail.attachMode = true }, true},
		{"QR", viewDetail, func(m *AppModel) { m.detail.showQR = true }, true},
		{"add attachment path", viewDetail, func(m *AppModel) { m.detail.addingAttachment = true }, false},
		{"extract attachment path", viewDetail, func(m *AppModel) { m.detail.extractingAttachment = true }, false},
		{"search", viewBrowser, func(m *AppModel) { m.browser.searching = true }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := createTestApp(t)
			m.cfg.KeyBindingsPtr = &config.KeyBindings{Lock: "L"}
			m.state = tc.state
			if tc.prepare != nil {
				tc.prepare(&m)
			}
			_, context := m.helpContext()
			if context == nil {
				t.Fatal("fixture has no contextual Help")
			}
			binding, found := helpBindingWithDescription(context.bindings, "Lock")
			if found != tc.wantLock || found && binding.key != "L" {
				t.Fatalf("Lock hint = %+v, present=%v; want present=%v", binding, found, tc.wantLock)
			}
		})
	}
}

func TestReadOnlyBrowsingHelpOmitsUnavailableActions(t *testing.T) {
	m := createTestApp(t)
	m.readOnly = true
	updated, _ := m.openHelp()
	m = updated.(AppModel)
	for section, unavailable := range map[int][]string{
		helpBrowser:    {"New entry", "New group", "Delete", "Rename group", "Duplicate", "Favorite", "Move: cut entry/group", "Move: paste here", "Save / save as", "Master password", "Undo"},
		helpDetail:     {"Edit entry", "Attachments"},
		helpRecycleBin: {"Delete permanently", "Restore to root", "Empty bin", "Save database"},
	} {
		bindings := m.help.sections()[section].bindings
		for _, description := range unavailable {
			if _, found := helpBindingWithDescription(bindings, description); found {
				t.Fatalf("read-only section %s advertises %q", m.help.sections()[section].title, description)
			}
		}
	}
	bindings := m.help.sections()[helpDetail].bindings
	for _, description := range []string{"Copy password", "Reveal / hide", "Password history", "Scroll a page", "Lock"} {
		if _, found := helpBindingWithDescription(bindings, description); !found {
			t.Fatalf("read-only entry Help lost available action %q", description)
		}
	}
}

func TestReadOnlyHistoryHidesRestoreInListPreviewAndHelp(t *testing.T) {
	m, entry := historyApp(t)
	m.readOnly = true
	m, _ = lockStep(t, m, ShowHistoryMsg{EntryUUID: entry.UUID, EntryTitle: entry.Title})
	for _, preview := range []bool{false, true} {
		m.history.previewing = preview
		m.history.width = 180
		if strings.Contains(ansi.Strip(m.history.footer()), "restore") {
			t.Fatal("read-only history footer advertises restore")
		}
		_, context := m.helpContext()
		for _, binding := range context.bindings {
			if strings.HasPrefix(binding.description, "Restore") {
				t.Fatal("read-only history Help advertises restore")
			}
		}
		if _, found := helpBindingWithDescription(context.bindings, "Lock"); !found {
			t.Fatal("read-only history Help lost Lock")
		}
	}
}

func TestSettingsHelpNamesTheScreenEscReturnsTo(t *testing.T) {
	for _, unlocked := range []bool{false, true} {
		m := createTestApp(t)
		want := "Back to browser"
		if !unlocked {
			m = NewApp(m.filePath, "", m.cfg)
			want = "Back to unlock"
		}
		m, _ = lockStep(t, m, ShowSettingsMsg{})
		m, _ = lockStep(t, m, tea.KeyPressMsg{Code: tea.KeyF1})
		bindings := m.help.sections()[m.help.section].bindings
		binding, found := helpBindingWithDescription(bindings, want)
		if !found || binding.key != "esc" {
			t.Fatalf("Settings Help did not advertise Esc %s", want)
		}
	}
}

func TestDatabaseSettingsHelpMatchesFocusedControlAndPermissions(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		for _, field := range []int{settingClipboardClear, settingPassphraseSeparator, settingPWUppercase} {
			m := createTestApp(t)
			m.state = viewSettings
			m.settings = NewSettingsModel(m.cfg)
			m.settings.setDatabase(config.Overrides{}, readOnly)
			m.settings.db.editing = true
			m.settings.db.focus = slices.Index(databaseSettingList, field)
			_, context := m.helpContext()
			var reset, edit, change bool
			var saveDescription string
			for _, binding := range context.bindings {
				reset = reset || binding.key == "backspace/del"
				edit = edit || binding.description == "Empty value inherits"
				change = change || binding.description == "Cycle toggle / choice"
				if binding.key == "ctrl+s" {
					saveDescription = binding.description
				}
			}
			choice := field == settingPWUppercase
			wantSave := "Save settings"
			if readOnly {
				wantSave = "Save global settings"
			}
			if reset != (!readOnly && choice) || change != (!readOnly && choice) || edit != (!readOnly && !choice) || saveDescription != wantSave {
				t.Fatalf("wrong database hints for %s (read-only %v): %+v", settingLabels[field], readOnly, context.bindings)
			}
		}
	}
}

func TestHelpDeduplicatesCtrlCQuitAndIncludesConfirmationShiftTab(t *testing.T) {
	kb := config.DefaultKeyBindings()
	kb.Quit = "ctrl+c"
	m := NewHelpModel(kb, 80, 23)
	for _, section := range m.sections() {
		for _, binding := range section.bindings {
			if strings.Contains(binding.key, "ctrl+c/ctrl+c") {
				t.Fatal("Quit aliases appear twice")
			}
		}
	}
	app := createTestApp(t)
	app.cfg.KeyBindingsPtr = &kb
	app.state = viewStats
	_, context := app.helpContext()
	if binding, ok := helpBindingWithDescription(context.bindings, "Quit"); !ok || binding.key != "ctrl+c" {
		t.Fatal("contextual Help did not deduplicate Ctrl+C Quit")
	}
	app.state = viewConfirm
	_, context = app.helpContext()
	if binding, ok := helpBindingWithDescription(context.bindings, "Switch button"); !ok || !strings.Contains(binding.key, "shift+tab") {
		t.Fatal("confirmation Help omits Shift+Tab")
	}
}
