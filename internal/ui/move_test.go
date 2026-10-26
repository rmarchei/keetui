package ui

import (
	"maps"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rmarchei/keetui/internal/config"
)

// press sends a key and delivers what its commands answer at once (a
// paste, the refresh and status after it), as the program loop would.
func press(t *testing.T, m AppModel, key tea.KeyPressMsg) AppModel {
	t.Helper()
	updated, cmd := m.Update(key)
	m = updated.(AppModel)
	for depth := 0; cmd != nil && depth < 3; depth++ {
		var next []tea.Cmd
		for _, msg := range quickMsgs(cmd) {
			switch msg.(type) {
			case PasteEntryMsg, PasteGroupMsg, RefreshMsg, StatusMsg:
				updated, c := m.Update(msg)
				m = updated.(AppModel)
				next = append(next, c)
			}
		}
		cmd = tea.Batch(next...)
	}
	return m
}

func letter(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

// selectItem moves the browser's cursor to the group or entry named.
func selectItem(t *testing.T, m AppModel, name string) AppModel {
	t.Helper()
	for i, it := range m.browser.items {
		if it.isGroup && it.group.Name == name || !it.isGroup && it.entry.Title == name {
			for m.browser.cursor < i {
				m = press(t, m, letter('j'))
			}
			for m.browser.cursor > i {
				m = press(t, m, letter('k'))
			}
			return m
		}
	}
	t.Fatalf("%q is not in the list", name)
	return m
}

// Cut and paste move an entry into a group and a group out of its parent;
// the footer offers the paste once something is cut, and the status names
// the save key. Attachments, a history entry's included, stay with what
// moved through a save, and undoing both moves puts everything back.
func TestCutAndPasteMoveKeepsAttachments(t *testing.T) {
	for _, kdbx4 := range []bool{false, true} {
		m := openUndoFixture(t, writeUndoFixture(t, kdbx4))
		m.width, m.height, m.browser.width, m.browser.height = 160, 30, 160, 29
		if footer := ansi.Strip(m.browser.footerView()); !strings.Contains(footer, "x move") || strings.Contains(footer, "paste here") {
			t.Fatalf("footer before cutting: %q", footer)
		}

		m = press(t, selectItem(t, m, "A"), letter('x'))
		if footer := ansi.Strip(m.browser.footerView()); !strings.Contains(footer, "v paste here") {
			t.Errorf("footer after cutting: %q", footer)
		}
		m = press(t, selectItem(t, m, "G"), tea.KeyPressMsg{Code: tea.KeyEnter})
		m = press(t, m, letter('v'))
		if !m.dirty || m.statusText != "Entry moved (use 's' to save to file)" {
			t.Fatalf("after pasting A: dirty %v, status %q", m.dirty, m.statusText)
		}

		m = press(t, selectItem(t, m, "S"), letter('x'))
		m = press(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
		m = press(t, m, letter('v'))
		if m.statusText != "Group moved (use 's' to save to file)" {
			t.Fatalf("after pasting S: status %q", m.statusText)
		}

		moved := maps.Clone(undoFixtureAttachments)
		delete(moved, "A:a.txt")
		delete(moved, "G/S/D:d.txt")
		moved["G/A:a.txt"], moved["S/D:d.txt"] = "attachment of A", "attachment of D"
		m = saveInPlace(t, m)
		assertSavedAttachmentsEqual(t, m.filePath, moved)

		m = saveInPlace(t, pressUndo(t, pressUndo(t, m)))
		assertSavedAttachments(t, m.filePath)
	}
}

// The reminder to save names the save key as bound.
func TestUnsavedStatusNamesTheSaveKey(t *testing.T) {
	m := createTestApp(t)
	m.cfg.KeyBindingsPtr = &config.KeyBindings{Save: "w"}
	if got := quickStatus(m.unsavedStatus("Entry moved")); got != "Entry moved (use 'w' to save to file)" {
		t.Errorf("status = %q", got)
	}
}
