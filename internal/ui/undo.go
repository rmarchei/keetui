package ui

import (
	"github.com/rmarchei/keetui/internal/keepass"
	"github.com/tobischo/gokeepasslib/v3"
)

const maxUndoStack = 20

type undoActionType int

const (
	undoDeleteEntry undoActionType = iota
	undoDeleteGroup
	undoEditEntry
	undoRenameGroup
	undoMoveEntry
	undoMoveGroup
	undoDuplicateEntry
	undoAddEntry
	undoAddGroup
	undoToggleFavorite
)

// undoAction captures a reversible operation.
type undoAction struct {
	actionType  undoActionType
	description string

	// For delete entry: a self-contained copy of the entry and its parent
	// group UUID. Raw copies would keep binary pool IDs, which saving renumbers.
	entrySnapshot *keepass.EntrySnapshot
	parentUUID    gokeepasslib.UUID

	// For delete group: a self-contained copy of the group and its parent group UUID
	groupSnapshot *keepass.GroupSnapshot

	// For edit entry: the previous simplified entry state
	entryUUID gokeepasslib.UUID
	prevEntry *keepass.Entry

	// For rename group: old name
	groupUUID gokeepasslib.UUID
	oldName   string

	// For move entry/group: source group UUID (to move back)
	sourceUUID gokeepasslib.UUID

	// For duplicate/add entry/group: UUID of the created item (to delete)
	createdUUID gokeepasslib.UUID
}

type undoStack struct {
	actions []undoAction
}

// Dropped and popped slots are cleared: the backing array would otherwise
// keep their deleted entries, with passwords and attachments, reachable.
func (s *undoStack) push(a undoAction) {
	s.actions = append(s.actions, a)
	if n := len(s.actions) - maxUndoStack; n > 0 {
		clear(s.actions[:n])
		s.actions = s.actions[n:]
	}
}

func (s *undoStack) pop() (undoAction, bool) {
	if len(s.actions) == 0 {
		return undoAction{}, false
	}
	last := len(s.actions) - 1
	a := s.actions[last]
	s.actions[last] = undoAction{}
	s.actions = s.actions[:last]
	return a, true
}

func (s *undoStack) length() int {
	return len(s.actions)
}

func (s *undoStack) clear() {
	s.actions = nil
}
