package keepass

import (
	"os"
	"path/filepath"
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
		name   string
		input  string
		want   string
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
