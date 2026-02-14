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
