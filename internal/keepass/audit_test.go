package keepass

import (
	"path/filepath"
	"testing"
	"time"

	w "github.com/tobischo/gokeepasslib/v3/wrappers"
)

func TestAuditWeakPasswords(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testmaster"

	createTestDB(t, dbPath, password)

	db, err := Open(dbPath, password)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	root := db.RootGroup()
	_ = db.AddEntry(root.Groups[0].UUID, Entry{
		Title:    "Weak",
		UserName: "user",
		Password: "123",
	})

	result := db.Audit()
	if len(result.WeakEntries) < 1 {
		t.Errorf("weak entries = %d, want at least 1", len(result.WeakEntries))
	}
}

func TestAuditReusedPasswords(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testmaster"

	createTestDB(t, dbPath, password)

	db, err := Open(dbPath, password)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	root := db.RootGroup()
	for i := 0; i < 2; i++ {
		_ = db.AddEntry(root.UUID, Entry{
			Title:    "Reused",
			UserName: "user",
			Password: "samepassword",
		})
	}

	result := db.Audit()
	if len(result.ReusedEntries) < 2 {
		t.Errorf("reused entries = %d, want at least 2", len(result.ReusedEntries))
	}
}

func TestAuditEmptyPasswords(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testmaster"

	createTestDB(t, dbPath, password)

	db, err := Open(dbPath, password)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	root := db.RootGroup()
	_ = db.AddEntry(root.UUID, Entry{
		Title:    "Empty",
		UserName: "user",
		Password: "",
	})

	result := db.Audit()
	if len(result.EmptyEntries) < 1 {
		t.Errorf("empty entries = %d, want at least 1", len(result.EmptyEntries))
	}
}

func TestAuditExpiredEntries(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testmaster"

	createTestDB(t, dbPath, password)

	db, err := Open(dbPath, password)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	root := db.RootGroup()
	_ = db.AddEntry(root.UUID, Entry{
		Title:      "Expired",
		UserName:   "user",
		Password:   "pass",
		Expires:    true,
		ExpiryTime: time.Now().Add(-24 * time.Hour),
	})

	result := db.Audit()
	if len(result.ExpiredEntries) < 1 {
		t.Errorf("expired entries = %d, want at least 1", len(result.ExpiredEntries))
	}
}

func TestAuditSkipsRecycleBin(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testmaster"

	createTestDB(t, dbPath, password)

	db, err := Open(dbPath, password)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Enable recycle bin and add a weak entry
	db.Database.Content.Meta.RecycleBinEnabled = w.NewBoolWrapper(true)

	root := db.RootGroup()
	_ = db.AddEntry(root.Groups[0].UUID, Entry{
		Title:    "Weak in Email",
		UserName: "user",
		Password: "123",
	})

	// Delete it (moves to recycle bin)
	root = db.RootGroup()
	for _, e := range root.Groups[0].Entries {
		if e.Title == "Weak in Email" {
			_ = db.DeleteEntry(e.UUID)
			break
		}
	}

	result := db.Audit()
	// The deleted entry should not appear in audit results
	for _, ae := range result.WeakEntries {
		if ae.Entry.Title == "Weak in Email" {
			t.Error("audit should skip entries in recycle bin")
		}
	}
}

func TestAuditEmptyDatabase(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testmaster"

	db, err := Create(dbPath, password)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	result := db.Audit()
	if result.TotalIssues() != 0 {
		t.Errorf("total issues = %d, want 0", result.TotalIssues())
	}
}
