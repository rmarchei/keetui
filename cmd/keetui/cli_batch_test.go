//go:build linux || darwin

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tobischo/gokeepasslib/v3"

	"github.com/rmarchei/keetui/internal/keepass"
)

var passwordPrompt = regexp.MustCompile(`(?:Enter|Confirm) master password(?: for [^\n]+)?: `)

type cliPasswordResponder struct {
	output    bytes.Buffer
	terminal  *os.File
	passwords []string
	sent      int
}

func (r *cliPasswordResponder) String() string { return r.output.String() }

func (r *cliPasswordResponder) Write(p []byte) (int, error) {
	n, _ := r.output.Write(p)
	count := len(passwordPrompt.FindAllIndex(r.output.Bytes(), -1))
	for r.sent < count {
		if r.sent == len(r.passwords) {
			return n, fmt.Errorf("unexpected password prompt %d", r.sent+1)
		}
		if _, err := fmt.Fprintln(r.terminal, r.passwords[r.sent]); err != nil {
			return n, err
		}
		r.sent++
	}
	return n, nil
}

func runBatchCLI(t *testing.T, passwords []string, args ...string) (string, string, int) {
	t.Helper()
	master, slave := openCLIPTY(t)
	cmd := newCLICommand(t, "{}", args...)
	var out bytes.Buffer
	response := &cliPasswordResponder{terminal: master, passwords: passwords}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, &out, response
	code := 0
	if err := cmd.Run(); err != nil {
		if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
			t.Fatalf("batch command failed: %v; stderr %s", err, response.String())
		}
		code = cmd.ProcessState.ExitCode()
	}
	if response.sent != len(passwords) {
		t.Fatalf("answered %d password prompts, expected %d; stderr %s", response.sent, len(passwords), response.String())
	}
	for _, password := range passwords {
		if password != "" && (strings.Contains(out.String(), password) || strings.Contains(response.String(), password)) {
			t.Fatal("password appeared in CLI output")
		}
	}
	return out.String(), response.String(), code
}

func syntheticCLIDatabase(t *testing.T, name string) *keepass.DB {
	t.Helper()
	db, err := keepass.Create(filepath.Join(t.TempDir(), name+".kdbx"), "synthetic-cli-password")
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// syntheticKDBX31Database writes a new KDBX 3.1 database with AES-KDF, as
// keetui created before KDBX 4 and other clients still may, and opens it.
func syntheticKDBX31Database(t *testing.T, name string) *keepass.DB {
	t.Helper()
	raw := gokeepasslib.NewDatabase(gokeepasslib.WithDatabaseKDBXVersion3())
	raw.Credentials = gokeepasslib.NewPasswordCredentials("synthetic-cli-password")
	root := gokeepasslib.NewGroup()
	root.Name = "Root"
	raw.Content.Root = &gokeepasslib.RootData{Groups: []gokeepasslib.Group{root}}
	var file bytes.Buffer
	if err := gokeepasslib.NewEncoder(&file).Encode(raw); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), name+".kdbx")
	if err := os.WriteFile(path, file.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := keepass.Open(path, "synthetic-cli-password")
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func addCLIFixture(t *testing.T, db *keepass.DB) {
	t.Helper()
	group, err := db.AddGroup(db.RootGroup().UUID, "Team")
	if err != nil {
		t.Fatal(err)
	}
	id, err := db.AddEntry(group, keepass.Entry{Title: "Quoted, \"entry\"", UserName: "synthetic-user", Password: "synthetic-entry-secret", Notes: "first line\nsecond line", CustomFields: []keepass.CustomField{{Key: "API token", Value: "synthetic-token", Protected: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AddAttachment(id, "synthetic.key", []byte("synthetic-attachment")); err != nil {
		t.Fatal(err)
	}
	if err := db.Save(); err != nil {
		t.Fatal(err)
	}
}

func TestCLIBatchExportImportPersistsEntriesAndBackup(t *testing.T) {
	for _, format := range []string{"csv", "xml"} {
		t.Run(format, func(t *testing.T) {
			source := syntheticCLIDatabase(t, "source")
			addCLIFixture(t, source)
			before, err := os.ReadFile(source.Path)
			if err != nil {
				t.Fatal(err)
			}
			exported := filepath.Join(t.TempDir(), "entries."+format)
			_, stderr, code := runBatchCLI(t, []string{"synthetic-cli-password"}, "-read-only", "-export-"+format, exported, source.Path)
			if code != 0 {
				t.Fatalf("export exit %d: %s", code, stderr)
			}
			info, err := os.Stat(exported)
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatalf("export permissions: %v, %v", info, err)
			}
			target := syntheticCLIDatabase(t, "target")
			target.SetCustomData("KEETUI_SETTINGS", `{"backup_on_save":true}`)
			if err := target.Save(); err != nil {
				t.Fatal(err)
			}
			previous, err := os.ReadFile(target.Path)
			if err != nil {
				t.Fatal(err)
			}
			stdout, stderr, code := runBatchCLI(t, []string{"synthetic-cli-password"}, "-import-"+format, exported, target.Path)
			if code != 0 || !strings.Contains(stdout, "Imported 1 entries") {
				t.Fatalf("import exit %d, stdout %s, stderr %s", code, stdout, stderr)
			}
			reopened, err := keepass.Open(target.Path, "synthetic-cli-password")
			if err != nil {
				t.Fatal(err)
			}
			groups := reopened.RootGroup().Groups
			if len(groups) != 1 || groups[0].Name != "Team" || len(groups[0].Entries) != 1 {
				t.Fatalf("imported groups: %+v", groups)
			}
			entry := groups[0].Entries[0]
			if entry.Title != "Quoted, \"entry\"" || entry.Password != "synthetic-entry-secret" || entry.Notes != "first line\nsecond line" {
				t.Fatal("batch round trip changed the entry")
			}
			if format == "xml" && (len(entry.CustomFields) != 1 || !entry.CustomFields[0].Protected || len(reopened.EntryAttachments(entry.UUID)) != 1) {
				t.Fatal("XML batch import lost protected fields or attachments")
			}
			backup, err := os.ReadFile(target.Path + ".bak")
			if err != nil || !bytes.Equal(backup, previous) {
				t.Fatal("batch import ignored the database's backup setting")
			}
			after, err := os.ReadFile(source.Path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("read-only export modified its source vault")
			}
		})
	}
}

func TestCLIBatchMergeDryRunAndWrite(t *testing.T) {
	source, target := syntheticCLIDatabase(t, "source"), syntheticCLIDatabase(t, "target")
	addCLIFixture(t, source)
	before, err := os.ReadFile(target.Path)
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runBatchCLI(t, []string{"synthetic-cli-password", "synthetic-cli-password"}, "-read-only", "-dry-run", "-merge", source.Path, target.Path)
	if code != 0 || !strings.Contains(stdout, "1 to add") || strings.Contains(stdout, "synthetic-entry-secret") {
		t.Fatalf("preview exit %d, stdout %s, stderr %s", code, stdout, stderr)
	}
	after, err := os.ReadFile(target.Path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("merge preview changed the destination")
	}
	stdout, stderr, code = runBatchCLI(t, []string{"synthetic-cli-password", "synthetic-cli-password"}, "-merge", source.Path, target.Path)
	if code != 0 || !strings.Contains(stdout, "Merged: 1 added") {
		t.Fatalf("merge exit %d, stdout %s, stderr %s", code, stdout, stderr)
	}
	reopened, err := keepass.Open(target.Path, "synthetic-cli-password")
	if err != nil {
		t.Fatal(err)
	}
	groups := reopened.RootGroup().Groups
	if len(groups) != 1 || len(groups[0].Entries) != 1 || groups[0].Entries[0].Password != "synthetic-entry-secret" {
		t.Fatal("batch merge did not persist source entries")
	}
}

func TestCLIBatchUpgradeKeepsEntriesAndAlreadyUpgradedFile(t *testing.T) {
	db := syntheticKDBX31Database(t, "upgrade")
	addCLIFixture(t, db)
	if db.IsKdbx4() {
		t.Fatal("fixture is not KDBX 3.1")
	}
	stdout, stderr, code := runBatchCLI(t, []string{"synthetic-cli-password"}, "-upgrade-kdbx4", db.Path)
	if code != 0 || !strings.Contains(stdout, "Upgraded") {
		t.Fatalf("upgrade exit %d, stdout %s, stderr %s", code, stdout, stderr)
	}
	reopened, err := keepass.Open(db.Path, "synthetic-cli-password")
	if err != nil || !reopened.IsKdbx4() {
		t.Fatalf("upgraded file unreadable: %v", err)
	}
	entry := reopened.RootGroup().Groups[0].Entries[0]
	if entry.Password != "synthetic-entry-secret" || len(reopened.EntryAttachments(entry.UUID)) != 1 {
		t.Fatal("upgrade lost entry data")
	}
	before, err := os.ReadFile(db.Path)
	if err != nil {
		t.Fatal(err)
	}
	_, stderr, code = runBatchCLI(t, []string{"synthetic-cli-password"}, "-upgrade-kdbx4", db.Path)
	if code != 0 || !strings.Contains(stderr, "already KDBX 4") {
		t.Fatalf("second upgrade exit %d: %s", code, stderr)
	}
	after, err := os.ReadFile(db.Path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("already-upgraded vault was rewritten")
	}
}

func TestCLINewRefusesEmptyOrMismatchedPasswords(t *testing.T) {
	for _, passwords := range [][]string{{""}, {"synthetic-one", "synthetic-two"}} {
		path := filepath.Join(t.TempDir(), "new.kdbx")
		_, stderr, code := runBatchCLI(t, passwords, "-new", path)
		if code != 1 || !strings.Contains(stderr, "Error: password") {
			t.Fatalf("invalid password exit %d: %s", code, stderr)
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("invalid password created a database")
		}
	}
}

func TestCLIBatchWrongPasswordLeavesDatabaseUntouched(t *testing.T) {
	db := syntheticCLIDatabase(t, "wrong-password")
	before, err := os.ReadFile(db.Path)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out.csv")
	_, stderr, code := runBatchCLI(t, []string{"synthetic-wrong-password"}, "-export-csv", out, db.Path)
	if code != 1 || !strings.Contains(stderr, "Error opening database") {
		t.Fatalf("wrong password exit %d: %s", code, stderr)
	}
	after, err := os.ReadFile(db.Path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed unlock modified the database")
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed unlock created a plaintext export")
	}
}

func TestCLINewAndExportWithPasswordOrKeyfile(t *testing.T) {
	for _, mode := range []string{"password", "keyfile", "password and keyfile"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			path, exported := filepath.Join(dir, "new.kdbx"), filepath.Join(dir, "new.csv")
			password := "synthetic-new-master"
			if mode == "keyfile" {
				password = ""
			}
			args := []string{"-new", "-export-csv", exported}
			key := ""
			if mode != "password" {
				key = filepath.Join(dir, "new.key")
				if err := os.WriteFile(key, []byte("synthetic keyfile contents"), 0600); err != nil {
					t.Fatal(err)
				}
				args = append(args, "-keyfile", key)
			}
			passwords := []string{password}
			if password != "" {
				passwords = append(passwords, password)
			}
			passwords = append(passwords, password) // Unlock the new vault for export.
			stdout, stderr, code := runBatchCLI(t, passwords, append(args, path)...)
			if code != 0 || !strings.Contains(stdout, "Database created:") || !strings.Contains(stdout, "Exported to") {
				t.Fatalf("create exit %d, stdout %s, stderr %s", code, stdout, stderr)
			}
			db, err := keepass.OpenWithKeyFile(path, password, key)
			if err != nil {
				t.Fatalf("new vault cannot be opened with its credentials: %v", err)
			}
			if len(db.RootGroup().Entries) != 0 {
				t.Fatal("new vault contains unexpected entries")
			}
			if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
				t.Fatalf("new vault permissions: %v, %v", info, err)
			}
		})
	}
}

func TestCLIBatchFailuresLeaveVaultUnchanged(t *testing.T) {
	for _, mode := range []string{"missing CSV", "malformed XML", "export directory", "wrong keyfile"} {
		t.Run(mode, func(t *testing.T) {
			db := syntheticCLIDatabase(t, "batch-failure")
			addCLIFixture(t, db)
			before, err := os.ReadFile(db.Path)
			if err != nil {
				t.Fatal(err)
			}
			var args []string
			var want string
			switch mode {
			case "missing CSV":
				args, want = []string{"-import-csv", filepath.Join(t.TempDir(), "missing.csv")}, "Error importing CSV"
			case "malformed XML":
				path := filepath.Join(t.TempDir(), "bad.xml")
				if err := os.WriteFile(path, []byte("<KeePassFile><Root>"), 0600); err != nil {
					t.Fatal(err)
				}
				args, want = []string{"-import-xml", path}, "Error importing XML"
			case "export directory":
				args, want = []string{"-export-xml", t.TempDir()}, "Error exporting XML"
			case "wrong keyfile":
				key := filepath.Join(t.TempDir(), "wrong.key")
				if err := os.WriteFile(key, []byte("synthetic unexpected key"), 0600); err != nil {
					t.Fatal(err)
				}
				args, want = []string{"-keyfile", key, "-export-csv", filepath.Join(t.TempDir(), "out.csv")}, "Error opening database"
			}
			_, stderr, code := runBatchCLI(t, []string{"synthetic-cli-password"}, append(args, db.Path)...)
			if code != 1 || !strings.Contains(stderr, want) {
				t.Fatalf("failed command exit %d: %s; want %s", code, stderr, want)
			}
			after, err := os.ReadFile(db.Path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("failed batch command changed its vault")
			}
		})
	}
}
