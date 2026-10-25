package main

import (
	"path/filepath"
	"testing"

	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/keepass"
)

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
