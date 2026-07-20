package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rmarchei/keetui/internal/keepass"
)

func TestAuditBack(t *testing.T) {
	m := NewAuditModel(keepass.AuditResult{})
	m.width, m.height = 80, 24

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(BackToBrowserMsg); !ok {
		t.Errorf("expected BackToBrowserMsg, got %T", msg)
	}
}

func TestAuditQuit(t *testing.T) {
	m := NewAuditModel(keepass.AuditResult{})
	m.width, m.height = 80, 24

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("q should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(RequestQuitMsg); !ok {
		t.Errorf("expected RequestQuitMsg, got %T", msg)
	}
}

func TestAuditNavigation(t *testing.T) {
	result := keepass.AuditResult{
		WeakEntries: []keepass.AuditEntry{
			{Entry: keepass.Entry{Title: "Weak1"}, GroupPath: "Root"},
			{Entry: keepass.Entry{Title: "Weak2"}, GroupPath: "Root"},
		},
	}
	m := NewAuditModel(result)
	m.width, m.height = 80, 24

	// First selectable item should be at index 1 (index 0 is header)
	if m.cursor != 1 {
		t.Errorf("initial cursor = %d, want 1 (first entry after header)", m.cursor)
	}

	// Move down should skip headers
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(AuditModel)
	if m.cursor != 2 {
		t.Errorf("cursor after j = %d, want 2", m.cursor)
	}

	// Move back up
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(AuditModel)
	if m.cursor != 1 {
		t.Errorf("cursor after k = %d, want 1", m.cursor)
	}
}

func TestAuditSelectEntry(t *testing.T) {
	result := keepass.AuditResult{
		WeakEntries: []keepass.AuditEntry{
			{Entry: keepass.Entry{Title: "Weak1"}, GroupPath: "Root"},
		},
	}
	m := NewAuditModel(result)
	m.width, m.height = 80, 24

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on entry should produce a command")
	}
	msg := cmd()
	viewMsg, ok := msg.(ViewEntryMsg)
	if !ok {
		t.Fatalf("expected ViewEntryMsg, got %T", msg)
	}
	if viewMsg.Entry.Title != "Weak1" {
		t.Errorf("entry title = %q, want %q", viewMsg.Entry.Title, "Weak1")
	}
}

func TestAuditEmptyView(t *testing.T) {
	m := NewAuditModel(keepass.AuditResult{})
	m.width, m.height = 80, 24

	view := m.View()
	if !strings.Contains(view, "No issues found") {
		t.Error("View() should contain 'No issues found' when empty")
	}
}

func TestAuditView(t *testing.T) {
	result := keepass.AuditResult{
		WeakEntries: []keepass.AuditEntry{
			{Entry: keepass.Entry{Title: "Weak1"}, GroupPath: "Root"},
		},
	}
	m := NewAuditModel(result)
	m.width, m.height = 80, 24

	view := m.View()
	if !strings.Contains(view, "Password Audit") {
		t.Error("View() should contain 'Password Audit'")
	}
	if !strings.Contains(view, "1 issues found") {
		t.Error("View() should contain issue count")
	}
}

func TestAuditViewFitsTerminalHeight(t *testing.T) {
	// Many issues across multiple sections used to overflow the terminal:
	// headers rendered two physical rows but the window math counted one.
	var result keepass.AuditResult
	for i := range 20 {
		e := keepass.AuditEntry{
			Entry:     keepass.Entry{Title: fmt.Sprintf("weak-%d", i), Password: "abc"},
			GroupPath: "Root",
			Category:  "Weak",
		}
		result.WeakEntries = append(result.WeakEntries, e)
		e.Category = "Empty"
		result.EmptyEntries = append(result.EmptyEntries, e)
	}

	m := NewAuditModel(result)
	m.width, m.height = 80, 23 // app hands the view height-1 for the status bar

	// Scroll to the bottom to exercise every window position.
	for range 45 {
		m.moveDown()
		if rows := strings.Count(m.View(), "\n"); rows > m.height {
			t.Fatalf("view renders %d rows into a %d-row budget", rows, m.height)
		}
	}
}
