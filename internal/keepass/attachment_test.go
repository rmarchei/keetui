package keepass

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tobischo/gokeepasslib/v3"
)

func TestExtractAttachmentRejectsTraversal(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "attach.kdbx")
	createTestDB(t, dbPath, "testpass")
	db, err := Open(dbPath, "testpass")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	root := db.RootGroup()
	entry := root.Groups[0].Entries[0]

	if err := db.AddAttachment(entry.UUID, "test.txt", []byte("hello")); err != nil {
		t.Fatalf("AddAttachment: %v", err)
	}

	// Paths containing ".." should be rejected
	traversalPaths := []string{
		"../evil.txt",
		"../../etc/passwd",
		"../../../.bashrc",
		"foo/../../bar.txt",
		"..",
	}
	for _, p := range traversalPaths {
		err := db.ExtractAttachment(entry.UUID, "test.txt", p)
		if err == nil {
			t.Errorf("ExtractAttachment(%q) should have been rejected", p)
		}
	}
}

func TestExtractAttachmentAllowsSafePaths(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "attach2.kdbx")
	createTestDB(t, dbPath, "testpass")
	db, err := Open(dbPath, "testpass")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	root := db.RootGroup()
	entry := root.Groups[0].Entries[0]

	if err := db.AddAttachment(entry.UUID, "test.txt", []byte("hello")); err != nil {
		t.Fatalf("AddAttachment: %v", err)
	}

	// Absolute path should work (used by tests and programmatic callers)
	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "extracted.txt")
	err = db.ExtractAttachment(entry.UUID, "test.txt", destPath)
	if err != nil {
		t.Fatalf("ExtractAttachment with absolute path: %v", err)
	}
	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read extracted: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("extracted content = %q, want %q", string(data), "hello")
	}

	// Simple filename should work (used by UI)
	origDir, _ := os.Getwd()
	cwd := t.TempDir()
	_ = os.Chdir(cwd)
	defer func() { _ = os.Chdir(origDir) }()

	err = db.ExtractAttachment(entry.UUID, "test.txt", "output.txt")
	if err != nil {
		t.Fatalf("ExtractAttachment with simple name: %v", err)
	}
	data, err = os.ReadFile(filepath.Join(cwd, "output.txt"))
	if err != nil {
		t.Fatalf("read extracted: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("extracted content = %q, want %q", string(data), "hello")
	}
}

func TestAddDeleteAttachment(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testpass"

	db, err := Create(dbPath, password)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	root := db.RootGroup()
	uuid, err := db.AddEntry(root.UUID, Entry{Title: "Test", UserName: "user"})
	if err != nil {
		t.Fatalf("AddEntry: %v", err)
	}

	// Add two attachments
	if err := db.AddAttachment(uuid, "file1.txt", []byte("content1")); err != nil {
		t.Fatalf("AddAttachment file1: %v", err)
	}
	if err := db.AddAttachment(uuid, "file2.txt", []byte("content2")); err != nil {
		t.Fatalf("AddAttachment file2: %v", err)
	}

	attachments := db.EntryAttachments(uuid)
	if len(attachments) != 2 {
		t.Fatalf("attachments = %d, want 2", len(attachments))
	}
	if attachments[0].Name != "file1.txt" || attachments[1].Name != "file2.txt" {
		t.Errorf("attachment names = [%q, %q], want [file1.txt, file2.txt]",
			attachments[0].Name, attachments[1].Name)
	}

	// Delete one
	if err := db.DeleteAttachment(uuid, "file1.txt"); err != nil {
		t.Fatalf("DeleteAttachment: %v", err)
	}

	attachments = db.EntryAttachments(uuid)
	if len(attachments) != 1 {
		t.Fatalf("attachments after delete = %d, want 1", len(attachments))
	}
	if attachments[0].Name != "file2.txt" {
		t.Errorf("remaining attachment = %q, want file2.txt", attachments[0].Name)
	}
}

func TestDeleteAttachmentMissing(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testpass"

	db, err := Create(dbPath, password)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	root := db.RootGroup()
	uuid, err := db.AddEntry(root.UUID, Entry{Title: "Test"})
	if err != nil {
		t.Fatalf("AddEntry: %v", err)
	}

	err = db.DeleteAttachment(uuid, "nonexistent.txt")
	if err == nil {
		t.Fatal("DeleteAttachment should fail for non-existent attachment")
	}
}

func TestAttachmentOnNonExistentEntry(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testpass"

	db, err := Create(dbPath, password)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	fakeUUID := gokeepasslib.NewUUID()

	// All attachment operations should fail for non-existent entry
	if err := db.AddAttachment(fakeUUID, "test.txt", []byte("data")); err == nil {
		t.Error("AddAttachment should fail for non-existent entry")
	}
	if err := db.DeleteAttachment(fakeUUID, "test.txt"); err == nil {
		t.Error("DeleteAttachment should fail for non-existent entry")
	}
	attachments := db.EntryAttachments(fakeUUID)
	if len(attachments) != 0 {
		t.Errorf("EntryAttachments for non-existent entry = %d, want 0", len(attachments))
	}
}

func TestAttachmentPersistence(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testpass"

	db, err := Create(dbPath, password)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	root := db.RootGroup()
	uuid, err := db.AddEntry(root.UUID, Entry{Title: "With Attachment"})
	if err != nil {
		t.Fatalf("AddEntry: %v", err)
	}

	if err := db.AddAttachment(uuid, "data.bin", []byte("binary content here")); err != nil {
		t.Fatalf("AddAttachment: %v", err)
	}

	// Save and reopen
	savePath := filepath.Join(dir, "saved.kdbx")
	if err := db.SaveAs(savePath); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}

	db2, err := Open(savePath, password)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	attachments := db2.EntryAttachments(uuid)
	if len(attachments) != 1 {
		t.Fatalf("attachments after reopen = %d, want 1", len(attachments))
	}
	if attachments[0].Name != "data.bin" {
		t.Errorf("attachment name = %q, want data.bin", attachments[0].Name)
	}
	if attachments[0].Size != len("binary content here") {
		t.Errorf("attachment size = %d, want %d", attachments[0].Size, len("binary content here"))
	}

	// Extract and verify content
	extractPath := filepath.Join(dir, "extracted.bin")
	if err := db2.ExtractAttachment(uuid, "data.bin", extractPath); err != nil {
		t.Fatalf("ExtractAttachment: %v", err)
	}
	data, err := os.ReadFile(extractPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "binary content here" {
		t.Errorf("extracted content = %q, want %q", string(data), "binary content here")
	}
}

func TestExtractAttachmentRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testpass"

	db, err := Create(dbPath, password)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	root := db.RootGroup()
	uuid, err := db.AddEntry(root.UUID, Entry{Title: "Test"})
	if err != nil {
		t.Fatalf("AddEntry: %v", err)
	}

	if err := db.AddAttachment(uuid, "test.txt", []byte("hello")); err != nil {
		t.Fatalf("AddAttachment: %v", err)
	}

	// Create a symlink and try to extract to it
	target := filepath.Join(dir, "real.txt")
	symlink := filepath.Join(dir, "link.txt")
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Symlink(target, symlink); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	err = db.ExtractAttachment(uuid, "test.txt", symlink)
	if err == nil {
		t.Error("ExtractAttachment should reject symlink destination")
	}

	// Verify original file was not overwritten
	data, _ := os.ReadFile(target)
	if string(data) != "original" {
		t.Error("original file should not be overwritten through symlink")
	}
}

func TestAddAttachmentFromFile(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testpass"

	db, err := Create(dbPath, password)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	root := db.RootGroup()
	uuid, err := db.AddEntry(root.UUID, Entry{Title: "Test"})
	if err != nil {
		t.Fatalf("AddEntry: %v", err)
	}

	// Create a file to attach
	filePath := filepath.Join(dir, "document.pdf")
	if err := os.WriteFile(filePath, []byte("fake pdf content"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := db.AddAttachmentFile(uuid, filePath); err != nil {
		t.Fatalf("AddAttachmentFile: %v", err)
	}

	attachments := db.EntryAttachments(uuid)
	if len(attachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(attachments))
	}
	if attachments[0].Name != "document.pdf" {
		t.Errorf("attachment name = %q, want document.pdf", attachments[0].Name)
	}
}

func TestAddAttachmentFromFileNotFound(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testpass"

	db, err := Create(dbPath, password)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	root := db.RootGroup()
	uuid, err := db.AddEntry(root.UUID, Entry{Title: "Test"})
	if err != nil {
		t.Fatalf("AddEntry: %v", err)
	}

	err = db.AddAttachmentFile(uuid, filepath.Join(dir, "nonexistent.txt"))
	if err == nil {
		t.Fatal("AddAttachmentFile should fail for non-existent file")
	}
}
