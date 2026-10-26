package ui

import (
	"maps"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rmarchei/keetui/internal/config"
)

// press sends a key (or another message) and delivers what its commands
// answer at once (a paste, the refresh and status after it), as the
// program loop would.
func press(t *testing.T, m AppModel, msg tea.Msg) AppModel {
	t.Helper()
	updated, cmd := m.Update(msg)
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

// footer is the browser's hint row as shown.
func footer(m AppModel) string { return ansi.Strip(m.browser.footerView()) }

// v pastes into the selected group without opening it, V into the group
// shown; the footer says which applies. Attachments, a history entry's
// included, stay with what moved through a save, and undoing both moves
// puts everything back.
func TestCutAndPasteMoveKeepsAttachments(t *testing.T) {
	for _, kdbx4 := range []bool{false, true} {
		m := openUndoFixture(t, writeUndoFixture(t, kdbx4))
		m.width, m.height, m.browser.width, m.browser.height = 160, 30, 160, 29
		if f := footer(m); !strings.Contains(f, "x move") || strings.Contains(f, "paste") {
			t.Fatalf("footer before cutting: %q", f)
		}

		// A onto G, from the top level.
		m = press(t, selectItem(t, m, "A"), letter('x'))
		if f := footer(m); !strings.Contains(f, "v paste here") || strings.Contains(f, "V paste here") {
			t.Errorf("footer with an entry selected: %q", f)
		}
		m = selectItem(t, m, "G")
		if f := footer(m); !strings.Contains(f, "v paste into G") || !strings.Contains(f, "V paste here") {
			t.Errorf("footer with a group selected: %q", f)
		}
		m = press(t, m, letter('v'))
		if !m.dirty || m.statusText != "Entry moved to G (use 's' to save to file)" {
			t.Fatalf("after pasting A: dirty %v, status %q", m.dirty, m.statusText)
		}

		// S out of G, onto the top level while a group is selected there.
		m = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
		m = press(t, selectItem(t, m, "S"), letter('x'))
		m = press(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
		m = press(t, selectItem(t, m, "G"), letter('V'))
		root := m.db.RootGroup().Name
		if m.statusText != "Group moved to "+root+" (use 's' to save to file)" {
			t.Fatalf("after pasting S here: status %q", m.statusText)
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

// A cut group pasted onto itself goes to the group shown instead.
func TestPasteGroupOntoItselfPastesHere(t *testing.T) {
	m := openUndoFixture(t, writeUndoFixture(t, false))
	m.width, m.browser.width = 160, 160
	m = press(t, selectItem(t, m, "G"), tea.KeyPressMsg{Code: tea.KeyEnter})
	m = press(t, selectItem(t, m, "S"), letter('x'))
	if f := footer(m); !strings.Contains(f, "v paste here") || strings.Contains(f, "paste into") {
		t.Errorf("footer with the cut group selected: %q", f)
	}
	m = press(t, m, letter('v'))
	if m.statusText != "Group moved to G (use 's' to save to file)" {
		t.Errorf("status = %q", m.statusText)
	}
}

// Nothing is pasted into the recycle bin, selected or shown: delete moves
// items there.
func TestPasteRefusesTheRecycleBin(t *testing.T) {
	m := openUndoFixture(t, writeUndoFixture(t, false))
	m.db.Database.Content.Meta.RecycleBinEnabled.Bool = true
	m = press(t, m, confirmDeleteEntryMsg{UUID: findEntryUUID(t, m.db.RootGroup(), "B")})
	bin := ""
	for _, it := range m.browser.items {
		if it.isGroup && m.db.IsRecycleBin(it.group.UUID) {
			bin = it.group.Name
		}
	}
	if bin == "" {
		t.Fatal("no recycle bin in the list")
	}
	m = press(t, selectItem(t, m, "A"), letter('x'))
	m = press(t, selectItem(t, m, bin), letter('v'))
	if !m.statusError || !strings.Contains(m.statusText, "recycle bin") {
		t.Errorf("status = %q (error %v)", m.statusText, m.statusError)
	}
	if parent, _ := m.db.FindEntryParentUUID(findEntryUUID(t, m.db.RootGroup(), "A")); !parent.Compare(m.db.RootGroup().UUID) {
		t.Error("A was moved")
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
