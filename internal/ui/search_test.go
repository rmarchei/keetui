package ui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rmarchei/keetui/internal/keepass"
)

func TestSearchHighlightPositions(t *testing.T) {
	for _, tc := range []struct {
		text, query string
		fuzzy       bool
		want        []int
	}{
		{"GitHub", "GIT", false, []int{0, 1, 2}},
		{"GitHub", "gh", true, []int{0, 3}},
		{"GitHub", "gh", false, nil},
		{"a-b ab", "ab", true, []int{4, 5}}, // prefer the contiguous match
		{"İ日本語", "i日", false, []int{0, 1}},  // lowercasing changes the byte length
		{"Cafe\u0301", "e", false, []int{3}},
		{"GitHub", "", true, nil},
		{"GitHub", "zzz", true, nil},
	} {
		if got := matchPositions(tc.text, tc.query, tc.fuzzy); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q / %q: positions %v, want %v", tc.text, tc.query, got, tc.want)
		}
		got := highlightSearch(tc.text, tc.query, entryStyle, tc.fuzzy)
		if ansi.Strip(got) != tc.text || lipgloss.Width(got) != lipgloss.Width(tc.text) {
			t.Errorf("highlight changed text or terminal width: %q", got)
		}
	}
	// An accent is styled with its base character, never split by an ANSI code.
	got := highlightSearch("Cafe\u0301", "e", entryStyle, false)
	if !strings.Contains(got, entryStyle.Bold(true).Underline(true).Render("e\u0301")) {
		t.Fatalf("combining cluster split or not highlighted: %q", got)
	}
}

func TestSearchExplainsHiddenFieldsWithoutDisplayingValues(t *testing.T) {
	for _, tc := range []struct {
		entry keepass.Entry
		field string
	}{
		{keepass.Entry{Title: "h-i-n-t", Notes: "hint-private-note-content"}, "notes"},
		{keepass.Entry{Title: "Example", URL: "https://example.com/hint-private-url-path"}, "URL"},
		{keepass.Entry{Title: "Example", CustomFields: []keepass.CustomField{{Key: "Extra", Value: "hint-private-custom-value"}}}, "custom field"},
		{keepass.Entry{Title: "Example", CustomFields: []keepass.CustomField{{Key: "Private key name", Value: "hint-private-protected-value", Protected: true}}}, "protected field"},
	} {
		db := createTestUIDB(t)
		if _, err := db.AddEntry(db.RootGroup().UUID, tc.entry); err != nil {
			t.Fatal(err)
		}
		m := startSearch(t, NewBrowserModel(db), "hint")
		for _, width := range []int{40, 80, 120} {
			m.width, m.height = width, 12
			view := m.View().Content
			assertFrame(t, view, width, 12)
			plain := ansi.Strip(view)
			if !strings.Contains(plain, "matches "+tc.field) || !strings.Contains(plain, "1 result") {
				t.Errorf("%s at %d: missing explanation or count:\n%s", tc.field, width, plain)
			}
			if strings.Contains(plain, "private-") || strings.Contains(plain, "Private key name") {
				t.Fatalf("search leaked a hidden value or protected name:\n%s", plain)
			}
			assertOnBackground(t, rowWith(t, view, tc.entry.Title), selectedRowStyle)
		}
	}
}

func TestSearchBarPersistsAndMouseFollowsRenderedRows(t *testing.T) {
	db := createTestUIDB(t)
	for i := range 25 {
		if _, err := db.AddEntry(db.RootGroup().UUID, keepass.Entry{Title: fmt.Sprintf("Result %02d 日本語", i), UserName: "someone"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, size := range [][2]int{{40, 11}, {80, 23}, {120, 39}} {
		m := NewBrowserModel(db)
		m.width, m.height = size[0], size[1]
		m = startSearch(t, m, "ReSuLt")
		for range 18 {
			updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
			m = updated.(BrowserModel)
		}
		before := ansi.Strip(m.searchView())
		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = updated.(BrowserModel)
		updated, _ = m.Update(RefreshMsg{})
		m = updated.(BrowserModel)
		if got := ansi.Strip(m.searchView()); got != before || !strings.Contains(got, "ReSuLt") || !strings.Contains(got, "25 results") {
			t.Fatalf("search row changed after returning: %q -> %q", before, got)
		}
		view := m.View().Content
		assertFrame(t, view, size[0], size[1])
		selected := rowWith(t, view, selectionCursor)
		assertOnBackground(t, selected, selectedRowStyle)
		if lipgloss.Width(selected) != size[0] {
			t.Fatal("highlight interrupted the selection bar")
		}
		// Click the second visible row, then click it again to open it.
		target := m.offset + 1
		click := tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: m.listStartRow() + 1}
		updated, _ = m.Update(click)
		m = updated.(BrowserModel)
		if m.cursor != target {
			t.Fatalf("click selected %d, want rendered row %d", m.cursor, target)
		}
		_, cmd := m.Update(click)
		if cmd == nil || cmd().(ViewEntryMsg).Entry.UUID != m.items[target].entry.UUID {
			t.Fatal("second click did not open the rendered result")
		}
		// Starting another search must not display stale highlights or reasons.
		updated, _ = m.Update(textKey("/"))
		m = updated.(BrowserModel)
		if m.searchQuery != "" || strings.Contains(ansi.Strip(m.View().Content), "matches ") {
			t.Fatal("new search retained the previous query")
		}
	}
}

func TestSearchBarFitsLongQueriesAndZeroResults(t *testing.T) {
	m := startSearch(t, NewBrowserModel(createTestUIDB(t)), strings.Repeat("日本語", 20))
	for _, width := range []int{12, 20, 40, 80} {
		m.width, m.height = width, 11
		assertFrame(t, m.View().Content, width, 11)
		bar := ansi.Strip(m.searchView())
		if !strings.Contains(bar, "0") || lipgloss.Width(bar) != width {
			t.Errorf("query displaced count at %d: %q", width, bar)
		}
	}
}
