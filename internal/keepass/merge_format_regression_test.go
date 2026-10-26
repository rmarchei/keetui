package keepass

import (
	"fmt"
	"reflect"
	"testing"
)

// Merging moves attachment contents between two independent binary pools,
// whose IDs may collide and whose formats may differ. Current and historical
// attachments must continue resolving after either database is saved or edited.
func TestMergeAcrossFormatsKeepsIndependentHistoryAttachments(t *testing.T) {
	for _, source4 := range []bool{false, true} {
		for _, target4 := range []bool{false, true} {
			t.Run(fmt.Sprintf("source4=%t/target4=%t", source4, target4), func(t *testing.T) {
				must := func(err error) {
					t.Helper()
					if err != nil {
						t.Fatal(err)
					}
				}
				source := newFormatDB(t, source4)
				target := newFormatDB(t, target4)
				distractor, err := target.AddEntry(target.RootGroup().UUID, Entry{Title: "Unrelated", Password: "unrelated secret"})
				must(err)
				must(target.AddAttachment(distractor, "current.bin", []byte("unrelated target attachment")))
				id, err := source.AddEntry(source.RootGroup().UUID, Entry{Title: "Imported", Password: "historical secret",
					CustomFields: []CustomField{{Key: "API token", Value: "historical token", Protected: true}}})
				must(err)
				must(source.AddAttachment(id, "history.bin", []byte("aGlzdG9yaWNhbCBieXRlcw==")))
				must(source.UpdateEntry(id, Entry{Title: "Imported", Password: "current secret",
					CustomFields: []CustomField{{Key: "API token", Value: "current token", Protected: true}}}))
				must(source.DeleteAttachment(id, "history.bin"))
				must(source.AddAttachment(id, "current.bin", []byte{0, 1, 2, 0xff, '\n'}))
				source = saveAndReopen(t, source)
				before := source.RootGroup()
				result, err := target.Merge(source)
				must(err)
				if result.EntriesAdded != 1 || result.EntriesUpdated != 0 || result.EntriesSkipped != 0 {
					t.Fatalf("merge counts = %+v, want one newly imported entry", result)
				}
				if !reflect.DeepEqual(source.RootGroup(), before) {
					t.Fatal("merging changed the source database")
				}
				target = saveAndReopen(t, target)
				// Mutating and saving the source must not modify the imported
				// entry or any of the target's independently pooled binaries.
				must(source.UpdateEntry(id, Entry{Title: "Changed source", Password: "later source secret"}))
				must(source.DeleteAttachment(id, "current.bin"))
				source = saveAndReopen(t, source)
				target = saveAndReopen(t, target)
				entry, err := target.GetEntry(id)
				must(err)
				if entry.Password != "current secret" || len(entry.CustomFields) != 1 ||
					entry.CustomFields[0].Value != "current token" || !entry.CustomFields[0].Protected {
					t.Fatal("editing the source changed the imported secrets or their protection")
				}
				if got := target.findBinaryByName(id, "current.bin"); !reflect.DeepEqual(got, []byte{0, 1, 2, 0xff, '\n'}) {
					t.Fatal("imported current attachment aliases an unrelated or deleted source binary")
				}
				if got := target.findBinaryByName(distractor, "current.bin"); string(got) != "unrelated target attachment" {
					t.Fatal("merge changed the pre-existing target attachment")
				}
				must(target.RestoreHistory(id, 0))
				target = saveAndReopen(t, target)
				entry, err = target.GetEntry(id)
				must(err)
				if entry.Password != "historical secret" || len(entry.CustomFields) != 1 ||
					entry.CustomFields[0].Value != "historical token" || !entry.CustomFields[0].Protected {
					t.Fatal("merged history lost the original protected fields")
				}
				if got := target.findBinaryByName(id, "history.bin"); string(got) != "aGlzdG9yaWNhbCBieXRlcw==" {
					t.Fatal("merged history lost or decoded its base64-looking attachment")
				}
				if got := source.findBinaryByName(id, "history.bin"); len(got) != 0 {
					t.Fatal("restoring target history changed the source's current attachments")
				}
			})
		}
	}
}
