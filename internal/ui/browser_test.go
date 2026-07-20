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
	if _, err := db.AddGroup(root.UUID, "Email"); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}

	root = db.RootGroup()
	emailGroup := root.Groups[0]

	// Add entries
	if _, err := db.AddEntry(emailGroup.UUID, keepass.Entry{
		Title:    "Gmail",
		UserName: "user@gmail.com",
		Password: "gmailpass",
		URL:      "https://gmail.com",
	}); err != nil {
		t.Fatalf("AddEntry: %v", err)
	}

	if _, err := db.AddEntry(root.UUID, keepass.Entry{
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

func TestBrowserSearchByURL(t *testing.T) {
	db := createTestUIDB(t)
	m := NewBrowserModel(db)
	m.width, m.height = 80, 24

	// Search by URL - "gmail.com" should match the Gmail entry
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(BrowserModel)
	for _, r := range "gmail.com" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(BrowserModel)
	}
	if len(m.items) != 1 {
		t.Errorf("search by URL: results = %d, want 1", len(m.items))
	} else if m.items[0].entry.Title != "Gmail" {
		t.Errorf("search by URL: result = %q, want %q", m.items[0].entry.Title, "Gmail")
	}
}

func TestBrowserSearchByNotesAndCustomFields(t *testing.T) {
	db := createTestUIDB(t)
	root := db.RootGroup()

	// Add entry with notes and custom fields
	_, _ = db.AddEntry(root.UUID, keepass.Entry{
		Title:    "Server",
		UserName: "admin",
		Password: "secret",
		Notes:    "Production database server",
		CustomFields: []keepass.CustomField{
			{Key: "hostname", Value: "db.example.org"},
		},
	})

	m := NewBrowserModel(db)
	m.width, m.height = 80, 24

	// Search by notes content
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(BrowserModel)
	for _, r := range "production" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(BrowserModel)
	}
	if len(m.items) != 1 {
		t.Errorf("search by notes: results = %d, want 1", len(m.items))
	} else if m.items[0].entry.Title != "Server" {
		t.Errorf("search by notes: result = %q, want %q", m.items[0].entry.Title, "Server")
	}

	// Cancel and search by custom field value
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(BrowserModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(BrowserModel)
	for _, r := range "example.org" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(BrowserModel)
	}
	if len(m.items) != 1 {
		t.Errorf("search by custom field: results = %d, want 1", len(m.items))
	} else if m.items[0].entry.Title != "Server" {
		t.Errorf("search by custom field: result = %q, want %q", m.items[0].entry.Title, "Server")
	}
}

func TestBrowserFuzzySearch(t *testing.T) {
	db := createTestUIDB(t)
	root := db.RootGroup()

	_, _ = db.AddEntry(root.UUID, keepass.Entry{
		Title:    "GitHub",
		UserName: "octocat",
		Password: "secret",
	})

	m := NewBrowserModel(db)
	m.width, m.height = 80, 24

	// "git" should fuzzy-match "GitHub" (g-i-t all appear in order)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(BrowserModel)
	for _, r := range "git" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(BrowserModel)
	}

	found := false
	for _, item := range m.items {
		if !item.isGroup && item.entry.Title == "GitHub" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("fuzzy search 'git' should match 'GitHub', got %d results", len(m.items))
	}
}

func TestFuzzyMatch(t *testing.T) {
	tests := []struct {
		s       string
		pattern string
		want    bool
	}{
		{"GitHub", "git", true},
		{"GitHub", "gth", true},
		{"GitHub", "hub", true},
		{"GitHub", "xyz", false},
		{"Hello World", "hwd", true},
		{"Hello World", "dwh", false},
		{"", "a", false},
		{"abc", "", true},
	}
	for _, tt := range tests {
		got := fuzzyMatch(tt.s, tt.pattern)
		if got != tt.want {
			t.Errorf("fuzzyMatch(%q, %q) = %v, want %v", tt.s, tt.pattern, got, tt.want)
		}
	}
}

func TestCountEntries(t *testing.T) {
	db := createTestUIDB(t)
	root := db.RootGroup()

	// Root has 1 direct entry ("Root Entry") + Email group with 1 entry ("Gmail")
	total := countEntries(root)
	if total != 2 {
		t.Errorf("countEntries(root) = %d, want 2", total)
	}

	// Email group has 1 entry
	emailGroup := root.Groups[0]
	emailCount := countEntries(emailGroup)
	if emailCount != 1 {
		t.Errorf("countEntries(Email) = %d, want 1", emailCount)
	}
}

func TestBrowserSortCycle(t *testing.T) {
	db := createTestUIDB(t)
	m := NewBrowserModel(db)
	m.width, m.height = 80, 24

	if m.sortBy != sortByName {
		t.Errorf("initial sortBy = %v, want sortByName", m.sortBy)
	}

	// Press S to cycle to sortByModified
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	m = updated.(BrowserModel)
	if m.sortBy != sortByModified {
		t.Errorf("sortBy after S = %v, want sortByModified", m.sortBy)
	}

	// Press S again to cycle to sortByCreated
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	m = updated.(BrowserModel)
	if m.sortBy != sortByCreated {
		t.Errorf("sortBy after S = %v, want sortByCreated", m.sortBy)
	}

	// Press S again to cycle back to sortByName
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	m = updated.(BrowserModel)
	if m.sortBy != sortByName {
		t.Errorf("sortBy after S = %v, want sortByName", m.sortBy)
	}
}

func TestBrowserTagFilter(t *testing.T) {
	db := createTestUIDB(t)
	root := db.RootGroup()

	// Add entries with tags
	_, _ = db.AddEntry(root.UUID, keepass.Entry{
		Title:    "Work Email",
		UserName: "work@example.com",
		Password: "pass",
		Tags:     "work",
	})
	_, _ = db.AddEntry(root.UUID, keepass.Entry{
		Title:    "Personal Email",
		UserName: "me@example.com",
		Password: "pass",
		Tags:     "personal;email",
	})

	m := NewBrowserModel(db)
	m.width, m.height = 80, 24

	initialCount := len(m.items)

	// Press t to filter by first tag
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = updated.(BrowserModel)
	if m.tagFilter == "" {
		t.Fatal("tagFilter should be set after pressing t")
	}

	// Filtered items should be fewer entries (groups always shown)
	if len(m.items) >= initialCount {
		t.Errorf("filtered items (%d) should be fewer than unfiltered (%d)", len(m.items), initialCount)
	}

	// Keep pressing t until filter clears
	for i := 0; i < 10 && m.tagFilter != ""; i++ {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
		m = updated.(BrowserModel)
	}
	if m.tagFilter != "" {
		t.Errorf("tagFilter should be empty after cycling through all tags, got %q", m.tagFilter)
	}
}

func TestCollectTags(t *testing.T) {
	db := createTestUIDB(t)
	root := db.RootGroup()

	// No tags initially (test DB entries have no tags)
	tags := collectTags(root)
	if len(tags) != 0 {
		t.Errorf("expected 0 tags initially, got %d: %v", len(tags), tags)
	}

	// Add entries with tags
	_, _ = db.AddEntry(root.UUID, keepass.Entry{
		Title: "A", Tags: "work;email",
	})
	_, _ = db.AddEntry(root.UUID, keepass.Entry{
		Title: "B", Tags: "personal",
	})

	root = db.RootGroup()
	tags = collectTags(root)
	if len(tags) != 3 {
		t.Errorf("expected 3 tags, got %d: %v", len(tags), tags)
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
	delMsg, ok := msg.(DeleteEntryMsg)
	if !ok {
		t.Fatalf("expected DeleteEntryMsg, got %T", msg)
	}
	if delMsg.Name == "" {
		t.Error("DeleteEntryMsg.Name should be populated")
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
	_, _ = db.AddEntry(root.UUID, keepass.Entry{
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

func TestBrowserRefreshPreservesStack(t *testing.T) {
	db := createTestUIDB(t)
	m := NewBrowserModel(db)
	m.width, m.height = 80, 24

	// Navigate into the group (first item should be "Work" group)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(BrowserModel)
	// Execute any command (ViewEntryMsg or navigation)
	if cmd != nil {
		msg := cmd()
		if _, isViewEntry := msg.(ViewEntryMsg); !isViewEntry {
			// It was a group navigation, apply the update
			updated, _ = m.Update(msg)
			m = updated.(BrowserModel)
		}
	}

	stackDepth := len(m.stack)
	if stackDepth < 2 {
		t.Fatalf("stack depth = %d, want >= 2 after entering a group", stackDepth)
	}

	// Send RefreshMsg
	updated, _ = m.Update(RefreshMsg{})
	m = updated.(BrowserModel)

	if len(m.stack) != stackDepth {
		t.Errorf("stack depth after refresh = %d, want %d (should be preserved)", len(m.stack), stackDepth)
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

func TestBrowserSearchCancelClearsState(t *testing.T) {
	db := createTestUIDB(t)
	m := NewBrowserModel(db)
	m.width, m.height = 80, 24
	baseline := len(m.items)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(BrowserModel)
	for _, r := range "gmail" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(BrowserModel)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(BrowserModel)

	if m.searchQuery != "" {
		t.Errorf("searchQuery = %q after esc, want empty", m.searchQuery)
	}

	// A refresh after canceling must not resurrect the search.
	updated, _ = m.Update(RefreshMsg{})
	m = updated.(BrowserModel)
	if m.searchQuery != "" {
		t.Error("refresh resurrected the canceled search")
	}
	if len(m.items) != baseline {
		t.Errorf("items = %d after refresh, want group view of %d", len(m.items), baseline)
	}
}

func TestBrowserEmptySearchEnterCancels(t *testing.T) {
	db := createTestUIDB(t)
	m := NewBrowserModel(db)
	m.width, m.height = 80, 24
	baseline := len(m.items)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(BrowserModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(BrowserModel)

	if m.searching {
		t.Error("still in search mode after enter")
	}
	if m.searchQuery != "" {
		t.Errorf("searchQuery = %q, want empty", m.searchQuery)
	}
	if len(m.items) != baseline {
		t.Errorf("items = %d, want the normal group view of %d (not the flattened all-entries list)", len(m.items), baseline)
	}
}

func TestBrowserSearchRecycleBinDispatch(t *testing.T) {
	db := createTestUIDB(t)
	root := db.RootGroup()
	db.Database.Content.Meta.RecycleBinEnabled.Bool = true
	binnedUUID, err := db.AddEntry(root.UUID, keepass.Entry{Title: "Trashed"})
	if err != nil {
		t.Fatalf("AddEntry: %v", err)
	}
	if err := db.DeleteEntry(binnedUUID); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}

	search := func(query string) BrowserModel {
		m := NewBrowserModel(db)
		m.width, m.height = 80, 24
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
		m = updated.(BrowserModel)
		for _, r := range query {
			updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
			m = updated.(BrowserModel)
		}
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(BrowserModel)
		if len(m.items) != 1 {
			t.Fatalf("search %q results = %d, want 1", query, len(m.items))
		}
		return m
	}
	pressDelete := func(m BrowserModel) tea.Msg {
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(m.keys.Delete)})
		if cmd == nil {
			t.Fatal("delete key produced no command")
		}
		return cmd()
	}

	// Deleting a recycle-bin entry found via search must be dispatched as a
	// permanent delete (the UI otherwise claims a soft delete that the data
	// layer silently turns into permanent removal).
	if msg := pressDelete(search("Trashed")); msg != nil {
		if _, ok := msg.(PermanentlyDeleteEntryMsg); !ok {
			t.Errorf("binned search result delete = %T, want PermanentlyDeleteEntryMsg", msg)
		}
	}

	// Deleting a live entry found via search stays a soft delete.
	if msg := pressDelete(search("Gmail")); msg != nil {
		if _, ok := msg.(DeleteEntryMsg); !ok {
			t.Errorf("live search result delete = %T, want DeleteEntryMsg", msg)
		}
	}

	// Restore now works on binned entries reached via search.
	m := search("Trashed")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(m.keys.Restore)})
	if cmd == nil {
		t.Fatal("restore key produced no command for a binned search result")
	}
	if _, ok := cmd().(RestoreEntryMsg); !ok {
		t.Errorf("restore = %T, want RestoreEntryMsg", cmd())
	}
}
