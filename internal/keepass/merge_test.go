package keepass

import (
	"path/filepath"
	"testing"
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
