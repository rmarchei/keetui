package keepass

import (
	"os"
	"testing"

	"github.com/tobischo/gokeepasslib/v3"
)

// TestMain gives the databases tests create a minimal key derivation cost:
// at the production cost every create, save and open would take most of a
// second. TestProductionKDF checks the production cost itself.
func TestMain(m *testing.M) {
	UseTestKDF()
	os.Exit(m.Run())
}

// setTargetKDF makes k the target key derivation until the test ends.
func setTargetKDF(t *testing.T, k KDF) {
	t.Helper()
	previous := targetKDF
	targetKDF = k
	t.Cleanup(func() { targetKDF = previous })
}

// createKDBX31 creates a KDBX 3.1 database with AES-KDF, as keetui did before
// it created KDBX 4 ones and other clients still may, protected by password
// and/or key file.
func createKDBX31(t *testing.T, path, password, keyFilePath string) *DB {
	t.Helper()
	db, err := create(path, password, keyFilePath, gokeepasslib.NewDatabase(gokeepasslib.WithDatabaseKDBXVersion3()))
	if err != nil {
		t.Fatalf("create KDBX 3.1 database: %v", err)
	}
	return db
}

// createFormat creates a password-protected database as Create does (KDBX
// 4), or a KDBX 3.1 one.
func createFormat(t *testing.T, path, password string, kdbx4 bool) *DB {
	t.Helper()
	if !kdbx4 {
		return createKDBX31(t, path, password, "")
	}
	db, err := Create(path, password)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return db
}
