package ui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestPasteIntoAnEmptyCurrentGroupRefreshesItsFirstEntry(t *testing.T) {
	m := createTestApp(t)
	root := m.db.RootGroup()
	empty, err := m.db.AddGroup(root.UUID, "Empty destination")
	if err != nil {
		t.Fatal(err)
	}
	entry := root.Entries[0]
	m = press(t, m, RefreshMsg{})
	m = press(t, selectItem(t, m, entry.Title), letter('x'))
	m = press(t, selectItem(t, m, "Empty destination"), tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(m.browser.items) != 0 {
		t.Fatal("the destination fixture is not empty")
	}
	m = press(t, m, letter('v'))
	parent, err := m.db.FindEntryParentUUID(entry.UUID)
	if err != nil || !parent.Compare(empty) || len(m.browser.items) != 1 || !m.browser.items[0].entry.UUID.Compare(entry.UUID) {
		t.Fatal("pasting into an empty group did not show the moved entry")
	}
	if m.browser.cutEntry != nil || !m.dirty || m.undoStack.length() != 1 {
		t.Fatal("successful move did not clear the cut selection and record an undo")
	}
	m = pressUndo(t, m)
	parent, err = m.db.FindEntryParentUUID(entry.UUID)
	if err != nil || !parent.Compare(root.UUID) {
		t.Fatal("undo did not return the entry to its original group")
	}
}

func TestRejectedDescendantPasteKeepsCutGroupAvailableForRetry(t *testing.T) {
	m := openUndoFixture(t, writeUndoFixture(t, false))
	root := m.db.RootGroup()
	peer, err := m.db.AddGroup(root.UUID, "Peer")
	if err != nil {
		t.Fatal(err)
	}
	m = press(t, m, RefreshMsg{})
	before := attachmentContents(t, m.db)
	group := findGroupUUID(t, root, "G")
	m = press(t, selectItem(t, m, "G"), letter('x'))
	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m = press(t, selectItem(t, m, "S"), letter('v'))
	if !m.statusError || !strings.HasPrefix(m.statusText, "Error moving group:") || m.dirty || m.undoStack.length() != 0 {
		t.Fatal("moving a group into its descendant should fail without recording a mutation")
	}
	if m.browser.cutGroup == nil || !m.browser.cutGroup.Compare(group) {
		t.Fatal("a failed paste lost the group being moved")
	}
	if !reflect.DeepEqual(before, attachmentContents(t, m.db)) {
		t.Fatal("a rejected move changed the tree or its attachments")
	}
	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	m = press(t, selectItem(t, m, "Peer"), letter('v'))
	parent, err := m.db.FindGroupParentUUID(group)
	if err != nil || !parent.Compare(peer) || m.browser.cutGroup != nil || !m.dirty || m.undoStack.length() != 1 {
		t.Fatal("the retained cut group could not be pasted into a valid destination")
	}
	if m.db.EntryCount() != 4 {
		t.Fatal("moving the recovered group lost one of its entries")
	}
}
