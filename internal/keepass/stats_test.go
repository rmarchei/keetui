package keepass

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStats(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testmaster"

	createTestDB(t, dbPath, password)

	db, err := Open(dbPath, password)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	stats := db.Stats()

	if stats.FilePath != dbPath {
		t.Errorf("file path = %q, want %q", stats.FilePath, dbPath)
	}

	if stats.FileSize == 0 {
		t.Error("file size = 0, want > 0")
	}

	// Root + Email = 2 groups
	if stats.TotalGroups < 2 {
		t.Errorf("total groups = %d, want at least 2", stats.TotalGroups)
	}

	// At least 1 entry
	if stats.TotalEntries < 1 {
		t.Errorf("total entries = %d, want at least 1", stats.TotalEntries)
	}
}

func TestStatsWeakPasswords(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testmaster"

	createTestDB(t, dbPath, password)

	db, err := Open(dbPath, password)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Add entry with weak password
	root := db.RootGroup()
	err = db.AddEntry(root.Groups[0].UUID, Entry{
		Title:    "Weak Entry",
		UserName: "user",
		Password: "123",
	})
	if err != nil {
		t.Fatalf("AddEntry: %v", err)
	}

	stats := db.Stats()
	if stats.WeakPasswords < 1 {
		t.Errorf("weak passwords = %d, want at least 1", stats.WeakPasswords)
	}
}

func TestStatsReusedPasswords(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testmaster"

	createTestDB(t, dbPath, password)

	db, err := Open(dbPath, password)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	root := db.RootGroup()
	// Add two entries with same password
	for i := 0; i < 2; i++ {
		err = db.AddEntry(root.UUID, Entry{
			Title:    "Reused Entry",
			UserName: "user",
			Password: "samepassword",
		})
		if err != nil {
			t.Fatalf("AddEntry: %v", err)
		}
	}

	stats := db.Stats()
	if stats.ReusedPasswords < 2 {
		t.Errorf("reused passwords = %d, want at least 2", stats.ReusedPasswords)
	}
}

func TestStatsExpiredEntries(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testmaster"

	createTestDB(t, dbPath, password)

	db, err := Open(dbPath, password)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	root := db.RootGroup()
	err = db.AddEntry(root.Groups[0].UUID, Entry{
		Title:      "Expired",
		UserName:   "user",
		Password:   "pass",
		Expires:    true,
		ExpiryTime: time.Now().Add(-24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("AddEntry: %v", err)
	}

	stats := db.Stats()
	if stats.ExpiredEntries < 1 {
		t.Errorf("expired entries = %d, want at least 1", stats.ExpiredEntries)
	}
}
