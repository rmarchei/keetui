package keepass

import (
	"os"
	"path/filepath"
	"testing"
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
	os.Chdir(cwd)
	defer os.Chdir(origDir)

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
