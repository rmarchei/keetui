package keepass

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/tobischo/gokeepasslib/v3"
	w "github.com/tobischo/gokeepasslib/v3/wrappers"
)

func TestCustomDataRoundTrips(t *testing.T) {
	for _, kdbx4 := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "synthetic.kdbx")
		db, err := Create(path, "synthetic")
		if err != nil {
			t.Fatal(err)
		}
		if kdbx4 {
			if err := db.ConvertToKDBX4(); err != nil {
				t.Fatal(err)
			}
		}
		other := gokeepasslib.CustomData{Key: "KPXC_SYNTHETIC", Value: "kept"}
		db.Database.Content.Meta.CustomData = append(db.Database.Content.Meta.CustomData, other)

		if db.SetCustomData("KEETUI_TEST", "") {
			t.Error("removing a missing item reported a change")
		}
		if !db.SetCustomData("KEETUI_TEST", `{"auto_lock_minutes":10}`) {
			t.Error("adding an item reported no change")
		}
		if db.SetCustomData("KEETUI_TEST", `{"auto_lock_minutes":10}`) {
			t.Error("storing the same value reported a change")
		}
		if err := db.Save(); err != nil {
			t.Fatalf("kdbx4=%v: save: %v", kdbx4, err)
		}

		reopened, err := Open(path, "synthetic")
		if err != nil {
			t.Fatalf("kdbx4=%v: reopen: %v", kdbx4, err)
		}
		if got := reopened.CustomData("KEETUI_TEST"); got != `{"auto_lock_minutes":10}` {
			t.Errorf("kdbx4=%v: reopened item = %q", kdbx4, got)
		}
		if got := reopened.CustomData("KPXC_SYNTHETIC"); got != "kept" {
			t.Errorf("kdbx4=%v: another application's item = %q, want it kept", kdbx4, got)
		}

		if !reopened.SetCustomData("KEETUI_TEST", "") || reopened.CustomData("KEETUI_TEST") != "" {
			t.Errorf("kdbx4=%v: the item was not removed", kdbx4)
		}
		if reopened.CustomData("KPXC_SYNTHETIC") != "kept" {
			t.Errorf("kdbx4=%v: removing an item dropped another one", kdbx4)
		}
	}
}

// KDBX 4.1 files can stamp custom data items; updating one refreshes the
// stamp, and the file stays 4.1.
func TestCustomDataKeepsKDBX41Stamps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic41.kdbx")
	db, err := Create(path, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ConvertToKDBX4(); err != nil {
		t.Fatal(err)
	}
	gokeepasslib.WithDatabaseKDBXVersion41()(db.Database)
	old := w.TimeWrapper{Time: time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)}
	db.Database.Content.Meta.CustomData = append(db.Database.Content.Meta.CustomData,
		gokeepasslib.CustomData{Key: "KEETUI_TEST", Value: "old", LastModificationTime: &old})

	before := time.Now().Add(-time.Minute)
	if !db.SetCustomData("KEETUI_TEST", "new") {
		t.Fatal("update reported no change")
	}
	if err := db.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	reopened, err := Open(path, "synthetic")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if !reopened.Database.Header.IsKdbx41() {
		t.Error("the file is no longer KDBX 4.1")
	}
	for _, item := range reopened.Database.Content.Meta.CustomData {
		if item.Key != "KEETUI_TEST" {
			continue
		}
		if item.Value != "new" {
			t.Errorf("value = %q, want new", item.Value)
		}
		if item.LastModificationTime == nil || item.LastModificationTime.Time.Before(before) {
			t.Errorf("stamp = %v, want refreshed", item.LastModificationTime)
		}
		return
	}
	t.Fatal("item lost")
}
