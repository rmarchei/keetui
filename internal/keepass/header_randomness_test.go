package keepass

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tobischo/gokeepasslib/v3"
)

// headerRandomness holds copies of the values a save must draw anew, by name.
type headerRandomness map[string][]byte

func randomnessOf(db *DB) headerRandomness {
	fh := db.Database.Header.FileHeaders
	if db.IsKdbx4() {
		return headerRandomness{
			"MasterSeed":           bytes.Clone(fh.MasterSeed),
			"EncryptionIV":         bytes.Clone(fh.EncryptionIV),
			"KDF salt":             bytes.Clone(fh.KdfParameters.Salt[:]),
			"InnerRandomStreamKey": bytes.Clone(db.Database.Content.InnerHeader.InnerRandomStreamKey),
		}
	}
	return headerRandomness{
		"MasterSeed":         bytes.Clone(fh.MasterSeed),
		"TransformSeed":      bytes.Clone(fh.TransformSeed),
		"EncryptionIV":       bytes.Clone(fh.EncryptionIV),
		"ProtectedStreamKey": bytes.Clone(fh.ProtectedStreamKey),
		"StreamStartBytes":   bytes.Clone(fh.StreamStartBytes),
	}
}

// fixedHeaderParams describes the header settings a save must keep.
func fixedHeaderParams(db *DB) string {
	h := db.Database.Header
	fh := h.FileHeaders
	params := fmt.Sprintf("KDBX %d.%d cipher=%x compression=%d",
		h.Signature.MajorVersion, h.Signature.MinorVersion, fh.CipherID, fh.CompressionFlags)
	if !db.IsKdbx4() {
		return params + fmt.Sprintf(" rounds=%d stream=%d", fh.TransformRounds, fh.InnerRandomStreamID)
	}
	kdf := fh.KdfParameters
	return params + fmt.Sprintf(" kdf=%x rounds=%d iterations=%d memory=%d parallelism=%d version=%d stream=%d",
		kdf.UUID, kdf.Rounds, kdf.Iterations, kdf.Memory, kdf.Parallelism, kdf.Version,
		db.Database.Content.InnerHeader.InnerRandomStreamID)
}

// assertFreshRandomness fails unless every value in got has the length of,
// but differs from, the same value in each earlier state.
func assertFreshRandomness(t *testing.T, got headerRandomness, earlier ...headerRandomness) {
	t.Helper()
	for i, prev := range earlier {
		if len(prev) != len(got) {
			t.Fatalf("state %d has %d random fields, want %d", i, len(prev), len(got))
		}
		for _, name := range slices.Sorted(maps.Keys(got)) {
			switch value, old := got[name], prev[name]; {
			case len(value) != len(old):
				t.Errorf("%s has %d bytes, state %d had %d", name, len(value), i, len(old))
			case bytes.Equal(value, old):
				t.Errorf("%s reused from state %d: %x", name, i, value)
			}
		}
	}
}

const saveTestPassword = "correct horse battery staple"

var saveTestBinary = func() []byte {
	data := make([]byte, 256)
	for i := range data {
		data[i] = byte(i)
	}
	return data
}()

type saveTestFormat struct {
	name    string
	keyFile bool
	setup   func(t *testing.T, db *DB) // adjusts the newly created KDBX 3.1 database
}

func saveTestFormats() []saveTestFormat {
	toKDBX4 := func(t *testing.T, db *DB) {
		t.Helper()
		if err := db.ConvertToKDBX4(); err != nil {
			t.Fatalf("convert to KDBX 4: %v", err)
		}
	}
	// Non-default costs show that a save keeps them instead of resetting them.
	return []saveTestFormat{
		{name: "KDBX 3.1", setup: func(t *testing.T, db *DB) {
			db.Database.Header.FileHeaders.TransformRounds = 7000
		}},
		{name: "KDBX 3.1 key file", keyFile: true},
		{name: "KDBX 4 Argon2", setup: func(t *testing.T, db *DB) {
			toKDBX4(t, db)
			kdf := db.Database.Header.FileHeaders.KdfParameters
			kdf.Iterations, kdf.Memory, kdf.Parallelism = 3, 2<<20, 1
		}},
		{name: "KDBX 4 Argon2 key file", keyFile: true, setup: toKDBX4},
		{name: "KDBX 4 AES-KDF", setup: func(t *testing.T, db *DB) {
			toKDBX4(t, db)
			fh := db.Database.Header.FileHeaders
			// KeePass and KeeWeb identify AES-KDF in KDBX 4 files by the UUID
			// that gokeepasslib calls KdfAES3.
			fh.KdfParameters = &gokeepasslib.KdfParameters{UUID: gokeepasslib.KdfAES3, Rounds: 1234}
			_, _ = rand.Read(fh.KdfParameters.Salt[:])
			fh.CipherID = gokeepasslib.CipherAES
			fh.EncryptionIV = make([]byte, 16)
			_, _ = rand.Read(fh.EncryptionIV)
		}},
	}
}

// createSaveTestDB writes a populated database in the given format. It returns
// the file's path and a function opening a file with the same credentials.
func createSaveTestDB(t *testing.T, format saveTestFormat) (string, func(*testing.T, string) *DB) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.kdbx")
	keyPath := ""
	if format.keyFile {
		keyPath = filepath.Join(dir, "vault.key")
		if err := os.WriteFile(keyPath, []byte("synthetic key file for save tests"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	db := createKDBX31(t, path, saveTestPassword, keyPath)
	if format.setup != nil {
		format.setup(t, db)
	}
	populateSaveTestDB(t, db)
	params := fixedHeaderParams(db)
	if err := db.Save(); err != nil {
		t.Fatalf("save populated database: %v", err)
	}
	open := func(t *testing.T, path string) *DB {
		t.Helper()
		db, err := OpenWithKeyFile(path, saveTestPassword, keyPath)
		if err != nil {
			t.Fatalf("open %s: %v", filepath.Base(path), err)
		}
		return db
	}
	if got := fixedHeaderParams(open(t, path)); got != params {
		t.Fatalf("saved header: %s, want %s", got, params)
	}
	return path, open
}

// populateSaveTestDB stores protected values everywhere a save re-encrypts
// them: passwords, TOTP secrets, custom fields and history, next to attachments.
func populateSaveTestDB(t *testing.T, db *DB) {
	t.Helper()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	root := db.RootGroup().UUID
	email, err := db.AddGroup(root, "Email")
	must(err)
	mail, err := db.AddEntry(email, Entry{Title: "mail", UserName: "alice", Password: "mail-pw-1"})
	must(err)
	// Every update keeps the previous values as a history record.
	must(db.UpdateEntry(mail, Entry{
		Title: "mail", UserName: "alice", Password: "mäil-pw-2 🔑", TOTP: "JBSWY3DPEHPK3PXP",
		CustomFields: []CustomField{{Key: "PIN", Value: "4321", Protected: true}, {Key: "Hint", Value: "blue"}},
	}))
	must(db.AddAttachment(mail, "note.txt", []byte("attached note")))
	bank, err := db.AddEntry(root, Entry{Title: "bank", Password: "bank-pw-1"})
	must(err)
	for _, password := range []string{"bank-pw-2", "bank-pw-3"} {
		must(db.UpdateEntry(bank, Entry{Title: "bank", Password: password, CustomFields: []CustomField{
			{Key: "Recovery", Value: "recovery-" + password, Protected: true},
		}}))
	}
	must(db.AddAttachment(bank, "key.bin", saveTestBinary))
	_, err = db.AddEntry(root, Entry{Title: "blank"})
	must(err)
}

func attachmentData(t *testing.T, db *DB, entryUUID gokeepasslib.UUID, name string) []byte {
	t.Helper()
	raw, err := db.GetRawEntry(entryUUID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range raw.Binaries {
		if ref.Name != name {
			continue
		}
		bin := db.Database.FindBinary(ref.Value.ID)
		if bin == nil {
			t.Fatalf("attachment %q has no binary", name)
		}
		data, err := bin.GetContentBytes()
		if err != nil {
			t.Fatalf("read attachment %q: %v", name, err)
		}
		return data
	}
	t.Fatalf("attachment %q not found", name)
	return nil
}

// assertSaveTestContent checks every value populateSaveTestDB stores.
func assertSaveTestContent(t *testing.T, db *DB) {
	t.Helper()
	if db.SuspectCorruption() {
		t.Fatal("protected values look corrupted")
	}
	entries := keewebEntries(t, db)
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	mail, bank := entries["mail"], entries["bank"]
	history := func(entry Entry) string {
		var passwords []string
		for _, h := range db.EntryHistory(entry.UUID) {
			passwords = append(passwords, h.Password)
		}
		return strings.Join(passwords, ",")
	}
	rawBank, err := db.GetRawEntry(bank.UUID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ what, got, want string }{
		{"mail password", mail.Password, "mäil-pw-2 🔑"},
		{"mail TOTP", mail.TOTP, "JBSWY3DPEHPK3PXP"},
		{"mail history", history(mail), "mail-pw-1"},
		{"bank password", bank.Password, "bank-pw-3"},
		{"bank history", history(bank), "bank-pw-2,bank-pw-1"},
		{"bank recovery in history", rawBank.Histories[0].Entries[1].GetContent("Recovery"), "recovery-bank-pw-2"},
		{"blank password", entries["blank"].Password, ""},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.what, c.got, c.want)
		}
	}
	if want := []CustomField{{Key: "PIN", Value: "4321", Protected: true}, {Key: "Hint", Value: "blue"}}; !slices.Equal(mail.CustomFields, want) {
		t.Errorf("mail custom fields = %+v, want %+v", mail.CustomFields, want)
	}
	if want := []CustomField{{Key: "Recovery", Value: "recovery-bank-pw-3", Protected: true}}; !slices.Equal(bank.CustomFields, want) {
		t.Errorf("bank custom fields = %+v, want %+v", bank.CustomFields, want)
	}
	if got := attachmentData(t, db, mail.UUID, "note.txt"); string(got) != "attached note" {
		t.Errorf("note.txt = %q, want %q", got, "attached note")
	}
	if got := attachmentData(t, db, bank.UUID, "key.bin"); !bytes.Equal(got, saveTestBinary) {
		t.Errorf("key.bin = %x, want %x", got, saveTestBinary)
	}
}

// Each save must encrypt with fresh randomness, as KeePass does: otherwise
// two versions of a file, such as the database and its backup, share a
// ChaCha20 keystream (XORing them yields the XOR of their plaintexts) or an
// AES-CBC prefix.
func TestSaveRefreshesRandomHeaderFields(t *testing.T) {
	for _, format := range saveTestFormats() {
		t.Run(format.name, func(t *testing.T) {
			path, open := createSaveTestDB(t, format)
			db := open(t, path)
			assertSaveTestContent(t, db)
			params := fixedHeaderParams(db)
			seen := []headerRandomness{randomnessOf(db)}
			db.BackupOnSave = true
			for i := range 2 {
				if err := db.Save(); err != nil {
					t.Fatalf("save %d: %v", i+1, err)
				}
				// Saving must leave the live database unlocked and intact.
				assertSaveTestContent(t, db)
				saved := open(t, path)
				assertSaveTestContent(t, saved)
				if got := fixedHeaderParams(saved); got != params {
					t.Errorf("save %d header: %s, want %s", i+1, got, params)
				}
				got := randomnessOf(saved)
				assertFreshRandomness(t, got, seen...)
				seen = append(seen, got)
			}
			// The backup holds the first save, whose values the file must not share.
			backup := open(t, path+".bak")
			assertSaveTestContent(t, backup)
			if got := randomnessOf(backup); !bytes.Equal(got["MasterSeed"], seen[1]["MasterSeed"]) {
				t.Error("backup is not the first save")
			}
		})
	}
}

func TestSaveAfterConvertToKDBX4RefreshesRandomHeaderFields(t *testing.T) {
	path, open := createSaveTestDB(t, saveTestFormat{name: "KDBX 3.1 key file", keyFile: true})
	db := open(t, path)
	if err := db.ConvertToKDBX4(); err != nil {
		t.Fatalf("convert to KDBX 4: %v", err)
	}
	var params string
	var seen []headerRandomness
	for i, name := range []string{"first.kdbx", "second.kdbx"} {
		target := filepath.Join(filepath.Dir(path), name)
		if err := db.SaveAs(target); err != nil {
			t.Fatalf("save %d: %v", i+1, err)
		}
		assertSaveTestContent(t, db)
		saved := open(t, target)
		if !saved.IsKdbx4() {
			t.Fatalf("save %d is not KDBX 4", i+1)
		}
		assertSaveTestContent(t, saved)
		if params == "" {
			params = fixedHeaderParams(saved)
		} else if got := fixedHeaderParams(saved); got != params {
			t.Errorf("save %d header: %s, want %s", i+1, got, params)
		}
		got := randomnessOf(saved)
		assertFreshRandomness(t, got, seen...)
		seen = append(seen, got)
	}
}

// A save can fail before, during or after encoding. The database must stay
// unlocked and consistent, so that a later save writes intact values that
// were never used before, not even by a failed attempt's temporary file.
func TestFailedSaveKeepsDatabaseUsable(t *testing.T) {
	for _, format := range saveTestFormats() {
		t.Run(format.name, func(t *testing.T) {
			path, open := createSaveTestDB(t, format)
			dir := filepath.Dir(path)
			db := open(t, path)
			seen := []headerRandomness{randomnessOf(db)}
			fail := func(what string, save func() error) {
				t.Helper()
				if err := save(); err == nil {
					t.Fatalf("%s: save succeeded", what)
				}
				assertSaveTestContent(t, db)
				seen = append(seen, randomnessOf(db))
			}

			fail("missing directory", func() error {
				return db.SaveAs(filepath.Join(dir, "missing", "vault.kdbx"))
			})
			// The encoder fails after its own unlock and re-lock cycle.
			credentials := db.Database.Credentials
			db.Database.Credentials = nil
			fail("missing credentials", db.Save)
			db.Database.Credentials = credentials
			// Writing the backup fails after the temporary file is complete.
			db.BackupOnSave = true
			if err := os.Symlink(filepath.Join(dir, "elsewhere"), path+".bak"); err != nil {
				t.Fatal(err)
			}
			fail("symlinked backup", db.Save)
			if err := os.Remove(path + ".bak"); err != nil {
				t.Fatal(err)
			}

			if err := db.Save(); err != nil {
				t.Fatalf("save after failures: %v", err)
			}
			assertSaveTestContent(t, db)
			saved := open(t, path)
			assertSaveTestContent(t, saved)
			assertFreshRandomness(t, randomnessOf(saved), seen...)
		})
	}
}
