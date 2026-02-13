package keepass

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tobischo/gokeepasslib/v3"
)

func createTestDB(t *testing.T, path, password string) {
	t.Helper()

	rootGroup := gokeepasslib.NewGroup()
	rootGroup.Name = "Root"

	subGroup := gokeepasslib.NewGroup()
	subGroup.Name = "Email"

	entry := gokeepasslib.NewEntry()
	entry.Values = []gokeepasslib.ValueData{
		mkValue("Title", "Test Entry"),
		mkValue("UserName", "testuser"),
		mkProtectedValue("Password", "testpass"),
		mkValue("URL", "https://example.com"),
		mkValue("Notes", "test notes"),
	}
	subGroup.Entries = append(subGroup.Entries, entry)
	rootGroup.Groups = append(rootGroup.Groups, subGroup)

	db := gokeepasslib.NewDatabase()
	db.Credentials = gokeepasslib.NewPasswordCredentials(password)
	db.Content.Root = &gokeepasslib.RootData{
		Groups: []gokeepasslib.Group{rootGroup},
	}

	db.LockProtectedEntries()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create test db file: %v", err)
	}
	defer file.Close()

	if err := gokeepasslib.NewEncoder(file).Encode(db); err != nil {
		t.Fatalf("encode test db: %v", err)
	}
}

func TestOpenAndRead(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testmaster"

	createTestDB(t, dbPath, password)

	db, err := Open(dbPath, password)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	root := db.RootGroup()
	if root.Name != "Root" {
		t.Errorf("root group name = %q, want %q", root.Name, "Root")
	}

	if len(root.Groups) != 1 {
		t.Fatalf("root groups = %d, want 1", len(root.Groups))
	}

	emailGroup := root.Groups[0]
	if emailGroup.Name != "Email" {
		t.Errorf("sub group name = %q, want %q", emailGroup.Name, "Email")
	}

	if len(emailGroup.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(emailGroup.Entries))
	}

	entry := emailGroup.Entries[0]
	if entry.Title != "Test Entry" {
		t.Errorf("title = %q, want %q", entry.Title, "Test Entry")
	}
	if entry.UserName != "testuser" {
		t.Errorf("username = %q, want %q", entry.UserName, "testuser")
	}
	if entry.Password != "testpass" {
		t.Errorf("password = %q, want %q", entry.Password, "testpass")
	}
	if entry.URL != "https://example.com" {
		t.Errorf("url = %q, want %q", entry.URL, "https://example.com")
	}
}

func TestOpenWrongPassword(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")

	createTestDB(t, dbPath, "correctpass")

	_, err := Open(dbPath, "wrongpass")
	if err == nil {
		t.Fatal("expected error with wrong password, got nil")
	}
}

func TestAddEntry(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testmaster"

	createTestDB(t, dbPath, password)

	db, err := Open(dbPath, password)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	root := db.RootGroup()
	emailGroup := root.Groups[0]

	err = db.AddEntry(emailGroup.UUID, Entry{
		Title:    "New Entry",
		UserName: "newuser",
		Password: "newpass",
		URL:      "https://new.example.com",
		Notes:    "new notes",
	})
	if err != nil {
		t.Fatalf("AddEntry: %v", err)
	}

	// Save and reopen
	savePath := filepath.Join(dir, "test2.kdbx")
	err = db.SaveAs(savePath)
	if err != nil {
		t.Fatalf("SaveAs: %v", err)
	}

	db2, err := Open(savePath, password)
	if err != nil {
		t.Fatalf("Open saved: %v", err)
	}

	root2 := db2.RootGroup()
	entries := root2.Groups[0].Entries
	if len(entries) != 2 {
		t.Fatalf("entries after add = %d, want 2", len(entries))
	}

	found := false
	for _, e := range entries {
		if e.Title == "New Entry" && e.UserName == "newuser" {
			found = true
		}
	}
	if !found {
		t.Error("new entry not found after save/reload")
	}
}

func TestUpdateEntry(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testmaster"

	createTestDB(t, dbPath, password)

	db, err := Open(dbPath, password)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	root := db.RootGroup()
	entry := root.Groups[0].Entries[0]

	err = db.UpdateEntry(entry.UUID, Entry{
		Title:    "Updated Title",
		UserName: "updateduser",
		Password: "updatedpass",
		URL:      "https://updated.example.com",
		Notes:    "updated notes",
	})
	if err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}

	savePath := filepath.Join(dir, "test2.kdbx")
	err = db.SaveAs(savePath)
	if err != nil {
		t.Fatalf("SaveAs: %v", err)
	}

	db2, err := Open(savePath, password)
	if err != nil {
		t.Fatalf("Open saved: %v", err)
	}

	updated := db2.RootGroup().Groups[0].Entries[0]
	if updated.Title != "Updated Title" {
		t.Errorf("title = %q, want %q", updated.Title, "Updated Title")
	}
	if updated.UserName != "updateduser" {
		t.Errorf("username = %q, want %q", updated.UserName, "updateduser")
	}
}

func TestDeleteEntry(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testmaster"

	createTestDB(t, dbPath, password)

	db, err := Open(dbPath, password)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	root := db.RootGroup()
	entry := root.Groups[0].Entries[0]

	err = db.DeleteEntry(entry.UUID)
	if err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}

	savePath := filepath.Join(dir, "test2.kdbx")
	err = db.SaveAs(savePath)
	if err != nil {
		t.Fatalf("SaveAs: %v", err)
	}

	db2, err := Open(savePath, password)
	if err != nil {
		t.Fatalf("Open saved: %v", err)
	}

	entries := db2.RootGroup().Groups[0].Entries
	if len(entries) != 0 {
		t.Errorf("entries after delete = %d, want 0", len(entries))
	}
}

func TestAddAndDeleteGroup(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testmaster"

	createTestDB(t, dbPath, password)

	db, err := Open(dbPath, password)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	root := db.RootGroup()

	err = db.AddGroup(root.UUID, "New Group")
	if err != nil {
		t.Fatalf("AddGroup: %v", err)
	}

	savePath := filepath.Join(dir, "test2.kdbx")
	err = db.SaveAs(savePath)
	if err != nil {
		t.Fatalf("SaveAs: %v", err)
	}

	db2, err := Open(savePath, password)
	if err != nil {
		t.Fatalf("Open saved: %v", err)
	}

	groups := db2.RootGroup().Groups
	if len(groups) != 2 {
		t.Fatalf("groups after add = %d, want 2", len(groups))
	}

	// Delete the new group
	var newGroupUUID gokeepasslib.UUID
	for _, g := range groups {
		if g.Name == "New Group" {
			newGroupUUID = g.UUID
		}
	}

	err = db2.DeleteGroup(newGroupUUID)
	if err != nil {
		t.Fatalf("DeleteGroup: %v", err)
	}

	savePath2 := filepath.Join(dir, "test3.kdbx")
	err = db2.SaveAs(savePath2)
	if err != nil {
		t.Fatalf("SaveAs: %v", err)
	}

	db3, err := Open(savePath2, password)
	if err != nil {
		t.Fatalf("Open saved: %v", err)
	}

	groups3 := db3.RootGroup().Groups
	if len(groups3) != 1 {
		t.Errorf("groups after delete = %d, want 1", len(groups3))
	}
}
