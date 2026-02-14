package keepass

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportCSV(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testmaster"

	createTestDB(t, dbPath, password)

	db, err := Open(dbPath, password)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	var buf bytes.Buffer
	if err := db.ExportCSV(&buf); err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}

	output := buf.String()
	// Should have header + at least 1 data row
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		t.Fatalf("CSV lines = %d, want at least 2", len(lines))
	}

	// Check header
	if !strings.HasPrefix(lines[0], "Group,Title,Username,Password,URL,Notes") {
		t.Errorf("CSV header = %q, want standard header", lines[0])
	}

	// Check data contains test entry
	if !strings.Contains(output, "Test Entry") {
		t.Error("CSV output does not contain 'Test Entry'")
	}
	if !strings.Contains(output, "testuser") {
		t.Error("CSV output does not contain 'testuser'")
	}
}

func TestExportXML(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testmaster"

	createTestDB(t, dbPath, password)

	db, err := Open(dbPath, password)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	var buf bytes.Buffer
	if err := db.ExportXML(&buf); err != nil {
		t.Fatalf("ExportXML: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "<KeePassFile>") {
		t.Error("XML output does not contain <KeePassFile>")
	}
	if !strings.Contains(output, "Test Entry") {
		t.Error("XML output does not contain 'Test Entry'")
	}
}

func TestImportCSV(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testmaster"

	db, err := Create(dbPath, password)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	csvData := `Group,Title,Username,Password,URL,Notes
Root,Imported Entry,importuser,importpass,https://import.example.com,imported notes
Root/SubGroup,Sub Entry,subuser,subpass,,
`

	n, err := db.ImportCSV(strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("ImportCSV: %v", err)
	}
	if n != 2 {
		t.Errorf("imported = %d, want 2", n)
	}

	// Save and reopen
	savePath := filepath.Join(dir, "imported.kdbx")
	if err := db.SaveAs(savePath); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}

	db2, err := Open(savePath, password)
	if err != nil {
		t.Fatalf("Open saved: %v", err)
	}

	root := db2.RootGroup()
	if len(root.Entries) < 1 {
		t.Error("no entries in root after import")
	}

	// Check the imported entry
	found := false
	for _, e := range root.Entries {
		if e.Title == "Imported Entry" && e.UserName == "importuser" {
			found = true
		}
	}
	if !found {
		t.Error("imported entry not found")
	}
}

func TestImportExportRoundTrip(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testmaster"

	createTestDB(t, dbPath, password)

	db, err := Open(dbPath, password)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Export to CSV
	var buf bytes.Buffer
	if err := db.ExportCSV(&buf); err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}

	// Create a new DB and import
	newDBPath := filepath.Join(dir, "new.kdbx")
	newDB, err := Create(newDBPath, password)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	n, err := newDB.ImportCSV(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatalf("ImportCSV: %v", err)
	}
	if n == 0 {
		t.Error("no entries imported in round trip")
	}
}
