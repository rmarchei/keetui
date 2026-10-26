package keepass

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/tobischo/gokeepasslib/v3"
	w "github.com/tobischo/gokeepasslib/v3/wrappers"
)

func TestMerge(t *testing.T) {
	dir := t.TempDir()
	password := "testmaster"

	// Create target DB
	targetPath := filepath.Join(dir, "target.kdbx")
	target, err := Create(targetPath, password)
	if err != nil {
		t.Fatalf("Create target: %v", err)
	}

	// Create source DB with some entries
	srcPath := filepath.Join(dir, "source.kdbx")
	createTestDB(t, srcPath, password)
	source, err := Open(srcPath, password)
	if err != nil {
		t.Fatalf("Open source: %v", err)
	}

	result, err := target.Merge(source)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	if result.EntriesAdded == 0 {
		t.Error("no entries added during merge")
	}

	// Save and verify
	savePath := filepath.Join(dir, "merged.kdbx")
	if err := target.SaveAs(savePath); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}

	merged, err := Open(savePath, password)
	if err != nil {
		t.Fatalf("Open merged: %v", err)
	}

	root := merged.RootGroup()
	totalEntries := countEntries(&root)
	if totalEntries == 0 {
		t.Error("no entries in merged database")
	}
}

func countEntries(g *Group) int {
	count := len(g.Entries)
	for i := range g.Groups {
		count += countEntries(&g.Groups[i])
	}
	return count
}

func TestMergeCreatesGroups(t *testing.T) {
	dir := t.TempDir()
	password := "testmaster"

	// Create target DB (empty)
	targetPath := filepath.Join(dir, "target.kdbx")
	target, err := Create(targetPath, password)
	if err != nil {
		t.Fatalf("Create target: %v", err)
	}

	// Create source with a sub-group
	srcPath := filepath.Join(dir, "source.kdbx")
	createTestDB(t, srcPath, password)
	source, err := Open(srcPath, password)
	if err != nil {
		t.Fatalf("Open source: %v", err)
	}

	result, err := target.Merge(source)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	if result.GroupsCreated == 0 {
		t.Error("no groups created during merge")
	}
}

func TestMergeSplitCounts(t *testing.T) {
	dir := t.TempDir()
	password := "testmaster"

	// Create target DB (empty)
	targetPath := filepath.Join(dir, "target.kdbx")
	target, err := Create(targetPath, password)
	if err != nil {
		t.Fatalf("Create target: %v", err)
	}

	// Create source with entries
	srcPath := filepath.Join(dir, "source.kdbx")
	createTestDB(t, srcPath, password)
	source, err := Open(srcPath, password)
	if err != nil {
		t.Fatalf("Open source: %v", err)
	}

	result, err := target.Merge(source)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	// All should be adds since target was empty
	if result.EntriesAdded == 0 {
		t.Error("should have added entries")
	}
	if result.EntriesUpdated != 0 {
		t.Errorf("EntriesUpdated = %d, want 0", result.EntriesUpdated)
	}
}

func TestMergePreview(t *testing.T) {
	dir := t.TempDir()
	password := "testmaster"

	// Create target DB (empty)
	targetPath := filepath.Join(dir, "target.kdbx")
	target, err := Create(targetPath, password)
	if err != nil {
		t.Fatalf("Create target: %v", err)
	}

	// Create source with entries
	srcPath := filepath.Join(dir, "source.kdbx")
	createTestDB(t, srcPath, password)
	source, err := Open(srcPath, password)
	if err != nil {
		t.Fatalf("Open source: %v", err)
	}

	result, err := target.MergePreview(source)
	if err != nil {
		t.Fatalf("MergePreview: %v", err)
	}

	if result.EntriesAdded == 0 {
		t.Error("preview should show entries to add")
	}
	if len(result.EntryActions) == 0 {
		t.Error("preview should have entry actions")
	}
	for _, ea := range result.EntryActions {
		if ea.Action != "add" {
			t.Errorf("all entries should be 'add' for empty target, got %q", ea.Action)
		}
	}

	// Verify target was NOT modified
	targetRoot := target.RootGroup()
	if countEntries(&targetRoot) != 0 {
		t.Error("target should not be modified by MergePreview")
	}
}

func TestMergeSkipsOlderEntries(t *testing.T) {
	dir := t.TempDir()
	password := "testmaster"

	// Create a DB with an entry and save it
	basePath := filepath.Join(dir, "base.kdbx")
	base, err := Create(basePath, password)
	if err != nil {
		t.Fatalf("Create base: %v", err)
	}
	root := base.RootGroup()
	uuid, err := base.AddEntry(root.UUID, Entry{
		Title:    "Shared Entry",
		UserName: "user",
		Password: "originalpass",
	})
	if err != nil {
		t.Fatalf("AddEntry: %v", err)
	}
	if err := base.SaveAs(basePath); err != nil {
		t.Fatalf("SaveAs base: %v", err)
	}

	// Open the same file as the source (has original timestamp)
	source, err := Open(basePath, password)
	if err != nil {
		t.Fatalf("Open source: %v", err)
	}

	// Reopen as target and update the entry (makes it newer)
	target, err := Open(basePath, password)
	if err != nil {
		t.Fatalf("Open target: %v", err)
	}
	if err := target.UpdateEntry(uuid, Entry{
		Title:    "Shared Entry",
		UserName: "user",
		Password: "updatedpass",
	}); err != nil {
		t.Fatalf("UpdateEntry target: %v", err)
	}

	// Merge source into target — source entry is older, should be skipped
	result, err := target.Merge(source)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	if result.EntriesSkipped != 1 {
		t.Errorf("EntriesSkipped = %d, want 1 (older entry should be skipped)", result.EntriesSkipped)
	}

	// Verify target entry was NOT overwritten
	entry, err := target.GetEntry(uuid)
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if entry.Password != "updatedpass" {
		t.Errorf("password = %q, want %q (should not be overwritten by older source)", entry.Password, "updatedpass")
	}
}

func TestMergePreviewDoesNotModify(t *testing.T) {
	dir := t.TempDir()
	password := "testmaster"

	// Create target with an existing entry
	targetPath := filepath.Join(dir, "target.kdbx")
	target, err := Create(targetPath, password)
	if err != nil {
		t.Fatalf("Create target: %v", err)
	}
	root := target.RootGroup()
	_, err = target.AddEntry(root.UUID, Entry{Title: "Existing", UserName: "user"})
	if err != nil {
		t.Fatalf("AddEntry: %v", err)
	}

	// Create source with entries
	srcPath := filepath.Join(dir, "source.kdbx")
	createTestDB(t, srcPath, password)
	source, err := Open(srcPath, password)
	if err != nil {
		t.Fatalf("Open source: %v", err)
	}

	// Count entries before preview
	targetRoot := target.RootGroup()
	beforeCount := countEntries(&targetRoot)

	result, err := target.MergePreview(source)
	if err != nil {
		t.Fatalf("MergePreview: %v", err)
	}

	// Verify target was NOT modified
	targetRoot = target.RootGroup()
	afterCount := countEntries(&targetRoot)
	if afterCount != beforeCount {
		t.Errorf("target entry count changed: before=%d, after=%d", beforeCount, afterCount)
	}

	// Verify result has actions
	if len(result.EntryActions) == 0 {
		t.Error("should have entry actions")
	}
	if result.EntriesAdded == 0 && result.EntriesUpdated == 0 && result.EntriesSkipped == 0 {
		t.Error("should have some entry action counts")
	}
}

func TestMergePreservesUUIDsAcrossRepeatMerges(t *testing.T) {
	dir := t.TempDir()
	password := "testmaster"

	target, err := Create(filepath.Join(dir, "target.kdbx"), password)
	if err != nil {
		t.Fatalf("Create target: %v", err)
	}

	srcPath := filepath.Join(dir, "source.kdbx")
	source, err := Create(srcPath, password)
	if err != nil {
		t.Fatalf("Create source: %v", err)
	}
	srcRoot := source.RootGroup()
	srcUUID, err := source.AddEntry(srcRoot.UUID, Entry{Title: "Synced", UserName: "u"})
	if err != nil {
		t.Fatalf("AddEntry: %v", err)
	}

	// First merge adds the entry under its original UUID.
	result, err := target.Merge(source)
	if err != nil {
		t.Fatalf("Merge 1: %v", err)
	}
	if result.EntriesAdded != 1 {
		t.Fatalf("merge 1 added = %d, want 1", result.EntriesAdded)
	}
	if _, err := target.GetEntry(srcUUID); err != nil {
		t.Fatalf("merged entry must keep the source UUID: %v", err)
	}

	// Repeat merge (the periodic-sync workflow) must not duplicate.
	result, err = target.Merge(source)
	if err != nil {
		t.Fatalf("Merge 2: %v", err)
	}
	if result.EntriesAdded != 0 {
		t.Errorf("merge 2 added = %d, want 0 (no duplication)", result.EntriesAdded)
	}
	if result.EntriesSkipped != 1 {
		t.Errorf("merge 2 skipped = %d, want 1", result.EntriesSkipped)
	}
	tRoot := target.RootGroup()
	if got := countEntries(&tRoot); got != 1 {
		t.Errorf("target entries after two merges = %d, want 1", got)
	}
}

func TestMergeCopiesAttachmentsHistoryAndTimestamps(t *testing.T) {
	dir := t.TempDir()
	password := "testmaster"

	target, err := Create(filepath.Join(dir, "target.kdbx"), password)
	if err != nil {
		t.Fatalf("Create target: %v", err)
	}

	source, err := Create(filepath.Join(dir, "source.kdbx"), password)
	if err != nil {
		t.Fatalf("Create source: %v", err)
	}
	srcRoot := source.RootGroup()
	srcUUID, err := source.AddEntry(srcRoot.UUID, Entry{Title: "Rich", Password: "old"})
	if err != nil {
		t.Fatalf("AddEntry: %v", err)
	}
	if err := source.UpdateEntry(srcUUID, Entry{Title: "Rich", Password: "new"}); err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}
	attachment := []byte("recovery codes")
	if err := source.AddAttachment(srcUUID, "codes.pdf", attachment); err != nil {
		t.Fatalf("AddAttachment: %v", err)
	}
	srcRaw := findEntry(&source.Database.Content.Root.Groups[0], srcUUID)
	srcMod := srcRaw.Times.LastModificationTime.Time

	if _, err := target.Merge(source); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	// Save and reopen so binary pool handling is exercised end to end.
	mergedPath := filepath.Join(dir, "merged.kdbx")
	if err := target.SaveAs(mergedPath); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	merged, err := Open(mergedPath, password)
	if err != nil {
		t.Fatalf("Open merged: %v", err)
	}

	atts := merged.EntryAttachments(srcUUID)
	if len(atts) != 1 || atts[0].Name != "codes.pdf" {
		t.Fatalf("attachments = %+v, want codes.pdf", atts)
	}
	dest := filepath.Join(dir, "extracted.pdf")
	if err := merged.ExtractAttachment(srcUUID, "codes.pdf", dest); err != nil {
		t.Fatalf("ExtractAttachment: %v", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read extracted: %v", err)
	}
	if !bytes.Equal(data, attachment) {
		t.Errorf("attachment content = %q, want %q", data, attachment)
	}

	history := merged.EntryHistory(srcUUID)
	if len(history) != 1 || history[0].Password != "old" {
		t.Errorf("history = %+v, want one snapshot with the old password", history)
	}

	// KDBX serialization stores times with second precision.
	srcMod = srcMod.Truncate(time.Second)
	mergedRaw := findEntry(&merged.Database.Content.Root.Groups[0], srcUUID)
	if !mergedRaw.Times.LastModificationTime.Time.Equal(srcMod) {
		t.Errorf("modification time = %v, want source's %v (timestamps must be preserved)",
			mergedRaw.Times.LastModificationTime.Time, srcMod)
	}
}

func TestMergeUpdateCopiesAttachments(t *testing.T) {
	dir := t.TempDir()
	password := "testmaster"

	// Both databases contain the same entry; the source copy is newer and
	// carries an attachment.
	basePath := filepath.Join(dir, "base.kdbx")
	base, err := Create(basePath, password)
	if err != nil {
		t.Fatalf("Create base: %v", err)
	}
	root := base.RootGroup()
	uuid, err := base.AddEntry(root.UUID, Entry{Title: "Shared", Password: "v1"})
	if err != nil {
		t.Fatalf("AddEntry: %v", err)
	}
	if err := base.SaveAs(basePath); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}

	target, err := Open(basePath, password)
	if err != nil {
		t.Fatalf("Open target: %v", err)
	}
	source, err := Open(basePath, password)
	if err != nil {
		t.Fatalf("Open source: %v", err)
	}
	if err := source.UpdateEntry(uuid, Entry{Title: "Shared", Password: "v2"}); err != nil {
		t.Fatalf("UpdateEntry source: %v", err)
	}
	attachment := []byte("new attachment")
	if err := source.AddAttachment(uuid, "new.txt", attachment); err != nil {
		t.Fatalf("AddAttachment: %v", err)
	}

	result, err := target.Merge(source)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if result.EntriesUpdated != 1 {
		t.Fatalf("updated = %d, want 1", result.EntriesUpdated)
	}

	got, err := target.GetEntry(uuid)
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if got.Password != "v2" {
		t.Errorf("password = %q, want v2 (newer source wins)", got.Password)
	}
	atts := target.EntryAttachments(uuid)
	if len(atts) != 1 || atts[0].Name != "new.txt" {
		t.Fatalf("attachments = %+v, want new.txt copied from source", atts)
	}

	// The overwritten target state must be recorded in history.
	history := target.EntryHistory(uuid)
	if len(history) == 0 || history[0].Password != "v1" {
		t.Errorf("history = %+v, want the pre-merge state (v1) preserved", history)
	}
}

// Entries merged from another database keep their custom icons; a target
// older than KDBX 4.1 drops the icon fields only 4.1 holds.
func TestMergeCopiesCustomIcons(t *testing.T) {
	for _, target4 := range []bool{false, true} {
		t.Run(fmt.Sprintf("target4=%t", target4), func(t *testing.T) {
			source := newFormatDB(t, true)
			gokeepasslib.WithDatabaseKDBXVersion41()(source.Database)
			icon := syntheticIcon()
			icon.Name, icon.LastModificationTime = "Favicon", &w.TimeWrapper{Time: syncBase}
			source.Database.Content.Meta.CustomIcons = append(source.Database.Content.Meta.CustomIcons, icon)
			id, err := source.AddEntry(source.RootGroup().UUID, Entry{Title: "Favicon"})
			if err != nil {
				t.Fatal(err)
			}
			findEntry(&source.Database.Content.Root.Groups[0], id).CustomIconUUID = icon.UUID
			target := newFormatDB(t, target4)
			if _, err := target.Merge(source); err != nil {
				t.Fatal(err)
			}
			target = saveAndReopen(t, target)
			if e := findEntry(&target.Database.Content.Root.Groups[0], id); e == nil || !e.CustomIconUUID.Compare(icon.UUID) {
				t.Fatal("the merged entry lost its icon")
			}
			if !slices.ContainsFunc(target.Database.Content.Meta.CustomIcons, func(c gokeepasslib.CustomIcon) bool {
				return c.UUID.Compare(icon.UUID) && c.Data == icon.Data
			}) {
				t.Fatal("the saved file lacks the icon its merged entry uses")
			}
			if source.Database.Content.Meta.CustomIcons[0].Name != "Favicon" {
				t.Fatal("merging changed the source's icon")
			}
		})
	}
}
