package keepass

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Save As to another path refuses a symbolic link there, dangling or not,
// which the save would follow wherever it points. A link to the database's
// own file is that file.
func TestSaveAsRefusesASymlinkElsewhere(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.kdbx")
	db, err := Create(path, "synthetic-master")
	if err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(dir, "victim.txt")
	if err := os.WriteFile(victim, []byte("synthetic victim"), 0600); err != nil {
		t.Fatal(err)
	}
	nowhere := filepath.Join(dir, "nowhere.kdbx")
	for target, link := range map[string]string{victim: "planted.kdbx", nowhere: "dangling.kdbx"} {
		link = filepath.Join(dir, link)
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if err := db.SaveAs(link); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Errorf("%s: %v", link, err)
		}
		if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s: the link was replaced", link)
		}
	}
	if data, err := os.ReadFile(victim); err != nil || string(data) != "synthetic victim" {
		t.Error("the save wrote the file the link points to")
	}
	if _, err := os.Lstat(nowhere); !os.IsNotExist(err) {
		t.Error("the save created the file a dangling link points to")
	}
	if db.Path != path {
		t.Errorf("a refused save moved the database to %s", db.Path)
	}

	own := filepath.Join(dir, "own.kdbx")
	if err := os.Symlink(path, own); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveAs(own); err != nil {
		t.Errorf("a link to the database's own file: %v", err)
	}
}
