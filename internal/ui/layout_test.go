package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/tobischo/gokeepasslib/v3"

	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/keepass"
)

func assertFrame(t *testing.T, content string, width, height int) {
	t.Helper()
	if got := lipgloss.Height(content); got != height {
		t.Fatalf("frame height = %d, want %d", got, height)
	}
	for row, line := range strings.Split(content, "\n") {
		if got := lipgloss.Width(line); got > width {
			t.Fatalf("row %d width = %d, exceeds %d: %q", row, got, width, line)
		}
	}
}

func TestLayoutHintsKeepWholeBindings(t *testing.T) {
	hints := []string{"F1: help", "ctrl+s: save", "esc: cancel", "tab/shift+tab: navigate", "ctrl+r: reveal", "ctrl+g: gen password", "秘密: 日本語"}
	for _, width := range []int{20, 40, 80, 110, 160} {
		footer := ansi.Strip(renderHints(width, hints...))
		if lipgloss.Height(footer) != 1 || !strings.Contains(footer, "F1 help") {
			t.Fatalf("invalid footer at %d: %q", width, footer)
		}
		for _, line := range strings.Split(footer, "\n") {
			if lipgloss.Width(line) > width {
				t.Fatalf("footer exceeded %d columns", width)
			}
			for _, item := range strings.Split(strings.TrimSpace(line), "  ") {
				found := false
				for _, hint := range hints {
					found = found || item == strings.Replace(hint, ": ", " ", 1)
				}
				if !found {
					t.Fatalf("partial binding at %d: %q", width, item)
				}
			}
		}
	}
}

func TestLayoutSettingsEveryFieldReachable(t *testing.T) {
	for _, size := range [][2]int{{80, 23}, {110, 23}, {40, 11}, {160, 39}} {
		m := NewSettingsModel(config.Default())
		updated, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = updated.(SettingsModel)
		for range settingCount * 2 {
			view := m.View().Content
			assertFrame(t, view, size[0], size[1])
			plain := ansi.Strip(view)
			if !strings.Contains(plain, settingLabels[m.focused]+":") {
				t.Fatalf("%v: focused setting %q hidden", size, settingLabels[m.focused])
			}
			if !strings.Contains(plain, "ctrl+s save") || !strings.Contains(plain, "F1 help") {
				t.Fatalf("%v: settings footer hidden", size)
			}
			updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
			m = updated.(SettingsModel)
		}
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
		m = updated.(SettingsModel)
		if !strings.Contains(ansi.Strip(m.View().Content), "Keybindings:") {
			t.Fatal("reverse wrap did not scroll to the last setting")
		}
	}
}

func TestLayoutEditorFocusAndResize(t *testing.T) {
	entry := keepass.Entry{Title: "Synthetic 日本語", Password: "dummy-password", Notes: "one\ntwo\nthree\nfour\nfive"}
	for i := range 12 {
		entry.CustomFields = append(entry.CustomFields, keepass.CustomField{Key: fmt.Sprintf("custom-%d", i), Value: fmt.Sprintf("value-%d", i)})
	}
	m := NewEditorModel(&entry, gokeepasslib.UUID{}, config.Default())
	for _, size := range [][2]int{{80, 23}, {40, 11}, {110, 23}, {160, 39}, {80, 23}} {
		updated, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = updated.(EditorModel)
		for range m.totalFields() {
			view := m.View().Content
			assertFrame(t, view, size[0], size[1])
			_, rows := m.body()
			row := rows[m.focused]
			visible := bodyHeight(size[1], m.footer())
			if row.start < m.offset || row.end >= m.offset+visible {
				t.Fatalf("%v: field %d rows %v outside viewport [%d,%d)", size, m.focused, row, m.offset, m.offset+visible)
			}
			plain := ansi.Strip(view)
			if !strings.Contains(plain, "ctrl+s save") || !strings.Contains(plain, "esc cancel") || !strings.Contains(plain, "F1 help") {
				t.Fatalf("%v: editor footer hidden", size)
			}
			updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
			m = updated.(EditorModel)
		}
	}
	if m.inputs[fieldPassword].Value() != entry.Password || m.notesInput.Value() != entry.Notes {
		t.Fatal("layout changed field values")
	}
}

func TestLayoutPathBasenameAndEditableValue(t *testing.T) {
	path := "/" + strings.Repeat("long-directory/", 15) + "秘密-vault.kdbx"
	for _, width := range []int{40, 80, 110} {
		label := pathLabel(path, width)
		if lipgloss.Width(label) > width || !strings.HasSuffix(ansi.Strip(label), "秘密-vault.kdbx") || !strings.HasPrefix(ansi.Strip(label), "…") {
			t.Fatalf("bad abbreviated path: %q", label)
		}
	}
	m := NewPathInputModel(path, "KDBX 3.1 (AES)", true)
	for _, width := range []int{110, 40, 80, 160, 40} {
		updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 11})
		m = updated.(PathInputModel)
		view := m.View().Content
		assertFrame(t, view, width, 11)
		if !strings.Contains(ansi.Strip(view), "秘密-vault.kdbx") {
			t.Fatalf("filename hidden at width %d", width)
		}
		if m.input.Value() != path || m.input.Position() != len([]rune(path)) {
			t.Fatal("resize changed the actual save path or cursor")
		}
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil || cmd().(SaveAsMsg).Path != path {
		t.Fatal("saving did not preserve the full path")
	}
}

func TestLayoutUnlockFullErrorAccessible(t *testing.T) {
	path := "/" + strings.Repeat("directory/", 15) + "test-vault.kdbx"
	message := "Wrong password? Database integrity verification failed. " + strings.Repeat("Additional details for diagnosis. ", 20) + "FINAL-ERROR-DETAIL"
	m := NewUnlockModel(path, "")
	m.err = errors.New(message)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	m = updated.(UnlockModel)
	var all strings.Builder
	for range 30 {
		view := m.View().Content
		assertFrame(t, view, 40, 12)
		plain := ansi.Strip(view)
		if !strings.Contains(plain, "test-vault.kdbx") || !strings.Contains(plain, "pgup/pgdown error") {
			t.Fatal("unlock lost filename or error scrolling hint")
		}
		all.WriteString(plain)
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
		m = updated.(UnlockModel)
	}
	if !strings.Contains(strings.ReplaceAll(all.String(), "\n", ""), "FINAL-ERROR-DETAIL") {
		t.Fatal("end of unlock error is inaccessible")
	}
	for range 30 {
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
		m = updated.(UnlockModel)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "Wrong password?") {
		t.Fatal("cannot return to start of unlock error")
	}
	if m.input.Value() != "" {
		t.Fatal("scroll keys changed the password")
	}
}

func TestLayoutAppTransitionsStatusAndHelp(t *testing.T) {
	m := createTestApp(t)
	for _, size := range [][2]int{{80, 24}, {40, 12}, {110, 24}, {160, 40}} {
		updated, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = updated.(AppModel)
		for _, msg := range []tea.Msg{ShowSettingsMsg{}, ShowStatsMsg{}, EditEntryMsg{Entry: testEntry()}, SaveAsStartMsg{}, ShowHelpMsg{}, CloseHelpMsg{}, BackToBrowserMsg{}} {
			updated, _ = m.Update(msg)
			m = updated.(AppModel)
			view := m.View().Content
			assertFrame(t, view, size[0], size[1])
			lines := strings.Split(view, "\n")
			if lines[len(lines)-1] != m.statusBar() {
				t.Fatalf("%T: status is not on the final row", msg)
			}
			switch m.state {
			case viewSettings:
				if m.settings.width != size[0] || m.settings.height != size[1]-1 {
					t.Fatal("new settings screen did not receive dimensions")
				}
			case viewEditor:
				if m.editor.width != size[0] || m.editor.height != size[1]-1 {
					t.Fatal("new editor did not receive dimensions")
				}
			case viewStats:
				if m.stats.width != size[0] || m.stats.height != size[1]-1 {
					t.Fatal("new stats screen did not receive dimensions")
				}
			case viewPathInput:
				if m.pathInput.width != size[0] || m.pathInput.height != size[1]-1 {
					t.Fatal("new save-as screen did not receive dimensions")
				}
			}
		}
	}
	m.statusText = strings.Repeat("A long status message 日本語. ", 20) + "END-OF-STATUS"
	m.statusError = true
	m.dirty = true
	m.readOnly = true
	assertFrame(t, m.statusBar(), m.width, 1)
	if !strings.Contains(ansi.Strip(m.statusBar()), "[modified]") || !strings.Contains(ansi.Strip(m.statusBar()), "[read-only]") {
		t.Fatal("critical status indicators disappeared")
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyF1})
	m = updated.(AppModel)
	if !strings.HasSuffix(m.help.message, "END-OF-STATUS") {
		t.Fatal("F1 lost the full status message")
	}
}

func TestLayoutHelpPreservesEditorAndRemasking(t *testing.T) {
	m := createTestApp(t)
	updated, _ := m.Update(EditEntryMsg{Entry: testEntry()})
	m = updated.(AppModel)
	m.editor.focused = fieldPassword
	m.editor.updateFocus()
	updated, _ = m.Update(tea.PasteMsg{Content: "dummy-secret"})
	m = updated.(AppModel)
	secret := m.editor.inputs[fieldPassword].Value()
	seq := m.editor.revealSeq
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyF1})
	m = updated.(AppModel)
	if m.state != viewHelp {
		t.Fatal("F1 did not open help")
	}
	updated, _ = m.Update(maskPasswordMsg{seq: seq})
	m = updated.(AppModel)
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	m = updated.(AppModel)
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = updated.(AppModel)
	updated, _ = m.Update(cmd())
	m = updated.(AppModel)
	if m.state != viewEditor || m.editor.inputs[fieldPassword].Value() != secret || m.editor.focused != fieldPassword {
		t.Fatal("closing help lost the current form")
	}
	if strings.Contains(m.editor.View().Content, secret) {
		t.Fatal("help swallowed the secret re-mask timer")
	}
	assertFrame(t, m.View().Content, 40, 12)
	updated, _ = m.Update(textKey("?"))
	m = updated.(AppModel)
	if m.state != viewEditor || !strings.Contains(m.editor.inputs[fieldPassword].Value(), "?") {
		t.Fatal("printable question mark was stolen from the input")
	}
}

func TestLayoutAllScreensFitOnResize(t *testing.T) {
	db := createTestUIDB(t)
	path := "/" + strings.Repeat("long-directory/", 12) + "vault.kdbx"
	entry := testEntry()
	entry.Title = strings.Repeat("秘密🔑", 40)
	stats := keepass.DBStats{FilePath: path, ReusedPasswords: 5}
	history := make([]keepass.HistoryEntry, 40)
	var audit keepass.AuditResult
	for i := range 40 {
		audit.WeakEntries = append(audit.WeakEntries, keepass.AuditEntry{Entry: keepass.Entry{Title: fmt.Sprintf("weak-%d", i)}})
	}
	custom := NewPresetPickerModel(config.Default(), "")
	custom.mode = pickerModeCustom
	preview := NewPresetPickerModel(config.Default(), "")
	preview.mode = pickerModePreview
	preview.generatedPassword = "synthetic-password"
	models := []struct {
		name  string
		model tea.Model
	}{
		{"browser", NewBrowserModel(db)},
		{"detail", NewDetailModel(entry, config.Default())},
		{"settings", NewSettingsModel(config.Default())},
		{"editor", NewEditorModel(&entry, gokeepasslib.UUID{}, config.Default())},
		{"unlock", NewUnlockModel(path, "synthetic.key")},
		{"stats", NewStatsModel(stats)},
		{"save as", NewPathInputModel(path, "KDBX 3.1 (AES)", true)},
		{"group", NewGroupInputModel(gokeepasslib.UUID{}, "")},
		{"password", NewPasswordModel()},
		{"confirm", NewConfirmModel(viewBrowser, "Quit?", nil)},
		{"history", NewHistoryModel(entry.Title, history)},
		{"audit", NewAuditModel(audit)},
		{"help", NewHelpModel(config.DefaultKeyBindings(), 80, 23)},
		{"presets", NewPresetPickerModel(config.Default(), "")},
		{"custom preset", custom},
		{"preview", preview},
	}
	for _, tt := range models {
		t.Run(tt.name, func(t *testing.T) {
			model := tt.model
			for _, size := range [][2]int{{80, 23}, {110, 23}, {40, 11}, {160, 39}, {80, 23}} {
				model, _ = model.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				assertFrame(t, model.View().Content, size[0], size[1])
			}
		})
	}
}

func TestLayoutEditorMouseUsesScrolledRows(t *testing.T) {
	m := NewEditorModel(nil, gokeepasslib.UUID{}, config.Default())
	m.customInputs = append(m.customInputs, newCustomFieldInput("custom-key", "custom-value", false))
	m.width, m.height = 40, 11
	m.focused = m.totalFields() - 1
	m.updateFocus()
	m.syncLayout()
	if m.offset == 0 {
		t.Fatal("test must exercise a scrolled form")
	}
	_, rows := m.body()
	keyIndex := fixedFieldCount
	y := rows[keyIndex].start - m.offset
	updated, _ := m.Update(tea.MouseClickMsg{X: 15, Y: y, Button: tea.MouseLeft})
	m = updated.(EditorModel)
	if m.focused != keyIndex {
		t.Fatalf("click focused %d, want custom key %d", m.focused, keyIndex)
	}
	updated, _ = m.Update(tea.MouseClickMsg{X: 15, Y: m.height - 1, Button: tea.MouseLeft})
	if updated.(EditorModel).focused != keyIndex {
		t.Fatal("footer click changed focus")
	}
}

func TestLayoutBrowserSelectionSurvivesShrink(t *testing.T) {
	m := NewBrowserModel(createTestUIDB(t))
	m.items = nil
	for i := range 50 {
		m.items = append(m.items, listItem{entry: keepass.Entry{Title: fmt.Sprintf("synthetic-%02d", i)}})
	}
	m.cursor = 49
	for _, size := range [][2]int{{160, 39}, {80, 23}, {40, 11}, {110, 23}} {
		updated, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = updated.(BrowserModel)
		view := m.View().Content
		assertFrame(t, view, size[0], size[1])
		if !strings.Contains(ansi.Strip(view), "synthetic-49") || !strings.Contains(ansi.Strip(view), "? help") {
			t.Fatal("resize hid the selected entry or help")
		}
		updated, cmd := m.Update(tea.MouseClickMsg{X: 4, Y: size[1] - 1, Button: tea.MouseLeft})
		if updated.(BrowserModel).cursor != 49 || cmd != nil {
			t.Fatal("footer click selected or opened an off-screen entry")
		}
	}
}

func TestLayoutStatsEndAndPath(t *testing.T) {
	m := NewStatsModel(keepass.DBStats{FilePath: "/" + strings.Repeat("directory/", 20) + "vault.kdbx"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 11})
	m = updated.(StatsModel)
	if !strings.Contains(ansi.Strip(m.View().Content), "vault.kdbx") {
		t.Fatal("stats hid the filename")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	m = updated.(StatsModel)
	if !strings.Contains(ansi.Strip(m.View().Content), "Expired:") {
		t.Fatal("stats bottom is unreachable")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyHome})
	m = updated.(StatsModel)
	if !strings.Contains(ansi.Strip(m.View().Content), "Database Info") {
		t.Fatal("cannot return to the top of stats")
	}
}

func TestLayoutHelpKeepsPendingConfirmation(t *testing.T) {
	m := createTestApp(t)
	updated, _ := m.Update(ShowSettingsMsg{})
	m = updated.(AppModel)
	updated, _ = m.Update(RequestQuitMsg{})
	m = updated.(AppModel)
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyF1})
	m = updated.(AppModel)
	updated, _ = m.Update(CloseHelpMsg{})
	m = updated.(AppModel)
	if m.state != viewConfirm || m.confirm.returnState != viewSettings {
		t.Fatal("help overwrote the confirmation return state")
	}
	updated, cmd := m.Update(textKey("n"))
	m = updated.(AppModel)
	updated, _ = m.Update(cmd())
	if updated.(AppModel).state != viewSettings {
		t.Fatal("canceling confirmation did not restore settings")
	}
}
