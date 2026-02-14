package ui

import (
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rmarchei/keetui/internal/keepass"
)

func createTestUIDB(t *testing.T) *keepass.DB {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "testmaster"

	db, err := keepass.Create(dbPath, password)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	root := db.RootGroup()

	// Add a group
	if err := db.AddGroup(root.UUID, "Email"); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}

	root = db.RootGroup()
	emailGroup := root.Groups[0]

	// Add entries
	if err := db.AddEntry(emailGroup.UUID, keepass.Entry{
		Title:    "Gmail",
		UserName: "user@gmail.com",
		Password: "gmailpass",
		URL:      "https://gmail.com",
	}); err != nil {
		t.Fatalf("AddEntry: %v", err)
	}

	if err := db.AddEntry(root.UUID, keepass.Entry{
		Title:    "Root Entry",
		UserName: "rootuser",
		Password: "rootpass",
	}); err != nil {
		t.Fatalf("AddEntry: %v", err)
	}

	return db
}

func TestBrowserNavigation(t *testing.T) {
	db := createTestUIDB(t)
	m := NewBrowserModel(db)
	m.width, m.height = 80, 24

	// Should start with items in root (Email group + Root Entry)
	if len(m.items) < 2 {
		t.Fatalf("items = %d, want at least 2", len(m.items))
	}
	if m.cursor != 0 {
		t.Errorf("initial cursor = %d, want 0", m.cursor)
	}

	// Move down
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(BrowserModel)
	if m.cursor != 1 {
		t.Errorf("cursor after j = %d, want 1", m.cursor)
	}

	// Move up
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(BrowserModel)
	if m.cursor != 0 {
		t.Errorf("cursor after k = %d, want 0", m.cursor)
	}

	// Should not go below 0
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(BrowserModel)
	if m.cursor != 0 {
		t.Errorf("cursor should not go below 0, got %d", m.cursor)
	}
}

func TestBrowserDrillInOut(t *testing.T) {
	db := createTestUIDB(t)
	m := NewBrowserModel(db)
	m.width, m.height = 80, 24

	// First item should be Email group (groups sort first)
	if !m.items[0].isGroup || m.items[0].group.Name != "Email" {
		t.Fatalf("first item should be Email group, got %v", m.items[0])
	}

	// Drill into Email group
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(BrowserModel)
	if len(m.stack) != 2 {
		t.Errorf("stack after enter = %d, want 2", len(m.stack))
	}
	if m.currentGroup().Name != "Email" {
		t.Errorf("current group = %q, want %q", m.currentGroup().Name, "Email")
	}

	// Drill out
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(BrowserModel)
	if len(m.stack) != 1 {
		t.Errorf("stack after esc = %d, want 1", len(m.stack))
	}
}

func TestBrowserSearch(t *testing.T) {
	db := createTestUIDB(t)
	m := NewBrowserModel(db)
	m.width, m.height = 80, 24

	// Enter search mode
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(BrowserModel)
	if !m.searching {
		t.Error("should be in search mode after '/'")
	}

	// Type a search query
	for _, r := range "gmail" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(BrowserModel)
	}

	// Should filter to Gmail entry
	if len(m.items) != 1 {
		t.Errorf("search results = %d, want 1", len(m.items))
	}
	if m.items[0].entry.Title != "Gmail" {
		t.Errorf("search result = %q, want %q", m.items[0].entry.Title, "Gmail")
	}

	// Cancel search
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(BrowserModel)
	if m.searching {
		t.Error("should not be in search mode after esc")
	}
}

func TestBrowserDeleteMsg(t *testing.T) {
	db := createTestUIDB(t)
	m := NewBrowserModel(db)
	m.width, m.height = 80, 24

	// Move cursor to an entry (skip the group)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(BrowserModel)

	// Press d to delete
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = updated.(BrowserModel)

	if cmd == nil {
		t.Fatal("d key should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(DeleteEntryMsg); !ok {
		t.Errorf("expected DeleteEntryMsg, got %T", msg)
	}
}

func TestBrowserNewEntryMsg(t *testing.T) {
	db := createTestUIDB(t)
	m := NewBrowserModel(db)
	m.width, m.height = 80, 24

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if cmd == nil {
		t.Fatal("n key should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(NewEntryMsg); !ok {
		t.Errorf("expected NewEntryMsg, got %T", msg)
	}
}

func TestBrowserDuplicateMsg(t *testing.T) {
	db := createTestUIDB(t)
	m := NewBrowserModel(db)
	m.width, m.height = 80, 24

	// Move to entry
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(BrowserModel)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if cmd == nil {
		t.Fatal("y key should produce a command for entries")
	}
	msg := cmd()
	if _, ok := msg.(DuplicateEntryMsg); !ok {
		t.Errorf("expected DuplicateEntryMsg, got %T", msg)
	}
}

func TestBrowserRefresh(t *testing.T) {
	db := createTestUIDB(t)
	m := NewBrowserModel(db)
	m.width, m.height = 80, 24

	initialCount := len(m.items)

	// Add a new entry directly to DB
	root := db.RootGroup()
	_ = db.AddEntry(root.UUID, keepass.Entry{
		Title:    "New",
		UserName: "new",
		Password: "new",
	})

	// Refresh
	updated, _ := m.Update(RefreshMsg{})
	m = updated.(BrowserModel)

	if len(m.items) <= initialCount {
		t.Errorf("items after refresh = %d, should be > %d", len(m.items), initialCount)
	}
}

func TestBrowserStatus(t *testing.T) {
	db := createTestUIDB(t)
	m := NewBrowserModel(db)
	m.width, m.height = 80, 24

	updated, _ := m.Update(StatusMsg{Text: "Test status", IsError: false})
	m = updated.(BrowserModel)

	if m.status == "" {
		t.Error("status should be set after StatusMsg")
	}
}

func TestBrowserView(t *testing.T) {
	db := createTestUIDB(t)
	m := NewBrowserModel(db)
	m.width, m.height = 80, 24

	view := m.View()
	if view == "" {
		t.Error("View() should return non-empty string")
	}
	// Should contain the title
	if !contains(view, "keetui") {
		t.Error("View() should contain 'keetui'")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
