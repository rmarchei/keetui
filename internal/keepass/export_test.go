package keepass

import (
	"bytes"
	"errors"
	"os"
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
	if !strings.HasPrefix(lines[0], "Group,Title,Username,Password,URL,Notes,TOTP") {
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

func TestXMLTOTPRoundTrip(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "totp.kdbx")
	password := "testmaster"

	db, err := Create(dbPath, password)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	root := db.RootGroup()
	_, err = db.AddEntry(root.UUID, Entry{
		Title:    "TOTP Entry",
		UserName: "totpuser",
		Password: "totppass",
		TOTP:     "otpauth://totp/Example:user@example.com?secret=JBSWY3DPEHPK3PXP&issuer=Example",
	})
	if err != nil {
		t.Fatalf("AddEntry: %v", err)
	}

	// Export to XML
	var buf bytes.Buffer
	if err := db.ExportXML(&buf); err != nil {
		t.Fatalf("ExportXML: %v", err)
	}

	xml := buf.String()
	if !strings.Contains(xml, "JBSWY3DPEHPK3PXP") {
		t.Error("XML export does not contain TOTP secret")
	}

	// Import into a new DB
	newDBPath := filepath.Join(dir, "imported.kdbx")
	newDB, err := Create(newDBPath, password)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	n, err := newDB.ImportXML(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("ImportXML: %v", err)
	}
	if n != 1 {
		t.Errorf("imported = %d, want 1", n)
	}

	// Save and reopen to verify persistence
	if err := newDB.SaveAs(filepath.Join(dir, "verify.kdbx")); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	verifyDB, err := Open(filepath.Join(dir, "verify.kdbx"), password)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	verifyRoot := verifyDB.RootGroup()
	found := false
	for _, e := range verifyRoot.Entries {
		if e.Title == "TOTP Entry" && strings.Contains(e.TOTP, "JBSWY3DPEHPK3PXP") {
			found = true
		}
	}
	if !found {
		t.Error("TOTP secret not preserved through XML export/import round trip")
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

func TestCSVTOTPRoundTrip(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "totp.kdbx")
	password := "testmaster"

	db, err := Create(dbPath, password)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Add an entry with a TOTP secret
	root := db.RootGroup()
	_, err = db.AddEntry(root.UUID, Entry{
		Title:    "TOTP Entry",
		UserName: "totpuser",
		Password: "totppass",
		TOTP:     "otpauth://totp/Example:user@example.com?secret=JBSWY3DPEHPK3PXP&issuer=Example",
	})
	if err != nil {
		t.Fatalf("AddEntry: %v", err)
	}

	// Export to CSV
	var buf bytes.Buffer
	if err := db.ExportCSV(&buf); err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}

	csv := buf.String()
	if !strings.Contains(csv, "JBSWY3DPEHPK3PXP") {
		t.Error("CSV export does not contain TOTP secret")
	}

	// Import into a new DB
	newDBPath := filepath.Join(dir, "imported.kdbx")
	newDB, err := Create(newDBPath, password)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	n, err := newDB.ImportCSV(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("ImportCSV: %v", err)
	}
	if n != 1 {
		t.Errorf("imported = %d, want 1", n)
	}

	// Save and reopen to verify persistence
	if err := newDB.SaveAs(filepath.Join(dir, "verify.kdbx")); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	verifyDB, err := Open(filepath.Join(dir, "verify.kdbx"), password)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	verifyRoot := verifyDB.RootGroup()
	found := false
	for _, e := range verifyRoot.Entries {
		if e.Title == "TOTP Entry" && strings.Contains(e.TOTP, "JBSWY3DPEHPK3PXP") {
			found = true
		}
	}
	if !found {
		t.Error("TOTP secret not preserved through CSV export/import round trip")
	}
}

func TestImportCSVWithTOTP(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testmaster"

	db, err := Create(dbPath, password)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	csvData := `Group,Title,Username,Password,URL,Notes,TOTP
Root,OTP Entry,otpuser,otppass,https://example.com,,JBSWY3DPEHPK3PXP
`

	n, err := db.ImportCSV(strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("ImportCSV: %v", err)
	}
	if n != 1 {
		t.Errorf("imported = %d, want 1", n)
	}

	// Save and reopen
	savePath := filepath.Join(dir, "saved.kdbx")
	if err := db.SaveAs(savePath); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}

	db2, err := Open(savePath, password)
	if err != nil {
		t.Fatalf("Open saved: %v", err)
	}

	root := db2.RootGroup()
	found := false
	for _, e := range root.Entries {
		if e.Title == "OTP Entry" && e.TOTP == "JBSWY3DPEHPK3PXP" {
			found = true
		}
	}
	if !found {
		t.Error("TOTP not preserved through CSV import")
	}
}

func TestExportCSVSpecialCharacters(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testmaster"

	db, err := Create(dbPath, password)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	root := db.RootGroup()
	_, err = db.AddEntry(root.UUID, Entry{
		Title:    `Entry with "quotes"`,
		UserName: "user,with,commas",
		Password: "pass\nwith\nnewlines",
		URL:      "https://example.com",
		Notes:    "notes with, commas and \"quotes\"",
	})
	if err != nil {
		t.Fatalf("AddEntry: %v", err)
	}

	var buf bytes.Buffer
	if err := db.ExportCSV(&buf); err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}

	output := buf.String()
	// CSV encoding should properly escape these values
	if !strings.Contains(output, `"quotes"`) {
		t.Error("CSV should contain the quotes in some form")
	}
	if !strings.Contains(output, "commas") {
		t.Error("CSV should contain 'commas'")
	}

	// Verify it can be imported back
	newDBPath := filepath.Join(dir, "imported.kdbx")
	newDB, err := Create(newDBPath, password)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	n, err := newDB.ImportCSV(strings.NewReader(output))
	if err != nil {
		t.Fatalf("ImportCSV: %v", err)
	}
	if n != 1 {
		t.Errorf("imported = %d, want 1", n)
	}
}

func TestExportCSVEmptyDatabase(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testmaster"

	db, err := Create(dbPath, password)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	var buf bytes.Buffer
	if err := db.ExportCSV(&buf); err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	// Should have header only
	if len(lines) != 1 {
		t.Errorf("CSV lines = %d, want 1 (header only for empty db)", len(lines))
	}
}

func TestExportXMLEmptyDatabase(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testmaster"

	db, err := Create(dbPath, password)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	var buf bytes.Buffer
	if err := db.ExportXML(&buf); err != nil {
		t.Fatalf("ExportXML: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "<KeePassFile>") {
		t.Error("XML output should have KeePassFile root even for empty db")
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

func TestImportXMLNestedGroups(t *testing.T) {
	dir := t.TempDir()
	db, err := Create(filepath.Join(dir, "src.kdbx"), "pw")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Root/Work/Email and Root/Personal/Email — same leaf name in two
	// different branches, two levels deep.
	root := db.RootGroup()
	workUUID, err := db.AddGroup(root.UUID, "Work")
	if err != nil {
		t.Fatalf("AddGroup Work: %v", err)
	}
	personalUUID, err := db.AddGroup(root.UUID, "Personal")
	if err != nil {
		t.Fatalf("AddGroup Personal: %v", err)
	}
	workEmail, err := db.AddGroup(workUUID, "Email")
	if err != nil {
		t.Fatalf("AddGroup Work/Email: %v", err)
	}
	personalEmail, err := db.AddGroup(personalUUID, "Email")
	if err != nil {
		t.Fatalf("AddGroup Personal/Email: %v", err)
	}
	if _, err := db.AddEntry(workEmail, Entry{Title: "Work Mail"}); err != nil {
		t.Fatalf("AddEntry: %v", err)
	}
	if _, err := db.AddEntry(personalEmail, Entry{Title: "Personal Mail"}); err != nil {
		t.Fatalf("AddEntry: %v", err)
	}

	var buf bytes.Buffer
	if err := db.ExportXML(&buf); err != nil {
		t.Fatalf("ExportXML: %v", err)
	}

	newDB, err := Create(filepath.Join(dir, "dst.kdbx"), "pw")
	if err != nil {
		t.Fatalf("Create dst: %v", err)
	}
	n, err := newDB.ImportXML(&buf)
	if err != nil {
		t.Fatalf("ImportXML: %v", err)
	}
	if n != 2 {
		t.Errorf("imported = %d, want 2", n)
	}

	newRoot := newDB.RootGroup()
	find := func(g *Group, name string) *Group {
		for i := range g.Groups {
			if g.Groups[i].Name == name {
				return &g.Groups[i]
			}
		}
		return nil
	}
	if find(&newRoot, "Email") != nil {
		t.Error("Email must not be created at root level (hierarchy was flattened)")
	}
	for branch, wantTitle := range map[string]string{"Work": "Work Mail", "Personal": "Personal Mail"} {
		b := find(&newRoot, branch)
		if b == nil {
			t.Fatalf("branch %s missing at root", branch)
		}
		email := find(b, "Email")
		if email == nil {
			t.Fatalf("%s/Email missing", branch)
		}
		if len(email.Entries) != 1 || email.Entries[0].Title != wantTitle {
			t.Errorf("%s/Email entries = %+v, want one entry %q", branch, email.Entries, wantTitle)
		}
	}
}

func TestExportFileHardening(t *testing.T) {
	dir := t.TempDir()
	db, err := Create(filepath.Join(dir, "src.kdbx"), "pw")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	root := db.RootGroup()
	if _, err := db.AddEntry(root.UUID, Entry{Title: "E", Password: "s3cret"}); err != nil {
		t.Fatalf("AddEntry: %v", err)
	}

	// Overwriting an existing world-readable file must tighten it to 0600.
	out := filepath.Join(dir, "out.csv")
	if err := os.WriteFile(out, []byte("old"), 0644); err != nil {
		t.Fatalf("write existing: %v", err)
	}
	if err := db.ExportCSVFile(out); err != nil {
		t.Fatalf("ExportCSVFile: %v", err)
	}
	fi, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("export file mode = %o, want 0600", fi.Mode().Perm())
	}

	// Symlink destinations are refused.
	target := filepath.Join(dir, "target.csv")
	link := filepath.Join(dir, "link.csv")
	if err := os.WriteFile(target, nil, 0600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	if err := db.ExportCSVFile(link); err == nil {
		t.Error("expected error exporting to a symlink destination")
	}
}

func TestCSVRoundTripNonRootName(t *testing.T) {
	dir := t.TempDir()
	db, err := Create(filepath.Join(dir, "src.kdbx"), "pw")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// KeePassXC-style databases name the root after the database.
	db.Database.Content.Root.Groups[0].Name = "MyVault"
	root := db.RootGroup()
	if _, err := db.AddEntry(root.UUID, Entry{Title: "TopLevel"}); err != nil {
		t.Fatalf("AddEntry: %v", err)
	}
	bankUUID, err := db.AddGroup(root.UUID, "Banking")
	if err != nil {
		t.Fatalf("AddGroup: %v", err)
	}
	if _, err := db.AddEntry(bankUUID, Entry{Title: "Bank"}); err != nil {
		t.Fatalf("AddEntry: %v", err)
	}

	var buf bytes.Buffer
	if err := db.ExportCSV(&buf); err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}

	dst, err := Create(filepath.Join(dir, "dst.kdbx"), "pw")
	if err != nil {
		t.Fatalf("Create dst: %v", err)
	}
	if _, err := dst.ImportCSV(strings.NewReader(buf.String())); err != nil {
		t.Fatalf("ImportCSV: %v", err)
	}

	newRoot := dst.RootGroup()
	for _, g := range newRoot.Groups {
		if g.Name == "MyVault" {
			t.Fatal("import must not create a wrapper group named after the source root")
		}
	}
	if len(newRoot.Entries) != 1 || newRoot.Entries[0].Title != "TopLevel" {
		t.Errorf("root entries = %+v, want TopLevel at root", newRoot.Entries)
	}
	found := false
	for _, g := range newRoot.Groups {
		if g.Name == "Banking" && len(g.Entries) == 1 && g.Entries[0].Title == "Bank" {
			found = true
		}
	}
	if !found {
		t.Error("Banking/Bank not reproduced at the correct level")
	}
}

// Protected custom fields stay protected through an XML export and import,
// and so do those of KeePass's own XML export (ProtectInMemory).
func TestXMLRoundTripKeepsProtection(t *testing.T) {
	dir := t.TempDir()
	db, err := Create(filepath.Join(dir, "source.kdbx"), "testmaster")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	root := db.RootGroup()
	if _, err := db.AddEntry(root.UUID, Entry{Title: "Service", Password: "synthetic-pass", CustomFields: []CustomField{
		{Key: "API token", Value: "synthetic-token", Protected: true},
		{Key: "Region", Value: "eu-west"},
	}}); err != nil {
		t.Fatalf("AddEntry: %v", err)
	}
	var buf bytes.Buffer
	if err := db.ExportXML(&buf); err != nil {
		t.Fatalf("ExportXML: %v", err)
	}
	keepassXML := `<?xml version="1.0" encoding="utf-8"?>
<KeePassFile><Root><Group><Name>Root</Name><Entry>
<String><Key>Title</Key><Value>From KeePass</Value></String>
<String><Key>PIN</Key><Value ProtectInMemory="True">1234</Value></String>
</Entry></Group></Root></KeePassFile>`
	for name, source := range map[string]string{"keetui": buf.String(), "KeePass": keepassXML} {
		target, err := Create(filepath.Join(dir, name+".kdbx"), "testmaster")
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if _, err := target.ImportXML(strings.NewReader(source)); err != nil {
			t.Fatalf("%s: ImportXML: %v", name, err)
		}
		protected := map[string]bool{}
		for _, e := range target.RootGroup().Entries {
			for _, cf := range e.CustomFields {
				protected[cf.Key] = cf.Protected
			}
		}
		want := map[string]bool{"API token": true, "Region": false}
		if name == "KeePass" {
			want = map[string]bool{"PIN": true}
		}
		for key, w := range want {
			if p, ok := protected[key]; !ok || p != w {
				t.Errorf("%s: %q protected = %v (found %v), want %v", name, key, p, ok, w)
			}
		}
	}
}

// failingWriter fails every write, as a full disk does.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("no space left on device") }

// The CSV writer buffers, so a small export fails only at its final flush,
// which must not report success.
func TestExportCSVReportsWriteFailure(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	createTestDB(t, dbPath, "testmaster")
	db, err := Open(dbPath, "testmaster")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.ExportCSV(failingWriter{}); err == nil || !strings.Contains(err.Error(), "no space left") {
		t.Fatalf("ExportCSV = %v, want the write error", err)
	}
}
