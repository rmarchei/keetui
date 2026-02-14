package ui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/keepass"
)

// createTestApp returns an AppModel already in the browser state with a test DB.
func createTestApp(t *testing.T) AppModel {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testmaster"

	db, err := keepass.Create(dbPath, password)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	root := db.RootGroup()
	if _, err := db.AddGroup(root.UUID, "Email"); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}

	root = db.RootGroup()
	emailGroup := root.Groups[0]
	if _, err := db.AddEntry(emailGroup.UUID, keepass.Entry{
		Title:    "Gmail",
		UserName: "user@gmail.com",
		Password: "gmailpass",
		URL:      "https://gmail.com",
	}); err != nil {
		t.Fatalf("AddEntry: %v", err)
	}
	if _, err := db.AddEntry(root.UUID, keepass.Entry{
		Title:    "Root Entry",
		UserName: "rootuser",
		Password: "rootpass",
	}); err != nil {
		t.Fatalf("AddEntry: %v", err)
	}

	cfg := config.Default()
	m := NewApp(dbPath, "", cfg)
	m.db = db
	m.browser = NewBrowserModel(db)
	m.state = viewBrowser
	m.width = 80
	m.height = 24
	m.browser.width = 80
	m.browser.height = 24
	return m
}

func TestAppInitialState(t *testing.T) {
	m := NewApp("/tmp/test.kdbx", "", config.Default())
	if m.state != viewUnlock {
		t.Errorf("initial state = %d, want %d (viewUnlock)", m.state, viewUnlock)
	}
}

func TestAppViewEntryTransition(t *testing.T) {
	m := createTestApp(t)

	// Navigate to entry and send ViewEntryMsg
	entry := keepass.Entry{
		Title:    "Test",
		UserName: "user",
		Password: "pass",
	}
	updated, _ := m.Update(ViewEntryMsg{Entry: entry})
	m = updated.(AppModel)

	if m.state != viewDetail {
		t.Errorf("state after ViewEntryMsg = %d, want %d (viewDetail)", m.state, viewDetail)
	}
}

func TestAppEditEntryTransition(t *testing.T) {
	m := createTestApp(t)

	entry := keepass.Entry{
		Title:    "Test",
		UserName: "user",
		Password: "pass",
	}
	updated, _ := m.Update(EditEntryMsg{Entry: entry})
	m = updated.(AppModel)

	if m.state != viewEditor {
		t.Errorf("state after EditEntryMsg = %d, want %d (viewEditor)", m.state, viewEditor)
	}
}

func TestAppNewEntryTransition(t *testing.T) {
	m := createTestApp(t)

	root := m.db.RootGroup()
	updated, _ := m.Update(NewEntryMsg{GroupUUID: root.UUID})
	m = updated.(AppModel)

	if m.state != viewEditor {
		t.Errorf("state after NewEntryMsg = %d, want %d (viewEditor)", m.state, viewEditor)
	}
}

func TestAppBackToBrowserTransition(t *testing.T) {
	m := createTestApp(t)

	// Go to detail
	entry := keepass.Entry{Title: "Test", Password: "pass"}
	updated, _ := m.Update(ViewEntryMsg{Entry: entry})
	m = updated.(AppModel)
	if m.state != viewDetail {
		t.Fatalf("state = %d, want viewDetail", m.state)
	}

	// Go back
	updated, _ = m.Update(BackToBrowserMsg{})
	m = updated.(AppModel)
	if m.state != viewBrowser {
		t.Errorf("state after BackToBrowserMsg = %d, want %d (viewBrowser)", m.state, viewBrowser)
	}
}

func TestAppCancelEditTransition(t *testing.T) {
	m := createTestApp(t)

	// Go to editor
	entry := keepass.Entry{Title: "Test", Password: "pass"}
	updated, _ := m.Update(EditEntryMsg{Entry: entry})
	m = updated.(AppModel)
	if m.state != viewEditor {
		t.Fatalf("state = %d, want viewEditor", m.state)
	}

	// Cancel
	updated, _ = m.Update(CancelEditMsg{})
	m = updated.(AppModel)
	if m.state != viewBrowser {
		t.Errorf("state after CancelEditMsg = %d, want %d (viewBrowser)", m.state, viewBrowser)
	}
}

func TestAppShowStatsTransition(t *testing.T) {
	m := createTestApp(t)

	updated, _ := m.Update(ShowStatsMsg{})
	m = updated.(AppModel)

	if m.state != viewStats {
		t.Errorf("state after ShowStatsMsg = %d, want %d (viewStats)", m.state, viewStats)
	}
}

func TestAppShowAuditTransition(t *testing.T) {
	m := createTestApp(t)

	updated, _ := m.Update(ShowAuditMsg{})
	m = updated.(AppModel)

	if m.state != viewAudit {
		t.Errorf("state after ShowAuditMsg = %d, want %d (viewAudit)", m.state, viewAudit)
	}
}

func TestAppShowHistoryTransition(t *testing.T) {
	m := createTestApp(t)

	// First add an entry with history
	root := m.db.RootGroup()
	entries := root.Entries
	if len(entries) == 0 {
		t.Fatal("expected at least one entry in root")
	}

	updated, _ := m.Update(ShowHistoryMsg{
		EntryTitle: entries[0].Title,
		EntryUUID:  entries[0].UUID,
	})
	m = updated.(AppModel)

	if m.state != viewHistory {
		t.Errorf("state after ShowHistoryMsg = %d, want %d (viewHistory)", m.state, viewHistory)
	}
}

func TestAppBackToDetailTransition(t *testing.T) {
	m := createTestApp(t)

	// Go to history view first
	root := m.db.RootGroup()
	entries := root.Entries
	if len(entries) == 0 {
		t.Fatal("expected at least one entry in root")
	}
	updated, _ := m.Update(ShowHistoryMsg{
		EntryTitle: entries[0].Title,
		EntryUUID:  entries[0].UUID,
	})
	m = updated.(AppModel)
	if m.state != viewHistory {
		t.Fatalf("state = %d, want viewHistory", m.state)
	}

	// Back to detail
	updated, _ = m.Update(BackToDetailMsg{})
	m = updated.(AppModel)
	if m.state != viewDetail {
		t.Errorf("state after BackToDetailMsg = %d, want %d (viewDetail)", m.state, viewDetail)
	}
}

func TestAppDirtyFlagOnSaveEntry(t *testing.T) {
	m := createTestApp(t)

	if m.dirty {
		t.Error("should not be dirty initially")
	}

	root := m.db.RootGroup()
	updated, _ := m.Update(SaveEntryMsg{
		IsNew:     true,
		GroupUUID: root.UUID,
		Entry: keepass.Entry{
			Title:    "New Entry",
			UserName: "user",
			Password: "pass",
		},
	})
	m = updated.(AppModel)

	if !m.dirty {
		t.Error("should be dirty after saving an entry")
	}
	if m.state != viewBrowser {
		t.Errorf("state = %d, want viewBrowser", m.state)
	}
}

func TestAppDirtyFlagOnDuplicate(t *testing.T) {
	m := createTestApp(t)

	root := m.db.RootGroup()
	entries := root.Entries
	if len(entries) == 0 {
		t.Fatal("expected at least one entry in root")
	}

	updated, _ := m.Update(DuplicateEntryMsg{EntryUUID: entries[0].UUID})
	m = updated.(AppModel)

	if !m.dirty {
		t.Error("should be dirty after duplicating an entry")
	}
}

func TestAppQuitWithoutDirty(t *testing.T) {
	m := createTestApp(t)

	_, cmd := m.Update(RequestQuitMsg{})
	if cmd == nil {
		t.Fatal("RequestQuitMsg should produce a command")
	}
	// Should quit directly since not dirty
}

func TestAppQuitWithDirty(t *testing.T) {
	m := createTestApp(t)
	m.dirty = true

	updated, _ := m.Update(RequestQuitMsg{})
	m = updated.(AppModel)

	if m.state != viewConfirm {
		t.Errorf("state = %d, want %d (viewConfirm) when dirty", m.state, viewConfirm)
	}
}

func TestAppQuitConfirmed(t *testing.T) {
	m := createTestApp(t)

	_, cmd := m.Update(quitMsg{})
	if cmd == nil {
		t.Fatal("quitMsg should produce a quit command")
	}
}

func TestAppDeleteEntryConfirm(t *testing.T) {
	m := createTestApp(t)

	root := m.db.RootGroup()
	entries := root.Entries
	if len(entries) == 0 {
		t.Fatal("expected at least one entry")
	}

	updated, _ := m.Update(DeleteEntryMsg{EntryUUID: entries[0].UUID})
	m = updated.(AppModel)

	if m.state != viewConfirm {
		t.Errorf("state = %d, want %d (viewConfirm) for delete", m.state, viewConfirm)
	}
}

func TestAppConfirmDeleteEntry(t *testing.T) {
	m := createTestApp(t)

	root := m.db.RootGroup()
	entries := root.Entries
	if len(entries) == 0 {
		t.Fatal("expected at least one entry")
	}

	updated, _ := m.Update(confirmDeleteEntryMsg{UUID: entries[0].UUID})
	m = updated.(AppModel)

	if !m.dirty {
		t.Error("should be dirty after deleting an entry")
	}
	if m.state != viewBrowser {
		t.Errorf("state = %d, want viewBrowser", m.state)
	}
}

func TestAppSaveDatabase(t *testing.T) {
	m := createTestApp(t)
	m.dirty = true

	updated, cmd := m.Update(SaveMsg{})
	m = updated.(AppModel)

	if m.dirty {
		t.Error("should not be dirty after saving")
	}
	if cmd == nil {
		t.Fatal("SaveMsg should produce a status command")
	}
}

func TestAppWindowResize(t *testing.T) {
	m := createTestApp(t)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(AppModel)

	if m.width != 120 || m.height != 40 {
		t.Errorf("size = %dx%d, want 120x40", m.width, m.height)
	}
}

func TestAppNewGroupTransition(t *testing.T) {
	m := createTestApp(t)

	root := m.db.RootGroup()
	updated, _ := m.Update(NewGroupMsg{ParentUUID: root.UUID})
	m = updated.(AppModel)

	if m.state != viewGroupInput {
		t.Errorf("state = %d, want %d (viewGroupInput)", m.state, viewGroupInput)
	}
}

func TestAppSaveGroup(t *testing.T) {
	m := createTestApp(t)

	root := m.db.RootGroup()
	updated, _ := m.Update(SaveGroupMsg{
		ParentUUID: root.UUID,
		Name:       "NewGroup",
	})
	m = updated.(AppModel)

	if !m.dirty {
		t.Error("should be dirty after saving a group")
	}
	if m.state != viewBrowser {
		t.Errorf("state = %d, want viewBrowser", m.state)
	}
}

func TestAppCancelEditReturnsToReturnState(t *testing.T) {
	m := createTestApp(t)

	// Set up a return state (like going to confirm from detail)
	m.state = viewConfirm
	m.returnState = viewDetail

	updated, _ := m.Update(CancelEditMsg{})
	m = updated.(AppModel)

	if m.state != viewDetail {
		t.Errorf("state = %d, want viewDetail (returnState)", m.state)
	}
	if m.returnState != 0 {
		t.Errorf("returnState = %d, want 0 (cleared)", m.returnState)
	}
}

func TestAppShowSettingsTransition(t *testing.T) {
	m := createTestApp(t)

	updated, _ := m.Update(ShowSettingsMsg{})
	m = updated.(AppModel)

	if m.state != viewSettings {
		t.Errorf("state after ShowSettingsMsg = %d, want %d (viewSettings)", m.state, viewSettings)
	}
}

func TestAppViewDelegation(t *testing.T) {
	m := createTestApp(t)

	// Ensure the app delegates key messages to the active browser view
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(AppModel)

	if m.browser.cursor != 1 {
		t.Errorf("browser cursor = %d, want 1 (delegation should work)", m.browser.cursor)
	}
}

func TestAppChangePasswordTransition(t *testing.T) {
	m := createTestApp(t)

	updated, _ := m.Update(ChangePasswordStartMsg{})
	m = updated.(AppModel)

	if m.state != viewPassword {
		t.Errorf("state = %d, want %d (viewPassword)", m.state, viewPassword)
	}
}

func TestAppChangePasswordDirty(t *testing.T) {
	m := createTestApp(t)

	updated, _ := m.Update(ChangePasswordMsg{NewPassword: "newpass"})
	m = updated.(AppModel)

	if !m.dirty {
		t.Error("should be dirty after changing password")
	}
	if m.state != viewBrowser {
		t.Errorf("state = %d, want viewBrowser", m.state)
	}
}

func TestStatusBarContent(t *testing.T) {
	m := createTestApp(t)
	m.refreshEntryCount()

	bar := m.statusBar()
	if !strings.Contains(bar, "test.kdbx") {
		t.Error("status bar should contain filename")
	}
	if !strings.Contains(bar, "entries") {
		t.Error("status bar should contain entry count")
	}
	if strings.Contains(bar, "[modified]") {
		t.Error("status bar should not show [modified] when not dirty")
	}

	m.dirty = true
	bar = m.statusBar()
	if !strings.Contains(bar, "[modified]") {
		t.Error("status bar should show [modified] when dirty")
	}
}

func TestStatusBarNotOnUnlock(t *testing.T) {
	m := NewApp("/tmp/test.kdbx", "", config.Default())
	m.width = 80
	m.height = 24

	view := m.View()
	if strings.Contains(view, "entries") {
		t.Error("unlock view should not contain status bar")
	}
}

func TestStatusBarOnBrowser(t *testing.T) {
	m := createTestApp(t)
	m.refreshEntryCount()

	view := m.View()
	if !strings.Contains(view, "test.kdbx") {
		t.Error("browser view should contain status bar with filename")
	}
}

func TestUndoDeleteEntry(t *testing.T) {
	m := createTestApp(t)

	root := m.db.RootGroup()
	entries := root.Entries
	if len(entries) == 0 {
		t.Fatal("expected at least one entry")
	}
	entryUUID := entries[0].UUID
	entryTitle := entries[0].Title

	// Delete the entry
	updated, _ := m.Update(confirmDeleteEntryMsg{UUID: entryUUID})
	m = updated.(AppModel)

	if !m.dirty {
		t.Fatal("should be dirty after delete")
	}
	if m.undoStack.length() != 1 {
		t.Fatalf("undo stack length = %d, want 1", m.undoStack.length())
	}

	// Verify entry is gone
	root = m.db.RootGroup()
	for _, e := range root.Entries {
		if e.Title == entryTitle {
			t.Fatal("entry should be deleted")
		}
	}

	// Undo
	m2, _ := m.handleUndo()
	m = m2.(AppModel)

	if m.undoStack.length() != 0 {
		t.Errorf("undo stack length = %d, want 0 after undo", m.undoStack.length())
	}

	// Verify entry is restored
	root = m.db.RootGroup()
	found := false
	for _, e := range root.Entries {
		if e.Title == entryTitle {
			found = true
			break
		}
	}
	if !found {
		t.Error("entry should be restored after undo")
	}
}

func TestUndoEditEntry(t *testing.T) {
	m := createTestApp(t)

	root := m.db.RootGroup()
	emailGroup := root.Groups[0]
	entries := emailGroup.Entries
	if len(entries) == 0 {
		t.Fatal("expected at least one entry in email group")
	}
	entryUUID := entries[0].UUID
	originalTitle := entries[0].Title

	// Edit the entry
	updated, _ := m.Update(SaveEntryMsg{
		IsNew:     false,
		EntryUUID: entryUUID,
		Entry: keepass.Entry{
			Title:    "Changed Title",
			UserName: entries[0].UserName,
			Password: entries[0].Password,
		},
	})
	m = updated.(AppModel)

	if m.undoStack.length() != 1 {
		t.Fatalf("undo stack length = %d, want 1", m.undoStack.length())
	}

	// Verify edit took effect
	entry, _ := m.db.GetEntry(entryUUID)
	if entry.Title != "Changed Title" {
		t.Fatalf("title = %q, want %q", entry.Title, "Changed Title")
	}

	// Undo
	m2, _ := m.handleUndo()
	m = m2.(AppModel)

	// Verify original title restored
	entry, _ = m.db.GetEntry(entryUUID)
	if entry.Title != originalTitle {
		t.Errorf("title after undo = %q, want %q", entry.Title, originalTitle)
	}
}

func TestUndoRenameGroup(t *testing.T) {
	m := createTestApp(t)

	root := m.db.RootGroup()
	emailGroup := root.Groups[0]
	originalName := emailGroup.Name

	// Rename the group
	updated, _ := m.Update(RenameGroupMsg{
		GroupUUID: emailGroup.UUID,
		NewName:   "Renamed Email",
	})
	m = updated.(AppModel)

	if m.undoStack.length() != 1 {
		t.Fatalf("undo stack length = %d, want 1", m.undoStack.length())
	}

	// Undo
	m2, _ := m.handleUndo()
	m = m2.(AppModel)

	// Verify name restored
	root = m.db.RootGroup()
	if root.Groups[0].Name != originalName {
		t.Errorf("name after undo = %q, want %q", root.Groups[0].Name, originalName)
	}
}

func TestUndoDuplicateEntry(t *testing.T) {
	m := createTestApp(t)

	root := m.db.RootGroup()
	entries := root.Entries
	if len(entries) == 0 {
		t.Fatal("expected at least one entry")
	}
	originalCount := len(entries)

	// Duplicate
	updated, _ := m.Update(DuplicateEntryMsg{EntryUUID: entries[0].UUID})
	m = updated.(AppModel)

	root = m.db.RootGroup()
	if len(root.Entries) != originalCount+1 {
		t.Fatalf("entries = %d, want %d", len(root.Entries), originalCount+1)
	}

	// Undo
	m2, _ := m.handleUndo()
	m = m2.(AppModel)

	root = m.db.RootGroup()
	if len(root.Entries) != originalCount {
		t.Errorf("entries after undo = %d, want %d", len(root.Entries), originalCount)
	}
}

func TestUndoNothingToUndo(t *testing.T) {
	m := createTestApp(t)

	_, cmd := m.handleUndo()
	if cmd == nil {
		t.Fatal("handleUndo should return a status command")
	}
	msg := cmd()
	statusMsg, ok := msg.(StatusMsg)
	if !ok {
		t.Fatalf("expected StatusMsg, got %T", msg)
	}
	if !strings.Contains(statusMsg.Text, "Nothing to undo") {
		t.Errorf("status = %q, want 'Nothing to undo'", statusMsg.Text)
	}
}

func TestUndoStackClearedOnDatabaseOpen(t *testing.T) {
	m := createTestApp(t)

	// Push something
	m.undoStack.push(undoAction{description: "test"})
	if m.undoStack.length() != 1 {
		t.Fatal("stack should have 1 item")
	}

	// Simulate database open
	m.undoStack.clear()
	if m.undoStack.length() != 0 {
		t.Error("stack should be empty after clear")
	}
}
