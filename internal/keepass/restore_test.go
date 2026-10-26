package keepass

import (
	"os"
	"path/filepath"
	"testing"
)

// Restoring a history version brings back its fields, protection and
// attachments, keeping the current state in the history; replacing the
// entry with a snapshot taken before undoes it. Saved and reopened, the
// restored attachment still resolves to its contents.
func TestRestoreHistoryBringsBackAVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.kdbx")
	db, err := Create(path, "testmaster")
	if err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	uuid, err := db.AddEntry(db.RootGroup().UUID, Entry{Title: "Mail", Password: "first",
		CustomFields: []CustomField{{Key: "PIN", Value: "1111", Protected: true}}})
	must(err)
	must(db.AddAttachment(uuid, "key.txt", []byte("first-key")))
	must(db.UpdateEntry(uuid, Entry{Title: "Mail", Password: "second"}))
	must(db.DeleteAttachment(uuid, "key.txt"))
	must(db.AddAttachment(uuid, "other.txt", []byte("second-key")))
	must(db.UpdateEntry(uuid, Entry{Title: "Mail", Password: "third"}))

	history := db.EntryHistory(uuid)
	if len(history) != 2 || history[0].Password != "second" || history[1].Password != "first" ||
		len(history[1].Attachments) != 1 || history[1].Attachments[0] != "key.txt" || len(history[1].Entry.CustomFields) != 1 {
		t.Fatalf("history = %+v", history)
	}
	if err := db.RestoreHistory(uuid, 2); err == nil {
		t.Error("restoring a version that does not exist should fail")
	}

	before, err := db.SnapshotEntry(uuid)
	must(err)
	check := func(password string, attachment string, versions int) {
		t.Helper()
		e, err := db.GetEntry(uuid)
		must(err)
		attachments := db.EntryAttachments(uuid)
		if e.Password != password || len(attachments) != 1 || attachments[0].Name != attachment || len(db.EntryHistory(uuid)) != versions {
			t.Fatalf("entry %q with %+v and %d versions; want %q with %s and %d", e.Password, attachments, len(db.EntryHistory(uuid)), password, attachment, versions)
		}
	}
	must(db.RestoreHistory(uuid, 1))
	check("first", "key.txt", 3)
	if e, _ := db.GetEntry(uuid); len(e.CustomFields) != 1 || !e.CustomFields[0].Protected || e.CustomFields[0].Value != "1111" {
		t.Errorf("custom fields = %+v, want the protected PIN back", e.CustomFields)
	}
	if h := db.EntryHistory(uuid); h[0].Password != "third" {
		t.Errorf("newest version = %q, want the state before the restore", h[0].Password)
	}

	must(db.ReplaceEntrySnapshot(before))
	check("third", "other.txt", 2)

	must(db.RestoreHistory(uuid, 1))
	must(db.Save())
	reopened, err := Open(path, "testmaster")
	must(err)
	out := filepath.Join(dir, "key.txt")
	must(reopened.ExtractAttachment(uuid, "key.txt", out))
	if data, _ := os.ReadFile(out); string(data) != "first-key" {
		t.Errorf("restored attachment = %q after saving", data)
	}
}
