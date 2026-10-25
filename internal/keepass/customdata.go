package keepass

import (
	"slices"

	"github.com/tobischo/gokeepasslib/v3"
	w "github.com/tobischo/gokeepasslib/v3/wrappers"
)

// CustomData returns a database-level custom data item: the KDBX metadata
// in which applications keep their own per-database settings, encrypted and
// authenticated with the rest of the database. It returns "" when the item
// does not exist.
func (d *DB) CustomData(key string) string {
	for _, item := range d.Database.Content.Meta.CustomData {
		if item.Key == key {
			return item.Value
		}
	}
	return ""
}

// SetCustomData stores value under key in the database-level custom data,
// removing the item when value is empty, and reports whether anything
// changed. Items of other applications are kept as they are. An item's
// modification time, which only KDBX 4.1 files carry, is updated only when
// the item already has one, so older formats stay writable.
func (d *DB) SetCustomData(key, value string) bool {
	meta := d.Database.Content.Meta
	for i, item := range meta.CustomData {
		if item.Key != key {
			continue
		}
		switch {
		case value == "":
			meta.CustomData = slices.Delete(meta.CustomData, i, i+1)
		case item.Value == value:
			return false
		default:
			meta.CustomData[i].Value = value
			if item.LastModificationTime != nil {
				now := w.Now()
				now.Formatted = item.LastModificationTime.Formatted
				meta.CustomData[i].LastModificationTime = &now
			}
		}
		return true
	}
	if value == "" {
		return false
	}
	meta.CustomData = append(meta.CustomData, gokeepasslib.CustomData{Key: key, Value: value})
	return true
}
