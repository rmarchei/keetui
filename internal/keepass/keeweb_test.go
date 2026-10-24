package keepass

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// These are synthetic databases produced by kdbxweb, not by gokeepasslib.
// See testdata/keeweb/README.md for their provenance and dummy contents.
type keewebFixture struct {
	file          string
	kdbx4         bool
	emptyTimes    bool
	attachment    bool
	passwords     map[string]string
	historyTitle  string
	historySecret string
	deleteTitle   string
}

func keewebFixtures() []keewebFixture {
	passwords := map[string]string{
		"e1-root": "PASS-e1-v2", "e2-in-g1": "PASS-e2", "e3-in-g2": "PASS-e3",
		"e4-root": "PASS-e4", "e5-empty": "",
	}
	var fixtures []keewebFixture
	for _, file := range []string{"group-first-3.1.kdbx", "protected-binary-3.1.kdbx", "protected-binary-4.kdbx"} {
		fixtures = append(fixtures, keewebFixture{
			file: file, kdbx4: file == "protected-binary-4.kdbx",
			attachment: file != "group-first-3.1.kdbx", passwords: passwords,
			historyTitle: "e1-root", historySecret: "PASS-e1", deleteTitle: "e2-in-g1",
		})
	}
	return append(fixtures, keewebFixture{
		file: "empty-times-3.1.kdbx", emptyTimes: true,
		passwords: map[string]string{
			"t1": "PASS-1", "t2": "PASS-2", "t3": "PASS-3",
			"t4": "PASS-4", "t5": "PASS-5", "t6-in-g1": "PASS-6",
		},
		historyTitle: "t3", historySecret: "PASS-3-old", deleteTitle: "t1",
	})
}

func keewebEntries(t *testing.T, db *DB) map[string]Entry {
	t.Helper()
	entries := make(map[string]Entry)
	var visit func(Group)
	visit = func(group Group) {
		for _, entry := range group.Entries {
			if _, exists := entries[entry.Title]; exists {
				t.Fatalf("duplicate fixture entry %q", entry.Title)
			}
			entries[entry.Title] = entry
		}
		for _, child := range group.Groups {
			visit(child)
		}
	}
	visit(db.RootGroup())
	return entries
}

func assertKeewebFixture(t *testing.T, db *DB, fixture keewebFixture, deleted bool) {
	t.Helper()
	if db.SuspectCorruption() {
		t.Fatal("valid KeeWeb fixture flagged as corrupted")
	}
	entries := keewebEntries(t, db)
	wantCount := len(fixture.passwords)
	if deleted {
		wantCount--
	}
	if len(entries) != wantCount {
		t.Fatalf("got %d entries, want %d", len(entries), wantCount)
	}
	for title, password := range fixture.passwords {
		entry, exists := entries[title]
		if deleted && title == fixture.deleteTitle {
			if exists {
				t.Errorf("deleted entry %q reappeared", title)
			}
			continue
		}
		if !exists {
			t.Fatalf("missing entry %q", title)
		}
		if entry.Password != password {
			t.Errorf("entry %q: password = %q, want %q", title, entry.Password, password)
		}
	}
	history := db.EntryHistory(entries[fixture.historyTitle].UUID)
	if len(history) != 1 || history[0].Password != fixture.historySecret {
		t.Errorf("history for %q was not preserved: %+v", fixture.historyTitle, history)
	}
	if fixture.emptyTimes {
		if !entries["t3"].ExpiryTime.IsZero() {
			t.Error("unset expiry time was not preserved")
		}
		changed := db.Database.Content.Meta.MasterKeyChanged
		if changed == nil || !changed.Time.IsZero() {
			t.Error("unset master-key timestamp was not preserved")
		}
	} else {
		if entries["e4-root"].TOTP != "JBSWY3DPEHPK3PXP" {
			t.Error("protected custom otp field was not preserved")
		}
		if entries["e1-root"].UserName != "" {
			t.Error("expected root entry with an empty username")
		}
	}
	if fixture.attachment && !deleted {
		entry := entries["e2-in-g1"]
		attachments := db.EntryAttachments(entry.UUID)
		if len(attachments) != 1 || attachments[0].Name != "a.bin" || attachments[0].Size != 16 {
			t.Fatalf("attachment metadata was not preserved: %+v", attachments)
		}
		path := filepath.Join(t.TempDir(), "a.bin")
		if err := db.ExtractAttachment(entry.UUID, "a.bin", path); err != nil {
			t.Fatalf("extract attachment: %v", err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, bytes.Repeat([]byte{0x07}, 16)) {
			t.Error("attachment bytes were not preserved")
		}
	}
}

func TestKeewebCompatibility(t *testing.T) {
	for _, fixture := range keewebFixtures() {
		t.Run(fixture.file, func(t *testing.T) {
			for _, operation := range []string{"save", "delete", "upgrade"} {
				t.Run(operation, func(t *testing.T) {
					if operation == "upgrade" && fixture.kdbx4 {
						t.Skip("already KDBX 4")
					}
					db, err := Open(filepath.Join("testdata", "keeweb", fixture.file), "123")
					if err != nil {
						t.Fatalf("open KeeWeb fixture: %v", err)
					}
					assertKeewebFixture(t, db, fixture, false)
					switch operation {
					case "delete":
						// Removing the attachment's entry also exercises binary-pool
						// cleanup, which changes the protected stream on the next save.
						entry := keewebEntries(t, db)[fixture.deleteTitle]
						if err := db.PermanentlyDeleteEntry(entry.UUID); err != nil {
							t.Fatalf("delete entry: %v", err)
						}
					case "upgrade":
						if err := db.ConvertToKDBX4(); err != nil {
							t.Fatalf("convert to KDBX 4: %v", err)
						}
					}
					path := filepath.Join(t.TempDir(), "saved.kdbx")
					// Saves must not reuse the randomness of KeeWeb's header either.
					// Converting replaces the KDBX 3.1 fields, so an upgrade only
					// compares its own saves.
					var seen []headerRandomness
					if operation != "upgrade" {
						seen = append(seen, randomnessOf(db))
					}
					for range 2 {
						if err := db.SaveAs(path); err != nil {
							t.Fatalf("save KeeWeb fixture: %v", err)
						}
						// Saving must leave the live UI's database unlocked and intact.
						assertKeewebFixture(t, db, fixture, operation == "delete")
						reopened, err := Open(path, "123")
						if err != nil {
							t.Fatalf("reopen saved fixture: %v", err)
						}
						if reopened.IsKdbx4() != (fixture.kdbx4 || operation == "upgrade") {
							t.Error("unexpected database format after saving")
						}
						assertKeewebFixture(t, reopened, fixture, operation == "delete")
						got := randomnessOf(reopened)
						assertFreshRandomness(t, got, seen...)
						seen = append(seen, got)
					}
				})
			}
		})
	}
}
