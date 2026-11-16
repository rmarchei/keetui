package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/keepass"
)

var ctrlL = tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl}

// lockStep sends msg and returns the updated app and its command.
func lockStep(t *testing.T, m AppModel, msg tea.Msg) (AppModel, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	return updated.(AppModel), cmd
}

// Ctrl+L without unsaved changes locks at once and clears keetui's copy.
func TestManualLockLocksAtOnce(t *testing.T) {
	m := copyForCountdown(t, createTestApp(t), "rootpass")
	clip := m.clipboard.(*testClipboard)
	m, _ = lockStep(t, m, ctrlL)
	if m.state != viewUnlock || m.db != nil || m.unlock.notice != "Locked." || m.unlock.err != nil {
		t.Fatalf("state %d, db open %v, notice %q, err %v", m.state, m.db != nil, m.unlock.notice, m.unlock.err)
	}
	if clip.value != "" || clip.releases != 1 {
		t.Errorf("clipboard holds %q after the lock (releases %d)", clip.value, clip.releases)
	}
}

// With unsaved changes Ctrl+L asks first: No keeps the database open and
// the changes unsaved; Yes saves them, then locks.
func TestManualLockAsksBeforeSaving(t *testing.T) {
	m := createTestApp(t)
	if _, err := m.db.AddEntry(m.db.RootGroup().UUID, keepass.Entry{Title: "Added before locking"}); err != nil {
		t.Fatal(err)
	}
	m.dirty = true
	m, _ = lockStep(t, m, ctrlL)
	if m.state != viewConfirm {
		t.Fatalf("state %d, want the question", m.state)
	}
	m, cmd := lockStep(t, m, tea.KeyPressMsg{Code: 'n', Text: "n"})
	m, _ = lockStep(t, m, cmd())
	if m.state != viewBrowser || m.db == nil || !m.dirty {
		t.Fatalf("after No: state %d, db open %v, dirty %v", m.state, m.db != nil, m.dirty)
	}
	m, _ = lockStep(t, m, ctrlL)
	m, cmd = lockStep(t, m, tea.KeyPressMsg{Code: 'y', Text: "y"})
	m, save := lockStep(t, m, cmd())
	if !m.saving || save == nil {
		t.Fatal("Yes did not start a save")
	}
	m, _ = lockStep(t, m, save())
	if m.state != viewUnlock || m.db != nil || m.unlock.notice != "Saved and locked." {
		t.Fatalf("after Yes: state %d, db open %v, notice %q", m.state, m.db != nil, m.unlock.notice)
	}
	db, err := keepass.Open(m.filePath, "testmaster")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range db.RootGroup().Entries {
		found = found || e.Title == "Added before locking"
	}
	if !found {
		t.Error("the change was not saved before locking")
	}
}

// failLockSave asks Ctrl+L to save and lock where the save fails.
func failLockSave(t *testing.T, m AppModel) AppModel {
	t.Helper()
	m, _ = lockStep(t, m, ctrlL)
	m, save := answer(t, m, keyY)
	m, _ = lockStep(t, m, save())
	return m
}

// A save that fails offers to lock anyway, holding the changes: No leaves
// the database open with them; Yes locks, and the password brings them
// back.
func TestManualLockOffersToHoldTheChangesWhenTheSaveFails(t *testing.T) {
	m := createTestApp(t)
	if _, err := m.db.AddEntry(m.db.RootGroup().UUID, keepass.Entry{Title: "Unsaved"}); err != nil {
		t.Fatal(err)
	}
	m.dirty = true
	m.filePath = filepath.Join(t.TempDir(), "missing", "test.kdbx")
	m = failLockSave(t, m)
	if m.state != viewConfirm || m.db == nil || m.lockAfterSave || !strings.Contains(m.confirm.message, "Lock anyway?") {
		t.Fatalf("state %d, db open %v, lock pending %v, question %q", m.state, m.db != nil, m.lockAfterSave, m.confirm.message)
	}
	if !m.statusError || !strings.HasPrefix(m.statusText, "Not locked:") {
		t.Errorf("status = %q", m.statusText)
	}
	m, _ = answer(t, m, keyN)
	if m.state != viewBrowser || m.db == nil || !m.dirty {
		t.Fatalf("No: state %d, db open %v, dirty %v", m.state, m.db != nil, m.dirty)
	}

	m = failLockSave(t, m)
	m, _ = answer(t, m, keyY)
	if m.state != viewUnlock || m.db != nil || m.held.db == nil || m.unlock.notice != "Locked." || !strings.Contains(m.unlock.held, "not saved") {
		t.Fatalf("Yes: state %d, db open %v, held %v, notice %q, %q", m.state, m.db != nil, m.held.db != nil, m.unlock.notice, m.unlock.held)
	}
	m = typePassword(t, m, "testmaster")
	if m.state != viewBrowser || m.db == nil || !m.dirty || !hasEntry(m.db, "Unsaved") {
		t.Fatalf("unlocked: state %d, db open %v, dirty %v", m.state, m.db != nil, m.dirty)
	}
}

// A folder that is gone fails the save like any error: the lock offers to
// hold the changes rather than leaving the database impossible to lock.
func TestManualLockOffersToHoldTheChangesWhenTheFolderIsGone(t *testing.T) {
	m := createTestApp(t)
	dir := filepath.Join(t.TempDir(), "volume")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	m = saveNow(t, m)
	m, save := lockStep(t, m, SaveAsMsg{Path: filepath.Join(dir, "test.kdbx")})
	m = finishSave(t, m, save)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	m.dirty = true
	m = failLockSave(t, m)
	if m.state != viewConfirm || !strings.Contains(m.confirm.message, "the folder of") || !strings.Contains(m.confirm.message, "Lock anyway?") {
		t.Fatalf("state %d, question %q", m.state, m.confirm.message)
	}
}

// Locking forgets the last message, which may name what was open: neither
// the settings nor their help, opened from the unlock screen, show it.
func TestLockForgetsTheLastStatus(t *testing.T) {
	for _, soft := range []bool{false, true} {
		m := createTestApp(t)
		m.lastActivity = time.Now().Add(-time.Hour)
		m, _ = lockStep(t, m, StatusMsg{Text: "Copied the password of Synthetic Bank", IsError: true, Detail: "Synthetic detail"})
		if soft {
			m, _ = lockStep(t, m, softLockMsg{err: errors.New("synthetic failure")})
		} else {
			m, _ = lockStep(t, m, autoLockMsg{gen: m.autoLockGen})
		}
		if m.state != viewUnlock || m.statusText != "" || m.statusError || m.statusDetail != "" {
			t.Fatalf("soft %v: state %d, status %q (error %v), detail %q", soft, m.state, m.statusText, m.statusError, m.statusDetail)
		}
		m = feed(t, m, ctrlO)
		m, _ = lockStep(t, m, tea.KeyPressMsg{Code: tea.KeyF1})
		if m.state != viewHelp || m.help.message != "" || strings.Contains(ansi.Strip(m.View().Content), "Synthetic") {
			t.Errorf("soft %v: state %d, help message %q", soft, m.state, m.help.message)
		}
	}
}

// A save in flight finishes before the lock.
func TestManualLockWaitsForTheSave(t *testing.T) {
	m := createTestApp(t)
	m.saving = true
	m, _ = lockStep(t, m, ctrlL)
	if m.db == nil || !m.lockAfterSave {
		t.Fatalf("db open %v, lock pending %v: want the lock to wait", m.db != nil, m.lockAfterSave)
	}
	m, _ = lockStep(t, m, saveResultMsg{path: m.filePath})
	if m.state != viewUnlock || m.db != nil {
		t.Fatalf("after the save: state %d, db open %v", m.state, m.db != nil)
	}
}

// Forms keep their input: Ctrl+L locks only screens that show the
// database. Help over the browser locks; a remapped key replaces Ctrl+L.
func TestManualLockScreensAndKey(t *testing.T) {
	m := createTestApp(t)
	m, _ = lockStep(t, m, EditEntryMsg{Entry: m.db.RootGroup().Entries[0]})
	m, _ = lockStep(t, m, ctrlL)
	if m.state != viewEditor || m.db == nil {
		t.Fatalf("Ctrl+L in the editor: state %d, db open %v", m.state, m.db != nil)
	}

	m = createTestApp(t)
	m, _ = lockStep(t, m, tea.KeyPressMsg{Code: tea.KeyF1})
	if m.state != viewHelp {
		t.Fatalf("state %d, want help", m.state)
	}
	if m, _ = lockStep(t, m, ctrlL); m.state != viewUnlock {
		t.Fatalf("Ctrl+L over help: state %d", m.state)
	}

	m = createTestApp(t)
	m.cfg.KeyBindingsPtr = &config.KeyBindings{Lock: "L"}
	if m, _ = lockStep(t, m, ctrlL); m.db == nil {
		t.Fatal("Ctrl+L locked with lock bound to L")
	}
	shiftL := tea.KeyPressMsg{Code: 'L', Text: "L"}
	if shiftL.String() != "L" {
		t.Fatalf("key string %q", shiftL.String())
	}
	if m, _ = lockStep(t, m, shiftL); m.state != viewUnlock {
		t.Fatalf("L: state %d, want locked", m.state)
	}
}
