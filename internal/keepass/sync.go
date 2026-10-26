package keepass

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/tobischo/gokeepasslib/v3"
	w "github.com/tobischo/gokeepasslib/v3/wrappers"
)

// SyncResult is what MergeDisk took from the database file.
type SyncResult struct {
	Added    int // entries added there
	Updated  int // entries changed there more recently than here
	Moved    int // entries and groups moved there more recently than here
	Restored int // entries deleted here but changed there since: an edit wins over a deletion
	Deleted  int // entries the file records as deleted there after their last change here
	Kept     int // entries missing there without such a record, kept here
	Groups   int // groups added
}

// MergeDisk merges the database file, as another program or a sync left it,
// into this database, which keeps its own changes. The file is read with the
// credentials it was last read or written with.
//
//   - Entries are matched by UUID. An entry added there is added here; one
//     changed on both sides keeps the more recent version, and the other
//     goes into its history (the two histories are joined).
//   - An entry or group moved there more recently than here (the recycle bin
//     included) moves here too.
//   - An entry deleted here stays deleted, unless it was changed there after
//     keetui last read the file.
//   - An entry deleted there goes here too when the file records the
//     deletion (KeePass clients keep DeletedObjects) after the entry's last
//     change here. One merely missing there is kept: an older copy of the
//     file (a sync conflict, a restored backup) must not delete entries.
//
// Afterwards a save no longer counts the file as changed (see CheckDisk).
func (d *DB) MergeDisk() (SyncResult, error) {
	data, err := os.ReadFile(d.Path)
	if err != nil {
		return SyncResult{}, fmt.Errorf("read file: %w", err)
	}
	creds := d.disk.creds
	if creds == nil {
		creds = d.Database.Credentials
	}
	theirs := gokeepasslib.NewDatabase()
	theirs.Credentials = creds
	if err := gokeepasslib.NewDecoder(bytes.NewReader(data)).Decode(theirs); err != nil {
		return SyncResult{}, fmt.Errorf("decode the file on disk (was its master password changed?): %w", err)
	}
	if err := theirs.UnlockProtectedEntries(); err != nil {
		return SyncResult{}, fmt.Errorf("unlock protected values: %w", err)
	}
	if detectCorruptedProtectedValues(theirs) {
		return SyncResult{}, fmt.Errorf("protected values in the file on disk look corrupted (incompatible writer?)")
	}
	if len(theirs.Content.Root.Groups) == 0 || len(d.Database.Content.Root.Groups) == 0 {
		return SyncResult{}, fmt.Errorf("database has no root group")
	}
	if err := d.adoptFormat(theirs); err != nil {
		return SyncResult{}, fmt.Errorf("adopt the format of the file on disk: %w", err)
	}
	s := syncer{d: d, theirs: &DB{Path: d.Path, Database: theirs}, base: d.disk, seen: make(map[gokeepasslib.UUID]bool)}
	s.run()
	d.disk = newDiskStamp(d.Path, sha256.Sum256(data), creds, theirs)
	return s.result, nil
}

// adoptFormat moves the database to the file's format when that is newer:
// KeePassXC upgrades a KDBX 3.1 file to 4.x when it saves it, and saving
// back in the older format would downgrade the file and fail on fields only
// the newer one holds (an entry's previous group, KDBX 4.1). The file's
// header comes along, cipher and key derivation included, as whoever
// upgraded the file chose them; saving draws its random fields anew.
func (d *DB) adoptFormat(theirs *gokeepasslib.Database) error {
	ours, file := d.Database.Header, theirs.Header
	if !file.IsKdbx4() || ours.IsKdbx4() && (ours.IsKdbx41() || !file.IsKdbx41()) {
		return nil
	}
	if !ours.IsKdbx4() {
		if err := d.ConvertToKDBX4(); err != nil {
			return err
		}
	}
	d.Database.Header = file
	return nil
}

// syncer merges theirs into d. It walks their tree and finds everything in
// d's by UUID again at each step, as inserting and moving reallocate the
// slices pointers would point into.
type syncer struct {
	d, theirs *DB
	base      diskStamp // the file as keetui last read or wrote it
	seen      map[gokeepasslib.UUID]bool
	result    SyncResult
}

func (s *syncer) root() *gokeepasslib.Group { return &s.d.Database.Content.Root.Groups[0] }

func (s *syncer) run() {
	rootUUID := s.root().UUID
	s.walk(&s.theirs.Database.Content.Root.Groups[0], func() gokeepasslib.UUID { return rootUUID }, true)
	s.applyDeletions()
	for id := range s.base.entries {
		if !s.seen[id] && findEntry(s.root(), id) != nil {
			s.result.Kept++
		}
	}
	s.adoptRecycleBin()
	s.joinDeletedObjects()
}

// walk merges their group tg, placed in the group parent resolves to. Their
// group's counterpart here is resolved lazily: one deleted here comes back
// only when something in it does.
func (s *syncer) walk(tg *gokeepasslib.Group, parent func() gokeepasslib.UUID, isRoot bool) {
	var here *gokeepasslib.UUID
	ensure := func() gokeepasslib.UUID {
		if here == nil {
			id := s.placeGroup(tg, parent())
			here = &id
		}
		return *here
	}
	switch {
	case isRoot:
		id := s.root().UUID
		here = &id
	case findGroup(s.root(), tg.UUID) != nil:
		id := tg.UUID
		here = &id
		s.syncGroup(tg, parent)
	case !s.base.groups[tg.UUID]:
		ensure() // added there: kept even when empty
	}
	for i := range tg.Entries {
		s.syncEntry(&tg.Entries[i], ensure)
	}
	for i := range tg.Groups {
		s.walk(&tg.Groups[i], ensure, false)
	}
}

// placeGroup adds their group tg, without its contents, to the parent here.
// A group of the same name there is taken for it: one created on both sides.
func (s *syncer) placeGroup(tg *gokeepasslib.Group, parentUUID gokeepasslib.UUID) gokeepasslib.UUID {
	parent := findGroup(s.root(), parentUUID)
	if parent == nil {
		return parentUUID
	}
	for _, g := range parent.Groups {
		if g.Name == tg.Name {
			return g.UUID
		}
	}
	g := *tg
	g.Entries, g.Groups = nil, nil
	g.CustomData = slices.Clone(tg.CustomData)
	parent.Groups = append(parent.Groups, g)
	s.result.Groups++
	return g.UUID
}

// syncGroup takes a group's name and place from there when they changed
// there more recently.
func (s *syncer) syncGroup(tg *gokeepasslib.Group, parent func() gokeepasslib.UUID) {
	ours := findGroup(s.root(), tg.UUID)
	if timeOf(tg.Times.LastModificationTime).After(timeOf(ours.Times.LastModificationTime)) {
		ours.Name, ours.Notes, ours.IconID, ours.CustomIconUUID = tg.Name, tg.Notes, tg.IconID, tg.CustomIconUUID
		ours.Times.LastModificationTime = tg.Times.LastModificationTime
	}
	if !timeOf(tg.Times.LocationChanged).After(timeOf(ours.Times.LocationChanged)) {
		return
	}
	target := parent()
	if p := findGroupParent(s.root(), tg.UUID); p == nil || p.UUID.Compare(target) {
		return
	}
	if s.d.MoveGroup(tg.UUID, target) == nil {
		g := findGroup(s.root(), tg.UUID)
		g.Times.LocationChanged, g.PreviousParentGroup = tg.Times.LocationChanged, tg.PreviousParentGroup
		s.result.Moved++
	}
}

// syncEntry merges their entry te, which is in the group parent resolves to.
func (s *syncer) syncEntry(te *gokeepasslib.Entry, parent func() gokeepasslib.UUID) {
	s.seen[te.UUID] = true
	ours := findEntry(s.root(), te.UUID)
	theirMod := timeOf(te.Times.LastModificationTime)
	if ours == nil {
		if baseMod, deletedHere := s.base.entries[te.UUID]; deletedHere {
			if !theirMod.After(baseMod) {
				return
			}
			s.result.Restored++
		} else {
			s.result.Added++
		}
		if s.d.InsertRawEntry(parent(), s.importEntry(*te)) == nil {
			s.d.forgetDeleted(te.UUID)
		}
		return
	}

	ourMod, ourPlace := timeOf(ours.Times.LastModificationTime), ours.Times.LocationChanged
	baseMod, known := s.base.entries[te.UUID]
	switch {
	case theirMod.After(ourMod):
		mine := cloneEntry(*ours)
		merged := s.importEntry(*te)
		s.joinHistory(&merged, history(merged), history(mine), withoutHistory(mine))
		merged.Times.LocationChanged = ourPlace // the place is merged below
		*ours = merged
		s.result.Updated++
	case ourMod.After(theirMod) && (!known || !theirMod.Equal(baseMod)):
		// Changed on both sides, more recently here: their version goes
		// into the history.
		imported := s.importEntry(*te)
		s.joinHistory(ours, history(*ours), history(imported), withoutHistory(imported))
	}

	if !timeOf(te.Times.LocationChanged).After(timeOf(ourPlace)) {
		return
	}
	target := parent()
	if p, _ := findEntryParent(s.root(), te.UUID); p == nil || p.UUID.Compare(target) {
		return
	}
	if s.d.MoveEntry(te.UUID, target) == nil {
		e := findEntry(s.root(), te.UUID)
		e.Times.LocationChanged, e.PreviousParentGroup = te.Times.LocationChanged, te.PreviousParentGroup
		s.result.Moved++
	}
}

// importEntry copies their entry, history included, putting its
// attachments in this database's pool as they were stored there (see
// repoolBinaries).
func (s *syncer) importEntry(e gokeepasslib.Entry) gokeepasslib.Entry {
	c := cloneEntry(e)
	binaries := s.theirs.newSnapshotBinaries()
	s.theirs.captureEntryBinaries(&c, binaries.byID)
	rebindEntryBinaries(&c, s.d.repoolBinaries(binaries))
	return c
}

func history(e gokeepasslib.Entry) []gokeepasslib.Entry {
	var versions []gokeepasslib.Entry
	for _, h := range e.Histories {
		versions = append(versions, h.Entries...)
	}
	return versions
}

func withoutHistory(e gokeepasslib.Entry) []gokeepasslib.Entry {
	e.Histories = nil
	return []gokeepasslib.Entry{e}
}

// joinHistory sets e's history to the given versions, one per modification
// time and none at e's own, oldest first, as many as the database keeps.
func (s *syncer) joinHistory(e *gokeepasslib.Entry, lists ...[]gokeepasslib.Entry) {
	maxItems := int(s.d.Database.Content.Meta.HistoryMaxItems)
	if maxItems == 0 {
		e.Histories = nil
		return
	}
	at := func(v gokeepasslib.Entry) time.Time { return timeOf(v.Times.LastModificationTime) }
	current := at(*e)
	var versions []gokeepasslib.Entry
	for _, list := range lists {
		for _, v := range list {
			t := at(v)
			if t.Equal(current) || slices.ContainsFunc(versions, func(o gokeepasslib.Entry) bool { return at(o).Equal(t) }) {
				continue
			}
			versions = append(versions, v)
		}
	}
	slices.SortStableFunc(versions, func(a, b gokeepasslib.Entry) int { return at(a).Compare(at(b)) })
	if maxItems > 0 && len(versions) > maxItems {
		versions = versions[len(versions)-maxItems:]
	}
	e.Histories = nil
	if len(versions) > 0 {
		e.Histories = []gokeepasslib.History{{Entries: versions}}
	}
}

// applyDeletions removes what the file records as deleted there after its
// last change here: entries, then groups left empty (the recycle bin aside).
func (s *syncer) applyDeletions() {
	deleted := make(map[gokeepasslib.UUID]time.Time)
	for _, o := range s.theirs.Database.Content.Root.DeletedObjects {
		deleted[o.UUID] = timeOf(o.DeletionTime)
	}
	if len(deleted) == 0 {
		return
	}
	after := func(id gokeepasslib.UUID, times gokeepasslib.TimeData) bool {
		at, ok := deleted[id]
		return ok && at.After(timeOf(times.LastModificationTime)) && at.After(timeOf(times.LocationChanged))
	}
	var entries, groups []gokeepasslib.UUID
	var walk func(g *gokeepasslib.Group)
	walk = func(g *gokeepasslib.Group) {
		for i := range g.Entries {
			if e := &g.Entries[i]; !s.seen[e.UUID] && after(e.UUID, e.Times) {
				entries = append(entries, e.UUID)
			}
		}
		for i := range g.Groups {
			walk(&g.Groups[i])
			if sg := &g.Groups[i]; after(sg.UUID, sg.Times) && !s.d.IsRecycleBin(sg.UUID) &&
				findGroup(&s.theirs.Database.Content.Root.Groups[0], sg.UUID) == nil {
				groups = append(groups, sg.UUID) // deepest first
			}
		}
	}
	walk(s.root())
	for _, id := range entries {
		if removeEntry(s.root(), id) {
			s.result.Deleted++
		}
	}
	for _, id := range groups {
		if g := findGroup(s.root(), id); g != nil && len(g.Entries) == 0 && len(g.Groups) == 0 {
			removeGroup(s.root(), id)
		}
	}
}

// adoptRecycleBin takes their recycle bin when there was none here and the
// sync brought theirs.
func (s *syncer) adoptRecycleBin() {
	ours, theirs := s.d.Database.Content.Meta, s.theirs.Database.Content.Meta
	if !theirs.RecycleBinEnabled.Bool || (ours.RecycleBinEnabled.Bool && findGroup(s.root(), ours.RecycleBinUUID) != nil) {
		return
	}
	if findGroup(s.root(), theirs.RecycleBinUUID) != nil {
		ours.RecycleBinEnabled, ours.RecycleBinUUID, ours.RecycleBinChanged = theirs.RecycleBinEnabled, theirs.RecycleBinUUID, theirs.RecycleBinChanged
	}
}

// joinDeletedObjects keeps their record of deletions, which other KeePass
// clients use when they sync.
func (s *syncer) joinDeletedObjects() {
	ours := &s.d.Database.Content.Root.DeletedObjects
	for _, o := range s.theirs.Database.Content.Root.DeletedObjects {
		if !slices.ContainsFunc(*ours, func(x gokeepasslib.DeletedObjectData) bool { return x.UUID.Compare(o.UUID) }) {
			*ours = append(*ours, o)
		}
	}
}

// recordDeleted notes that g's entries and groups, and g itself unless it
// stays, were deleted for good, as KeePass does: syncs elsewhere then
// delete them too instead of bringing them back (see applyDeletions).
func (d *DB) recordDeleted(g *gokeepasslib.Group, stays bool) {
	now := w.Now()
	record := func(id gokeepasslib.UUID) {
		d.Database.Content.Root.DeletedObjects = append(d.Database.Content.Root.DeletedObjects,
			gokeepasslib.DeletedObjectData{UUID: id, DeletionTime: &now})
	}
	if !stays {
		record(g.UUID)
	}
	for i := range g.Entries {
		record(g.Entries[i].UUID)
	}
	for i := range g.Groups {
		d.recordDeleted(&g.Groups[i], false)
	}
}

// recordDeletedEntry notes one entry deleted for good.
func (d *DB) recordDeletedEntry(id gokeepasslib.UUID) {
	d.recordDeleted(&gokeepasslib.Group{Entries: []gokeepasslib.Entry{{UUID: id}}}, true)
}

// forgetDeleted drops the deletion records of an entry or group put back
// (an undo), with everything in the group.
func (d *DB) forgetDeleted(ids ...gokeepasslib.UUID) {
	back := make(map[gokeepasslib.UUID]bool, len(ids))
	for _, id := range ids {
		back[id] = true
	}
	objects := &d.Database.Content.Root.DeletedObjects
	*objects = slices.DeleteFunc(*objects, func(o gokeepasslib.DeletedObjectData) bool { return back[o.UUID] })
}

// groupIDs lists the UUIDs of g, its entries and everything below.
func groupIDs(g *gokeepasslib.Group) []gokeepasslib.UUID {
	ids := []gokeepasslib.UUID{g.UUID}
	for i := range g.Entries {
		ids = append(ids, g.Entries[i].UUID)
	}
	for i := range g.Groups {
		ids = append(ids, groupIDs(&g.Groups[i])...)
	}
	return ids
}
