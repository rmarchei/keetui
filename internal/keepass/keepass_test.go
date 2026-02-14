package keepass

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
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

func TestOpenKdbFileRejected(t *testing.T) {
	_, err := Open("/tmp/test.kdb", "password")
	if err == nil {
		t.Fatal("expected error opening .kdb file, got nil")
	}
	if !strings.Contains(err.Error(), ".kdb files are not supported") {
		t.Errorf("error = %q, want message about .kdb not supported", err.Error())
	}

	// Also test case-insensitive matching
	_, err = Open("/tmp/test.KDB", "password")
	if err == nil {
		t.Fatal("expected error opening .KDB file, got nil")
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

func TestTOTPSecret(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"raw base32", "JBSWY3DPEHPK3PXP", "JBSWY3DPEHPK3PXP"},
		{"otpauth URI", "otpauth://totp/Example:user@example.com?secret=JBSWY3DPEHPK3PXP&issuer=Example", "JBSWY3DPEHPK3PXP"},
		{"otpauth with period", "otpauth://totp/Test?secret=ABCDEFGH&period=60", "ABCDEFGH"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TOTPSecret(tt.input)
			if got != tt.want {
				t.Errorf("TOTPSecret(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTOTPPeriod(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  uint
	}{
		{"empty", "", 30},
		{"raw secret", "JBSWY3DPEHPK3PXP", 30},
		{"otpauth default", "otpauth://totp/Test?secret=ABC", 30},
		{"otpauth period 60", "otpauth://totp/Test?secret=ABC&period=60", 60},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TOTPPeriod(tt.input)
			if got != tt.want {
				t.Errorf("TOTPPeriod(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestTOTPCodeGeneration(t *testing.T) {
	// Use a known secret and verify code generation works
	secret := "JBSWY3DPEHPK3PXP"
	now := time.Now()

	code, err := totp.GenerateCodeCustom(secret, now, totp.ValidateOpts{
		Period: 30,
		Digits: 6,
	})
	if err != nil {
		t.Fatalf("GenerateCodeCustom: %v", err)
	}
	if len(code) != 6 {
		t.Errorf("TOTP code length = %d, want 6", len(code))
	}

	// Validate the code we just generated
	valid, err := totp.ValidateCustom(code, secret, now, totp.ValidateOpts{
		Period: 30,
		Digits: 6,
	})
	if err != nil {
		t.Fatalf("ValidateCustom: %v", err)
	}
	if !valid {
		t.Error("generated TOTP code failed validation")
	}
}

func TestAddEntryWithTOTP(t *testing.T) {
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

	// Add entry with raw base32 TOTP secret
	err = db.AddEntry(emailGroup.UUID, Entry{
		Title:    "TOTP Entry",
		UserName: "totpuser",
		Password: "totppass",
		TOTP:     "JBSWY3DPEHPK3PXP",
	})
	if err != nil {
		t.Fatalf("AddEntry with TOTP: %v", err)
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
	var found *Entry
	for i, e := range entries {
		if e.Title == "TOTP Entry" {
			found = &entries[i]
		}
	}
	if found == nil {
		t.Fatal("TOTP entry not found after save/reload")
	}
	if found.TOTP != "JBSWY3DPEHPK3PXP" {
		t.Errorf("TOTP = %q, want %q", found.TOTP, "JBSWY3DPEHPK3PXP")
	}
}

func TestAddEntryWithOTPAuthURI(t *testing.T) {
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

	otpauthURI := "otpauth://totp/Example:user@example.com?secret=JBSWY3DPEHPK3PXP&issuer=Example"
	err = db.AddEntry(emailGroup.UUID, Entry{
		Title:    "OTPAuth Entry",
		UserName: "otpuser",
		Password: "otppass",
		TOTP:     otpauthURI,
	})
	if err != nil {
		t.Fatalf("AddEntry with otpauth URI: %v", err)
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
	var found *Entry
	for i, e := range entries {
		if e.Title == "OTPAuth Entry" {
			found = &entries[i]
		}
	}
	if found == nil {
		t.Fatal("OTPAuth entry not found after save/reload")
	}
	if found.TOTP != otpauthURI {
		t.Errorf("TOTP = %q, want %q", found.TOTP, otpauthURI)
	}
	// Verify we can extract the secret from the URI
	secret := TOTPSecret(found.TOTP)
	if secret != "JBSWY3DPEHPK3PXP" {
		t.Errorf("extracted secret = %q, want %q", secret, "JBSWY3DPEHPK3PXP")
	}
}

func TestCreateDatabase(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "new.kdbx")
	password := "newpass"

	db, err := Create(dbPath, password)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	root := db.RootGroup()
	if root.Name != "Root" {
		t.Errorf("root group name = %q, want %q", root.Name, "Root")
	}

	// Verify we can reopen the created database
	db2, err := Open(dbPath, password)
	if err != nil {
		t.Fatalf("Open created db: %v", err)
	}

	root2 := db2.RootGroup()
	if root2.Name != "Root" {
		t.Errorf("reopened root group name = %q, want %q", root2.Name, "Root")
	}

	// Add an entry and save to verify it's functional
	err = db2.AddEntry(root2.UUID, Entry{
		Title:    "First Entry",
		UserName: "user1",
		Password: "pass1",
	})
	if err != nil {
		t.Fatalf("AddEntry to created db: %v", err)
	}

	savePath := filepath.Join(dir, "new2.kdbx")
	err = db2.SaveAs(savePath)
	if err != nil {
		t.Fatalf("SaveAs: %v", err)
	}

	db3, err := Open(savePath, password)
	if err != nil {
		t.Fatalf("Open saved: %v", err)
	}

	entries := db3.RootGroup().Entries
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].Title != "First Entry" {
		t.Errorf("title = %q, want %q", entries[0].Title, "First Entry")
	}
}

func TestMoveEntry(t *testing.T) {
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
	entry := emailGroup.Entries[0]

	// Add a second group to move into
	err = db.AddGroup(root.UUID, "Target Group")
	if err != nil {
		t.Fatalf("AddGroup: %v", err)
	}

	// Find the new group UUID
	root = db.RootGroup()
	var targetUUID gokeepasslib.UUID
	for _, g := range root.Groups {
		if g.Name == "Target Group" {
			targetUUID = g.UUID
		}
	}

	err = db.MoveEntry(entry.UUID, targetUUID)
	if err != nil {
		t.Fatalf("MoveEntry: %v", err)
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

	// Email group should be empty
	var emailEntries int
	var targetEntries int
	for _, g := range root2.Groups {
		if g.Name == "Email" {
			emailEntries = len(g.Entries)
		}
		if g.Name == "Target Group" {
			targetEntries = len(g.Entries)
		}
	}
	if emailEntries != 0 {
		t.Errorf("email group entries = %d, want 0", emailEntries)
	}
	if targetEntries != 1 {
		t.Errorf("target group entries = %d, want 1", targetEntries)
	}
}

func TestMoveEntryInvalidTargetPreservesEntry(t *testing.T) {
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
	entry := emailGroup.Entries[0]

	// Try to move to a non-existent group
	bogusUUID := gokeepasslib.NewUUID()
	err = db.MoveEntry(entry.UUID, bogusUUID)
	if err == nil {
		t.Fatal("expected error for invalid target group, got nil")
	}

	// Entry must still exist in its original group
	root = db.RootGroup()
	if len(root.Groups[0].Entries) != 1 {
		t.Fatalf("entry was lost after failed move: entries = %d, want 1", len(root.Groups[0].Entries))
	}
}

func TestDuplicateEntry(t *testing.T) {
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

	err = db.DuplicateEntry(entry.UUID)
	if err != nil {
		t.Fatalf("DuplicateEntry: %v", err)
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

	entries := db2.RootGroup().Groups[0].Entries
	if len(entries) != 2 {
		t.Fatalf("entries after dup = %d, want 2", len(entries))
	}

	// The duplicate should have " (copy)" suffix
	found := false
	for _, e := range entries {
		if e.Title == "Test Entry (copy)" {
			found = true
			if e.UserName != "testuser" {
				t.Errorf("dup username = %q, want %q", e.UserName, "testuser")
			}
		}
	}
	if !found {
		t.Error("duplicate entry not found")
	}
}

func TestRenameGroup(t *testing.T) {
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

	err = db.RenameGroup(emailGroup.UUID, "Work Email")
	if err != nil {
		t.Fatalf("RenameGroup: %v", err)
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
	if len(groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(groups))
	}
	if groups[0].Name != "Work Email" {
		t.Errorf("name = %q, want %q", groups[0].Name, "Work Email")
	}
}

func TestCustomFields(t *testing.T) {
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
		Title:    "Custom Entry",
		UserName: "user",
		Password: "pass",
		CustomFields: []CustomField{
			{Key: "RecoveryKey", Value: "abc123", Protected: true},
			{Key: "SecurityQ", Value: "pet name", Protected: false},
		},
	})
	if err != nil {
		t.Fatalf("AddEntry with custom fields: %v", err)
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
	var found *Entry
	for i, e := range entries {
		if e.Title == "Custom Entry" {
			found = &entries[i]
		}
	}
	if found == nil {
		t.Fatal("custom entry not found")
	}
	if len(found.CustomFields) != 2 {
		t.Fatalf("custom fields = %d, want 2", len(found.CustomFields))
	}

	for _, cf := range found.CustomFields {
		switch cf.Key {
		case "RecoveryKey":
			if cf.Value != "abc123" {
				t.Errorf("RecoveryKey value = %q, want %q", cf.Value, "abc123")
			}
		case "SecurityQ":
			if cf.Value != "pet name" {
				t.Errorf("SecurityQ value = %q, want %q", cf.Value, "pet name")
			}
		default:
			t.Errorf("unexpected custom field: %q", cf.Key)
		}
	}
}

func TestChangePassword(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testmaster"

	createTestDB(t, dbPath, password)

	db, err := Open(dbPath, password)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	newPassword := "newpassword123"
	db.ChangePassword(newPassword)

	savePath := filepath.Join(dir, "test2.kdbx")
	err = db.SaveAs(savePath)
	if err != nil {
		t.Fatalf("SaveAs: %v", err)
	}

	// Old password should fail
	_, err = Open(savePath, password)
	if err == nil {
		t.Fatal("expected error with old password, got nil")
	}

	// New password should work
	db2, err := Open(savePath, newPassword)
	if err != nil {
		t.Fatalf("Open with new password: %v", err)
	}

	root := db2.RootGroup()
	if len(root.Groups) != 1 {
		t.Errorf("groups = %d, want 1", len(root.Groups))
	}
}

func TestMoveGroup(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testmaster"

	createTestDB(t, dbPath, password)

	db, err := Open(dbPath, password)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	root := db.RootGroup()

	// Add a second group to move the first into
	err = db.AddGroup(root.UUID, "Target Group")
	if err != nil {
		t.Fatalf("AddGroup: %v", err)
	}

	root = db.RootGroup()
	emailGroup := root.Groups[0]
	var targetUUID gokeepasslib.UUID
	for _, g := range root.Groups {
		if g.Name == "Target Group" {
			targetUUID = g.UUID
		}
	}

	err = db.MoveGroup(emailGroup.UUID, targetUUID)
	if err != nil {
		t.Fatalf("MoveGroup: %v", err)
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

	// Root should only have "Target Group" now
	if len(root2.Groups) != 1 {
		t.Fatalf("root groups = %d, want 1", len(root2.Groups))
	}
	if root2.Groups[0].Name != "Target Group" {
		t.Errorf("root group name = %q, want %q", root2.Groups[0].Name, "Target Group")
	}

	// Target Group should contain the Email group
	if len(root2.Groups[0].Groups) != 1 {
		t.Fatalf("target subgroups = %d, want 1", len(root2.Groups[0].Groups))
	}
	if root2.Groups[0].Groups[0].Name != "Email" {
		t.Errorf("moved group name = %q, want %q", root2.Groups[0].Groups[0].Name, "Email")
	}

	// Entry should still be there
	if len(root2.Groups[0].Groups[0].Entries) != 1 {
		t.Errorf("moved group entries = %d, want 1", len(root2.Groups[0].Groups[0].Entries))
	}
}

func TestMoveGroupIntoSelfFails(t *testing.T) {
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

	err = db.MoveGroup(emailGroup.UUID, emailGroup.UUID)
	if err == nil {
		t.Fatal("expected error moving group into itself, got nil")
	}
}

func TestMoveGroupIntoDescendantFails(t *testing.T) {
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

	// Add a sub-group under Email
	err = db.AddGroup(emailGroup.UUID, "Sub Group")
	if err != nil {
		t.Fatalf("AddGroup: %v", err)
	}

	root = db.RootGroup()
	emailGroup = root.Groups[0]
	subGroup := emailGroup.Groups[0]

	// Try to move Email into Sub Group (its own descendant)
	err = db.MoveGroup(emailGroup.UUID, subGroup.UUID)
	if err == nil {
		t.Fatal("expected error moving group into descendant, got nil")
	}
}

func TestSaveAsAtomic(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testmaster"

	createTestDB(t, dbPath, password)

	db, err := Open(dbPath, password)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Save to same path (overwrite)
	err = db.Save()
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify the file is still valid
	db2, err := Open(dbPath, password)
	if err != nil {
		t.Fatalf("Open after save: %v", err)
	}

	root := db2.RootGroup()
	if root.Name != "Root" {
		t.Errorf("root name = %q, want %q", root.Name, "Root")
	}
}

func TestAddAndListAttachments(t *testing.T) {
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

	// Initially no attachments
	attachments := db.EntryAttachments(entry.UUID)
	if len(attachments) != 0 {
		t.Fatalf("initial attachments = %d, want 0", len(attachments))
	}

	// Add an attachment
	data := []byte("hello world")
	err = db.AddAttachment(entry.UUID, "test.txt", data)
	if err != nil {
		t.Fatalf("AddAttachment: %v", err)
	}

	attachments = db.EntryAttachments(entry.UUID)
	if len(attachments) != 1 {
		t.Fatalf("attachments after add = %d, want 1", len(attachments))
	}
	if attachments[0].Name != "test.txt" {
		t.Errorf("attachment name = %q, want %q", attachments[0].Name, "test.txt")
	}
	if attachments[0].Size != len(data) {
		t.Errorf("attachment size = %d, want %d", attachments[0].Size, len(data))
	}
}

func TestAddAttachmentFile(t *testing.T) {
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

	// Create a file to attach
	attachFile := filepath.Join(dir, "document.pdf")
	content := []byte("fake pdf content")
	if err := os.WriteFile(attachFile, content, 0600); err != nil {
		t.Fatalf("write attachment file: %v", err)
	}

	err = db.AddAttachmentFile(entry.UUID, attachFile)
	if err != nil {
		t.Fatalf("AddAttachmentFile: %v", err)
	}

	attachments := db.EntryAttachments(entry.UUID)
	if len(attachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(attachments))
	}
	if attachments[0].Name != "document.pdf" {
		t.Errorf("attachment name = %q, want %q", attachments[0].Name, "document.pdf")
	}
	if attachments[0].Size != len(content) {
		t.Errorf("attachment size = %d, want %d", attachments[0].Size, len(content))
	}
}

func TestExtractAttachment(t *testing.T) {
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

	data := []byte("secret document content")
	err = db.AddAttachment(entry.UUID, "secret.txt", data)
	if err != nil {
		t.Fatalf("AddAttachment: %v", err)
	}

	// Save and reopen to ensure attachment persists
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
	entry2 := root2.Groups[0].Entries[0]

	// Extract the attachment
	destPath := filepath.Join(dir, "extracted.txt")
	err = db2.ExtractAttachment(entry2.UUID, "secret.txt", destPath)
	if err != nil {
		t.Fatalf("ExtractAttachment: %v", err)
	}

	extracted, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read extracted: %v", err)
	}
	if string(extracted) != string(data) {
		t.Errorf("extracted content = %q, want %q", string(extracted), string(data))
	}
}

func TestDeleteAttachment(t *testing.T) {
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

	// Add two attachments
	err = db.AddAttachment(entry.UUID, "file1.txt", []byte("content1"))
	if err != nil {
		t.Fatalf("AddAttachment file1: %v", err)
	}
	err = db.AddAttachment(entry.UUID, "file2.txt", []byte("content2"))
	if err != nil {
		t.Fatalf("AddAttachment file2: %v", err)
	}

	attachments := db.EntryAttachments(entry.UUID)
	if len(attachments) != 2 {
		t.Fatalf("attachments = %d, want 2", len(attachments))
	}

	// Delete one
	err = db.DeleteAttachment(entry.UUID, "file1.txt")
	if err != nil {
		t.Fatalf("DeleteAttachment: %v", err)
	}

	attachments = db.EntryAttachments(entry.UUID)
	if len(attachments) != 1 {
		t.Fatalf("attachments after delete = %d, want 1", len(attachments))
	}
	if attachments[0].Name != "file2.txt" {
		t.Errorf("remaining attachment = %q, want %q", attachments[0].Name, "file2.txt")
	}
}

func TestDeleteAttachmentNotFound(t *testing.T) {
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

	err = db.DeleteAttachment(entry.UUID, "nonexistent.txt")
	if err == nil {
		t.Fatal("expected error deleting nonexistent attachment, got nil")
	}
}

func TestDuplicateEntryWithAttachments(t *testing.T) {
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

	// Add an attachment to the original entry
	err = db.AddAttachment(entry.UUID, "readme.md", []byte("# README"))
	if err != nil {
		t.Fatalf("AddAttachment: %v", err)
	}

	// Duplicate the entry
	err = db.DuplicateEntry(entry.UUID)
	if err != nil {
		t.Fatalf("DuplicateEntry: %v", err)
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

	entries := db2.RootGroup().Groups[0].Entries
	if len(entries) != 2 {
		t.Fatalf("entries after dup = %d, want 2", len(entries))
	}

	// Find the duplicate
	for _, e := range entries {
		if e.Title == "Test Entry (copy)" {
			attachments := db2.EntryAttachments(e.UUID)
			if len(attachments) != 1 {
				t.Fatalf("dup attachments = %d, want 1", len(attachments))
			}
			if attachments[0].Name != "readme.md" {
				t.Errorf("dup attachment name = %q, want %q", attachments[0].Name, "readme.md")
			}
			return
		}
	}
	t.Fatal("duplicate entry not found")
}

func TestExportCSVWithAttachments(t *testing.T) {
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

	err = db.AddAttachment(entry.UUID, "doc.pdf", []byte("pdf data"))
	if err != nil {
		t.Fatalf("AddAttachment: %v", err)
	}
	err = db.AddAttachment(entry.UUID, "key.pem", []byte("key data"))
	if err != nil {
		t.Fatalf("AddAttachment: %v", err)
	}

	var buf strings.Builder
	err = db.ExportCSV(&buf)
	if err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}

	csv := buf.String()
	if !strings.Contains(csv, "Attachments") {
		t.Error("CSV header missing Attachments column")
	}
	if !strings.Contains(csv, "doc.pdf;key.pem") {
		t.Errorf("CSV missing attachment names, got:\n%s", csv)
	}
}

func TestExportImportXMLWithAttachments(t *testing.T) {
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

	attachData := []byte("attachment content for xml test")
	err = db.AddAttachment(entry.UUID, "notes.txt", attachData)
	if err != nil {
		t.Fatalf("AddAttachment: %v", err)
	}

	// Export to XML
	var buf strings.Builder
	err = db.ExportXML(&buf)
	if err != nil {
		t.Fatalf("ExportXML: %v", err)
	}

	xmlContent := buf.String()
	if !strings.Contains(xmlContent, "notes.txt") {
		t.Error("XML export missing attachment name")
	}

	// Import into a fresh database
	dbPath2 := filepath.Join(dir, "import.kdbx")
	db2, err := Create(dbPath2, password)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	n, err := db2.ImportXML(strings.NewReader(xmlContent))
	if err != nil {
		t.Fatalf("ImportXML: %v", err)
	}
	if n != 1 {
		t.Fatalf("imported = %d, want 1", n)
	}

	// Find the imported entry and check attachments
	root2 := db2.RootGroup()
	var found *Entry
	for i := range root2.Groups {
		for j := range root2.Groups[i].Entries {
			if root2.Groups[i].Entries[j].Title == "Test Entry" {
				found = &root2.Groups[i].Entries[j]
			}
		}
	}
	if found == nil {
		t.Fatal("imported entry not found")
	}

	attachments := db2.EntryAttachments(found.UUID)
	if len(attachments) != 1 {
		t.Fatalf("imported attachments = %d, want 1", len(attachments))
	}
	if attachments[0].Name != "notes.txt" {
		t.Errorf("imported attachment name = %q, want %q", attachments[0].Name, "notes.txt")
	}

	// Verify we can extract and the content matches
	destPath := filepath.Join(dir, "extracted.txt")
	err = db2.ExtractAttachment(found.UUID, "notes.txt", destPath)
	if err != nil {
		t.Fatalf("ExtractAttachment: %v", err)
	}

	extracted, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read extracted: %v", err)
	}
	if string(extracted) != string(attachData) {
		t.Errorf("extracted content = %q, want %q", string(extracted), string(attachData))
	}
}

func TestExpiryFields(t *testing.T) {
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

	pastExpiry := time.Now().Add(-24 * time.Hour)
	futureExpiry := time.Now().Add(30 * 24 * time.Hour)

	// Add expired entry
	err = db.AddEntry(emailGroup.UUID, Entry{
		Title:      "Expired Entry",
		UserName:   "user",
		Password:   "pass",
		Expires:    true,
		ExpiryTime: pastExpiry,
	})
	if err != nil {
		t.Fatalf("AddEntry expired: %v", err)
	}

	// Add future-expiry entry
	err = db.AddEntry(emailGroup.UUID, Entry{
		Title:      "Future Entry",
		UserName:   "user",
		Password:   "pass",
		Expires:    true,
		ExpiryTime: futureExpiry,
	})
	if err != nil {
		t.Fatalf("AddEntry future: %v", err)
	}

	// Add no-expiry entry
	err = db.AddEntry(emailGroup.UUID, Entry{
		Title:    "No Expiry",
		UserName: "user",
		Password: "pass",
	})
	if err != nil {
		t.Fatalf("AddEntry no expiry: %v", err)
	}

	// Save and reopen
	savePath := filepath.Join(dir, "test2.kdbx")
	if err := db.SaveAs(savePath); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}

	db2, err := Open(savePath, password)
	if err != nil {
		t.Fatalf("Open saved: %v", err)
	}

	entries := db2.RootGroup().Groups[0].Entries
	for _, e := range entries {
		switch e.Title {
		case "Expired Entry":
			if !e.Expires {
				t.Error("Expired Entry: Expires should be true")
			}
			if !e.IsExpired() {
				t.Error("Expired Entry: IsExpired() should be true")
			}
		case "Future Entry":
			if !e.Expires {
				t.Error("Future Entry: Expires should be true")
			}
			if e.IsExpired() {
				t.Error("Future Entry: IsExpired() should be false")
			}
		case "No Expiry":
			if e.Expires {
				t.Error("No Expiry: Expires should be false")
			}
			if e.IsExpired() {
				t.Error("No Expiry: IsExpired() should be false")
			}
		}
	}
}

func TestIsExpired(t *testing.T) {
	tests := []struct {
		name    string
		entry   Entry
		expired bool
	}{
		{"no expiry", Entry{}, false},
		{"expires false", Entry{Expires: false, ExpiryTime: time.Now().Add(-time.Hour)}, false},
		{"expired", Entry{Expires: true, ExpiryTime: time.Now().Add(-time.Hour)}, true},
		{"not yet expired", Entry{Expires: true, ExpiryTime: time.Now().Add(time.Hour)}, false},
		{"expires with zero time", Entry{Expires: true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.entry.IsExpired(); got != tt.expired {
				t.Errorf("IsExpired() = %v, want %v", got, tt.expired)
			}
		})
	}
}

func TestUpdateEntryExpiry(t *testing.T) {
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

	futureExpiry := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	err = db.UpdateEntry(entry.UUID, Entry{
		Title:      entry.Title,
		UserName:   entry.UserName,
		Password:   entry.Password,
		Expires:    true,
		ExpiryTime: futureExpiry,
	})
	if err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}

	savePath := filepath.Join(dir, "test2.kdbx")
	if err := db.SaveAs(savePath); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}

	db2, err := Open(savePath, password)
	if err != nil {
		t.Fatalf("Open saved: %v", err)
	}

	updated := db2.RootGroup().Groups[0].Entries[0]
	if !updated.Expires {
		t.Error("Expires should be true after update")
	}
	if !updated.ExpiryTime.Equal(futureExpiry) {
		t.Errorf("ExpiryTime = %v, want %v", updated.ExpiryTime, futureExpiry)
	}
}
