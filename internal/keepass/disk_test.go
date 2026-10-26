package keepass

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tobischo/gokeepasslib/v3"
	w "github.com/tobischo/gokeepasslib/v3/wrappers"
)

// syncBase is when the synthetic fixture's entries were last changed; the
// tests date every later change explicitly, as KDBX keeps whole seconds.
var syncBase = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func setModified(t *testing.T, db *DB, id gokeepasslib.UUID, at time.Time) {
	t.Helper()
	e := findEntry(&db.Database.Content.Root.Groups[0], id)
	if e == nil {
		t.Fatal("entry not found")
	}
	e.Times.LastModificationTime = &w.TimeWrapper{Time: at}
}

func setMoved(t *testing.T, db *DB, id gokeepasslib.UUID, at time.Time) {
	t.Helper()
	e := findEntry(&db.Database.Content.Root.Groups[0], id)
	if e == nil {
		t.Fatal("entry not found")
	}
	e.Times.LocationChanged = &w.TimeWrapper{Time: at}
}

func setPassword(t *testing.T, db *DB, id gokeepasslib.UUID, password string, at time.Time) {
	t.Helper()
	e, err := db.GetEntry(id)
	if err != nil {
		t.Fatal(err)
	}
	e.Password = password
	if err := db.UpdateEntry(id, e); err != nil {
		t.Fatal(err)
	}
	setModified(t, db, id, at)
}

func entryByTitle(t *testing.T, db *DB, title string) (Entry, bool) {
	t.Helper()
	var found *Entry
	var walk func(g Group)
	walk = func(g Group) {
		for _, e := range g.Entries {
			if e.Title == title {
				found = &e
			}
		}
		for _, sg := range g.Groups {
			walk(sg)
		}
	}
	walk(db.RootGroup())
	if found == nil {
		return Entry{}, false
	}
	return *found, true
}

// syncFixture saves a synthetic vault and opens it twice: ours is keetui's
// copy, theirs another program's.
func syncFixture(t *testing.T, kdbx4 bool) (path string, ours, theirs *DB, ids map[string]gokeepasslib.UUID) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "synthetic.kdbx")
	db, err := Create(path, "synthetic-master")
	if err != nil {
		t.Fatal(err)
	}
	if kdbx4 {
		if err := db.ConvertToKDBX4(); err != nil {
			t.Fatal(err)
		}
	}
	db.Database.Content.Meta.RecycleBinEnabled = w.NewBoolWrapper(true)
	mail, err := db.AddGroup(db.RootGroup().UUID, "Mail")
	if err != nil {
		t.Fatal(err)
	}
	ids = map[string]gokeepasslib.UUID{"Mail": mail}
	for _, title := range []string{"Edited there", "Edited here", "Deleted here", "Moved there", "Deleted there", "Missing there",
		"Deleted there, edited here", "Edited on both", "Edited there, deleted here"} {
		id, err := db.AddEntry(mail, Entry{Title: title, Password: "base-" + title})
		if err != nil {
			t.Fatal(err)
		}
		setModified(t, db, id, syncBase)
		setMoved(t, db, id, syncBase)
		ids[title] = id
	}
	if err := db.Overwrite(); err != nil {
		t.Fatal(err)
	}
	if ours, err = Open(path, "synthetic-master"); err != nil {
		t.Fatal(err)
	}
	if theirs, err = Open(path, "synthetic-master"); err != nil {
		t.Fatal(err)
	}
	return path, ours, theirs, ids
}

// An attachment whose contents look like base64, which the pinned
// gokeepasslib decodes when a KDBX 4 attachment is re-added by contents.
const syncAttachment = "QUJDRA=="

// Another program changes the vault while keetui has it open: keetui's save
// stops instead of overwriting, and the merge keeps both sides' changes.
func TestMergeDiskKeepsBothSidesChanges(t *testing.T) {
	for _, kdbx4 := range []bool{false, true} {
		t.Run(map[bool]string{false: "KDBX 3.1", true: "KDBX 4"}[kdbx4], func(t *testing.T) {
			path, ours, theirs, ids := syncFixture(t, kdbx4)
			there, here := syncBase.Add(time.Hour), syncBase.Add(2*time.Hour)

			added, err := theirs.AddEntry(ids["Mail"], Entry{Title: "Added there", Password: "there-new"})
			if err != nil {
				t.Fatal(err)
			}
			if err := theirs.AddAttachment(added, "synthetic.txt", []byte(syncAttachment)); err != nil {
				t.Fatal(err)
			}
			setPassword(t, theirs, ids["Edited there"], "there-edit", there)
			setPassword(t, theirs, ids["Edited on both"], "there-both", there)
			setPassword(t, theirs, ids["Edited there, deleted here"], "there-revived", there)
			if err := theirs.DeleteEntry(ids["Moved there"]); err != nil { // to the recycle bin
				t.Fatal(err)
			}
			setMoved(t, theirs, ids["Moved there"], there)
			for _, title := range []string{"Deleted there", "Deleted there, edited here"} {
				if err := theirs.PermanentlyDeleteEntry(ids[title]); err != nil {
					t.Fatal(err)
				}
			}
			for i, o := range theirs.Database.Content.Root.DeletedObjects {
				if o.UUID.Compare(ids["Deleted there, edited here"]) {
					theirs.Database.Content.Root.DeletedObjects[i].DeletionTime = &w.TimeWrapper{Time: there}
				}
			}
			// Gone without a record of its deletion, as in an older copy.
			removeEntry(&theirs.Database.Content.Root.Groups[0], ids["Missing there"])
			group, err := theirs.AddGroup(theirs.RootGroup().UUID, "New there")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := theirs.AddEntry(group, Entry{Title: "In new group", Password: "there-group"}); err != nil {
				t.Fatal(err)
			}
			if err := theirs.Save(); err != nil {
				t.Fatal(err)
			}

			setPassword(t, ours, ids["Edited here"], "here-edit", here)
			setPassword(t, ours, ids["Deleted there, edited here"], "here-kept", here)
			setPassword(t, ours, ids["Edited on both"], "here-both", here)
			for _, title := range []string{"Deleted here", "Edited there, deleted here"} {
				if err := ours.PermanentlyDeleteEntry(ids[title]); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := ours.AddEntry(ids["Mail"], Entry{Title: "Added here", Password: "here-new"}); err != nil {
				t.Fatal(err)
			}

			theirBytes, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var diskErr *DiskError
			if err := ours.Save(); !errors.As(err, &diskErr) || diskErr.State != DiskChanged {
				t.Fatalf("save over another program's changes: %v, want a DiskChanged error", err)
			}
			if now, _ := os.ReadFile(path); string(now) != string(theirBytes) {
				t.Fatal("the refused save wrote the file")
			}

			result, err := ours.MergeDisk()
			if err != nil {
				t.Fatal(err)
			}
			// The groups added there: its recycle bin and "New there".
			want := SyncResult{Added: 2, Updated: 1, Moved: 1, Restored: 1, Deleted: 1, Kept: 2, Groups: 2}
			if result != want {
				t.Fatalf("merge result %+v, want %+v", result, want)
			}
			if err := ours.Save(); err != nil {
				t.Fatalf("save after the merge: %v", err)
			}
			if state := ours.CheckDisk(); state != DiskUnchanged {
				t.Fatalf("after saving, the file reads as %v", state)
			}

			saved, err := Open(path, "synthetic-master")
			if err != nil {
				t.Fatal(err)
			}
			for title, password := range map[string]string{
				"Added there":                "there-new",
				"Added here":                 "here-new",
				"Edited there":               "there-edit",
				"Edited here":                "here-edit",
				"Edited on both":             "here-both",
				"Edited there, deleted here": "there-revived",
				"Missing there":              "base-Missing there",
				"Deleted there, edited here": "here-kept",
				"In new group":               "there-group",
			} {
				e, ok := entryByTitle(t, saved, title)
				if !ok || e.Password != password {
					t.Errorf("%s: found %v, password %q, want %q", title, ok, e.Password, password)
				}
			}
			for _, title := range []string{"Deleted here", "Deleted there"} {
				if _, ok := entryByTitle(t, saved, title); ok {
					t.Errorf("%q came back", title)
				}
			}
			if parent, err := saved.FindEntryParentUUID(ids["Moved there"]); err != nil || !saved.IsRecycleBin(parent) {
				t.Error("the entry moved to the recycle bin there is not in the recycle bin")
			}
			history := func(id gokeepasslib.UUID) []string {
				var passwords []string
				for _, h := range saved.EntryHistory(id) {
					passwords = append(passwords, h.Password)
				}
				return passwords
			}
			if got := history(ids["Edited on both"]); !strings.Contains(strings.Join(got, ","), "there-both") {
				t.Errorf("history of the entry edited on both sides %v lacks their version", got)
			}
			if got := history(ids["Edited there"]); !strings.Contains(strings.Join(got, ","), "base-Edited there") {
				t.Errorf("history of the entry edited there %v lacks the version before", got)
			}
			addedThere, _ := entryByTitle(t, saved, "Added there")
			out := filepath.Join(t.TempDir(), "out.txt")
			if err := saved.ExtractAttachment(addedThere.UUID, "synthetic.txt", out); err != nil {
				t.Fatal(err)
			}
			if data, _ := os.ReadFile(out); string(data) != syncAttachment {
				t.Fatalf("attachment added there reads %q, want %q", data, syncAttachment)
			}
		})
	}
}

func TestCheckDiskStates(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "volume")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "synthetic.kdbx")
	if _, err := Create(path, "synthetic-master"); err != nil {
		t.Fatal(err)
	}
	ours, err := Open(path, "synthetic-master")
	if err != nil {
		t.Fatal(err)
	}
	if state := ours.CheckDisk(); state != DiskUnchanged {
		t.Fatalf("just opened: %v", state)
	}
	// A sync that writes the same bytes again changes nothing.
	data, _ := os.ReadFile(path)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := ours.Save(); err != nil {
		t.Fatalf("save after the same bytes were written again: %v", err)
	}
	if err := ours.Save(); err != nil {
		t.Fatalf("a second save of keetui's own: %v", err)
	}

	theirs, err := Open(path, "synthetic-master")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := theirs.AddEntry(theirs.RootGroup().UUID, Entry{Title: "Theirs"}); err != nil {
		t.Fatal(err)
	}
	if err := theirs.Save(); err != nil {
		t.Fatal(err)
	}
	if state := ours.CheckDisk(); state != DiskChanged {
		t.Fatalf("after another program saved: %v", state)
	}
	if err := ours.Overwrite(); err != nil {
		t.Fatal(err)
	}
	if reopened, _ := Open(path, "synthetic-master"); reopened == nil {
		t.Fatal("overwritten vault does not open")
	} else if _, ok := entryByTitle(t, reopened, "Theirs"); ok {
		t.Fatal("overwrite kept the other program's entry")
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	var diskErr *DiskError
	if err := ours.Save(); !errors.As(err, &diskErr) || diskErr.State != DiskMissing {
		t.Fatalf("save to a deleted file: %v, want DiskMissing", err)
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := ours.Save(); !errors.As(err, &diskErr) || diskErr.State != DiskNoFolder {
		t.Fatalf("save with the folder gone: %v, want DiskNoFolder", err)
	}
}

// The file on disk is read with the credentials it was written with: a
// master password changed here applies from the next save.
func TestMergeDiskUsesTheFilesCredentials(t *testing.T) {
	path, ours, theirs, _ := syncFixture(t, true)
	if _, err := theirs.AddEntry(theirs.RootGroup().UUID, Entry{Title: "Theirs"}); err != nil {
		t.Fatal(err)
	}
	if err := theirs.Save(); err != nil {
		t.Fatal(err)
	}
	if err := ours.ChangePassword("synthetic-new"); err != nil {
		t.Fatal(err)
	}
	if _, err := ours.MergeDisk(); err != nil {
		t.Fatalf("merge after changing the master password here: %v", err)
	}
	if err := ours.Save(); err != nil {
		t.Fatal(err)
	}
	saved, err := Open(path, "synthetic-new")
	if err != nil {
		t.Fatalf("the merged vault does not open with the new password: %v", err)
	}
	if _, ok := entryByTitle(t, saved, "Theirs"); !ok {
		t.Fatal("the other program's entry is missing")
	}

	// Changed there: keetui cannot read the file, and changes nothing.
	other, err := Open(path, "synthetic-new")
	if err != nil {
		t.Fatal(err)
	}
	if err := other.ChangePassword("synthetic-theirs"); err != nil {
		t.Fatal(err)
	}
	if err := other.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := ours.MergeDisk(); err == nil || !strings.Contains(err.Error(), "master password") {
		t.Fatalf("merge of a file with another master password: %v", err)
	}
	if state := ours.CheckDisk(); state != DiskChanged {
		t.Fatalf("a failed merge counts the file as %v", state)
	}
}

// Save as moves the database: later saves check the new file.
func TestSaveAsMovesTheChecks(t *testing.T) {
	path, ours, theirs, _ := syncFixture(t, false)
	moved := filepath.Join(filepath.Dir(path), "moved.kdbx")
	if err := ours.SaveAs(moved); err != nil {
		t.Fatal(err)
	}
	if ours.Path != moved {
		t.Fatalf("path %q after save as, want %q", ours.Path, moved)
	}
	if err := theirs.Save(); err != nil { // the old file
		t.Fatal(err)
	}
	if err := ours.Save(); err != nil {
		t.Fatalf("save after the old file changed: %v", err)
	}
}

// keetui records moves and changes the way a sync tells sides apart.
func TestChangesRecordTheirTime(t *testing.T) {
	_, ours, _, ids := syncFixture(t, false)
	before := time.Now().Add(-time.Second)
	if err := ours.ToggleFavorite(ids["Edited here"]); err != nil {
		t.Fatal(err)
	}
	if err := ours.AddAttachment(ids["Edited there"], "a.txt", []byte("synthetic")); err != nil {
		t.Fatal(err)
	}
	if err := ours.MoveEntry(ids["Moved there"], ours.RootGroup().UUID); err != nil {
		t.Fatal(err)
	}
	if err := ours.RenameGroup(ids["Mail"], "Renamed"); err != nil {
		t.Fatal(err)
	}
	root := &ours.Database.Content.Root.Groups[0]
	for name, at := range map[string]*w.TimeWrapper{
		"favorite":   findEntry(root, ids["Edited here"]).Times.LastModificationTime,
		"attachment": findEntry(root, ids["Edited there"]).Times.LastModificationTime,
		"move":       findEntry(root, ids["Moved there"]).Times.LocationChanged,
		"rename":     findGroup(root, ids["Mail"]).Times.LastModificationTime,
	} {
		if at == nil || at.Time.Before(before) {
			t.Errorf("%s: time %v not recorded", name, at)
		}
	}
}

// Permanent deletions are recorded, as KeePass does, so that syncs elsewhere
// delete too; putting an entry or group back (undo) drops the record.
func TestPermanentDeletionsAreRecorded(t *testing.T) {
	_, db, _, ids := syncFixture(t, false)
	recorded := func(id gokeepasslib.UUID) bool {
		for _, o := range db.Database.Content.Root.DeletedObjects {
			if o.UUID.Compare(id) {
				return true
			}
		}
		return false
	}
	entry := ids["Deleted here"]
	snapshot, err := db.SnapshotEntry(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PermanentlyDeleteEntry(entry); err != nil || !recorded(entry) {
		t.Fatalf("permanent delete: %v, recorded %v", err, recorded(entry))
	}
	if err := db.InsertEntrySnapshot(ids["Mail"], snapshot); err != nil || recorded(entry) {
		t.Fatalf("undo: %v, still recorded %v", err, recorded(entry))
	}

	group, err := db.SnapshotGroup(ids["Mail"])
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PermanentlyDeleteGroup(ids["Mail"]); err != nil {
		t.Fatal(err)
	}
	if !recorded(ids["Mail"]) || !recorded(ids["Edited here"]) {
		t.Fatal("a group deleted for good is not recorded with its entries")
	}
	if err := db.InsertGroupSnapshot(db.RootGroup().UUID, group); err != nil {
		t.Fatal(err)
	}
	if recorded(ids["Mail"]) || recorded(ids["Edited here"]) {
		t.Fatal("undoing the group's deletion kept its records")
	}

	if err := db.DeleteEntry(ids["Moved there"]); err != nil { // to the recycle bin
		t.Fatal(err)
	}
	if recorded(ids["Moved there"]) {
		t.Fatal("a move to the recycle bin was recorded as a deletion")
	}
	if err := db.EmptyRecycleBin(); err != nil {
		t.Fatal(err)
	}
	if !recorded(ids["Moved there"]) || recorded(db.RecycleBinUUID()) {
		t.Fatal("emptying the recycle bin recorded the wrong things")
	}
}

// KeePassXC upgrades a KDBX 3.1 file when it saves it, and its KDBX 4.1
// fields cannot go back into 3.1: the merge moves the database to the
// file's format, attachments included.
func TestMergeDiskAdoptsANewerFormat(t *testing.T) {
	path, ours, theirs, ids := syncFixture(t, false)
	if err := theirs.ConvertToKDBX4(); err != nil {
		t.Fatal(err)
	}
	gokeepasslib.WithDatabaseKDBXVersion41()(theirs.Database)
	if err := theirs.MoveEntry(ids["Moved there"], theirs.RootGroup().UUID); err != nil {
		t.Fatal(err)
	}
	setMoved(t, theirs, ids["Moved there"], syncBase.Add(time.Hour))
	mail := ids["Mail"]
	findEntry(&theirs.Database.Content.Root.Groups[0], ids["Moved there"]).PreviousParentGroup = &mail
	if err := theirs.Save(); err != nil {
		t.Fatal(err)
	}
	if err := ours.AddAttachment(ids["Edited here"], "synthetic.txt", []byte(syncAttachment)); err != nil {
		t.Fatal(err)
	}
	if _, err := ours.MergeDisk(); err != nil {
		t.Fatal(err)
	}
	if err := ours.Save(); err != nil {
		t.Fatalf("save after merging a KDBX 4.1 file: %v", err)
	}
	saved, err := Open(path, "synthetic-master")
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Database.Header.IsKdbx41() {
		t.Fatalf("saved as %s, want the file's KDBX 4.1", saved.FormatVersion())
	}
	if e := findEntry(&saved.Database.Content.Root.Groups[0], ids["Moved there"]); e == nil || e.PreviousParentGroup == nil {
		t.Fatal("the entry's previous group was dropped")
	}
	out := filepath.Join(t.TempDir(), "out.txt")
	if err := saved.ExtractAttachment(ids["Edited here"], "synthetic.txt", out); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(out); string(data) != syncAttachment {
		t.Fatalf("attachment reads %q after the format change", data)
	}
}

// syntheticIcon is a custom icon as another client adds one (a downloaded
// favicon): the PNG signature, base64.
func syntheticIcon() gokeepasslib.CustomIcon {
	return gokeepasslib.CustomIcon{UUID: gokeepasslib.NewUUID(), Data: "iVBORw0KGgo="}
}

// Icons and database settings changed there come along: the icon of an
// entry added there, the name changed there more recently, and the custom
// data item by item against the file as keetui last read it. KDBX 4.1 dates
// items, so an item changed on both sides keeps the more recent value.
func TestMergeDiskJoinsIconsAndSettings(t *testing.T) {
	for _, kdbx41 := range []bool{false, true} {
		t.Run(fmt.Sprintf("kdbx41=%t", kdbx41), func(t *testing.T) {
			path, ours, _, _ := syncFixture(t, true)
			if kdbx41 {
				gokeepasslib.WithDatabaseKDBXVersion41()(ours.Database)
			}
			later := syncBase.Add(time.Hour)
			setItem := func(db *DB, key, value string, at time.Time) {
				t.Helper()
				db.SetCustomData(key, value)
				if !kdbx41 || value == "" {
					return
				}
				items := db.Database.Content.Meta.CustomData
				items[slices.IndexFunc(items, func(c gokeepasslib.CustomData) bool { return c.Key == key })].LastModificationTime = &w.TimeWrapper{Time: at}
			}
			for _, key := range []string{"Changed there", "Changed here", "Changed on both", "Removed there", "Removed here", "Removed here, changed there"} {
				setItem(ours, key, "base", syncBase)
			}
			meta := ours.Database.Content.Meta
			meta.DatabaseName, meta.DatabaseNameChanged = "Base name", &w.TimeWrapper{Time: syncBase}
			meta.DatabaseDescription, meta.DatabaseDescriptionChanged = "Base description", &w.TimeWrapper{Time: syncBase}
			if err := ours.Overwrite(); err != nil {
				t.Fatal(err)
			}

			theirs, err := Open(path, "synthetic-master")
			if err != nil {
				t.Fatal(err)
			}
			setItem(theirs, "Changed there", "there", later)
			setItem(theirs, "Changed on both", "there", later.Add(time.Minute))
			setItem(theirs, "Removed there", "", later)
			setItem(theirs, "Removed here, changed there", "there", later)
			setItem(theirs, "Added there", "there", later)
			theirMeta := theirs.Database.Content.Meta
			theirMeta.DatabaseName, theirMeta.DatabaseNameChanged = "Their name", &w.TimeWrapper{Time: later}
			theirMeta.DatabaseDescription, theirMeta.DatabaseDescriptionChanged = "Their description", &w.TimeWrapper{Time: later}
			icon := syntheticIcon()
			theirMeta.CustomIcons = append(theirMeta.CustomIcons, icon)
			id, err := theirs.AddEntry(theirs.RootGroup().UUID, Entry{Title: "Favicon"})
			if err != nil {
				t.Fatal(err)
			}
			findEntry(&theirs.Database.Content.Root.Groups[0], id).CustomIconUUID = icon.UUID
			if err := theirs.Save(); err != nil {
				t.Fatal(err)
			}

			setItem(ours, "Changed here", "here", later)
			setItem(ours, "Changed on both", "here", later)
			setItem(ours, "Removed here", "", later)
			setItem(ours, "Removed here, changed there", "", later)
			setItem(ours, "Added here", "here", later)
			meta.DatabaseDescription, meta.DatabaseDescriptionChanged = "Our description", &w.TimeWrapper{Time: later.Add(time.Minute)}

			r, err := ours.MergeDisk()
			if err != nil {
				t.Fatal(err)
			}
			// The name; changed there, removed there, added there, removed
			// here but changed there since; with dates, changed on both.
			bothChanged, settings := "here", 5
			if kdbx41 {
				bothChanged, settings = "there", 6
			}
			if r.Icons != 1 || r.Settings != settings {
				t.Fatalf("icons %d, settings %d: want 1 and %d", r.Icons, r.Settings, settings)
			}
			if err := ours.Save(); err != nil {
				t.Fatal(err)
			}
			saved, err := Open(path, "synthetic-master")
			if err != nil {
				t.Fatal(err)
			}
			for key, want := range map[string]string{"Changed there": "there", "Changed here": "here", "Changed on both": bothChanged,
				"Removed there": "", "Removed here": "", "Removed here, changed there": "there", "Added there": "there", "Added here": "here"} {
				if got := saved.CustomData(key); got != want {
					t.Errorf("%s: %q, want %q", key, got, want)
				}
			}
			if m := saved.Database.Content.Meta; m.DatabaseName != "Their name" || m.DatabaseDescription != "Our description" {
				t.Errorf("name %q, description %q: want each side's more recent one", m.DatabaseName, m.DatabaseDescription)
			}
			if e := findEntry(&saved.Database.Content.Root.Groups[0], id); e == nil || !e.CustomIconUUID.Compare(icon.UUID) {
				t.Fatal("the entry added there lost its icon")
			}
			if !slices.ContainsFunc(saved.Database.Content.Meta.CustomIcons, func(c gokeepasslib.CustomIcon) bool {
				return c.UUID.Compare(icon.UUID) && c.Data == icon.Data
			}) {
				t.Fatal("the saved file lacks the icon its entry uses")
			}
		})
	}
}
