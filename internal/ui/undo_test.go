package ui

import (
	"testing"

	"github.com/tobischo/gokeepasslib/v3"
)

func TestUndoStackPushPop(t *testing.T) {
	var s undoStack

	s.push(undoAction{actionType: undoDeleteEntry, description: "first"})
	s.push(undoAction{actionType: undoEditEntry, description: "second"})

	if s.length() != 2 {
		t.Fatalf("length = %d, want 2", s.length())
	}

	a, ok := s.pop()
	if !ok {
		t.Fatal("pop should return true")
	}
	if a.description != "second" {
		t.Errorf("description = %q, want %q", a.description, "second")
	}

	a, ok = s.pop()
	if !ok {
		t.Fatal("pop should return true")
	}
	if a.description != "first" {
		t.Errorf("description = %q, want %q", a.description, "first")
	}

	if s.length() != 0 {
		t.Errorf("length after pops = %d, want 0", s.length())
	}
}

func TestUndoStackEmpty(t *testing.T) {
	var s undoStack

	_, ok := s.pop()
	if ok {
		t.Error("pop from empty stack should return false")
	}
}

func TestUndoStackMaxSize(t *testing.T) {
	var s undoStack

	for range maxUndoStack + 10 {
		s.push(undoAction{description: "item"})
	}

	if s.length() != maxUndoStack {
		t.Errorf("length = %d, want %d (maxUndoStack)", s.length(), maxUndoStack)
	}
}

func TestUndoStackClear(t *testing.T) {
	var s undoStack

	s.push(undoAction{description: "one"})
	s.push(undoAction{description: "two"})
	s.clear()

	if s.length() != 0 {
		t.Errorf("length after clear = %d, want 0", s.length())
	}

	_, ok := s.pop()
	if ok {
		t.Error("pop after clear should return false")
	}
}

func TestUndoStackPreservesMetadata(t *testing.T) {
	var s undoStack

	testUUID := gokeepasslib.NewUUID()
	parentUUID := gokeepasslib.NewUUID()

	s.push(undoAction{
		actionType:  undoDeleteEntry,
		description: "Delete test entry",
		entryUUID:   testUUID,
		parentUUID:  parentUUID,
	})

	a, ok := s.pop()
	if !ok {
		t.Fatal("pop should return true")
	}
	if a.actionType != undoDeleteEntry {
		t.Errorf("actionType = %d, want %d (undoDeleteEntry)", a.actionType, undoDeleteEntry)
	}
	if a.entryUUID != testUUID {
		t.Error("entryUUID not preserved")
	}
	if a.parentUUID != parentUUID {
		t.Error("parentUUID not preserved")
	}
}

func TestUndoStackMaxSizeEvictsOldest(t *testing.T) {
	var s undoStack

	// Push maxUndoStack+5 items, verify oldest are evicted
	for range maxUndoStack + 5 {
		s.push(undoAction{description: "item"})
	}

	if s.length() != maxUndoStack {
		t.Errorf("length = %d, want %d", s.length(), maxUndoStack)
	}

	// All pops should succeed
	for i := range maxUndoStack {
		_, ok := s.pop()
		if !ok {
			t.Fatalf("pop %d should return true", i)
		}
	}

	// Next pop should fail
	_, ok := s.pop()
	if ok {
		t.Error("pop from empty stack should return false")
	}
}

func TestUndoStackActionTypes(t *testing.T) {
	var s undoStack

	// Verify all action types can be pushed and popped
	types := []undoActionType{
		undoDeleteEntry, undoDeleteGroup, undoEditEntry,
		undoRenameGroup, undoMoveEntry, undoMoveGroup,
		undoDuplicateEntry, undoAddEntry, undoAddGroup,
		undoToggleFavorite,
	}
	for _, at := range types {
		s.push(undoAction{actionType: at})
	}

	// Pop in reverse order
	for i := len(types) - 1; i >= 0; i-- {
		a, ok := s.pop()
		if !ok {
			t.Fatalf("pop for type %d should return true", types[i])
		}
		if a.actionType != types[i] {
			t.Errorf("actionType = %d, want %d", a.actionType, types[i])
		}
	}
}
