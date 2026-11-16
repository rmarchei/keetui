package keepass

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// A failed save must leave both the last persisted vault and the current
// unlocked values usable. Retrying also exercises header randomization and
// protected-value masking after each failure point.
func TestSaveFailurePreservesVaultAndAllowsRetry(t *testing.T) {
	for _, kdbx4 := range []bool{false, true} {
		for _, failure := range []string{"missing parent", "backup symlink", "destination directory"} {
			format := "KDBX3"
			if kdbx4 {
				format = "KDBX4"
			}
			t.Run(format+"/"+failure, func(t *testing.T) {
				dir := t.TempDir()
				path := filepath.Join(dir, "vault.kdbx")
				db := createKDBX31(t, path, "synthetic-master", "")
				if kdbx4 {
					if err := db.ConvertToKDBX4(); err != nil {
						t.Fatal(err)
					}
				}
				id, err := db.AddEntry(db.RootGroup().UUID, Entry{Title: "Service", Password: "persisted-password"})
				if err != nil {
					t.Fatal(err)
				}
				if err := db.AddAttachment(id, "recovery.key", []byte("synthetic-attachment")); err != nil {
					t.Fatal(err)
				}
				if err := db.Save(); err != nil {
					t.Fatal(err)
				}
				persisted, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := db.UpdateEntry(id, Entry{Title: "Service", Password: "draft-password",
					CustomFields: []CustomField{{Key: "Recovery PIN", Value: "synthetic-pin", Protected: true}}}); err != nil {
					t.Fatal(err)
				}
				destination := path
				switch failure {
				case "missing parent":
					destination = filepath.Join(dir, "missing", "vault.kdbx")
				case "backup symlink":
					db.BackupOnSave = true
					if err := os.Symlink(path, path+".bak"); err != nil {
						t.Fatal(err)
					}
				case "destination directory":
					destination = filepath.Join(dir, "directory.kdbx")
					if err := os.Mkdir(destination, 0700); err != nil {
						t.Fatal(err)
					}
				}
				if err := db.SaveAs(destination); err == nil {
					t.Fatal("expected save failure")
				}
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(after, persisted) {
					t.Fatal("failed save changed the persisted vault")
				}
				leftovers, err := filepath.Glob(filepath.Join(dir, ".keetui-save-*.tmp"))
				if err != nil || len(leftovers) != 0 {
					t.Fatalf("failed save left temporary vaults: %v, %v", leftovers, err)
				}
				entry, err := db.GetEntry(id)
				if err != nil || entry.Password != "draft-password" || len(entry.CustomFields) != 1 ||
					entry.CustomFields[0].Value != "synthetic-pin" || !entry.CustomFields[0].Protected {
					t.Fatal("failed save damaged the unlocked draft secrets")
				}
				old, err := Open(path, "synthetic-master")
				if err != nil {
					t.Fatal(err)
				}
				if entry, err := old.GetEntry(id); err != nil || entry.Password != "persisted-password" {
					t.Fatal("last persisted vault no longer opens with its original values")
				}
				db.BackupOnSave = false
				if err := db.Save(); err != nil {
					t.Fatalf("retry save: %v", err)
				}
				reopened, err := Open(path, "synthetic-master")
				if err != nil {
					t.Fatal(err)
				}
				entry, err = reopened.GetEntry(id)
				if err != nil || entry.Password != "draft-password" || len(entry.CustomFields) != 1 ||
					entry.CustomFields[0].Value != "synthetic-pin" || !entry.CustomFields[0].Protected {
					t.Fatal("retried save did not preserve protected values")
				}
				out := filepath.Join(dir, "recovered.key")
				if err := reopened.ExtractAttachment(id, "recovery.key", out); err != nil {
					t.Fatal(err)
				}
				if data, err := os.ReadFile(out); err != nil || string(data) != "synthetic-attachment" {
					t.Fatal("retry lost the attachment")
				}
			})
		}
	}
}

func TestSavingSymlinkedVaultKeepsLinkAndUpdatesTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "vault.kdbx")
	link := filepath.Join(dir, "linked.kdbx")
	if _, err := Create(target, "synthetic-master"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(target), link); err != nil {
		t.Fatal(err)
	}
	db, err := Open(link, "synthetic-master")
	if err != nil {
		t.Fatal(err)
	}
	id, err := db.AddEntry(db.RootGroup().UUID, Entry{Title: "Updated through link", Password: "synthetic-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Save(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("saving replaced the symlink with an independent vault")
	}
	reopened, err := Open(target, "synthetic-master")
	if err != nil {
		t.Fatal(err)
	}
	if entry, err := reopened.GetEntry(id); err != nil || entry.Password != "synthetic-secret" {
		t.Fatal("the symlink target did not receive the saved entry")
	}
}
