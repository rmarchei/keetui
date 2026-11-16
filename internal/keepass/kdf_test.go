package keepass

import (
	"os"
	"path/filepath"
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
			KDF{Name: "unknown key derivation 010203"}, "unknown key derivation 010203"},
		{kdbx4(nil), KDF{}, "no key derivation parameters"},
	} {
		got := kdfOf(tc.header)
		if got != tc.want || got.String() != tc.text {
			t.Errorf("got %+v (%q), want %+v (%q)", got, got, tc.want, tc.text)
		}
	}
}
