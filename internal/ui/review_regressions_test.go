package ui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/keepass"
	"github.com/tobischo/gokeepasslib/v3"
)

func TestPasswordConfirmReachableOnShortTerminal(t *testing.T) {
	for _, width := range []int{40, 80} {
		for _, height := range []int{4, 5, 6} {
			m := NewPasswordModel()
			m.newInput.EchoMode, m.confirmInput.EchoMode = textinput.EchoNormal, textinput.EchoNormal
			m.confirmInput.SetValue("confirm-value")
			updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
			updated, _ = updated.Update(tea.KeyPressMsg{Code: tea.KeyTab})
			view := updated.View().Content
			assertFrame(t, view, width, height)
			if !strings.Contains(ansi.Strip(view), "confirm-value") {
				t.Fatalf("confirm input unreachable at %dx%d: %q", width, height, ansi.Strip(view))
			}
		}
	}
}

func TestQuitFromHelpRestoresPendingConfirmation(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{textKey("q"), {Code: 'c', Mod: tea.ModCtrl}} {
		m := createTestApp(t)
		m.editor = NewEditorModel(nil, gokeepasslib.UUID{}, m.cfg)
		m.state, m.returnState = viewConfirm, viewEditor
		m.confirm = NewConfirmModel("Original confirmation", func() tea.Msg { return StatusMsg{Text: "original action"} })
		updated, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
		updated, _ = updated.Update(tea.KeyPressMsg{Code: tea.KeyF1})
		updated, cmd := updated.Update(key)
		updated, _ = updated.Update(cmd())
		m = updated.(AppModel)
		if m.state != viewConfirm || m.confirm.width != 40 || m.confirm.height != 11 {
			t.Fatal("quit prompt was not sized")
		}
		updated, _ = m.Update(RequestQuitMsg{})
		updated, _ = updated.Update(CancelEditMsg{})
		m = updated.(AppModel)
		if m.state != viewHelp {
			t.Fatal("cancel quit should return to help")
		}
		updated, _ = m.Update(CloseHelpMsg{})
		m = updated.(AppModel)
		if m.state != viewConfirm || m.confirm.message != "Original confirmation" || m.returnState != viewEditor {
			t.Fatal("original confirmation was lost or replaced by a stale quit prompt")
		}
		if msg := m.confirm.onConfirm().(StatusMsg); msg.Text != "original action" {
			t.Fatal("original confirmation action was lost")
		}
		updated, _ = m.Update(CancelEditMsg{})
		if updated.(AppModel).state != viewEditor {
			t.Fatal("cancel pending confirmation should return to editor")
		}
	}
}

func TestReplacingConfirmationKeepsTerminalSize(t *testing.T) {
	m := createTestApp(t)
	m.state, m.returnState = viewConfirm, viewSettings
	m.confirm = NewConfirmModel("Original", nil)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 110, Height: 18})
	updated, _ = updated.Update(RequestQuitMsg{})
	m = updated.(AppModel)
	if m.confirm.width != 110 || m.confirm.height != 17 {
		t.Fatal("replacement dialog used fallback dimensions")
	}
	updated, _ = m.Update(CancelEditMsg{})
	m = updated.(AppModel)
	if m.state != viewConfirm || m.confirm.message != "Original" || m.returnState != viewSettings {
		t.Fatal("canceling quit lost original dialog")
	}
}

func TestQuitFromHelpOverQuitKeepsOriginalOrigin(t *testing.T) {
	m := createTestApp(t)
	updated, _ := m.Update(ShowSettingsMsg{})
	updated, _ = updated.Update(RequestQuitMsg{})
	updated, _ = updated.Update(ShowHelpMsg{})
	updated, _ = updated.Update(RequestQuitMsg{})
	m = updated.(AppModel)
	if m.state != viewConfirm {
		t.Fatal("quit from help should return to the existing quit prompt")
	}
	updated, _ = m.Update(CancelEditMsg{})
	if updated.(AppModel).state != viewSettings {
		t.Fatal("quit cancellation did not restore the original settings view")
	}
}

func TestNestedHelpDuringQuitRestoresOriginalHelp(t *testing.T) {
	m := createTestApp(t)
	updated, _ := m.Update(EditEntryMsg{Entry: testEntry()})
	updated, _ = updated.Update(ShowHelpMsg{})
	updated, _ = updated.Update(RequestQuitMsg{})
	updated, _ = updated.Update(ShowHelpMsg{})
	updated, _ = updated.Update(CloseHelpMsg{})
	updated, _ = updated.Update(CancelEditMsg{})
	m = updated.(AppModel)
	if m.state != viewHelp || m.helpReturnState != viewEditor || m.help.section != helpEditor {
		t.Fatal("help opened during quit overwrote original help context")
	}
	updated, _ = m.Update(CloseHelpMsg{})
	if updated.(AppModel).state != viewEditor {
		t.Fatal("closing restored help resurrected a stale confirmation")
	}
}

func TestHelpForwardsBackgroundUpdatesWithoutRestartingTimers(t *testing.T) {
	m := createTestApp(t)
	entry := testEntry()
	entry.TOTP = "JBSWY3DPEHPK3PXP"
	updated, _ := m.Update(ViewEntryMsg{Entry: entry})
	m = updated.(AppModel)
	seq := m.detail.totpSeq
	updated, _ = m.Update(ShowHelpMsg{})
	updated, cmd := updated.Update(totpTickMsg{seq: seq})
	if cmd == nil || updated.(AppModel).state != viewHelp {
		t.Fatal("help dropped the TOTP tick chain")
	}
	updated, cmd = updated.Update(CloseHelpMsg{})
	if cmd != nil || updated.(AppModel).detail.totpSeq != seq {
		t.Fatal("closing help restarted a still-live timer")
	}
	updated, _ = updated.Update(EditEntryMsg{Entry: entry})
	updated, _ = updated.Update(ShowHelpMsg{})
	updated, _ = updated.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	m = updated.(AppModel)
	if m.editor.width != 40 || m.editor.height != 11 {
		t.Fatal("hidden editor did not receive resize")
	}
	updated, _ = m.Update(CloseHelpMsg{})
	updated, _ = updated.Update(ShowPresetPickerMsg{})
	updated, _ = updated.Update(ShowHelpMsg{})
	updated, _ = updated.Update(tea.WindowSizeMsg{Width: 110, Height: 24})
	m = updated.(AppModel)
	if m.presetPicker.width != 110 || m.editor.width != 110 {
		t.Fatal("nested help/picker overlays did not forward background updates")
	}
}

func TestStatusKeepsUndoAndCountWithMessage(t *testing.T) {
	m := createTestApp(t)
	m.dirty = true
	m.entryCount = 3
	m.undoStack.push(undoAction{description: "edit"})
	m.statusText = strings.Repeat("Entry updated ", 30)
	bar := m.statusBar()
	assertFrame(t, bar, 80, 1)
	for _, want := range []string{"ctrl+z: undo", "3 entries", "F1: details", "Entry"} {
		if !strings.Contains(ansi.Strip(bar), want) {
			t.Fatalf("status hides %q: %q", want, ansi.Strip(bar))
		}
	}
	m.width = 40
	if !strings.Contains(ansi.Strip(m.statusBar()), "ctrl+z: undo") {
		t.Fatal("narrow status hides undo")
	}
}

func TestEditorDrawnAndStoredOffsetsAgreeAfterResizeAndDelete(t *testing.T) {
	entry := testEntry()
	for i := range 8 {
		entry.CustomFields = append(entry.CustomFields, keepass.CustomField{Key: fmt.Sprintf("key-%d", i), Value: "value"})
	}
	m := NewEditorModel(&entry, gokeepasslib.UUID{}, config.Default())
	m.focused = m.totalFields() - 1
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 11})
	m = updated.(EditorModel)
	if m.offset == 0 {
		t.Fatal("fixture did not scroll")
	}
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 60})
	m = updated.(EditorModel)
	if m.offset != 0 {
		t.Fatal("resize left a stale scroll offset")
	}
	updated, _ = m.Update(tea.MouseClickMsg{X: 3, Y: m.rows[fieldTitle].start, Button: tea.MouseLeft})
	if updated.(EditorModel).focused != fieldTitle {
		t.Fatal("click did not select the drawn title field")
	}
	for len(m.customInputs) > 0 {
		m.focused = m.totalFields() - 1
		m.syncLayout()
		updated, _ = m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
		m = updated.(EditorModel)
		body, _ := m.body()
		if m.offset != clampOffset(body, bodyHeight(m.height, m.footer()), m.offset) {
			t.Fatal("field deletion left a stale scroll offset")
		}
	}
}

func TestEditorStrengthUsesAvailableSpaceAndFollowsPassword(t *testing.T) {
	entry := keepass.Entry{Password: "PASS-e1-v2"}
	m := NewEditorModel(&entry, gokeepasslib.UUID{}, config.Default())
	m.focused = fieldPassword
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 23})
	m = updated.(EditorModel)
	body, _ := m.body()
	inline := false
	for _, row := range strings.Split(ansi.Strip(body), "\n") {
		inline = inline || strings.Contains(row, "Password:") && strings.Contains(row, "guess bits")
	}
	if !inline {
		t.Fatal("strength did not fit inline on a wide terminal")
	}
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 40, Height: 11})
	view := ansi.Strip(updated.View().Content)
	if !strings.Contains(view, "Password:") || !strings.Contains(view, "guess bits") {
		t.Fatalf("selected password/strength annotation not kept together: %q", view)
	}
}

func TestStatsFirstUpAfterEndMovesContent(t *testing.T) {
	m := NewStatsModel(keepass.DBStats{})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	updated, _ = updated.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	before := updated.View().Content
	updated, _ = updated.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if updated.View().Content == before {
		t.Fatal("first up after End did not move the drawn content")
	}
	updated, _ = updated.Update(tea.WindowSizeMsg{Width: 80, Height: 50})
	if updated.(StatsModel).offset != 0 {
		t.Fatal("stats offset did not clamp after terminal grew")
	}
}

func TestListOffsetsClampAfterResize(t *testing.T) {
	history := NewHistoryModel("test", make([]keepass.HistoryEntry, 30))
	audit := NewAuditModel(keepass.AuditResult{WeakEntries: make([]keepass.AuditEntry, 30)})
	browser := createTestApp(t).browser
	for i := range 30 {
		browser.items = append(browser.items, listItem{entry: keepass.Entry{Title: fmt.Sprintf("entry-%d", i)}})
	}
	for _, model := range []tea.Model{history, audit, browser} {
		model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 11})
		for range 40 {
			model, _ = model.Update(textKey("j"))
		}
		model, _ = model.Update(tea.WindowSizeMsg{Width: 160, Height: 80})
		var offset int
		switch m := model.(type) {
		case HistoryModel:
			offset = m.offset
		case AuditModel:
			offset = m.offset
		case BrowserModel:
			offset = m.offset
		}
		if offset != 0 {
			t.Fatalf("%T retained stale offset %d", model, offset)
		}
	}
}
