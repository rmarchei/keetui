package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/keepass"
)

// TestMain gives the databases tests create a minimal key derivation cost,
// in the test process and in the CLI children it re-executes (see
// TestCLIHelperProcess), which run TestMain too.
func TestMain(m *testing.M) {
	keepass.UseTestKDF()
	os.Exit(m.Run())
}

func TestZeroBytes(t *testing.T) {
	b := []byte{1, 2, 3, 4, 5}
	zeroBytes(b)
	for i, v := range b {
		if v != 0 {
			t.Errorf("b[%d] = %d, want 0", i, v)
		}
	}
}

func TestZeroBytesEmpty(t *testing.T) {
	b := []byte{}
	zeroBytes(b) // should not panic
}

func TestZeroBytesNil(t *testing.T) {
	zeroBytes(nil) // should not panic
}

func TestSelfHash(t *testing.T) {
	h, err := selfHash()
	if err != nil {
		t.Fatalf("selfHash: %v", err)
	}
	if len(h) != 64 { // SHA256 hex is 64 chars
		t.Errorf("hash length = %d, want 64", len(h))
	}
}

func TestBatchOperationsUseTheDatabaseBackupSetting(t *testing.T) {
	db, err := keepass.Create(filepath.Join(t.TempDir(), "synthetic.kdbx"), "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	if err := applyDatabaseSettings(db, config.Default()); err != nil || db.BackupOnSave {
		t.Fatalf("without database settings: backup %v, err %v; want the global off", db.BackupOnSave, err)
	}
	db.SetCustomData(config.OverridesKey, `{"backup_on_save":true}`)
	if err := applyDatabaseSettings(db, config.Default()); err != nil || !db.BackupOnSave {
		t.Errorf("the database's backup_on_save was ignored (err %v)", err)
	}
	db.SetCustomData(config.OverridesKey, `{"backup_on_save":`)
	if err := applyDatabaseSettings(db, config.Default()); err == nil || db.BackupOnSave {
		t.Errorf("unreadable settings: backup %v, err %v; want the global off and an error", db.BackupOnSave, err)
	}
}

// An export truncates its destination before writing: one that names a
// file the command reads (through a hard link or a relative path too) would
// replace the vault or its key file with plaintext.
func TestExportRefusesToOverwriteInputs(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	vault := filepath.Join(dir, "vault.kdbx")
	key := filepath.Join(dir, "vault.key")
	for _, f := range []string{vault, key} {
		if err := os.WriteFile(f, []byte("synthetic"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(dir, "link.kdbx")
	if err := os.Link(vault, link); err != nil {
		t.Fatal(err)
	}
	inputs := []namedPath{{"the database", vault}, {"the key file", key}, {"the merge source", ""}}
	for _, tc := range []struct {
		exports []string
		want    string // "" when the exports are fine
	}{
		{[]string{vault, ""}, "it is the database"},
		{[]string{"", "vault.kdbx"}, "it is the database"},
		{[]string{link, ""}, "it is the database"},
		{[]string{key, ""}, "it is the key file"},
		{[]string{"out.csv", "out.csv"}, "both go to"},
		{[]string{"out.csv", "out.xml"}, ""},
		{[]string{"", ""}, ""},
	} {
		err := checkExportDestinations(tc.exports, inputs)
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("exports %q: %v, want %q", tc.exports, err, tc.want)
		}
	}
}

// The merge preview prints names from a database that may be someone
// else's: their control sequences (OSC 52 clipboard writes, here) must not
// reach the terminal.
func TestMergePreviewSanitizesNames(t *testing.T) {
	osc52 := "\x1b]52;c;c3ludGhldGlj\x07"
	var out bytes.Buffer
	printMergePreview(&out, keepass.MergeResult{
		GroupActions: []keepass.MergeGroupAction{{Name: "Team" + osc52, ParentPath: "Root\x1b[2J", Action: "create"}},
		EntryActions: []keepass.MergeEntryAction{{Title: "Mail" + osc52, UserName: "user\r", GroupPath: "Team", Action: "add"}},
	})
	if got := out.String(); strings.ContainsAny(got, "\x1b\x07\r") || !strings.Contains(got, "+ ADD     Mail") {
		t.Errorf("preview = %q", got)
	}
}
