package ui

import (
	"encoding/base64"
	"maps"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/tobischo/gokeepasslib/v3"

	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/keepass"
)

const undoFixturePassword = "synthetic"

// Every entry owns one attachment and C's history entry an older one, keyed
// by group path, title and attachment name.
var undoFixtureAttachments = map[string]string{
	"A:a.txt":             "attachment of A",
	"B:b.txt":             "attachment of B",
	"G/C:c.txt":           "attachment of C",
	"G/C (history):c.txt": "old attachment of C",
	"G/S/D:d.txt":         "attachment of D",
}

// writeUndoFixture creates a synthetic keetui database (recycle bin off):
// root entries A and B, group G with entry C, and G's subgroup S with entry
// D. Attachments are added in the order A, C (history), C, D, B, so a save
// that drops deleted binaries renumbers the ones after them.
func writeUndoFixture(t *testing.T, kdbx4 bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "undo.kdbx")
	db, err := keepass.Create(path, undoFixturePassword)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if kdbx4 {
		if err := db.ConvertToKDBX4(); err != nil {
			t.Fatalf("ConvertToKDBX4: %v", err)
		}
	}
	root := db.RootGroup().UUID
	group, err := db.AddGroup(root, "G")
	if err != nil {
		t.Fatalf("AddGroup G: %v", err)
	}
	sub, err := db.AddGroup(group, "S")
	if err != nil {
		t.Fatalf("AddGroup S: %v", err)
	}
	add := func(parent gokeepasslib.UUID, title string) gokeepasslib.UUID {
		t.Helper()
		uuid, err := db.AddEntry(parent, keepass.Entry{Title: title, Password: "password of " + title})
		if err != nil {
			t.Fatalf("AddEntry %s: %v", title, err)
		}
		return uuid
	}
	attach := func(entry gokeepasslib.UUID, name, content string) {
		t.Helper()
		if err := db.AddAttachment(entry, name, []byte(content)); err != nil {
			t.Fatalf("AddAttachment %s: %v", name, err)
		}
	}
	a, b, c, d := add(root, "A"), add(root, "B"), add(group, "C"), add(sub, "D")
	attach(a, "a.txt", "attachment of A")
	attach(c, "c.txt", "old attachment of C")
	if err := db.UpdateEntry(c, keepass.Entry{Title: "C", Password: "new password of C"}); err != nil {
		t.Fatalf("UpdateEntry C: %v", err)
	}
	if err := db.DeleteAttachment(c, "c.txt"); err != nil {
		t.Fatalf("DeleteAttachment C: %v", err)
	}
	attach(c, "c.txt", "attachment of C")
	attach(d, "d.txt", "attachment of D")
	attach(b, "b.txt", "attachment of B")
	if err := db.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	return path
}

// openUndoFixture unlocks the database through the real unlock flow.
func openUndoFixture(t *testing.T, path string) AppModel {
	t.Helper()
	m := NewApp(path, "", config.Default())
	m.clipboard = &testClipboard{}
	updated, cmd := m.Update(DatabaseOpenedMsg{FilePath: path, Password: undoFixturePassword})
	result, ok := cmd().(databaseOpenResultMsg)
	if !ok {
		t.Fatal("expected an async open result")
	}
	updated, _ = updated.(AppModel).Update(result)
	m = updated.(AppModel)
	if m.state != viewBrowser || m.db == nil {
		t.Fatalf("unlock failed: %v", m.unlock.err)
	}
	return m
}

// saveInPlace saves to the open file and delivers the async result.
func saveInPlace(t *testing.T, m AppModel) AppModel {
	t.Helper()
	updated, cmd := m.Update(SaveAsMsg{Path: m.filePath})
	m = completeSave(t, updated.(AppModel), cmd)
	if m.dirty {
		t.Fatal("save did not complete")
	}
	return m
}

func pressUndo(t *testing.T, m AppModel) AppModel {
	t.Helper()
	before := m.undoStack.length()
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
	m = updated.(AppModel)
	if m.undoStack.length() != before-1 || !m.dirty {
		t.Fatal("ctrl+z did not undo the last action")
	}
	return m
}

func findGroupUUID(t *testing.T, g keepass.Group, name string) gokeepasslib.UUID {
	t.Helper()
	for _, sub := range g.Groups {
		if sub.Name == name {
			return sub.UUID
		}
	}
	t.Fatalf("group %q not found", name)
	return gokeepasslib.UUID{}
}

func findEntryUUID(t *testing.T, g keepass.Group, title string) gokeepasslib.UUID {
	t.Helper()
	for _, e := range g.Entries {
		if e.Title == title {
			return e.UUID
		}
	}
	t.Fatalf("entry %q not found", title)
	return gokeepasslib.UUID{}
}

// attachmentContents maps "group path/title:name" to the bytes every
// attachment of the database resolves to, including history entries'.
func attachmentContents(t *testing.T, db *keepass.DB) map[string]string {
	t.Helper()
	got := make(map[string]string)
	var visitEntry func(e gokeepasslib.Entry, label string)
	visitEntry = func(e gokeepasslib.Entry, label string) {
		for _, ref := range e.Binaries {
			key := label + ":" + ref.Name
			bin := db.Database.FindBinary(ref.Value.ID)
			if bin == nil {
				got[key] = "<missing binary>"
				continue
			}
			// KDBX 4 stores attachments raw: GetContentBytes would decode
			// any that happen to be valid base64.
			data := bin.Content
			if !db.IsKdbx4() {
				var err error
				if data, err = bin.GetContentBytes(); err != nil {
					t.Fatalf("decode %s: %v", key, err)
				}
			}
			got[key] = string(data)
		}
		for _, h := range e.Histories {
			for _, he := range h.Entries {
				visitEntry(he, label+" (history)")
			}
		}
	}
	var visitGroup func(g gokeepasslib.Group, path string)
	visitGroup = func(g gokeepasslib.Group, path string) {
		for _, e := range g.Entries {
			visitEntry(e, path+e.GetTitle())
		}
		for _, sub := range g.Groups {
			visitGroup(sub, path+sub.Name+"/")
		}
	}
	visitGroup(db.Database.Content.Root.Groups[0], "")
	return got
}

func assertSavedAttachments(t *testing.T, path string) {
	t.Helper()
	assertSavedAttachmentsEqual(t, path, undoFixtureAttachments)
}

func assertSavedAttachmentsEqual(t *testing.T, path string, want map[string]string) {
	t.Helper()
	db, err := keepass.Open(path, undoFixturePassword)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := attachmentContents(t, db); !maps.Equal(got, want) {
		t.Errorf("attachments after save and reopen:\n got %q\nwant %q", got, want)
	}
}

// Undo used to restore the raw entry or group with its old binary pool IDs,
// which the save in between had renumbered: A came back with another entry's
// attachment, and the group's attachments went missing.
func TestUndoDeleteAfterSaveKeepsAttachments(t *testing.T) {
	for _, format := range []struct {
		name  string
		kdbx4 bool
	}{{"KDBX 3.1", false}, {"KDBX 4", true}} {
		t.Run(format.name, func(t *testing.T) {
			t.Run("entry", func(t *testing.T) {
				m := openUndoFixture(t, writeUndoFixture(t, format.kdbx4))
				a := findEntryUUID(t, m.db.RootGroup(), "A")
				updated, _ := m.Update(confirmDeleteEntryMsg{UUID: a})
				m = saveInPlace(t, updated.(AppModel))
				m = saveInPlace(t, pressUndo(t, m))
				assertSavedAttachments(t, m.filePath)
			})
			t.Run("group", func(t *testing.T) {
				m := openUndoFixture(t, writeUndoFixture(t, format.kdbx4))
				g := findGroupUUID(t, m.db.RootGroup(), "G")
				updated, _ := m.Update(confirmDeleteGroupMsg{UUID: g})
				m = saveInPlace(t, updated.(AppModel))
				m = saveInPlace(t, pressUndo(t, m))
				assertSavedAttachments(t, m.filePath)
			})
		})
	}
}

// With a recycle bin, deleting only moves the item: undo moves it back and
// no snapshot of its attachments is kept.
func TestUndoRecycleBinDeleteMovesBack(t *testing.T) {
	m := openUndoFixture(t, writeUndoFixture(t, false))
	m.db.Database.Content.Meta.RecycleBinEnabled.Bool = true
	root := m.db.RootGroup()
	updated, _ := m.Update(confirmDeleteEntryMsg{UUID: findEntryUUID(t, root, "A")})
	updated, _ = updated.(AppModel).Update(confirmDeleteGroupMsg{UUID: findGroupUUID(t, root, "G")})
	m = updated.(AppModel)
	for _, action := range m.undoStack.actions {
		if action.actionType != undoMoveEntry && action.actionType != undoMoveGroup {
			t.Errorf("undo action %q has type %d, want a move", action.description, action.actionType)
		}
		if action.entrySnapshot != nil || action.groupSnapshot != nil {
			t.Errorf("undo action %q keeps a snapshot", action.description)
		}
	}
	m = saveInPlace(t, m)
	m = saveInPlace(t, pressUndo(t, pressUndo(t, m)))
	assertSavedAttachments(t, m.filePath)
}

// writeKeyFixture creates a synthetic keetui database with root entries Y,
// whose attachment is a 32-byte key, and X, whose attachment is the key file
// holding that key as base64 text. It returns the path and the attachments.
func writeKeyFixture(t *testing.T, kdbx4 bool) (string, map[string]string) {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	attachments := map[string]string{
		"Y:key.bin": string(key),
		"X:key.txt": base64.StdEncoding.EncodeToString(key) + "\n",
	}
	path := filepath.Join(t.TempDir(), "keys.kdbx")
	db, err := keepass.Create(path, undoFixturePassword)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if kdbx4 {
		if err := db.ConvertToKDBX4(); err != nil {
			t.Fatalf("ConvertToKDBX4: %v", err)
		}
	}
	for _, a := range []struct{ title, name string }{{"Y", "key.bin"}, {"X", "key.txt"}} {
		uuid, err := db.AddEntry(db.RootGroup().UUID, keepass.Entry{Title: a.title})
		if err != nil {
			t.Fatalf("AddEntry %s: %v", a.title, err)
		}
		if err := db.AddAttachment(uuid, a.name, []byte(attachments[a.title+":"+a.name])); err != nil {
			t.Fatalf("AddAttachment %s: %v", a.name, err)
		}
	}
	if err := db.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	return path, attachments
}

// A KDBX 4 key file is valid base64 text, and undo used to read it decoded:
// the restored key file came back as the key itself, and a restored key was
// pointed at the key file holding it as text, so the next save dropped the
// key for good.
func TestUndoDeleteKeepsBase64LookingAttachments(t *testing.T) {
	for _, format := range []struct {
		name  string
		kdbx4 bool
	}{{"KDBX 3.1", false}, {"KDBX 4", true}} {
		t.Run(format.name, func(t *testing.T) {
			t.Run("key undone before save", func(t *testing.T) {
				path, want := writeKeyFixture(t, format.kdbx4)
				m := openUndoFixture(t, path)
				updated, _ := m.Update(confirmDeleteEntryMsg{UUID: findEntryUUID(t, m.db.RootGroup(), "Y")})
				m = saveInPlace(t, pressUndo(t, updated.(AppModel)))
				assertSavedAttachmentsEqual(t, m.filePath, want)
			})
			t.Run("key file undone after save", func(t *testing.T) {
				path, want := writeKeyFixture(t, format.kdbx4)
				m := openUndoFixture(t, path)
				updated, _ := m.Update(confirmDeleteEntryMsg{UUID: findEntryUUID(t, m.db.RootGroup(), "X")})
				m = saveInPlace(t, updated.(AppModel))
				m = saveInPlace(t, pressUndo(t, m))
				assertSavedAttachmentsEqual(t, m.filePath, want)
			})
		})
	}
}
