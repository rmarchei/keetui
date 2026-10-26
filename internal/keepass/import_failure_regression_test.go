package keepass

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type importReadFailure struct{ err error }

func (r importReadFailure) Read([]byte) (int, error) { return 0, r.err }

// A corrupt/truncated tail must not leave the earlier valid records or
// groups partially imported, regardless of whether input came from a file
// or a reader that failed while being read.
func TestFailedImportsDoNotPartiallyAddEntries(t *testing.T) {
	validCSV := "Group,Title,Password\nImported group,Valid entry,synthetic-secret\n"
	validXML := `<KeePassFile><Root><Group><Name>Imported group</Name><Entry><String><Key>Title</Key><Value>Valid entry</Value></String></Entry></Group></Root></KeePassFile>`
	readErr := errors.New("synthetic import read failure")
	for _, tc := range []struct {
		name     string
		contents string
		file     bool
		xml      bool
	}{
		{"CSV file with invalid tail", validCSV + `"unterminated`, true, false},
		{"XML file with invalid tail", strings.TrimSuffix(validXML, "</KeePassFile>"), true, true},
		{"CSV reader with failed tail", validCSV, false, false},
		{"XML reader with failed tail", validXML, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newFormatDB(t, false)
			if _, err := db.AddEntry(db.RootGroup().UUID, Entry{Title: "Existing", Password: "preserved secret"}); err != nil {
				t.Fatal(err)
			}
			before := db.RootGroup()
			var count int
			var err error
			if tc.file {
				path := filepath.Join(t.TempDir(), "malformed-input")
				if err := os.WriteFile(path, []byte(tc.contents), 0600); err != nil {
					t.Fatal(err)
				}
				if tc.xml {
					count, err = db.ImportXMLFile(path)
				} else {
					count, err = db.ImportCSVFile(path)
				}
			} else {
				reader := io.MultiReader(strings.NewReader(tc.contents), importReadFailure{readErr})
				if tc.xml {
					count, err = db.ImportXML(reader)
				} else {
					count, err = db.ImportCSV(reader)
				}
				if !errors.Is(err, readErr) {
					t.Fatalf("import did not preserve the input error: %v", err)
				}
			}
			if err == nil || count != 0 {
				t.Fatalf("failed import reported count=%d, error=%v", count, err)
			}
			if !reflect.DeepEqual(db.RootGroup(), before) {
				t.Fatal("failed import left partially imported entries or groups")
			}
		})
	}
}
