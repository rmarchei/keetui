package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/tobischo/gokeepasslib/v3"
)

func TestManualLockCannotDismissPendingQuestionsThroughHelp(t *testing.T) {
	m := createTestApp(t)
	entry := m.db.RootGroup().Entries[0]
	m, _ = lockStep(t, m, DeleteEntryMsg{EntryUUID: entry.UUID, Name: entry.Title})
	question := m.confirm.message
	m, _ = lockStep(t, m, tea.KeyPressMsg{Code: tea.KeyF1})
	m, _ = lockStep(t, m, ctrlL)
	if m.state != viewHelp || m.db == nil || m.confirm.message != question {
		t.Fatal("locking from Help discarded a pending confirmation")
	}
	m, _ = lockStep(t, m, CloseHelpMsg{})
	if m.state != viewConfirm || m.confirm.message != question {
		t.Fatal("the original confirmation was not restored")
	}
	m, cmd := lockStep(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	m, _ = lockStep(t, m, cmd())
	if _, err := m.db.GetEntry(entry.UUID); err != nil || m.state != viewBrowser || m.dirty {
		t.Fatal("cancelling the preserved confirmation changed the database")
	}
}

func TestManualLockFromHistoryKeepsPreviewWhenSavingIsDeclined(t *testing.T) {
	m, _ := historyApp(t)
	m, _ = lockStep(t, m, textKey("j"))
	m, _ = lockStep(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m.dirty = true
	m, _ = lockStep(t, m, tea.KeyPressMsg{Code: tea.KeyF1})
	m, _ = lockStep(t, m, ctrlL)
	if m.state != viewConfirm {
		t.Fatal("dirty history did not ask before saving and locking")
	}
	m, cmd := lockStep(t, m, textKey("n"))
	m, _ = lockStep(t, m, cmd())
	if m.state != viewHistory || !m.history.previewing || m.history.cursor != 1 || m.db == nil || !m.dirty {
		t.Fatal("declining the lock lost the source view or its pending database changes")
	}
}

func TestFailedLockSaveDoesNotLockAfterAnOrdinaryRetry(t *testing.T) {
	m := createTestApp(t)
	m.dirty = true
	m.filePath = filepath.Join(t.TempDir(), "missing", "vault.kdbx")
	m, _ = lockStep(t, m, ctrlL)
	m, cmd := lockStep(t, m, textKey("y"))
	m, save := lockStep(t, m, cmd())
	m, status := lockStep(t, m, save())
	if m.db == nil || !m.dirty || !strings.HasPrefix(quickStatus(status), "Not locked:") {
		t.Fatal("failed save did not keep the database unlocked with its changes")
	}
	retryPath := filepath.Join(t.TempDir(), "retry.kdbx")
	m, save = lockStep(t, m, SaveAsMsg{Path: retryPath})
	if !m.saving || save == nil {
		t.Fatal("ordinary retry did not start saving")
	}
	m, _ = lockStep(t, m, save())
	if m.state != viewBrowser || m.db == nil || m.dirty || m.filePath != retryPath {
		t.Fatal("a cancelled lock request leaked into the successful retry")
	}
}

func TestReadOnlyManualLockDoesNotAttemptSaving(t *testing.T) {
	m := createTestApp(t)
	originalPath := m.filePath
	before, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatal(err)
	}
	m.readOnly, m.dirty = true, true
	m.filePath = filepath.Join(t.TempDir(), "missing", "unwritable.kdbx")
	m, _ = lockStep(t, m, ctrlL)
	if m.state != viewUnlock || m.db != nil || m.saving {
		t.Fatal("read-only locking should not start a save or ask to write")
	}
	after, err := os.ReadFile(originalPath)
	if err != nil || string(before) != string(after) {
		t.Fatal("read-only locking changed the file")
	}
}

func TestLockedAppDropsLateRecoveryAndSaveRequests(t *testing.T) {
	m, entry := historyApp(t)
	m, _ = lockStep(t, m, ctrlL)
	target := filepath.Join(t.TempDir(), "must-not-be-written.kdbx")
	for _, msg := range []tea.Msg{
		RestoreHistoryMsg{EntryUUID: entry.UUID},
		confirmRestoreHistoryMsg{EntryUUID: entry.UUID},
		confirmLockMsg{}, SaveAsMsg{Path: target}, confirmSaveAsMsg{Path: target},
	} {
		var cmd tea.Cmd
		m, cmd = lockStep(t, m, msg)
		if m.state != viewUnlock || m.db != nil || cmd != nil {
			t.Fatalf("%T acted on the locked database", msg)
		}
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("late save wrote its destination: %v", err)
	}
}

func TestGroupFormRenameCanBeUndoneWithoutLosingChildren(t *testing.T) {
	m := createTestApp(t)
	group := m.db.RootGroup().Groups[0]
	m, _ = lockStep(t, m, SaveGroupMsg{ParentUUID: group.UUID, Name: "Renamed / 日本語", Rename: true})
	if got := m.db.RootGroup().Groups[0]; got.Name != "Renamed / 日本語" || len(got.Entries) != len(group.Entries) || !m.dirty {
		t.Fatal("renaming through the group form lost its contents")
	}
	m = pressUndo(t, m)
	if got := m.db.RootGroup().Groups[0]; got.Name != group.Name || !got.UUID.Compare(group.UUID) || len(got.Entries) != len(group.Entries) {
		t.Fatal("undo did not restore the group name and identity")
	}
}

func TestGroupFormMissingDestinationReportsFailureWithoutUndo(t *testing.T) {
	for _, rename := range []bool{false, true} {
		m := createTestApp(t)
		missing := gokeepasslib.UUID{1, 2, 3}
		m, cmd := lockStep(t, m, SaveGroupMsg{ParentUUID: missing, Name: "Unreachable", Rename: rename})
		if m.state != viewBrowser || m.dirty || m.undoStack.length() != 0 || !strings.HasPrefix(quickStatus(cmd), "Error ") {
			t.Fatal("a missing group destination should report failure without recording a mutation")
		}
		if m.db.EntryCount() != 2 || len(m.db.RootGroup().Groups) != 1 {
			t.Fatal("failed group operation changed the database tree")
		}
	}
}
