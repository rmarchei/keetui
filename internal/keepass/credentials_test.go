package keepass

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckCredentials(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "synthetic.key")
	other := filepath.Join(dir, "other.key")
	for path, content := range map[string]string{key: "synthetic key file", other: "another key file"} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	type try struct {
		password, key string
		ok            bool
	}
	for _, tc := range []struct {
		name          string
		password, key string // what the database is created with
		tries         []try
	}{
		{"password", "synthetic", "", []try{
			{"synthetic", "", true}, {"wrong", "", false}, {"", "", false}, {"synthetic", key, false}}},
		{"password and key file", "synthetic", key, []try{
			{"synthetic", key, true}, {"synthetic", "", false}, {"synthetic", other, false}, {"", key, false}}},
		{"key file only", "", key, []try{
			{"", key, true}, {"synthetic", key, false}, {"", other, false}}},
	} {
		db, err := CreateWithKeyFile(filepath.Join(t.TempDir(), "synthetic.kdbx"), tc.password, tc.key)
		if err != nil {
			t.Fatal(err)
		}
		for _, try := range tc.tries {
			err := db.CheckCredentials(try.password, try.key)
			if try.ok && err != nil || !try.ok && !errors.Is(err, ErrWrongCredentials) {
				t.Errorf("%s: password %q, key %q: %v", tc.name, try.password, try.key, err)
			}
		}
	}
}

// The check is of the credentials in memory: after a change of the master
// password, the new one, even before the database is saved, and with its
// file gone.
func TestCheckCredentialsAfterChangeAndWithoutTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic.kdbx")
	db, err := Create(path, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ChangePassword("changed"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := db.CheckCredentials("changed", ""); err != nil {
		t.Errorf("new password: %v", err)
	}
	if err := db.CheckCredentials("synthetic", ""); !errors.Is(err, ErrWrongCredentials) {
		t.Errorf("old password: %v", err)
	}
	missing := filepath.Join(t.TempDir(), "missing.key")
	if err := db.CheckCredentials("changed", missing); err == nil || errors.Is(err, ErrWrongCredentials) {
		t.Errorf("missing key file: %v", err)
	}
}
