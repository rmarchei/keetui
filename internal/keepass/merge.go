package keepass

import (
	"fmt"
	"time"

	"github.com/tobischo/gokeepasslib/v3"
)

// MergeEntryAction describes what would happen to a single entry during merge.
type MergeEntryAction struct {
	Title     string
	UserName  string
	GroupPath string
	Action    string // "add", "update", "skip"
}

// MergeGroupAction describes what would happen to a single group during merge.
type MergeGroupAction struct {
	Name       string
	ParentPath string
	Action     string // "create", "exists"
}

// MergeResult holds the result of a database merge operation.
type MergeResult struct {
	EntriesAdded   int
	EntriesUpdated int
	EntriesSkipped int
	GroupsCreated  int
	EntryActions   []MergeEntryAction
	GroupActions   []MergeGroupAction
}

// Merge merges entries and groups from another database into this one.
// New groups are created as needed. Entries are matched by UUID; entries
// not present in the target are added as full copies (preserving UUID,
// timestamps, history, and attachments), and entries present in both are
// overwritten if the source has a newer modification time.
func (d *DB) Merge(other *DB) (MergeResult, error) {
	if len(d.Database.Content.Root.Groups) == 0 {
		return MergeResult{}, fmt.Errorf("target database has no root group")
	}
	if len(other.Database.Content.Root.Groups) == 0 {
		return MergeResult{}, fmt.Errorf("source database has no root group")
	}

	otherRoot := other.RootGroup()
	result := MergeResult{}

	d.mergeGroup(&otherRoot, other, d.RootGroup().UUID, &result)
	// Merged entries and groups refer to the source's custom icons.
	d.joinCustomIcons(other)

	return result, nil
}

func (d *DB) mergeGroup(src *Group, srcDB *DB, targetParentUUID [16]byte, result *MergeResult) {
	for _, srcEntry := range src.Entries {
		srcRaw := findEntry(&srcDB.Database.Content.Root.Groups[0], srcEntry.UUID)
		if srcRaw == nil {
			continue
		}
		existing := findEntry(&d.Database.Content.Root.Groups[0], srcEntry.UUID)
		if existing == nil {
			clone := cloneEntry(*srcRaw)
			d.importEntryBinaries(&clone, srcDB)
			if err := d.InsertRawEntry(targetParentUUID, clone); err == nil {
				result.EntriesAdded++
			}
		} else {
			var existingMod time.Time
			if existing.Times.LastModificationTime != nil {
				existingMod = existing.Times.LastModificationTime.Time
			}
			if srcEntry.ModificationTime.After(existingMod) {
				d.overwriteEntryFromSource(existing, srcRaw, srcDB)
				result.EntriesUpdated++
			} else {
				result.EntriesSkipped++
			}
		}
	}

	for _, srcGroup := range src.Groups {
		targetGroupUUID := d.findOrCreateGroup(targetParentUUID, srcGroup.Name, result)
		d.mergeGroup(&srcGroup, srcDB, targetGroupUUID, result)
	}
}

// overwriteEntryFromSource records the target entry's current state in its
// history (same policy as UpdateEntry), then replaces its content with a
// copy of the source entry — preserving the source's values, tags, icon,
// timestamps, and attachments while keeping the target's history chain.
func (d *DB) overwriteEntryFromSource(target, src *gokeepasslib.Entry, srcDB *DB) {
	maxItems := int(d.Database.Content.Meta.HistoryMaxItems)
	if maxItems != 0 {
		snap := cloneEntry(*target)
		snap.Histories = nil
		if len(target.Histories) == 0 {
			target.Histories = []gokeepasslib.History{{}}
		}
		target.Histories[0].Entries = append(target.Histories[0].Entries, snap)
		if maxItems > 0 && len(target.Histories[0].Entries) > maxItems {
			target.Histories[0].Entries = target.Histories[0].Entries[len(target.Histories[0].Entries)-maxItems:]
		}
	}

	clone := cloneEntry(*src)
	clone.Histories = nil
	d.importEntryBinaries(&clone, srcDB)

	histories := target.Histories
	*target = clone
	target.Histories = histories
}

// importEntryBinaries rewrites the entry's binary references (including
// those inside history snapshots) to point at binaries copied from the
// source database into this one. References that cannot be resolved in the
// source are dropped so they can never alias an unrelated target binary.
func (d *DB) importEntryBinaries(e *gokeepasslib.Entry, srcDB *DB) {
	kept := e.Binaries[:0]
	for _, ref := range e.Binaries {
		bin := srcDB.Database.FindBinary(ref.Value.ID)
		if bin == nil {
			continue
		}
		data, err := bin.GetContentBytes()
		if err != nil {
			continue
		}
		ref.Value.ID = d.Database.AddBinary(data).ID
		kept = append(kept, ref)
	}
	e.Binaries = kept
	for hi := range e.Histories {
		for ei := range e.Histories[hi].Entries {
			d.importEntryBinaries(&e.Histories[hi].Entries[ei], srcDB)
		}
	}
}

func (d *DB) findOrCreateGroup(parentUUID [16]byte, name string, result *MergeResult) [16]byte {
	uuid, created := d.findOrCreateChildGroup(parentUUID, name)
	if created {
		result.GroupsCreated++
	}
	return uuid
}

// findOrCreateChildGroup returns the UUID of the direct child group of
// parentUUID with the given name, creating it if it does not exist.
func (d *DB) findOrCreateChildGroup(parentUUID [16]byte, name string) ([16]byte, bool) {
	root := d.RootGroup()
	if parent := findGroupSimple(&root, parentUUID); parent != nil {
		for _, g := range parent.Groups {
			if g.Name == name {
				return g.UUID, false
			}
		}
	}
	newUUID, err := d.AddGroup(parentUUID, name)
	if err != nil {
		return parentUUID, false
	}
	return newUUID, true
}

// MergePreview reports what a merge would do without modifying the database.
func (d *DB) MergePreview(other *DB) (MergeResult, error) {
	if len(d.Database.Content.Root.Groups) == 0 {
		return MergeResult{}, fmt.Errorf("target database has no root group")
	}

	otherRoot := other.RootGroup()
	result := MergeResult{}
	rootName := d.RootGroup().Name
	if rootName == "" {
		rootName = "Root"
	}

	d.mergePreviewGroup(&otherRoot, d.RootGroup().UUID, rootName, &result)

	return result, nil
}

func (d *DB) mergePreviewGroup(src *Group, targetParentUUID [16]byte, parentPath string, result *MergeResult) {
	for _, srcEntry := range src.Entries {
		existing := findEntry(&d.Database.Content.Root.Groups[0], srcEntry.UUID)
		action := MergeEntryAction{
			Title:     srcEntry.Title,
			UserName:  srcEntry.UserName,
			GroupPath: parentPath,
		}
		if existing == nil {
			action.Action = "add"
			result.EntriesAdded++
		} else {
			var existingMod time.Time
			if existing.Times.LastModificationTime != nil {
				existingMod = existing.Times.LastModificationTime.Time
			}
			if srcEntry.ModificationTime.After(existingMod) {
				action.Action = "update"
				result.EntriesUpdated++
			} else {
				action.Action = "skip"
				result.EntriesSkipped++
			}
		}
		result.EntryActions = append(result.EntryActions, action)
	}

	for _, srcGroup := range src.Groups {
		groupAction := MergeGroupAction{
			Name:       srcGroup.Name,
			ParentPath: parentPath,
		}
		root := d.RootGroup()
		parent := findGroupSimple(&root, targetParentUUID)
		childPath := parentPath + " > " + srcGroup.Name
		if parent != nil {
			found := false
			for _, g := range parent.Groups {
				if g.Name == srcGroup.Name {
					groupAction.Action = "exists"
					result.GroupActions = append(result.GroupActions, groupAction)
					d.mergePreviewGroup(&srcGroup, g.UUID, childPath, result)
					found = true
					break
				}
			}
			if !found {
				groupAction.Action = "create"
				result.GroupsCreated++
				result.GroupActions = append(result.GroupActions, groupAction)
				// Group doesn't exist; all entries in subtree would be added
				mergePreviewAllAdd(&srcGroup, childPath, result)
			}
		}
	}
}

// mergePreviewAllAdd counts all entries in a source subtree as "add" actions.
func mergePreviewAllAdd(src *Group, path string, result *MergeResult) {
	for _, e := range src.Entries {
		result.EntryActions = append(result.EntryActions, MergeEntryAction{
			Title:     e.Title,
			UserName:  e.UserName,
			GroupPath: path,
			Action:    "add",
		})
		result.EntriesAdded++
	}
	for _, sg := range src.Groups {
		childPath := path + " > " + sg.Name
		result.GroupActions = append(result.GroupActions, MergeGroupAction{
			Name:       sg.Name,
			ParentPath: path,
			Action:     "create",
		})
		result.GroupsCreated++
		mergePreviewAllAdd(&sg, childPath, result)
	}
}
