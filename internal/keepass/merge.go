package keepass

import (
	"fmt"
	"time"
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
			_, _ = d.AddEntry(targetParentUUID, srcEntry)
			result.EntriesAdded++
		} else {
			var existingMod time.Time
			if existing.Times.LastModificationTime != nil {
				existingMod = existing.Times.LastModificationTime.Time
			}
			if srcEntry.ModificationTime.After(existingMod) {
				_ = d.UpdateEntry(srcEntry.UUID, srcEntry)
				result.EntriesUpdated++
			} else {
				result.EntriesSkipped++
			}
		}
	}

	for _, srcGroup := range src.Groups {
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
	_, _ = d.AddGroup(parentUUID, name)
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
