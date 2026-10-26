package keepass

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/tobischo/gokeepasslib/v3"
)

func TestRestoreHistorySelectsVersionBeforeHistoryLimitEvictsIt(t *testing.T) {
	forEachFormat(t, func(t *testing.T, kdbx4 bool) {
		for _, limit := range []int64{1, -1} {
			t.Run(fmt.Sprintf("limit=%d", limit), func(t *testing.T) {
				db := newFormatDB(t, kdbx4)
				must := func(err error) {
					t.Helper()
					if err != nil {
						t.Fatal(err)
					}
				}
				first := Entry{Title: "Original", UserName: "old user", Password: "old password",
					URL: "https://example.invalid/old", Notes: "old notes", TOTP: "JBSWY3DPEHPK3PXP",
					Tags: "favorite;old", Expires: true, ExpiryTime: time.Date(2035, 2, 3, 4, 5, 6, 0, time.UTC),
					CustomFields: []CustomField{{Key: "API token", Value: "old token", Protected: true}}}
				id, err := db.AddEntry(db.RootGroup().UUID, first)
				must(err)
				initial, err := db.GetEntry(id)
				must(err)
				for i := 1; i <= 3; i++ {
					must(db.UpdateEntry(id, Entry{Title: "New", Password: fmt.Sprintf("new-%d", i)}))
				}
				group, err := db.AddGroup(db.RootGroup().UUID, "Moved")
				must(err)
				must(db.MoveEntry(id, group))
				db.Database.Content.Meta.HistoryMaxItems = limit
				// Selecting the oldest version with a limit of one means adding
				// the current state evicts the selected history record itself.
				must(db.RestoreHistory(id, 2))
				db = saveAndReopen(t, db)
				got, err := db.GetEntry(id)
				must(err)
				if got.UUID != id || got.Title != first.Title || got.UserName != first.UserName || got.Password != first.Password ||
					got.URL != first.URL || got.Notes != first.Notes || got.TOTP != first.TOTP || got.Tags != first.Tags ||
					got.Expires != first.Expires || !got.ExpiryTime.Equal(first.ExpiryTime) || !reflect.DeepEqual(got.CustomFields, first.CustomFields) {
					t.Fatalf("history restore lost the selected version's fields: %+v", got)
				}
				if !got.CreationTime.Equal(initial.CreationTime.Truncate(time.Second)) {
					t.Fatal("history restore changed the entry's creation time")
				}
				parent, err := db.FindEntryParentUUID(id)
				must(err)
				if parent != group {
					t.Fatal("history restore moved the entry out of its current group")
				}
				history := db.EntryHistory(id)
				wantHistory := 4
				if limit == 1 {
					wantHistory = 1
				}
				if len(history) != wantHistory || history[0].Password != "new-3" {
					t.Fatal("restoring did not preserve the replaced current state within the history limit")
				}
			})
		}
	})
}

func TestRejectedHistoryRestoresLeaveEntriesUnchanged(t *testing.T) {
	db := newFormatDB(t, false)
	id, err := db.AddEntry(db.RootGroup().UUID, Entry{Title: "Service", Password: "old"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateEntry(id, Entry{Title: "Service", Password: "current"}); err != nil {
		t.Fatal(err)
	}
	before := db.RootGroup()
	for _, candidate := range []struct {
		id    gokeepasslib.UUID
		index int
	}{{id, -1}, {id, 1}, {gokeepasslib.NewUUID(), 0}} {
		if err := db.RestoreHistory(candidate.id, candidate.index); err == nil {
			t.Fatal("invalid history restore succeeded")
		}
		if !reflect.DeepEqual(db.RootGroup(), before) {
			t.Fatal("invalid history restore changed the database")
		}
	}
}

// Undo must restore the whole pre-restore state after a save has renumbered
// binary IDs, while keeping the entry in the group it has since moved into.
func TestReplaceHistorySnapshotAfterSaveAndMove(t *testing.T) {
	forEachFormat(t, func(t *testing.T, kdbx4 bool) {
		db := newFormatDB(t, kdbx4)
		must := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}
		id, err := db.AddEntry(db.RootGroup().UUID, Entry{Title: "Service", Password: "old"})
		must(err)
		must(db.AddAttachment(id, "old.key", []byte("old attachment")))
		must(db.UpdateEntry(id, Entry{Title: "Service", Password: "current"}))
		must(db.DeleteAttachment(id, "old.key"))
		must(db.AddAttachment(id, "current.key", []byte("Y3VycmVudCBhdHRhY2htZW50")))
		db = saveAndReopen(t, db)
		before, err := db.SnapshotEntry(id)
		must(err)
		must(db.RestoreHistory(id, 0))
		group, err := db.AddGroup(db.RootGroup().UUID, "Moved since restore")
		must(err)
		must(db.MoveEntry(id, group))
		// Discard the temporary restore's added history so its current.key
		// reference survives only in the independent undo snapshot.
		findEntry(&db.Database.Content.Root.Groups[0], id).Histories = nil
		db = saveAndReopen(t, db)
		must(db.ReplaceEntrySnapshot(before))
		db = saveAndReopen(t, db)
		entry, err := db.GetEntry(id)
		must(err)
		if entry.Password != "current" || len(db.EntryHistory(id)) != 1 || db.EntryHistory(id)[0].Password != "old" {
			t.Fatal("snapshot replacement did not restore the pre-restore entry and history")
		}
		parent, err := db.FindEntryParentUUID(id)
		must(err)
		if parent != group {
			t.Fatal("undo moved the entry back to the snapshot's original group")
		}
		for name, want := range map[string]string{"current.key": "Y3VycmVudCBhdHRhY2htZW50"} {
			path := filepath.Join(t.TempDir(), name)
			must(db.ExtractAttachment(id, name, path))
			if data := db.findBinaryByName(id, name); string(data) != want {
				t.Fatal("undo attached another binary after the save renumbered IDs")
			}
		}
		must(db.RestoreHistory(id, 0))
		if data := db.findBinaryByName(id, "old.key"); string(data) != "old attachment" {
			t.Fatal("undo lost an attachment referenced only by the restored history")
		}
	})
}
