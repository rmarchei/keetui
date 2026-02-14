package keepass

import (
	"fmt"
)

// MergeResult holds the result of a database merge operation.
type MergeResult struct {
	EntriesAdded  int
	GroupsCreated int
}

// Merge merges entries and groups from another database into this one.
// New groups are created as needed. Entries are matched by UUID;
// entries not present in the target are added, and entries present in
// both are updated if the source has a newer modification time.
func (d *DB) Merge(other *DB) (MergeResult, error) {
	if len(d.Database.Content.Root.Groups) == 0 {
		return MergeResult{}, fmt.Errorf("target database has no root group")
	}

	otherRoot := other.RootGroup()
	result := MergeResult{}

	d.mergeGroup(&otherRoot, d.RootGroup().UUID, &result)

	return result, nil
}

func (d *DB) mergeGroup(src *Group, targetParentUUID [16]byte, result *MergeResult) {
	for _, srcEntry := range src.Entries {
		existing := findEntry(&d.Database.Content.Root.Groups[0], srcEntry.UUID)
		if existing == nil {
			// Entry doesn't exist in target, add it
			_ = d.AddEntry(targetParentUUID, srcEntry)
			result.EntriesAdded++
		} else {
			// Entry exists, update if source is newer
			if srcEntry.ModificationTime.After(existing.Times.LastModificationTime.Time) {
				_ = d.UpdateEntry(srcEntry.UUID, srcEntry)
				result.EntriesAdded++ // count updates too
			}
		}
	}

	for _, srcGroup := range src.Groups {
		// Find or create the corresponding group in target
		targetGroupUUID := d.findOrCreateGroup(targetParentUUID, srcGroup.Name, result)
		d.mergeGroup(&srcGroup, targetGroupUUID, result)
	}
}

func (d *DB) findOrCreateGroup(parentUUID [16]byte, name string, result *MergeResult) [16]byte {
	root := d.RootGroup()
	parent := findGroupSimple(&root, parentUUID)
	if parent != nil {
		for _, g := range parent.Groups {
			if g.Name == name {
				return g.UUID
			}
		}
	}

	// Create the group
	_ = d.AddGroup(parentUUID, name)
	result.GroupsCreated++

	// Re-read to get the UUID
	root = d.RootGroup()
	parent = findGroupSimple(&root, parentUUID)
	if parent != nil {
		for _, g := range parent.Groups {
			if g.Name == name {
				return g.UUID
			}
		}
	}

	return parentUUID
}
