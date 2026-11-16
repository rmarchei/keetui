package keepass

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobischo/gokeepasslib/v3"
)

// New databases are KDBX 4 and derive their key with Argon2d at the target
// cost, a new salt each, whatever protects them.
func TestCreateUsesKDBX4WithTargetKDF(t *testing.T) {
	for _, keyFile := range []bool{false, true} {
		dir := t.TempDir()
		path, keyPath := filepath.Join(dir, "vault.kdbx"), ""
		if keyFile {
			keyPath = filepath.Join(dir, "vault.key")
			if err := os.WriteFile(keyPath, []byte("synthetic key file"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		db, err := CreateWithKeyFile(path, "synthetic", keyPath)
		if err != nil {
			t.Fatalf("key file %t: %v", keyFile, err)
		}
		if !db.IsKdbx4() || db.KDF() != targetKDF {
			t.Fatalf("key file %t: created %s with %s, want KDBX 4 with %s", keyFile, db.FormatVersion(), db.KDF(), targetKDF)
		}
		if db.Database.Header.FileHeaders.KdfParameters.Salt == [32]byte{} {
			t.Errorf("key file %t: the KDF salt is zero", keyFile)
		}
		reopened, err := OpenWithKeyFile(path, "synthetic", keyPath)
		if err != nil {
			t.Fatalf("key file %t: reopen: %v", keyFile, err)
		}
		if !reopened.IsKdbx4() || reopened.KDF() != targetKDF {
			t.Errorf("key file %t: file holds %s with %s, want KDBX 4 with %s", keyFile, reopened.FormatVersion(), reopened.KDF(), targetKDF)
		}
	}
}

// The production cost is KeePassXC's default memory, two lanes and passes
// taking most of a second, and Create writes it. This is the one test that
// derives a key at that cost; the others use testKDF (see TestMain).
func TestProductionKDF(t *testing.T) {
	want := KDF{Name: "Argon2d", Memory: 64 << 20, Iterations: 20, Parallelism: 2}
	if productionKDF != want {
		t.Fatalf("production KDF is %s, want %s", productionKDF, want)
	}
	if testKDF == productionKDF || testKDF.Name != kdfArgon2d {
		t.Fatalf("test KDF %s must be a cheaper Argon2d", testKDF)
	}
	setTargetKDF(t, productionKDF)
	if raceDetector {
		t.Skip("deriving a key at the production cost takes about 10 s with the race detector")
	}
	path := filepath.Join(t.TempDir(), "vault.kdbx")
	start := time.Now()
	if _, err := Create(path, "synthetic"); err != nil {
		t.Fatal(err)
	}
	created := time.Since(start)
	start = time.Now()
	db, err := Open(path, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	opened := time.Since(start)
	if !db.IsKdbx4() || db.KDF() != want {
		t.Fatalf("file holds %s with %s, want KDBX 4 with %s", db.FormatVersion(), db.KDF(), want)
	}
	t.Logf("%s: create %v, open %v", want, created.Round(time.Millisecond), opened.Round(time.Millisecond))
}

// Converting KDBX 3.1 to KDBX 4 gives the key derivation the cost of new
// databases, not gokeepasslib's default (1 MiB, 2 passes), whatever AES-KDF
// cost the database had.
func TestConvertToKDBX4UsesTargetKDF(t *testing.T) {
	convert := func(t *testing.T, target KDF) *DB {
		t.Helper()
		setTargetKDF(t, target)
		db := createKDBX31(t, filepath.Join(t.TempDir(), "vault.kdbx"), "synthetic", "")
		db.Database.Header.FileHeaders.TransformRounds = 5_000_000
		if err := db.ConvertToKDBX4(); err != nil {
			t.Fatal(err)
		}
		if got := db.KDF(); got != target {
			t.Fatalf("converted to %s, want %s", got, target)
		}
		if db.Database.Header.FileHeaders.KdfParameters.Salt == [32]byte{} {
			t.Error("the KDF salt is zero")
		}
		return db
	}
	t.Run("production", func(t *testing.T) {
		// In memory only: saving would derive a key at the production cost.
		convert(t, productionKDF)
	})
	t.Run("saved", func(t *testing.T) {
		target := KDF{Name: kdfArgon2d, Memory: 2 << 20, Iterations: 3, Parallelism: 2}
		db := convert(t, target)
		if err := db.Save(); err != nil {
			t.Fatal(err)
		}
		reopened, err := Open(db.Path, "synthetic")
		if err != nil {
			t.Fatal(err)
		}
		if got := reopened.KDF(); got != target {
			t.Errorf("file holds %s, want %s", got, target)
		}
	})
}

// strengthenTarget is a target above testKDF, cheap enough to save with.
var strengthenTarget = KDF{Name: kdfArgon2d, Memory: 1 << 20, Iterations: 3, Parallelism: 2}

// setArgon2 gives a KDBX 4 database Argon2 key derivation at the given cost.
func setArgon2(db *DB, uuid []byte, memory, iterations uint64, parallelism uint32) {
	p := db.Database.Header.FileHeaders.KdfParameters
	p.UUID, p.Memory, p.Iterations, p.Parallelism = uuid, memory, iterations, parallelism
}

// setAESKDF gives a KDBX 4 database AES-KDF, as KeePass writes it.
func setAESKDF(db *DB) {
	fh := db.Database.Header.FileHeaders
	fh.KdfParameters = &gokeepasslib.KdfParameters{UUID: gokeepasslib.KdfAES3, Rounds: 1234}
	fh.CipherID = gokeepasslib.CipherAES
	fh.EncryptionIV = make([]byte, 16)
}

func TestWeakKDF(t *testing.T) {
	setTargetKDF(t, strengthenTarget)
	m, i, p := strengthenTarget.Memory, strengthenTarget.Iterations, strengthenTarget.Parallelism
	for _, tc := range []struct {
		name  string
		setup func(db *DB)
		weak  bool
	}{
		{"KDBX 3.1", nil, true},
		{"KDBX 4 AES-KDF", setAESKDF, true},
		{"less memory", func(db *DB) { setArgon2(db, gokeepasslib.KdfArgon2, m/2, i*10, p) }, true},
		{"fewer passes", func(db *DB) { setArgon2(db, gokeepasslib.KdfArgon2, m*10, i-1, p) }, true},
		{"Argon2id with fewer passes", func(db *DB) { setArgon2(db, argon2idUUID, m, i-1, p) }, true},
		{"the target", func(db *DB) { setArgon2(db, gokeepasslib.KdfArgon2, m, i, p) }, false},
		{"fewer lanes", func(db *DB) { setArgon2(db, gokeepasslib.KdfArgon2, m, i, 1) }, false},
		{"above the target", func(db *DB) { setArgon2(db, gokeepasslib.KdfArgon2, m*2, i*2, p*2) }, false},
		{"Argon2id", func(db *DB) { setArgon2(db, argon2idUUID, m, i, p) }, false},
	} {
		db := &DB{Database: gokeepasslib.NewDatabase()}
		if tc.setup != nil {
			db.Database = gokeepasslib.NewDatabase(gokeepasslib.WithDatabaseKDBXVersion40())
			tc.setup(db)
		}
		if got := db.WeakKDF(); got != tc.weak {
			t.Errorf("%s (%s): weak = %t, want %t", tc.name, db.KDF(), got, tc.weak)
		}
	}
}

// StrengthenKDF brings every weak key derivation to at least the target and
// never lowers a parameter already above it; the database keeps its entries
// and attachments, and the file the new cost.
func TestStrengthenKDF(t *testing.T) {
	setTargetKDF(t, strengthenTarget)
	m, i, p := strengthenTarget.Memory, strengthenTarget.Iterations, strengthenTarget.Parallelism
	for _, tc := range []struct {
		name  string
		kdbx4 bool
		setup func(db *DB)
		want  KDF
	}{
		{"KDBX 3.1", false, nil, strengthenTarget},
		{"KDBX 4 AES-KDF", true, setAESKDF, strengthenTarget},
		{"gokeepasslib's default", true, func(db *DB) { setArgon2(db, gokeepasslib.KdfArgon2, 1<<20, 2, 2) }, strengthenTarget},
		{"less memory, more passes and lanes", true, func(db *DB) { setArgon2(db, gokeepasslib.KdfArgon2, m/4, i*2, p*2) },
			KDF{Name: kdfArgon2d, Memory: m, Iterations: i * 2, Parallelism: p * 2}},
		{"more memory, fewer passes and lanes", true, func(db *DB) { setArgon2(db, gokeepasslib.KdfArgon2, m*2, 1, 1) },
			KDF{Name: kdfArgon2d, Memory: m * 2, Iterations: i, Parallelism: p}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "vault.kdbx")
			db := createFormat(t, path, "synthetic", tc.kdbx4)
			if tc.setup != nil {
				tc.setup(db)
			}
			id, err := db.AddEntry(db.RootGroup().UUID, Entry{Title: "Service", Password: "synthetic-secret"})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.AddAttachment(id, "recovery.txt", []byte("synthetic attachment")); err != nil {
				t.Fatal(err)
			}
			if err := db.Overwrite(); err != nil {
				t.Fatal(err)
			}
			before := db.KDF()
			if !db.WeakKDF() {
				t.Fatalf("%s is not weak", before)
			}
			changed, err := db.StrengthenKDF()
			if err != nil || !changed {
				t.Fatalf("strengthen %s: changed %t, %v", before, changed, err)
			}
			if got := db.KDF(); !db.IsKdbx4() || got != tc.want || db.WeakKDF() {
				t.Fatalf("strengthened %s to %s, want KDBX 4 with %s", before, got, tc.want)
			}
			if err := db.Save(); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(path, "synthetic")
			if err != nil {
				t.Fatal(err)
			}
			if got := reopened.KDF(); got != tc.want {
				t.Errorf("file holds %s, want %s", got, tc.want)
			}
			entry, err := reopened.GetEntry(id)
			if err != nil || entry.Password != "synthetic-secret" {
				t.Fatalf("entry after strengthening: %+v, %v", entry, err)
			}
			if data := attachmentData(t, reopened, id, "recovery.txt"); string(data) != "synthetic attachment" {
				t.Errorf("attachment after strengthening: %q", data)
			}

			again, err := reopened.StrengthenKDF()
			if err != nil || again || reopened.KDF() != tc.want {
				t.Errorf("strengthening again: changed %t, %v, now %s", again, err, reopened.KDF())
			}
		})
	}
}

func TestKDFDescription(t *testing.T) {
	kdbx4 := func(p *gokeepasslib.KdfParameters) *gokeepasslib.DBHeader {
		h := gokeepasslib.NewKDBX40Header()
		h.FileHeaders.KdfParameters = p
		return h
	}
	kdbx31 := gokeepasslib.NewKDBX3Header()
	kdbx31.FileHeaders.TransformRounds = 60000
	for _, tc := range []struct {
		header *gokeepasslib.DBHeader
		want   KDF
		text   string
	}{
		{kdbx31, KDF{Name: "AES-KDF", Rounds: 60000}, "AES-KDF, 60000 rounds"},
		{kdbx4(&gokeepasslib.KdfParameters{UUID: gokeepasslib.KdfAES3, Rounds: 1234}),
			KDF{Name: "AES-KDF", Rounds: 1234}, "AES-KDF, 1234 rounds"},
		{kdbx4(&gokeepasslib.KdfParameters{UUID: gokeepasslib.KdfAES4, Rounds: 99}),
			KDF{Name: "AES-KDF", Rounds: 99}, "AES-KDF, 99 rounds"},
		{kdbx4(&gokeepasslib.KdfParameters{UUID: gokeepasslib.KdfArgon2, Memory: 64 << 20, Iterations: 20, Parallelism: 2}),
			KDF{Name: "Argon2d", Memory: 64 << 20, Iterations: 20, Parallelism: 2}, "Argon2d, 64 MiB, 20 iterations, parallelism 2"},
		{kdbx4(&gokeepasslib.KdfParameters{UUID: argon2idUUID, Memory: 1536 << 10, Iterations: 3, Parallelism: 4}),
			KDF{Name: "Argon2id", Memory: 1536 << 10, Iterations: 3, Parallelism: 4}, "Argon2id, 1536 KiB, 3 iterations, parallelism 4"},
		{kdbx4(&gokeepasslib.KdfParameters{UUID: gokeepasslib.KdfArgon2, Memory: 1000, Iterations: 1, Parallelism: 1}),
			KDF{Name: "Argon2d", Memory: 1000, Iterations: 1, Parallelism: 1}, "Argon2d, 1000 bytes, 1 iterations, parallelism 1"},
		{kdbx4(&gokeepasslib.KdfParameters{UUID: []byte{1, 2, 3}}),
			KDF{Name: "unknown (UUID 010203)"}, "unknown (UUID 010203)"},
		{kdbx4(nil), KDF{}, "no key derivation parameters"},
	} {
		got := kdfOf(tc.header)
		if got != tc.want || got.String() != tc.text {
			t.Errorf("got %+v (%q), want %+v (%q)", got, got, tc.want, tc.text)
		}
	}
}

func TestKDFLimits(t *testing.T) {
	argon2d := func(memory, iterations uint64, parallelism uint32) KDF {
		return KDF{Name: kdfArgon2d, Memory: memory, Iterations: iterations, Parallelism: parallelism}
	}
	for _, tc := range []struct {
		kdf KDF
		ok  bool
	}{
		// KeePass clients' defaults, and their benchmarks of a few seconds.
		{KDF{Name: kdfAES, Rounds: 6000}, true},
		{KDF{Name: kdfAES, Rounds: 300_000_000}, true},
		{productionKDF, true},
		{argon2d(64<<20, 1000, 8), true},
		{argon2d(1<<30, 100, 4), true},
		// The limits.
		{KDF{Name: kdfAES, Rounds: 1 << 32}, true},
		{argon2d(2<<30, 64, 1), true},
		{argon2d(64<<20, 2048, 255), true},
		// gokeepasslib refuses these itself, before any work.
		{argon2d(64<<20, 0, 2), true},
		{argon2d(64<<20, 2, 0), true},
		{KDF{}, true},
		// Beyond the limits.
		{KDF{Name: kdfAES, Rounds: 1<<32 + 1}, false},
		{KDF{Name: kdfAES, Rounds: 1 << 62}, false},
		{argon2d(2<<30+1<<10, 1, 1), false},
		{argon2d(4<<30-1<<10, 1, 1), false},
		{argon2d(1<<62, 1, 1), false},
		{argon2d(64<<20, 2049, 2), false},
		{argon2d(2<<30, 65, 2), false},
		{argon2d(0, 1<<32-1, 1), false}, // Argon2 takes 8 KiB per lane at least
		{argon2d(64<<20, 1<<40, 2), false},
		{argon2d(64<<20, 2, 256), false},
		{argon2d(64<<20, 2, 1<<24), false},
	} {
		err := tc.kdf.checkLimits()
		if (err == nil) != tc.ok || err != nil && !errors.Is(err, errKDFLimit) {
			t.Errorf("%s: %v", tc.kdf, err)
		}
	}
	// gokeepasslib computes neither: it would derive a wrong key.
	for _, k := range []KDF{{Name: kdfArgon2id, Memory: 64 << 20, Iterations: 2, Parallelism: 2}, {Name: "unknown (UUID 010203)"}} {
		err := k.checkLimits()
		if err == nil || errors.Is(err, errKDFLimit) || !strings.Contains(err.Error(), "unsupported key derivation: "+k.Name) {
			t.Errorf("%s: %v", k, err)
		}
	}
}

// patchRounds rewrites the AES-KDF rounds in a KDBX 3.1 file's header, as
// whoever can write the file but does not know its key can: nothing
// authenticates the header before the key is derived with it.
func patchRounds(t *testing.T, data []byte, rounds uint64) []byte {
	t.Helper()
	out := bytes.Clone(data)
	for off := 12; off+3 <= len(out) && out[off] != 0; {
		size := int(binary.LittleEndian.Uint16(out[off+1:]))
		if out[off] == 6 { // TransformRounds
			binary.LittleEndian.PutUint64(out[off+3:], rounds)
			return out
		}
		off += 3 + size
	}
	t.Fatal("no TransformRounds header field")
	return nil
}

// patchKDFParameter rewrites a KDF parameter ($UUID, M, I or P) in a KDBX 4
// file's header with a value of the same length, and the header's SHA-256
// after it, which is not keyed. The HMAC after that is keyed, but checked
// only after the key is derived.
func patchKDFParameter(t *testing.T, data []byte, name string, value []byte) []byte {
	t.Helper()
	out := bytes.Clone(data)
	patched := false
	off := 12
	for {
		id, size := out[off], int(binary.LittleEndian.Uint32(out[off+1:]))
		if id == 11 { // KdfParameters: a variant dictionary
			dict := out[off+5 : off+5+size]
			for item := 2; dict[item] != 0; {
				nameEnd := item + 5 + int(binary.LittleEndian.Uint32(dict[item+1:]))
				valueSize := int(binary.LittleEndian.Uint32(dict[nameEnd:]))
				if string(dict[item+5:nameEnd]) == name && valueSize == len(value) {
					copy(dict[nameEnd+4:], value)
					patched = true
				}
				item = nameEnd + 4 + valueSize
			}
		}
		off += 5 + size
		if id == 0 {
			break
		}
	}
	if !patched {
		t.Fatalf("no %d-byte KDF parameter %q", len(value), name)
	}
	sum := sha256.Sum256(out[:off])
	copy(out[off:], sum[:])
	return out
}

// promptly returns fn's error, failing the test if fn still runs after a
// few seconds: it would be deriving a key that takes years.
func promptly(t *testing.T, fn func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("still deriving a key after 10 s")
		return nil
	}
}

// A file whose header asks for an absurd key derivation is refused before
// any key is derived, whatever the credentials: on opening it, and on
// merging it as the file under an open database.
func TestHostileKDFRefusedBeforeDerivingAKey(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "vault.key")
	if err := os.WriteFile(keyPath, []byte("synthetic key file"), 0600); err != nil {
		t.Fatal(err)
	}
	read := func(path string) []byte {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	v31 := read(createKDBX31(t, filepath.Join(dir, "v31.kdbx"), "synthetic", keyPath).Path)
	db4, err := CreateWithKeyFile(filepath.Join(dir, "v4.kdbx"), "synthetic", keyPath)
	if err != nil {
		t.Fatal(err)
	}
	v4 := read(db4.Path)
	uint64LE := func(v uint64) []byte { return binary.LittleEndian.AppendUint64(nil, v) }
	for _, tc := range []struct {
		name string
		data []byte
		want string // in the error
	}{
		{"2^62 AES-KDF rounds", patchRounds(t, v31, 1<<62), "4611686018427387904 AES-KDF rounds"},
		{"4 GiB of Argon2 memory", patchKDFParameter(t, v4, "M", uint64LE(4<<30-1<<10)), "4194303 KiB of Argon2 memory"},
		{"2^31 Argon2 passes", patchKDFParameter(t, v4, "I", uint64LE(1<<31)), "2147483648 Argon2 passes"},
		// gokeepasslib would compute one lane: a wrong key.
		{"257 Argon2 lanes", patchKDFParameter(t, v4, "P", binary.LittleEndian.AppendUint32(nil, 257)), "257 Argon2 lanes"},
		{"Argon2id", patchKDFParameter(t, v4, "$UUID", argon2idUUID), "unsupported key derivation: Argon2id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "vault.kdbx")
			db, err := CreateWithKeyFile(path, "synthetic", keyPath)
			if err != nil {
				t.Fatal(err)
			}
			// Whoever can write the file replaces it under the open database.
			if err := os.WriteFile(path, tc.data, 0600); err != nil {
				t.Fatal(err)
			}
			for name, fn := range map[string]func() error{
				"Open":            func() error { _, err := Open(path, "wrong password"); return err },
				"OpenWithKeyFile": func() error { _, err := OpenWithKeyFile(path, "synthetic", keyPath); return err },
				"MergeDisk":       func() error { _, err := db.MergeDisk(); return err },
			} {
				if err := promptly(t, fn); err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Errorf("%s: %v, want an error naming %q", name, err, tc.want)
				}
			}
		})
	}
}
