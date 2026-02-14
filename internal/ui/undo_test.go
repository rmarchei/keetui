package ui

import "testing"

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

	for i := 0; i < maxUndoStack+10; i++ {
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
