package ui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/keepass"
)

func TestHistoryPreviewActionsRespectRemappedKeys(t *testing.T) {
	m, entry := historyApp(t)
	m.history.keys = config.KeyBindings{NavDown: "J", NavUp: "K", Open: "L", Back: "B", Quit: "X", Restore: "U", CopyPassword: "C", TogglePassword: "T"}.Merge(config.DefaultKeyBindings())
	m, _ = lockStep(t, m, textKey("J"))
	m, _ = lockStep(t, m, textKey("L"))
	if !m.history.previewing || m.history.cursor != 1 {
		t.Fatal("custom Open did not preview the selected history version")
	}
	m, _ = lockStep(t, m, textKey("T"))
	if view := ansi.Strip(m.history.View().Content); !strings.Contains(view, "1111") {
		t.Fatal("custom reveal did not show the selected protected field")
	}
	_, cmd := m.Update(textKey("C"))
	var copied CopyToClipboardMsg
	for _, msg := range quickMsgs(cmd) {
		if copy, ok := msg.(CopyToClipboardMsg); ok {
			copied = copy
		}
	}
	if copied.Value != "first" {
		t.Fatalf("preview copied %q, want the selected historical password", copied.Value)
	}
	_, cmd = m.Update(textKey("U"))
	found := false
	for _, msg := range quickMsgs(cmd) {
		if request, ok := msg.(RestoreHistoryMsg); ok {
			found = request.EntryUUID.Compare(entry.UUID) && request.Index == 1
		}
	}
	if !found {
		t.Fatal("custom Restore did not name the previewed version")
	}
	m, _ = lockStep(t, m, textKey("B"))
	if m.state != viewHistory || m.history.previewing || m.history.cursor != 1 {
		t.Fatal("Back should close the preview and retain its list selection")
	}
}

func TestHistoryRestoreCancellationPreservesPreviewAndDatabase(t *testing.T) {
	m, entry := historyApp(t)
	before, err := m.db.GetEntry(entry.UUID)
	if err != nil {
		t.Fatal(err)
	}
	historyBefore := m.db.EntryHistory(entry.UUID)
	m, _ = lockStep(t, m, textKey("j"))
	m, _ = lockStep(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m, cmd := lockStep(t, m, textKey("R"))
	m, _ = lockStep(t, m, cmd())
	m, cmd = lockStep(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	m, _ = lockStep(t, m, cmd())
	after, err := m.db.GetEntry(entry.UUID)
	if err != nil {
		t.Fatal(err)
	}
	if m.state != viewHistory || !m.history.previewing || m.history.cursor != 1 || m.dirty || m.undoStack.length() != 0 {
		t.Fatal("cancelling restore changed the preview, dirty state, or undo history")
	}
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(historyBefore, m.db.EntryHistory(entry.UUID)) {
		t.Fatal("cancelling restore changed the entry or its history")
	}
}

func TestHistoryRestoreFailureDoesNotCreateUndoOrDirtyState(t *testing.T) {
	m, entry := historyApp(t)
	before, _ := m.db.GetEntry(entry.UUID)
	m, cmd := lockStep(t, m, confirmRestoreHistoryMsg{EntryUUID: entry.UUID, Index: len(m.history.history)})
	if m.state != viewHistory || m.dirty || m.undoStack.length() != 0 || !strings.HasPrefix(quickStatus(cmd), "Restore failed:") {
		t.Fatal("an unavailable historical version should leave the database unchanged and report failure")
	}
	after, _ := m.db.GetEntry(entry.UUID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("failed restore changed the current entry")
	}
}

func TestHistoryRestoreUndoSurvivesSavingAttachmentReferences(t *testing.T) {
	for _, format := range []struct {
		name  string
		kdbx4 bool
	}{{"KDBX3", false}, {"KDBX4", true}} {
		t.Run(format.name, func(t *testing.T) {
			m := openUndoFixture(t, writeUndoFixture(t, format.kdbx4))
			group := m.db.RootGroup().Groups[0]
			entry := group.Entries[0]
			m, _ = lockStep(t, m, ShowHistoryMsg{EntryTitle: entry.Title, EntryUUID: entry.UUID})
			m, cmd := lockStep(t, m, textKey("R"))
			m, _ = lockStep(t, m, cmd())
			m, cmd = lockStep(t, m, textKey("y"))
			m, _ = lockStep(t, m, cmd())
			m = saveInPlace(t, m)
			assertAttachment := func(want string) {
				t.Helper()
				db, err := keepass.Open(m.filePath, undoFixturePassword)
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(t.TempDir(), "restored-attachment")
				if err := db.ExtractAttachment(entry.UUID, "c.txt", path); err != nil {
					t.Fatal(err)
				}
				got, err := os.ReadFile(path)
				if err != nil || string(got) != want {
					t.Fatalf("saved attachment = %q, %v; want %q", got, err, want)
				}
			}
			assertAttachment("old attachment of C")
			m = saveInPlace(t, pressUndo(t, m))
			assertAttachment("attachment of C")
			back, err := m.db.GetEntry(entry.UUID)
			if err != nil || back.Password != "new password of C" || len(m.db.EntryHistory(entry.UUID)) != 1 {
				t.Fatalf("undo failed to restore the complete entry: %+v, %v", back, err)
			}
		})
	}
}
