package keepass

import (
	"testing"

	"github.com/tobischo/gokeepasslib/v3"
	w "github.com/tobischo/gokeepasslib/v3/wrappers"
)

func corruptionTestValue(key, content string, protected bool) gokeepasslib.ValueData {
	return gokeepasslib.ValueData{
		Key: key,
		Value: gokeepasslib.V{
			Content:   content,
			Protected: w.NewBoolWrapper(protected),
		},
	}
}

func corruptionTestDB(entries ...gokeepasslib.Entry) *gokeepasslib.Database {
	db := gokeepasslib.NewDatabase()
	root := gokeepasslib.NewGroup()
	root.Name = "Root"
	root.Entries = entries
	db.Content.Root.Groups = []gokeepasslib.Group{root}
	return db
}

func entryWithPassword(pw string) gokeepasslib.Entry {
	e := gokeepasslib.NewEntry()
	e.Values = append(e.Values, corruptionTestValue("Password", pw, true))
	return e
}

func TestDetectCorruptedProtectedValues(t *testing.T) {
	garbage1 := "VId\x86J\x92\x9b\x8d6Rg\xc5A;"
	garbage2 := "\x9ejz\xab\xce+\xf2"

	t.Run("clean database", func(t *testing.T) {
		db := corruptionTestDB(entryWithPassword("hello"), entryWithPassword("wörld 🔑"))
		if detectCorruptedProtectedValues(db) {
			t.Error("expected clean database not to be flagged")
		}
	})

	t.Run("single invalid value below threshold", func(t *testing.T) {
		db := corruptionTestDB(entryWithPassword(garbage1), entryWithPassword("ok"))
		if detectCorruptedProtectedValues(db) {
			t.Error("expected a single invalid value not to be flagged")
		}
	})

	t.Run("two invalid values flagged", func(t *testing.T) {
		db := corruptionTestDB(entryWithPassword(garbage1), entryWithPassword(garbage2))
		if !detectCorruptedProtectedValues(db) {
			t.Error("expected two invalid protected values to be flagged")
		}
	})

	t.Run("empty and unprotected values ignored", func(t *testing.T) {
		e1 := gokeepasslib.NewEntry()
		e1.Values = append(e1.Values,
			corruptionTestValue("Password", "", true),
			corruptionTestValue("Notes", garbage1, false),
		)
		e2 := gokeepasslib.NewEntry()
		e2.Values = append(e2.Values,
			corruptionTestValue("Password", "", true),
			corruptionTestValue("Notes", garbage2, false),
		)
		db := corruptionTestDB(e1, e2)
		if detectCorruptedProtectedValues(db) {
			t.Error("expected empty protected and invalid unprotected values not to be flagged")
		}
	})

	t.Run("corrupted history values flagged", func(t *testing.T) {
		e := entryWithPassword("current-ok")
		e.Histories = []gokeepasslib.History{{
			Entries: []gokeepasslib.Entry{entryWithPassword(garbage1), entryWithPassword(garbage2)},
		}}
		db := corruptionTestDB(e)
		if !detectCorruptedProtectedValues(db) {
			t.Error("expected corrupted history values to be flagged")
		}
	})

	t.Run("nested group corruption flagged", func(t *testing.T) {
		sub := gokeepasslib.NewGroup()
		sub.Entries = []gokeepasslib.Entry{entryWithPassword(garbage1), entryWithPassword(garbage2)}
		db := corruptionTestDB()
		db.Content.Root.Groups[0].Groups = []gokeepasslib.Group{sub}
		if !detectCorruptedProtectedValues(db) {
			t.Error("expected corruption in nested group to be flagged")
		}
	})
}
