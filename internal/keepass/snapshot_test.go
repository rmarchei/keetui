package keepass

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"maps"
	"path/filepath"
	"testing"

	"github.com/tobischo/gokeepasslib/v3"
	"github.com/tobischo/gokeepasslib/v3/wrappers"
)

const snapshotTestPassword = "synthetic"

// Attachment contents are "attachment of <title>". None of them is valid
// base64, so reading KDBX 4 binaries does not depend on content sniffing.
var undoFixtureContents = map[string]string{
	"A:a.txt":             "attachment of A",
	"B:b.txt":             "attachment of B",
	"G/C:c.txt":           "attachment of C",
	"G/C (history):c.txt": "old attachment of C",
	"G/S/D:d.txt":         "attachment of D",
}

func forEachFormat(t *testing.T, test func(t *testing.T, kdbx4 bool)) {
	t.Helper()
	for _, format := range []struct {
		name  string
		kdbx4 bool
	}{{"KDBX 3.1", false}, {"KDBX 4", true}} {
		t.Run(format.name, func(t *testing.T) { test(t, format.kdbx4) })
	}
}

// newFormatDB creates a synthetic database as keetui does (recycle bin off).
func newFormatDB(t *testing.T, kdbx4 bool) *DB {
	t.Helper()
	db, err := Create(filepath.Join(t.TempDir(), "undo.kdbx"), snapshotTestPassword)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if kdbx4 {
		if err := db.ConvertToKDBX4(); err != nil {
			t.Fatalf("ConvertToKDBX4: %v", err)
		}
	}
	return db
}

func addAttachedEntry(t *testing.T, db *DB, parent gokeepasslib.UUID, title, name string) gokeepasslib.UUID {
	t.Helper()
	uuid, err := db.AddEntry(parent, Entry{Title: title, Password: "password of " + title})
	if err != nil {
		t.Fatalf("AddEntry %s: %v", title, err)
	}
	if err := db.AddAttachment(uuid, name, []byte("attachment of "+title)); err != nil {
		t.Fatalf("AddAttachment %s: %v", name, err)
	}
	return uuid
}

func mustSave(t *testing.T, db *DB) {
	t.Helper()
	if err := db.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

func saveAndReopen(t *testing.T, db *DB) *DB {
	t.Helper()
	mustSave(t, db)
	reopened, err := Open(db.Path, snapshotTestPassword)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	return reopened
}

// openUndoFixture saves and reopens a database with root entries A and B,
// group G with entry C, and G's subgroup S with entry D. Every entry owns an
// attachment and C's history entry an older one. They are added in the order
// A, C (history), C, D, B, so dropping any binary renumbers those after it.
func openUndoFixture(t *testing.T, kdbx4 bool) *DB {
	t.Helper()
	db := newFormatDB(t, kdbx4)
	root := db.RootGroup().UUID
	g, err := db.AddGroup(root, "G")
	if err != nil {
		t.Fatalf("AddGroup G: %v", err)
	}
	s, err := db.AddGroup(g, "S")
	if err != nil {
		t.Fatalf("AddGroup S: %v", err)
	}
	addAttachedEntry(t, db, root, "A", "a.txt")
	c, err := db.AddEntry(g, Entry{Title: "C", Password: "old password of C"})
	if err != nil {
		t.Fatalf("AddEntry C: %v", err)
	}
	if err := db.AddAttachment(c, "c.txt", []byte("old attachment of C")); err != nil {
		t.Fatalf("AddAttachment old c.txt: %v", err)
	}
	if err := db.UpdateEntry(c, Entry{Title: "C", Password: "password of C"}); err != nil {
		t.Fatalf("UpdateEntry C: %v", err)
	}
	if err := db.DeleteAttachment(c, "c.txt"); err != nil {
		t.Fatalf("DeleteAttachment c.txt: %v", err)
	}
	if err := db.AddAttachment(c, "c.txt", []byte("attachment of C")); err != nil {
		t.Fatalf("AddAttachment c.txt: %v", err)
	}
	addAttachedEntry(t, db, s, "D", "d.txt")
	addAttachedEntry(t, db, root, "B", "b.txt")
	return saveAndReopen(t, db)
}

func entryUUIDByTitle(t *testing.T, db *DB, title string) gokeepasslib.UUID {
	t.Helper()
	var find func(g Group) (gokeepasslib.UUID, bool)
	find = func(g Group) (gokeepasslib.UUID, bool) {
		for _, e := range g.Entries {
			if e.Title == title {
				return e.UUID, true
			}
		}
		for _, sub := range g.Groups {
			if uuid, ok := find(sub); ok {
				return uuid, true
			}
		}
		return gokeepasslib.UUID{}, false
	}
	uuid, ok := find(db.RootGroup())
	if !ok {
		t.Fatalf("entry %q not found", title)
	}
	return uuid
}

func groupUUIDByName(t *testing.T, db *DB, name string) gokeepasslib.UUID {
	t.Helper()
	root := &db.Database.Content.Root.Groups[0]
	var find func(g *gokeepasslib.Group) *gokeepasslib.Group
	find = func(g *gokeepasslib.Group) *gokeepasslib.Group {
		for i := range g.Groups {
			if g.Groups[i].Name == name {
				return &g.Groups[i]
			}
			if found := find(&g.Groups[i]); found != nil {
				return found
			}
		}
		return nil
	}
	g := find(root)
	if g == nil {
		t.Fatalf("group %q not found", name)
	}
	return g.UUID
}

// deleteEntryWithSnapshot deletes an entry the way the UI does without a
// recycle bin, returning the undo snapshot and the entry's parent group.
func deleteEntryWithSnapshot(t *testing.T, db *DB, uuid gokeepasslib.UUID) (*EntrySnapshot, gokeepasslib.UUID) {
	t.Helper()
	parent, err := db.FindEntryParentUUID(uuid)
	if err != nil {
		t.Fatalf("FindEntryParentUUID: %v", err)
	}
	s, err := db.SnapshotEntry(uuid)
	if err != nil {
		t.Fatalf("SnapshotEntry: %v", err)
	}
	if err := db.DeleteEntry(uuid); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}
	return s, parent
}

func deleteGroupWithSnapshot(t *testing.T, db *DB, uuid gokeepasslib.UUID) (*GroupSnapshot, gokeepasslib.UUID) {
	t.Helper()
	parent, err := db.FindGroupParentUUID(uuid)
	if err != nil {
		t.Fatalf("FindGroupParentUUID: %v", err)
	}
	s, err := db.SnapshotGroup(uuid)
	if err != nil {
		t.Fatalf("SnapshotGroup: %v", err)
	}
	if err := db.DeleteGroup(uuid); err != nil {
		t.Fatalf("DeleteGroup: %v", err)
	}
	return s, parent
}

// storedBytes returns the attachment bytes a pooled binary holds. KDBX 4
// stores them raw, and GetContentBytes would decode any that happen to be
// valid base64.
func storedBytes(t *testing.T, db *DB, b *gokeepasslib.Binary) []byte {
	t.Helper()
	if db.IsKdbx4() {
		return b.Content
	}
	data, err := b.GetContentBytes()
	if err != nil {
		t.Fatalf("decode binary %d: %v", b.ID, err)
	}
	return data
}

// forEachAttachment calls fn for every attachment reference, including those
// of history entries, labelled "group path/title:name".
func forEachAttachment(db *DB, fn func(label string, ref gokeepasslib.BinaryReference)) {
	var visitEntry func(e *gokeepasslib.Entry, label string)
	visitEntry = func(e *gokeepasslib.Entry, label string) {
		for _, ref := range e.Binaries {
			fn(label+":"+ref.Name, ref)
		}
		for hi := range e.Histories {
			for ei := range e.Histories[hi].Entries {
				visitEntry(&e.Histories[hi].Entries[ei], label+" (history)")
			}
		}
	}
	var visitGroup func(g *gokeepasslib.Group, path string)
	visitGroup = func(g *gokeepasslib.Group, path string) {
		for i := range g.Entries {
			visitEntry(&g.Entries[i], path+g.Entries[i].GetTitle())
		}
		for i := range g.Groups {
			visitGroup(&g.Groups[i], path+g.Groups[i].Name+"/")
		}
	}
	visitGroup(&db.Database.Content.Root.Groups[0], "")
}

// attachmentContents maps the label of every attachment to the contents it
// resolves to.
func attachmentContents(t *testing.T, db *DB) map[string]string {
	t.Helper()
	got := make(map[string]string)
	forEachAttachment(db, func(label string, ref gokeepasslib.BinaryReference) {
		if _, dup := got[label]; dup {
			t.Fatalf("ambiguous attachment label %q", label)
		}
		if bin := db.Database.FindBinary(ref.Value.ID); bin != nil {
			got[label] = string(storedBytes(t, db, bin))
		} else {
			got[label] = "<missing binary>"
		}
	})
	return got
}

// attachmentBinaries maps the label of every attachment to a copy of the
// binary it references.
func attachmentBinaries(t *testing.T, db *DB) map[string]gokeepasslib.Binary {
	t.Helper()
	got := make(map[string]gokeepasslib.Binary)
	forEachAttachment(db, func(label string, ref gokeepasslib.BinaryReference) {
		bin := db.Database.FindBinary(ref.Value.ID)
		if bin == nil {
			t.Fatalf("attachment %q references missing binary %d", label, ref.Value.ID)
		}
		got[label] = *bin
	})
	return got
}

func assertAttachmentContents(t *testing.T, db *DB, want map[string]string) {
	t.Helper()
	if got := attachmentContents(t, db); !maps.Equal(got, want) {
		t.Errorf("attachments:\n got %v\nwant %v", got, want)
	}
}

// assertTidyBinaryPool fails unless every reference resolves, every pooled
// binary is referenced, and no two binaries hold the same contents.
func assertTidyBinaryPool(t *testing.T, db *DB) {
	t.Helper()
	referenced := make(map[int]bool)
	forEachAttachment(db, func(label string, ref gokeepasslib.BinaryReference) {
		if db.Database.FindBinary(ref.Value.ID) == nil {
			t.Errorf("attachment %q references missing binary %d", label, ref.Value.ID)
		}
		referenced[ref.Value.ID] = true
	})

	owners := make(map[string]int)
	for _, b := range *db.binaryPool() {
		if !referenced[b.ID] {
			t.Errorf("binary %d is not referenced by any attachment", b.ID)
		}
		data := storedBytes(t, db, &b)
		if other, dup := owners[string(data)]; dup {
			t.Errorf("binaries %d and %d hold the same contents %q", other, b.ID, data)
		}
		owners[string(data)] = b.ID
	}
	if db.IsKdbx4() && len(db.Database.Content.Meta.Binaries) != 0 {
		t.Errorf("KDBX 4 file also stores %d binaries in its XML", len(db.Database.Content.Meta.Binaries))
	}
}

// The reported bug: entries A and B each have one attachment. Saving after
// A's deletion renumbers B's binary to A's old ID, so a raw copy of A came
// back with B's attachment and A's own attachment was lost.
func TestEntrySnapshotSurvivesSave(t *testing.T) {
	forEachFormat(t, func(t *testing.T, kdbx4 bool) {
		db := newFormatDB(t, kdbx4)
		root := db.RootGroup().UUID
		addAttachedEntry(t, db, root, "A", "a.txt")
		addAttachedEntry(t, db, root, "B", "b.txt")
		db = saveAndReopen(t, db)

		s, parent := deleteEntryWithSnapshot(t, db, entryUUIDByTitle(t, db, "A"))
		mustSave(t, db)
		if err := db.InsertEntrySnapshot(parent, s); err != nil {
			t.Fatalf("InsertEntrySnapshot: %v", err)
		}
		db = saveAndReopen(t, db)
		assertAttachmentContents(t, db, map[string]string{
			"A:a.txt": "attachment of A",
			"B:b.txt": "attachment of B",
		})
		assertTidyBinaryPool(t, db)
		if got, err := db.GetEntry(entryUUIDByTitle(t, db, "A")); err != nil || got.Password != "password of A" {
			t.Errorf("restored A = %+v, %v; want its password back", got, err)
		}
	})
}

func TestGroupSnapshotRestoresNestedAndHistoryAttachments(t *testing.T) {
	forEachFormat(t, func(t *testing.T, kdbx4 bool) {
		db := openUndoFixture(t, kdbx4)
		s, parent := deleteGroupWithSnapshot(t, db, groupUUIDByName(t, db, "G"))
		mustSave(t, db)
		if err := db.InsertGroupSnapshot(parent, s); err != nil {
			t.Fatalf("InsertGroupSnapshot: %v", err)
		}
		db = saveAndReopen(t, db)
		assertAttachmentContents(t, db, undoFixtureContents)
		assertTidyBinaryPool(t, db)
		history := db.EntryHistory(entryUUIDByTitle(t, db, "C"))
		if len(history) != 1 || history[0].Password != "old password of C" {
			t.Errorf("C history = %+v, want the old password", history)
		}
	})
}

// Before any save the deleted binaries are still pooled: undo must point the
// restored attachments back at them instead of adding duplicates.
func TestSnapshotInsertWithoutSaveReusesBinaries(t *testing.T) {
	forEachFormat(t, func(t *testing.T, kdbx4 bool) {
		db := openUndoFixture(t, kdbx4)
		pooled := len(*db.binaryPool())
		es, ep := deleteEntryWithSnapshot(t, db, entryUUIDByTitle(t, db, "A"))
		gs, gp := deleteGroupWithSnapshot(t, db, groupUUIDByName(t, db, "G"))
		if err := db.InsertGroupSnapshot(gp, gs); err != nil {
			t.Fatalf("InsertGroupSnapshot: %v", err)
		}
		if err := db.InsertEntrySnapshot(ep, es); err != nil {
			t.Fatalf("InsertEntrySnapshot: %v", err)
		}
		if got := len(*db.binaryPool()); got != pooled {
			t.Errorf("binary pool grew from %d to %d", pooled, got)
		}
		db = saveAndReopen(t, db)
		assertAttachmentContents(t, db, undoFixtureContents)
		assertTidyBinaryPool(t, db)
	})
}

func TestSnapshotInsertAfterTwoSaves(t *testing.T) {
	forEachFormat(t, func(t *testing.T, kdbx4 bool) {
		db := openUndoFixture(t, kdbx4)
		s, parent := deleteEntryWithSnapshot(t, db, entryUUIDByTitle(t, db, "A"))
		mustSave(t, db)
		mustSave(t, db)
		if err := db.InsertEntrySnapshot(parent, s); err != nil {
			t.Fatalf("InsertEntrySnapshot: %v", err)
		}
		db = saveAndReopen(t, db)
		assertAttachmentContents(t, db, undoFixtureContents)
		assertTidyBinaryPool(t, db)
	})
}

func TestSnapshotsUndoneInReverseOrderAcrossSaves(t *testing.T) {
	forEachFormat(t, func(t *testing.T, kdbx4 bool) {
		db := openUndoFixture(t, kdbx4)
		es, ep := deleteEntryWithSnapshot(t, db, entryUUIDByTitle(t, db, "A"))
		mustSave(t, db)
		gs, gp := deleteGroupWithSnapshot(t, db, groupUUIDByName(t, db, "G"))
		mustSave(t, db)
		if err := db.InsertGroupSnapshot(gp, gs); err != nil {
			t.Fatalf("InsertGroupSnapshot: %v", err)
		}
		mustSave(t, db)
		if err := db.InsertEntrySnapshot(ep, es); err != nil {
			t.Fatalf("InsertEntrySnapshot: %v", err)
		}
		db = saveAndReopen(t, db)
		assertAttachmentContents(t, db, undoFixtureContents)
		assertTidyBinaryPool(t, db)
	})
}

// B's attachment is shared with its duplicate. Restoring B must reuse that
// binary, which the save renumbered, rather than store a second copy.
func TestEntrySnapshotReusesSharedAttachment(t *testing.T) {
	forEachFormat(t, func(t *testing.T, kdbx4 bool) {
		db := openUndoFixture(t, kdbx4)
		b := entryUUIDByTitle(t, db, "B")
		if _, err := db.DuplicateEntry(b); err != nil {
			t.Fatalf("DuplicateEntry: %v", err)
		}
		// Deleting A for good makes the next save renumber B's binary.
		if err := db.DeleteEntry(entryUUIDByTitle(t, db, "A")); err != nil {
			t.Fatalf("DeleteEntry A: %v", err)
		}
		s, parent := deleteEntryWithSnapshot(t, db, b)
		mustSave(t, db)
		if err := db.InsertEntrySnapshot(parent, s); err != nil {
			t.Fatalf("InsertEntrySnapshot: %v", err)
		}
		db = saveAndReopen(t, db)
		want := maps.Clone(undoFixtureContents)
		delete(want, "A:a.txt")
		want["B (copy):b.txt"] = "attachment of B"
		assertAttachmentContents(t, db, want)
		assertTidyBinaryPool(t, db)
	})
}

// Snapshots taken in KDBX 3.1 re-add their attachments to the KDBX 4 pool
// after an upgrade, which moves and renumbers the binaries.
func TestSnapshotInsertAfterKDBX4Upgrade(t *testing.T) {
	db := openUndoFixture(t, false)
	es, ep := deleteEntryWithSnapshot(t, db, entryUUIDByTitle(t, db, "A"))
	gs, gp := deleteGroupWithSnapshot(t, db, groupUUIDByName(t, db, "G"))
	mustSave(t, db)
	if err := db.ConvertToKDBX4(); err != nil {
		t.Fatalf("ConvertToKDBX4: %v", err)
	}
	if err := db.InsertGroupSnapshot(gp, gs); err != nil {
		t.Fatalf("InsertGroupSnapshot: %v", err)
	}
	if err := db.InsertEntrySnapshot(ep, es); err != nil {
		t.Fatalf("InsertEntrySnapshot: %v", err)
	}
	db = saveAndReopen(t, db)
	if !db.IsKdbx4() {
		t.Fatal("expected a KDBX 4 file")
	}
	assertAttachmentContents(t, db, undoFixtureContents)
	assertTidyBinaryPool(t, db)
}

// A reference whose binary was already missing is dropped, not pointed at
// whichever binary later takes over its ID.
func TestEntrySnapshotDropsDanglingReference(t *testing.T) {
	forEachFormat(t, func(t *testing.T, kdbx4 bool) {
		db := openUndoFixture(t, kdbx4)
		a := entryUUIDByTitle(t, db, "A")
		next := len(*db.binaryPool())
		raw := findEntry(&db.Database.Content.Root.Groups[0], a)
		raw.Binaries = append(raw.Binaries, gokeepasslib.NewBinaryReference("ghost.txt", next))
		s, parent := deleteEntryWithSnapshot(t, db, a)
		if err := db.AddAttachment(entryUUIDByTitle(t, db, "B"), "new.txt", []byte("new attachment of B")); err != nil {
			t.Fatalf("AddAttachment: %v", err)
		}
		if db.Database.FindBinary(next) == nil {
			t.Fatalf("expected the new attachment to take binary ID %d", next)
		}
		if err := db.InsertEntrySnapshot(parent, s); err != nil {
			t.Fatalf("InsertEntrySnapshot: %v", err)
		}
		db = saveAndReopen(t, db)
		want := maps.Clone(undoFixtureContents)
		want["B:new.txt"] = "new attachment of B"
		assertAttachmentContents(t, db, want)
		assertTidyBinaryPool(t, db)
	})
}

func TestInsertSnapshotIntoMissingGroupLeavesPoolUntouched(t *testing.T) {
	db := openUndoFixture(t, false)
	es, _ := deleteEntryWithSnapshot(t, db, entryUUIDByTitle(t, db, "A"))
	gs, _ := deleteGroupWithSnapshot(t, db, groupUUIDByName(t, db, "G"))
	mustSave(t, db)
	pooled := len(*db.binaryPool())
	missing := gokeepasslib.NewUUID()
	if err := db.InsertEntrySnapshot(missing, es); err == nil {
		t.Error("InsertEntrySnapshot into a missing group should fail")
	}
	if err := db.InsertGroupSnapshot(missing, gs); err == nil {
		t.Error("InsertGroupSnapshot into a missing group should fail")
	}
	if got := len(*db.binaryPool()); got != pooled {
		t.Errorf("failed inserts changed the binary pool from %d to %d binaries", pooled, got)
	}
}

// An attachment that happens to be valid base64, such as a key file, must
// come back with its own bytes. Reading the KDBX 4 pool with GetContentBytes
// decoded such contents: undo restored the decoded key file, or mistook it
// for the key it encodes and pointed both attachments at one binary, so the
// next save dropped the other one for good.
func TestSnapshotKeepsBase64LookingAttachments(t *testing.T) {
	key, otherKey := make([]byte, 32), make([]byte, 32)
	for i := range key {
		key[i], otherKey[i] = byte(i), byte(32+i)
	}
	keyFile := base64.StdEncoding.EncodeToString(key) + "\n"
	otherKeyFile := base64.StdEncoding.EncodeToString(otherKey) + "\n"
	want := map[string]string{
		"W:w.key":     otherKeyFile,
		"G/X:key.txt": keyFile,
		"G/Y:key.bin": string(key),
	}
	for _, tc := range []struct {
		name      string
		fileFirst bool // pool X's key file before Y's key
		group     bool // delete and undo G instead of the entry
		title     string
		save      bool // save between the delete and the undo
		upgrade   bool // then upgrade to KDBX 4
	}{
		{name: "key undone before save", title: "Y"},
		{name: "key file undone after save", title: "W", save: true},
		{name: "key file of a pooled key undone after save", title: "X", save: true},
		{name: "group undone before save", group: true},
		{name: "group with the key file first undone before save", group: true, fileFirst: true},
		{name: "group undone after save", group: true, save: true},
		{name: "group undone after an upgrade", group: true, upgrade: true},
		{name: "group undone after save and upgrade", group: true, save: true, upgrade: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			forEachFormat(t, func(t *testing.T, kdbx4 bool) {
				if tc.upgrade && kdbx4 {
					t.Skip("already KDBX 4")
				}
				db := newFormatDB(t, kdbx4)
				root := db.RootGroup().UUID
				g, err := db.AddGroup(root, "G")
				if err != nil {
					t.Fatalf("AddGroup G: %v", err)
				}
				attach := func(parent gokeepasslib.UUID, title, name, data string) {
					t.Helper()
					uuid, err := db.AddEntry(parent, Entry{Title: title})
					if err != nil {
						t.Fatalf("AddEntry %s: %v", title, err)
					}
					if err := db.AddAttachment(uuid, name, []byte(data)); err != nil {
						t.Fatalf("AddAttachment %s: %v", name, err)
					}
				}
				attach(root, "W", "w.key", otherKeyFile)
				if tc.fileFirst {
					attach(g, "X", "key.txt", keyFile)
					attach(g, "Y", "key.bin", string(key))
				} else {
					attach(g, "Y", "key.bin", string(key))
					attach(g, "X", "key.txt", keyFile)
				}
				db = saveAndReopen(t, db)
				between := func() {
					t.Helper()
					if tc.save {
						mustSave(t, db)
					}
					if tc.upgrade {
						if err := db.ConvertToKDBX4(); err != nil {
							t.Fatalf("ConvertToKDBX4: %v", err)
						}
					}
				}

				if tc.group {
					s, parent := deleteGroupWithSnapshot(t, db, groupUUIDByName(t, db, "G"))
					between()
					if err := db.InsertGroupSnapshot(parent, s); err != nil {
						t.Fatalf("InsertGroupSnapshot: %v", err)
					}
				} else {
					s, parent := deleteEntryWithSnapshot(t, db, entryUUIDByTitle(t, db, tc.title))
					between()
					if err := db.InsertEntrySnapshot(parent, s); err != nil {
						t.Fatalf("InsertEntrySnapshot: %v", err)
					}
				}
				db = saveAndReopen(t, db)
				assertAttachmentContents(t, db, want)
				assertTidyBinaryPool(t, db)
			})
		})
	}
}

// protectAttachment protects the binary of the labelled attachment the way
// KeePass stores a protected attachment: flagged in the KDBX 4 inner header,
// or encrypted with the inner stream (and so never compressed) in KDBX 3.1.
func protectAttachment(t *testing.T, db *DB, label string) {
	t.Helper()
	var bin *gokeepasslib.Binary
	forEachAttachment(db, func(l string, ref gokeepasslib.BinaryReference) {
		if l == label {
			bin = db.Database.FindBinary(ref.Value.ID)
		}
	})
	if bin == nil {
		t.Fatalf("attachment %q not found", label)
	}
	if db.IsKdbx4() {
		bin.MemoryProtection = 1
		return
	}
	data := storedBytes(t, db, bin)
	protected := wrappers.NewBoolWrapper(true)
	bin.Protected = &protected
	bin.Compressed = wrappers.NewBoolWrapper(false)
	bin.Content = []byte(base64.StdEncoding.EncodeToString(data))
}

// completeGzipStreams re-encodes the compressed KDBX 3.1 binaries with
// complete gzip streams, as KeePass and KeeWeb write them. The pinned
// gokeepasslib leaves the base64 encoder open when it compresses, which cuts
// off up to two bytes at the end of the stream.
func completeGzipStreams(t *testing.T, db *DB) {
	t.Helper()
	for i := range db.Database.Content.Meta.Binaries {
		b := &db.Database.Content.Meta.Binaries[i]
		if !b.Compressed.Bool || (b.Protected != nil && b.Protected.Bool) {
			continue
		}
		data := storedBytes(t, db, b)
		var buf bytes.Buffer
		encoder := base64.NewEncoder(base64.StdEncoding, &buf)
		writer := gzip.NewWriter(encoder)
		if _, err := writer.Write(data); err != nil {
			t.Fatalf("compress binary %d: %v", b.ID, err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("compress binary %d: %v", b.ID, err)
		}
		if err := encoder.Close(); err != nil {
			t.Fatalf("encode binary %d: %v", b.ID, err)
		}
		b.Content = buf.Bytes()
	}
}

// Undo must put the deleted binaries back as they were. Re-encoding their
// contents dropped the protection of protected attachments, and in KDBX 3.1
// cut off the end of every compressed one, so KeeWeb refused to open the file.
func TestSnapshotInsertRepoolsBinariesUnchanged(t *testing.T) {
	forEachFormat(t, func(t *testing.T, kdbx4 bool) {
		db := openUndoFixture(t, kdbx4)
		protectAttachment(t, db, "G/C:c.txt")
		if !kdbx4 {
			completeGzipStreams(t, db)
		}
		db = saveAndReopen(t, db)
		want := attachmentBinaries(t, db)

		s, parent := deleteGroupWithSnapshot(t, db, groupUUIDByName(t, db, "G"))
		mustSave(t, db)
		if err := db.InsertGroupSnapshot(parent, s); err != nil {
			t.Fatalf("InsertGroupSnapshot: %v", err)
		}
		db = saveAndReopen(t, db)
		got := attachmentBinaries(t, db)
		protected := func(b gokeepasslib.Binary) bool { return b.Protected != nil && b.Protected.Bool }
		for label, w := range want {
			g, ok := got[label]
			if !ok || !bytes.Equal(g.Content, w.Content) || g.Compressed != w.Compressed ||
				protected(g) != protected(w) || g.MemoryProtection != w.MemoryProtection {
				t.Errorf("%s: restored binary {%v, Protected: %v}, want {%v, Protected: %v}",
					label, g, protected(g), w, protected(w))
			}
		}
		assertAttachmentContents(t, db, undoFixtureContents)
		assertTidyBinaryPool(t, db)
	})
}
