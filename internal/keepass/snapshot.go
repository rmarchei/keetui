package keepass

import (
	"bytes"
	"fmt"
	"maps"
	"slices"

	"github.com/tobischo/gokeepasslib/v3"
)

// EntrySnapshot is a self-contained copy of an entry, including the
// attachments of the entry and of its history entries.
//
// A raw copy cannot undo a deletion: attachment references are binary pool
// IDs, and every save drops the binaries nothing references any more and
// renumbers the rest, so after a save they point at another attachment or at
// nothing. A snapshot keeps copies of the referenced binaries instead and
// puts them back in the pool when inserted, however many saves happened in
// between.
type EntrySnapshot struct {
	entry    gokeepasslib.Entry
	binaries snapshotBinaries
}

// GroupSnapshot is the EntrySnapshot of a group: a self-contained copy of the
// group and its subtree, including the attachments of every entry and
// history entry in it.
type GroupSnapshot struct {
	group    gokeepasslib.Group
	binaries snapshotBinaries
}

// snapshotBinaries holds copies of the binaries a snapshot references, keyed
// by pool ID at snapshot time and stored as they were in the pool then.
type snapshotBinaries struct {
	kdbx4 bool
	byID  map[int]gokeepasslib.Binary
}

// SnapshotEntry returns a self-contained copy of the entry, which
// InsertEntrySnapshot can restore after the entry has been deleted.
func (d *DB) SnapshotEntry(entryUUID gokeepasslib.UUID) (*EntrySnapshot, error) {
	raw, err := d.GetRawEntry(entryUUID)
	if err != nil {
		return nil, err
	}
	s := &EntrySnapshot{entry: *raw, binaries: d.newSnapshotBinaries()}
	d.captureEntryBinaries(&s.entry, s.binaries.byID)
	return s, nil
}

// SnapshotGroup returns a self-contained copy of the group, which
// InsertGroupSnapshot can restore after the group has been deleted.
func (d *DB) SnapshotGroup(groupUUID gokeepasslib.UUID) (*GroupSnapshot, error) {
	raw, err := d.GetRawGroup(groupUUID)
	if err != nil {
		return nil, err
	}
	s := &GroupSnapshot{group: *raw, binaries: d.newSnapshotBinaries()}
	d.captureGroupBinaries(&s.group, s.binaries.byID)
	return s, nil
}

// InsertEntrySnapshot inserts a copy of the snapshot's entry (preserving its
// UUID) into the given group, putting its attachments back in the pool.
func (d *DB) InsertEntrySnapshot(groupUUID gokeepasslib.UUID, s *EntrySnapshot) error {
	if len(d.Database.Content.Root.Groups) == 0 {
		return fmt.Errorf("database has no root group")
	}
	g := findGroup(&d.Database.Content.Root.Groups[0], groupUUID)
	if g == nil {
		return fmt.Errorf("group not found")
	}
	entry := cloneEntry(s.entry)
	rebindEntryBinaries(&entry, d.repoolBinaries(s.binaries))
	g.Entries = append(g.Entries, entry)
	return nil
}

// ReplaceEntrySnapshot puts a copy of the snapshot's entry back in place of
// the entry with its UUID, wherever that is now, attachments and history
// included: the undo of a change that replaced the whole entry.
func (d *DB) ReplaceEntrySnapshot(s *EntrySnapshot) error {
	if len(d.Database.Content.Root.Groups) == 0 {
		return fmt.Errorf("database has no root group")
	}
	entry := findEntry(&d.Database.Content.Root.Groups[0], s.entry.UUID)
	if entry == nil {
		return fmt.Errorf("entry not found")
	}
	restored := cloneEntry(s.entry)
	rebindEntryBinaries(&restored, d.repoolBinaries(s.binaries))
	*entry = restored
	return nil
}

// InsertGroupSnapshot inserts a copy of the snapshot's group (preserving its
// UUID and subtree) into the given parent group, putting its attachments
// back in the pool.
func (d *DB) InsertGroupSnapshot(parentUUID gokeepasslib.UUID, s *GroupSnapshot) error {
	if len(d.Database.Content.Root.Groups) == 0 {
		return fmt.Errorf("database has no root group")
	}
	parent := findGroup(&d.Database.Content.Root.Groups[0], parentUUID)
	if parent == nil {
		return fmt.Errorf("parent group not found")
	}
	group := cloneGroup(s.group)
	rebindGroupBinaries(&group, d.repoolBinaries(s.binaries))
	parent.Groups = append(parent.Groups, group)
	return nil
}

func (d *DB) newSnapshotBinaries() snapshotBinaries {
	return snapshotBinaries{kdbx4: d.Database.Header.IsKdbx4(), byID: make(map[int]gokeepasslib.Binary)}
}

// captureEntryBinaries copies every binary referenced by e or its history
// entries, keyed by current pool ID. References whose binary is missing are
// left out, so inserting the snapshot drops them.
func (d *DB) captureEntryBinaries(e *gokeepasslib.Entry, binaries map[int]gokeepasslib.Binary) {
	for _, ref := range e.Binaries {
		if _, done := binaries[ref.Value.ID]; done {
			continue
		}
		if bin := d.Database.FindBinary(ref.Value.ID); bin != nil {
			binaries[ref.Value.ID] = copyBinary(*bin)
		}
	}
	for hi := range e.Histories {
		for ei := range e.Histories[hi].Entries {
			d.captureEntryBinaries(&e.Histories[hi].Entries[ei], binaries)
		}
	}
}

func (d *DB) captureGroupBinaries(g *gokeepasslib.Group, binaries map[int]gokeepasslib.Binary) {
	for i := range g.Entries {
		d.captureEntryBinaries(&g.Entries[i], binaries)
	}
	for i := range g.Groups {
		d.captureGroupBinaries(&g.Groups[i], binaries)
	}
}

// repoolBinaries puts snapshotted binaries back in the pool and maps each
// snapshot-time ID to a current one. They go back exactly as they were
// stored: re-adding their decoded contents would drop their protection, and
// the pinned gokeepasslib decodes KDBX 4 contents that look like base64 and
// truncates the gzip streams it writes for KDBX 3.1. A binary the pool still
// holds as stored (the deleted one itself before any save, or one another
// attachment shares) is reused instead of being stored twice.
func (d *DB) repoolBinaries(s snapshotBinaries) map[int]int {
	if s.kdbx4 != d.Database.Header.IsKdbx4() {
		return d.convertBinaries(s)
	}
	pool := d.binaryPool()
	ids := make(map[int]int, len(s.byID))
	for _, oldID := range slices.Sorted(maps.Keys(s.byID)) {
		b := s.byID[oldID]
		if id, ok := findStoredBinary(*pool, b, oldID); ok {
			ids[oldID] = id
			continue
		}
		b = copyBinary(b)
		b.ID = nextBinaryID(*pool)
		*pool = append(*pool, b)
		ids[oldID] = b.ID
	}
	return ids
}

// convertBinaries adds the contents of binaries snapshotted before the
// database changed format, i.e. before an upgrade from KDBX 3.1, to the pool
// the way ConvertToKDBX4 does. Contents that cannot be read are left out, so
// their references are dropped.
func (d *DB) convertBinaries(s snapshotBinaries) map[int]int {
	ids := make(map[int]int, len(s.byID))
	for _, oldID := range slices.Sorted(maps.Keys(s.byID)) {
		b := s.byID[oldID]
		data := b.Content // KDBX 4 stores contents raw
		if !s.kdbx4 {
			var err error
			if data, err = b.GetContentBytes(); err != nil {
				continue
			}
		}
		ids[oldID] = d.Database.AddBinary(data).ID
	}
	return ids
}

// findStoredBinary returns the ID of a pooled binary stored exactly like b,
// preferring the one that still has b's snapshot-time ID.
func findStoredBinary(pool gokeepasslib.Binaries, b gokeepasslib.Binary, id int) (int, bool) {
	if p := pool.Find(id); p != nil && sameStoredBinary(*p, b) {
		return id, true
	}
	for _, p := range pool {
		if sameStoredBinary(p, b) {
			return p.ID, true
		}
	}
	return 0, false
}

func sameStoredBinary(a, b gokeepasslib.Binary) bool {
	return a.MemoryProtection == b.MemoryProtection && a.Compressed == b.Compressed &&
		streamProtected(a) == streamProtected(b) && bytes.Equal(a.Content, b.Content)
}

// streamProtected reports whether a KDBX 3.1 binary is protected, i.e.
// encrypted with the inner stream when saved.
func streamProtected(b gokeepasslib.Binary) bool {
	return b.Protected != nil && b.Protected.Bool
}

// copyBinary returns a copy of b that shares no memory with it.
func copyBinary(b gokeepasslib.Binary) gokeepasslib.Binary {
	b.Content = bytes.Clone(b.Content)
	if b.Protected != nil {
		protected := *b.Protected
		b.Protected = &protected
	}
	return b
}

// nextBinaryID returns an ID no pooled binary has.
func nextBinaryID(pool gokeepasslib.Binaries) int {
	next := 0
	for _, b := range pool {
		next = max(next, b.ID+1)
	}
	return next
}

// binaryPool returns the binaries attachment references point into: the
// inner header in KDBX 4, the XML metadata in KDBX 3.1.
func (d *DB) binaryPool() *gokeepasslib.Binaries {
	if d.Database.Header.IsKdbx4() {
		return &d.Database.Content.InnerHeader.Binaries
	}
	return &d.Database.Content.Meta.Binaries
}

// rebindEntryBinaries rewrites the attachment references of e and its
// history entries through ids. Unlike remapEntryBinaryIDs it drops references
// without a mapping (their binary was already missing when snapshotted, or
// unreadable after a format change) so they can never alias an unrelated
// binary.
func rebindEntryBinaries(e *gokeepasslib.Entry, ids map[int]int) {
	kept := e.Binaries[:0]
	for _, ref := range e.Binaries {
		if id, ok := ids[ref.Value.ID]; ok {
			ref.Value.ID = id
			kept = append(kept, ref)
		}
	}
	e.Binaries = kept
	for hi := range e.Histories {
		for ei := range e.Histories[hi].Entries {
			rebindEntryBinaries(&e.Histories[hi].Entries[ei], ids)
		}
	}
}

func rebindGroupBinaries(g *gokeepasslib.Group, ids map[int]int) {
	for i := range g.Entries {
		rebindEntryBinaries(&g.Entries[i], ids)
	}
	for i := range g.Groups {
		rebindGroupBinaries(&g.Groups[i], ids)
	}
}
