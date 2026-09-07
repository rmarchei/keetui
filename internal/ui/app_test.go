package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

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
	m.clipboard = &testClipboard{}
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

func TestAppQuitWithoutDirtyWhenConfirmationDisabled(t *testing.T) {
	m := createTestApp(t)
	disabled := false
	m.cfg.ConfirmQuit = &disabled

	_, cmd := m.Update(RequestQuitMsg{})
	if cmd == nil {
		t.Fatal("RequestQuitMsg should produce a command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("should quit directly when clean and confirmation is disabled")
	}
}

func TestAppQuitWithDirty(t *testing.T) {
	m := createTestApp(t)
	m.dirty = true
	disabled := false
	m.cfg.ConfirmQuit = &disabled

	updated, _ := m.Update(RequestQuitMsg{})
	m = updated.(AppModel)

	if m.state != viewConfirm {
		t.Errorf("state = %d, want %d (viewConfirm) when dirty", m.state, viewConfirm)
	}
	if !strings.Contains(m.confirm.message, "unsaved changes") {
		t.Error("disabling quit confirmation must not disable the unsaved-changes warning")
	}
}

func TestAppQuitKeyConfirmsByDefault(t *testing.T) {
	for _, response := range []string{"n", "esc", "enter", "y"} {
		t.Run(response, func(t *testing.T) {
			m := createTestApp(t)
			m.browser.cursor = 1
			updated, cmd := m.Update(textKey("q"))
			m = updated.(AppModel)
			if cmd == nil {
				t.Fatal("q should request quitting")
			}
			updated, cmd = m.Update(cmd())
			m = updated.(AppModel)
			if m.state != viewConfirm || cmd != nil || m.confirm.selected {
				t.Fatal("q should show confirmation with No selected, without quitting")
			}
			key := textKey(response)
			switch response {
			case "esc":
				key = tea.KeyPressMsg{Code: tea.KeyEsc}
			case "enter":
				key = tea.KeyPressMsg{Code: tea.KeyEnter}
			}
			updated, cmd = m.Update(key)
			m = updated.(AppModel)
			if cmd == nil {
				t.Fatal("confirmation response should produce an action")
			}
			updated, cmd = m.Update(cmd())
			m = updated.(AppModel)
			if response == "y" {
				if cmd == nil {
					t.Fatal("confirming should quit")
				}
				if _, ok := cmd().(tea.QuitMsg); !ok {
					t.Fatal("confirmation did not produce the quit command")
				}
			} else if m.state != viewBrowser || m.browser.cursor != 1 || cmd != nil {
				t.Fatal("canceling should restore the browser and its selection")
			}
		})
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

	// Save now goes through path input
	updated, _ := m.Update(SaveAsStartMsg{})
	m = updated.(AppModel)
	if m.state != viewPathInput {
		t.Fatalf("state = %d, want viewPathInput", m.state)
	}

	// Confirm save to same path
	updated, cmd := m.Update(SaveAsMsg{Path: m.filePath})
	m = updated.(AppModel)
	m = completeSave(t, m, cmd)

	if m.dirty {
		t.Error("should not be dirty after saving")
	}
	if m.state != viewBrowser {
		t.Fatalf("state = %d, want viewBrowser", m.state)
	}
}

func TestAsyncUnlockFlow(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	if _, err := keepass.Create(dbPath, "pw"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	m := NewApp(dbPath, "", config.Default())
	updated, cmd := m.Update(DatabaseOpenedMsg{FilePath: dbPath, Password: "pw"})
	m = updated.(AppModel)
	if !m.unlock.unlocking {
		t.Fatal("unlocking indicator should be set while the open runs")
	}
	if cmd == nil {
		t.Fatal("expected an async open command")
	}
	result, ok := cmd().(databaseOpenResultMsg)
	if !ok {
		t.Fatalf("expected databaseOpenResultMsg, got %T", cmd())
	}
	updated, _ = m.Update(result)
	m = updated.(AppModel)
	if m.state != viewBrowser || m.db == nil {
		t.Errorf("state = %d, db nil = %v; want browser with open db", m.state, m.db == nil)
	}

	// Wrong password: the error surfaces and the indicator clears.
	m2 := NewApp(dbPath, "", config.Default())
	updated, cmd = m2.Update(DatabaseOpenedMsg{FilePath: dbPath, Password: "wrong"})
	m2 = updated.(AppModel)
	result = cmd().(databaseOpenResultMsg)
	updated, _ = m2.Update(result)
	m2 = updated.(AppModel)
	if m2.state != viewUnlock || m2.unlock.err == nil {
		t.Error("failed unlock must stay on the unlock screen with an error")
	}
	if m2.unlock.unlocking {
		t.Error("unlocking indicator must clear after a failed open")
	}
}

// completeSave executes a pending async save command and delivers its
// result message, as the Bubble Tea runtime would.
func completeSave(t *testing.T, m AppModel, cmd tea.Cmd) AppModel {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected an async save command")
	}
	msg, ok := cmd().(saveResultMsg)
	if !ok {
		t.Fatalf("expected saveResultMsg, got %T", msg)
	}
	updated, _ := m.Update(msg)
	return updated.(AppModel)
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
	m.confirm = NewConfirmModel(viewDetail, "Confirm", nil)

	updated, _ := m.Update(CancelEditMsg{})
	m = updated.(AppModel)

	if m.state != viewDetail {
		t.Errorf("state = %d, want viewDetail (returnState)", m.state)
	}
	if m.confirm.returnState != viewNone {
		t.Errorf("returnState = %d, want viewNone (cleared)", m.confirm.returnState)
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
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j', Text: string('j')})
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

func TestSaveAsOverwriteConfirmation(t *testing.T) {
	m := createTestApp(t)
	dir := t.TempDir()

	// Saving over a different existing file must ask first.
	existing := filepath.Join(dir, "other.kdbx")
	if err := os.WriteFile(existing, []byte("another vault"), 0600); err != nil {
		t.Fatalf("write existing: %v", err)
	}
	updated, _ := m.Update(SaveAsMsg{Path: existing})
	m = updated.(AppModel)
	if m.state != viewConfirm {
		t.Fatalf("state = %d, want viewConfirm before overwriting an existing file", m.state)
	}

	// Confirming performs the save.
	updated, cmd := m.Update(confirmSaveAsMsg{Path: existing})
	m = updated.(AppModel)
	m = completeSave(t, m, cmd)
	if m.state != viewBrowser {
		t.Errorf("state after confirm = %d, want viewBrowser", m.state)
	}
	if m.filePath != existing {
		t.Errorf("filePath = %q, want %q after confirmed save", m.filePath, existing)
	}

	// A fresh path saves without any prompt.
	fresh := filepath.Join(dir, "fresh.kdbx")
	updated, cmd = m.Update(SaveAsMsg{Path: fresh})
	m = updated.(AppModel)
	if m.state != viewBrowser {
		t.Errorf("state = %d, want viewBrowser (no prompt for a new file)", m.state)
	}
	m = completeSave(t, m, cmd)
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("expected database saved at %s: %v", fresh, err)
	}
}

func TestExtractAttachmentOverwriteConfirmation(t *testing.T) {
	m := createTestApp(t)
	root := m.db.RootGroup()
	uuid, err := m.db.AddEntry(root.UUID, keepass.Entry{Title: "WithAttachment"})
	if err != nil {
		t.Fatalf("AddEntry: %v", err)
	}
	if err := m.db.AddAttachment(uuid, "notes.txt", []byte("attachment data")); err != nil {
		t.Fatalf("AddAttachment: %v", err)
	}

	dir := t.TempDir()
	dest := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(dest, []byte("unrelated existing file"), 0600); err != nil {
		t.Fatalf("write existing: %v", err)
	}

	// Extracting onto an existing file must ask first and not touch it.
	updated, _ := m.Update(ExtractAttachmentMsg{EntryUUID: uuid, Name: "notes.txt", DestPath: dest})
	m = updated.(AppModel)
	if m.state != viewConfirm {
		t.Fatalf("state = %d, want viewConfirm before overwriting", m.state)
	}
	data, _ := os.ReadFile(dest)
	if string(data) != "unrelated existing file" {
		t.Fatal("file must not be overwritten before confirmation")
	}

	// Confirming overwrites.
	updated, _ = m.Update(confirmExtractAttachmentMsg{EntryUUID: uuid, Name: "notes.txt", DestPath: dest})
	m = updated.(AppModel)
	data, _ = os.ReadFile(dest)
	if string(data) != "attachment data" {
		t.Errorf("file content = %q, want extracted attachment after confirm", data)
	}

	// A fresh destination extracts without any prompt.
	fresh := filepath.Join(dir, "fresh.txt")
	updated, _ = m.Update(ExtractAttachmentMsg{EntryUUID: uuid, Name: "notes.txt", DestPath: fresh})
	m = updated.(AppModel)
	if m.state == viewConfirm {
		t.Error("no prompt expected for a new destination file")
	}
	data, _ = os.ReadFile(fresh)
	if string(data) != "attachment data" {
		t.Errorf("fresh file content = %q, want extracted attachment", data)
	}
}

func TestConfirmCtrlCRequestsQuit(t *testing.T) {
	c := NewConfirmModel(viewBrowser, "Delete?", nil)
	_, cmd := c.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c should produce a command")
	}
	if _, ok := cmd().(RequestQuitMsg); !ok {
		t.Errorf("ctrl+c in confirm = %T, want RequestQuitMsg (unsaved-changes prompt must apply)", cmd())
	}
}

func TestTOTPTickLifecycle(t *testing.T) {
	m := createTestApp(t)
	root := m.db.RootGroup()
	uuid, err := m.db.AddEntry(root.UUID, keepass.Entry{Title: "OTP", TOTP: "JBSWY3DPEHPK3PXP"})
	if err != nil {
		t.Fatalf("AddEntry: %v", err)
	}
	entry, err := m.db.GetEntry(uuid)
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}

	// Opening the entry arms a tick chain.
	updated, cmd := m.Update(ViewEntryMsg{Entry: entry})
	m = updated.(AppModel)
	if cmd == nil {
		t.Fatal("viewing a TOTP entry must start the tick chain")
	}
	firstSeq := m.detail.totpSeq

	// Returning from a sub-view restarts the chain under a new sequence.
	updated, cmd = m.Update(BackToDetailMsg{})
	m = updated.(AppModel)
	if cmd == nil {
		t.Fatal("returning to the detail view must restart the tick chain")
	}
	if m.detail.totpSeq == firstSeq {
		t.Fatal("restart must take a fresh sequence number")
	}

	// A tick from the superseded chain is dropped without re-arming.
	_, dcmd := m.detail.Update(totpTickMsg{seq: firstSeq})
	if dcmd != nil {
		t.Error("stale tick must not re-arm (would duplicate chains)")
	}

	// A tick from the live chain refreshes and re-arms.
	_, dcmd = m.detail.Update(totpTickMsg{seq: m.detail.totpSeq})
	if dcmd == nil {
		t.Error("live tick must re-arm the chain")
	}
}

func TestAutoLockStaleGenerationIgnored(t *testing.T) {
	m := createTestApp(t)
	m.autoLockGen = 5
	m.lastActivity = time.Now().Add(-time.Hour)

	updated, cmd := m.Update(autoLockMsg{gen: 4})
	m = updated.(AppModel)

	if m.state != viewBrowser || m.db == nil {
		t.Error("stale-generation tick must not lock")
	}
	if cmd != nil {
		t.Error("stale-generation tick must not reschedule")
	}
}

func TestAutoLockActivityReschedules(t *testing.T) {
	m := createTestApp(t)
	m.lastActivity = time.Now() // activity since the timer was armed

	updated, cmd := m.Update(autoLockMsg{gen: m.autoLockGen})
	m = updated.(AppModel)

	if m.state != viewBrowser || m.db == nil {
		t.Error("must not lock while the user is active")
	}
	if cmd == nil {
		t.Error("expected a reschedule command so auto-lock stays armed")
	}
}

func TestAutoLockLocksAfterIdle(t *testing.T) {
	m := createTestApp(t)
	m.lastActivity = time.Now().Add(-time.Hour)

	updated, _ := m.Update(autoLockMsg{gen: m.autoLockGen})
	m = updated.(AppModel)

	if m.state != viewUnlock {
		t.Errorf("state = %d, want viewUnlock after idle expiry", m.state)
	}
	if m.db != nil {
		t.Error("db should be dropped on lock")
	}
}

func TestAutoLockSaveFailureAbortsLock(t *testing.T) {
	m := createTestApp(t)
	m.dirty = true
	// Point the database at a nonexistent directory so Save() fails.
	m.db.Path = filepath.Join(t.TempDir(), "missing", "test.kdbx")
	m.lastActivity = time.Now().Add(-time.Hour)

	updated, cmd := m.Update(autoLockMsg{gen: m.autoLockGen})
	m = updated.(AppModel)

	if m.state != viewBrowser {
		t.Errorf("state = %d, want viewBrowser (lock must be aborted on save failure)", m.state)
	}
	if m.db == nil {
		t.Error("db must be retained when the save fails")
	}
	if !m.dirty {
		t.Error("dirty must be retained when the save fails")
	}
	if cmd == nil {
		t.Error("expected a retry command after aborting the lock")
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

	view := m.View().Content
	if strings.Contains(view, "entries") {
		t.Error("unlock view should not contain status bar")
	}
}

func TestStatusBarOnBrowser(t *testing.T) {
	m := createTestApp(t)
	m.refreshEntryCount()

	view := m.View().Content
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

func TestReadOnlyBlocksMutations(t *testing.T) {
	m := createTestApp(t)
	m.readOnly = true
	m.browser.readOnly = true

	root := m.db.RootGroup()
	entries := root.Entries
	if len(entries) == 0 {
		t.Fatal("expected at least one entry")
	}
	entryUUID := entries[0].UUID

	// SaveEntryMsg should be blocked
	updated, cmd := m.Update(SaveEntryMsg{
		IsNew:     false,
		EntryUUID: entryUUID,
		Entry:     keepass.Entry{Title: "Changed"},
	})
	m = updated.(AppModel)
	if m.dirty {
		t.Error("should not be dirty in read-only mode")
	}
	if cmd == nil {
		t.Fatal("expected status command")
	}
	msg := cmd()
	if sm, ok := msg.(StatusMsg); !ok || !sm.IsError {
		t.Errorf("expected error StatusMsg, got %T", msg)
	}

	// SaveAsStartMsg should be blocked
	updated, cmd = m.Update(SaveAsStartMsg{})
	m = updated.(AppModel)
	if cmd == nil {
		t.Fatal("expected status command for SaveAsStartMsg")
	}
	msg = cmd()
	if sm, ok := msg.(StatusMsg); !ok || !sm.IsError {
		t.Errorf("expected error StatusMsg for SaveAsStartMsg, got %T", msg)
	}

	// DuplicateEntryMsg should be blocked
	updated, cmd = m.Update(DuplicateEntryMsg{EntryUUID: entryUUID})
	m = updated.(AppModel)
	if cmd == nil {
		t.Fatal("expected status command for DuplicateEntryMsg")
	}
	msg = cmd()
	if sm, ok := msg.(StatusMsg); !ok || !sm.IsError {
		t.Errorf("expected error StatusMsg for DuplicateEntryMsg, got %T", msg)
	}
}

func TestReadOnlyStatusBar(t *testing.T) {
	m := createTestApp(t)
	m.readOnly = true

	bar := m.statusBar()
	if !strings.Contains(bar, "[read-only]") {
		t.Error("status bar should show [read-only] in read-only mode")
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

func TestAppQuitWithConfirmQuit(t *testing.T) {
	m := createTestApp(t)
	tr := true
	m.cfg.ConfirmQuit = &tr

	updated, _ := m.Update(RequestQuitMsg{})
	m = updated.(AppModel)

	if m.state != viewConfirm {
		t.Errorf("state = %d, want %d (viewConfirm) with ConfirmQuit enabled", m.state, viewConfirm)
	}
}

func TestAppDeleteEntryConfirmMessage(t *testing.T) {
	m := createTestApp(t)

	updated, _ := m.Update(DeleteEntryMsg{EntryUUID: m.db.RootGroup().Entries[0].UUID, Name: "MyEntry"})
	m = updated.(AppModel)

	if m.state != viewConfirm {
		t.Fatalf("state = %d, want viewConfirm", m.state)
	}
	if !strings.Contains(m.confirm.message, "MyEntry") {
		t.Errorf("confirm message = %q, want it to contain 'MyEntry'", m.confirm.message)
	}
}

func TestAppDeleteGroupConfirmMessage(t *testing.T) {
	m := createTestApp(t)

	root := m.db.RootGroup()
	if len(root.Groups) == 0 {
		t.Fatal("expected at least one group")
	}

	updated, _ := m.Update(DeleteGroupMsg{GroupUUID: root.Groups[0].UUID, Name: "Email"})
	m = updated.(AppModel)

	if m.state != viewConfirm {
		t.Fatalf("state = %d, want viewConfirm", m.state)
	}
	if !strings.Contains(m.confirm.message, "Email") {
		t.Errorf("confirm message = %q, want it to contain 'Email'", m.confirm.message)
	}
}

func TestAppSaveAsTransition(t *testing.T) {
	m := createTestApp(t)

	updated, _ := m.Update(SaveAsStartMsg{})
	m = updated.(AppModel)

	if m.state != viewPathInput {
		t.Fatalf("state = %d, want viewPathInput", m.state)
	}
}

func TestAppSaveAsCompletes(t *testing.T) {
	m := createTestApp(t)
	m.dirty = true

	dir := t.TempDir()
	newPath := filepath.Join(dir, "saved-as.kdbx")

	updated, cmd := m.Update(SaveAsMsg{Path: newPath})
	m = updated.(AppModel)

	if m.state != viewBrowser {
		t.Fatalf("state = %d, want viewBrowser", m.state)
	}
	if !m.saving {
		t.Fatal("saving flag should be set while the save runs")
	}
	if cmd == nil {
		t.Fatal("expected an async save command")
	}

	// The save runs in a goroutine; execute the command and deliver its
	// result like the Bubble Tea runtime would.
	result := cmd()
	saveMsg, ok := result.(saveResultMsg)
	if !ok {
		t.Fatalf("expected saveResultMsg, got %T", result)
	}
	updated, _ = m.Update(saveMsg)
	m = updated.(AppModel)

	if m.saving {
		t.Error("saving flag should clear after the result arrives")
	}
	if m.dirty {
		t.Error("dirty should be false after save-as")
	}
	if m.filePath != newPath {
		t.Errorf("filePath = %q, want %q", m.filePath, newPath)
	}
	if m.db.Path != newPath {
		t.Errorf("db.Path = %q, want %q", m.db.Path, newPath)
	}
}

func TestAppSaveAsReadOnly(t *testing.T) {
	m := createTestApp(t)
	m.readOnly = true

	updated, _ := m.Update(SaveAsStartMsg{})
	m = updated.(AppModel)

	// Should stay in browser, not transition to path input
	if m.state == viewPathInput {
		t.Error("should not enter path input in read-only mode")
	}
}
