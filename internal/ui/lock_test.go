package ui

import (
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

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

// A save that fails leaves the database open, unlocked, with the error.
func TestManualLockStaysOpenWhenTheSaveFails(t *testing.T) {
	m := createTestApp(t)
	m.dirty = true
	m.filePath = filepath.Join(t.TempDir(), "missing", "test.kdbx")
	m, _ = lockStep(t, m, ctrlL)
	m, cmd := lockStep(t, m, tea.KeyPressMsg{Code: 'y', Text: "y"})
	m, save := lockStep(t, m, cmd())
	m, status := lockStep(t, m, save())
	if m.state != viewBrowser || m.db == nil || m.lockAfterSave || !m.dirty {
		t.Fatalf("state %d, db open %v, lock pending %v, dirty %v", m.state, m.db != nil, m.lockAfterSave, m.dirty)
	}
	if text := quickStatus(status); len(text) < 11 || text[:11] != "Not locked:" {
		t.Errorf("status = %q", text)
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
