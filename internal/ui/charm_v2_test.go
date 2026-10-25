package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/keepass"
)

func TestAppTerminalModes(t *testing.T) {
	m := createTestApp(t)
	for _, state := range []viewState{viewUnlock, viewBrowser, viewDetail, viewConfirm} {
		m.state = state
		view := m.View()
		if !view.AltScreen || view.MouseMode != tea.MouseModeCellMotion {
			t.Errorf("state %d lost full-screen or mouse mode", state)
		}
		if view.Cursor != nil || view.DisableBracketedPasteMode {
			t.Errorf("state %d should use virtual cursors and bracketed paste", state)
		}
	}
}

func TestBrowserPasteSearch(t *testing.T) {
	m := NewBrowserModel(createTestUIDB(t))
	updated, _ := m.Update(textKey("/"))
	m = updated.(BrowserModel)
	updated, _ = m.Update(tea.PasteMsg{Content: "Gmail"})
	m = updated.(BrowserModel)
	if m.searchInput.Value() != "Gmail" || m.searchQuery != "gmail" {
		t.Fatalf("paste did not update search: input=%q query=%q", m.searchInput.Value(), m.searchQuery)
	}
	if len(m.items) != 1 || m.items[0].isGroup || m.items[0].entry.Title != "Gmail" {
		t.Fatal("pasted search did not filter entries")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(BrowserModel)
	if m.searching || len(m.items) != 1 {
		t.Fatal("enter did not preserve pasted search results")
	}
}

func TestAppPasteActivityAndSaveGate(t *testing.T) {
	for _, saving := range []bool{false, true} {
		m := createTestApp(t)
		updated, _ := m.Update(textKey("/"))
		m = updated.(AppModel)
		m.saving = saving
		idleSince := time.Now().Add(-time.Minute)
		m.lastActivity = idleSince
		m.statusText = "previous status"
		for _, msg := range []tea.Msg{tea.PasteStartMsg{}, tea.PasteMsg{Content: "Gmail"}, tea.PasteEndMsg{}} {
			updated, _ = m.Update(msg)
			m = updated.(AppModel)
		}
		if saving {
			if m.browser.searchInput.Value() != "" || !m.lastActivity.Equal(idleSince) {
				t.Fatal("paste bypassed the save input gate")
			}
		} else if m.browser.searchInput.Value() != "Gmail" || !m.lastActivity.After(idleSince) || m.statusText != "" {
			t.Fatal("paste did not count as user activity and update the active input")
		}
	}
}

func TestUnlockPasteAndBusyGate(t *testing.T) {
	m := NewUnlockModel("synthetic.kdbx", "")
	updated, _ := m.Update(tea.PasteMsg{Content: "dummy-master-password"})
	m = updated.(UnlockModel)
	if m.input.Value() != "dummy-master-password" {
		t.Fatal("master password paste was dropped")
	}
	if strings.Contains(m.View().Content, "dummy-master-password") {
		t.Fatal("master password became visible")
	}
	m.unlocking = true
	updated, _ = m.Update(tea.PasteMsg{Content: "ignored"})
	m = updated.(UnlockModel)
	if m.input.Value() != "dummy-master-password" {
		t.Fatal("paste changed the password while unlocking")
	}
}

func TestEditorPastePasswordAndNotes(t *testing.T) {
	db := createTestUIDB(t)
	m := NewEditorModel(nil, db.RootGroup().UUID, config.Default())
	m.focused = fieldPassword
	m.updateFocus()
	updated, cmd := m.Update(tea.PasteMsg{Content: "dummy-secret"})
	m = updated.(EditorModel)
	if m.inputs[fieldPassword].Value() != "dummy-secret" {
		t.Fatal("password paste was dropped")
	}
	// Editing intentionally reveals the value for one second, just as in v1.
	if cmd == nil || m.revealSeq == 0 {
		t.Fatal("password edit did not schedule re-masking")
	}
	updated, _ = m.Update(maskPasswordMsg{seq: m.revealSeq})
	m = updated.(EditorModel)
	if strings.Contains(m.View().Content, "dummy-secret") {
		t.Fatal("pasted password was not masked")
	}
	m.focused = fieldNotes
	m.updateFocus()
	updated, _ = m.Update(tea.PasteMsg{Content: "line one\nline two"})
	m = updated.(EditorModel)
	if m.notesInput.Value() != "line one\nline two" {
		t.Fatalf("multiline paste changed: %q", m.notesInput.Value())
	}
	if !m.notesInput.VirtualCursor() || !m.inputs[fieldPassword].VirtualCursor() {
		t.Fatal("nested editor inputs must use virtual cursors")
	}
}

func TestAttachmentPathPaste(t *testing.T) {
	for _, extracting := range []bool{false, true} {
		m := NewDetailModel(testEntry(), config.Default())
		m.SetSize(100, 30)
		m.attachMode = true
		m.addingAttachment = !extracting
		m.extractingAttachment = extracting
		m.attachPathInput.Focus()
		updated, _ := m.Update(tea.PasteMsg{Content: "/tmp/dummy attachment.txt"})
		m = updated.(DetailModel)
		if m.attachPathInput.Value() != "/tmp/dummy attachment.txt" {
			t.Fatal("attachment path paste was dropped")
		}
		if !strings.Contains(m.viewport.GetContent(), "/tmp/dummy attachment.txt") {
			t.Fatal("pasted path was not reflected in the viewport")
		}
	}
}

func TestSpaceTogglesAndPresetPaste(t *testing.T) {
	preset := NewPresetPickerModel(config.Default(), "")
	preset.mode = pickerModeCustom
	preset.customFocus = 0
	preset.lengthInput.SetValue("")
	preset.updateCustomFocus()
	updated, _ := preset.Update(tea.PasteMsg{Content: "24"})
	preset = updated.(PresetPickerModel)
	if preset.lengthInput.Value() != "24" {
		t.Fatal("custom password length paste was dropped")
	}
	preset.customFocus = 1
	before := preset.toggles[0]
	updated, _ = preset.Update(textKey(" "))
	preset = updated.(PresetPickerModel)
	if preset.toggles[0] == before {
		t.Fatal("space no longer toggles character sets")
	}
	path := NewPathInputModel("/tmp/synthetic.kdbx", "KDBX 3.1 (AES)", true)
	updated, _ = path.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	path = updated.(PathInputModel)
	before = path.upgrade
	updated, _ = path.Update(textKey(" "))
	if updated.(PathInputModel).upgrade == before {
		t.Fatal("space no longer toggles the save format")
	}
}

func TestBrowserMouseEventTypes(t *testing.T) {
	m := NewBrowserModel(createTestUIDB(t))
	m.width, m.height = 100, 30
	for _, msg := range []tea.Msg{
		tea.MouseMotionMsg{Button: tea.MouseLeft, X: 4, Y: browserHeaderRows + 1},
		tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 4, Y: browserHeaderRows + 1},
	} {
		updated, cmd := m.Update(msg)
		m = updated.(BrowserModel)
		if m.cursor != 0 || cmd != nil {
			t.Fatal("motion/release was treated as a click")
		}
	}
	updated, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 4, Y: browserHeaderRows + 1})
	m = updated.(BrowserModel)
	if m.cursor != 1 {
		t.Fatal("click did not select the second row")
	}
	_, cmd := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 4, Y: browserHeaderRows + 1})
	if cmd == nil {
		t.Fatal("click on selected entry did not open it")
	}
	if _, ok := cmd().(ViewEntryMsg); !ok {
		t.Fatal("click produced the wrong action")
	}
	updated, _ = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if updated.(BrowserModel).cursor != 0 {
		t.Fatal("mouse wheel no longer navigates")
	}
}

func TestDetailRevealFrameHeightAcrossThemes(t *testing.T) {
	t.Cleanup(func() { ApplyTheme(GetTheme("dark")) })
	for _, theme := range ThemeNames() {
		ApplyTheme(GetTheme(theme))
		for _, size := range [][2]int{{80, 24}, {120, 40}, {40, 16}} {
			entry := keepass.Entry{Title: "Synthetic 日本語 👩‍💻", Password: "dummy-秘密-🔑", Notes: "one\ntwo"}
			m := NewDetailModel(entry, config.Default())
			m.SetSize(size[0], size[1])
			m.selectedField = 1
			for range 4 {
				updated, _ := m.Update(textKey("p"))
				m = updated.(DetailModel)
				if height := lipgloss.Height(m.View().Content); height != size[1] {
					t.Fatalf("theme=%s size=%v: frame height=%d", theme, size, height)
				}
				if !m.viewport.FillHeight {
					t.Fatal("detail viewport must fill its assigned height")
				}
			}
			updated, _ := m.Update(tea.WindowSizeMsg{Width: size[0] + 10, Height: size[1] + 5})
			m = updated.(DetailModel)
			if height := lipgloss.Height(m.View().Content); height != size[1]+5 {
				t.Fatalf("resized frame height=%d, want %d", height, size[1]+5)
			}
		}
	}
}

func TestKeyReleaseDoesNotTriggerAction(t *testing.T) {
	m := NewDetailModel(testEntry(), config.Default())
	m.SetSize(80, 24)
	m.selectedField = 1
	updated, _ := m.Update(tea.KeyReleaseMsg{Code: 'p', Text: "p"})
	if updated.(DetailModel).revealedFields["builtin:password"] {
		t.Fatal("key release toggled password visibility")
	}
	// Shifted printable keys still use the existing uppercase shortcuts.
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'q', ShiftedCode: 'Q', Text: "Q", Mod: tea.ModShift})
	if cmd != nil || !updated.(DetailModel).showQR {
		t.Fatal("uppercase Q should display QR, not quit")
	}
}
