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
		{"FastestVPN", "test", false, []int{3, 4, 5, 6}},
		{"Ataner NDTrust", "test", false, nil},
		{"Ataner NDTrust", "test", true, []int{1, 4, 12, 13}},
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

func TestLiteralSearchRequiresContiguousTextInEveryField(t *testing.T) {
	setters := map[string]func(*keepass.Entry, string){
		"title":     func(e *keepass.Entry, s string) { e.Title = s },
		"username":  func(e *keepass.Entry, s string) { e.UserName = s },
		"URL":       func(e *keepass.Entry, s string) { e.URL = s },
		"notes":     func(e *keepass.Entry, s string) { e.Notes = s },
		"field key": func(e *keepass.Entry, s string) { e.CustomFields = []keepass.CustomField{{Key: s}} },
		"field value": func(e *keepass.Entry, s string) {
			e.CustomFields = []keepass.CustomField{{Key: "Extra", Value: s}}
		},
		"protected value": func(e *keepass.Entry, s string) {
			e.CustomFields = []keepass.CustomField{{Key: "Extra", Value: s, Protected: true}}
		},
	}
	for field, set := range setters {
		t.Run(field, func(t *testing.T) {
			m := NewBrowserModel(createTestUIDB(t))
			m.searching = true
			for _, value := range []string{"prefix TeSt suffix", "prefix t-e-s-t suffix", "t", "e", "s", "tset"} {
				entry := keepass.Entry{Title: "Example"}
				set(&entry, value)
				m.allItems = append(m.allItems, listItem{entry: entry})
			}
			m.searchInput.SetValue("TeSt")
			m.applySearch()
			if len(m.items) != 1 || !reflect.DeepEqual(m.items[0], m.allItems[0]) {
				t.Fatalf("literal search returned %d results, want only the contiguous match", len(m.items))
			}
			if got := searchMatchField(m.allItems[1], "test", false); got != "" {
				t.Fatalf("literal mode explained a scattered match as %q", got)
			}
			updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
			m = updated.(BrowserModel)
			if len(m.items) != 2 || !reflect.DeepEqual(m.items, m.allItems[:2]) {
				t.Fatalf("fuzzy search returned %d results, want exact then scattered, excluding single letters and wrong order", len(m.items))
			}
			if searchMatchField(m.allItems[1], "test", true) == "" {
				t.Fatal("fuzzy mode lost its match explanation")
			}
		})
	}
}

func TestSearchModeTogglePreservesQueryAndResetsForNewSearch(t *testing.T) {
	db := createTestUIDB(t)
	for _, title := range []string{"FastestVPN", "Ataner NDTrust"} {
		if _, err := db.AddEntry(db.RootGroup().UUID, keepass.Entry{Title: title}); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 20 {
		if _, err := db.AddEntry(db.RootGroup().UUID, keepass.Entry{Title: fmt.Sprintf("t-e-s-t %02d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	m := NewBrowserModel(db)
	m.width, m.height = 80, 12
	m = startSearch(t, m, "TeSt")
	press := func(code rune) {
		updated, _ := m.Update(tea.KeyPressMsg{Code: code})
		m = updated.(BrowserModel)
	}
	if m.searchFuzzy || len(m.items) != 1 || m.items[0].entry.Title != "FastestVPN" {
		t.Fatal("literal search did not exclude the scattered matches from the screenshot")
	}
	if !strings.Contains(ansi.Strip(m.searchView()), "literal · 1 result") || !strings.Contains(ansi.Strip(m.footerView()), "tab fuzzy") {
		t.Fatal("literal mode or fuzzy toggle is not discoverable")
	}
	press(tea.KeyTab)
	if len(m.items) != 22 || m.items[0].entry.Title != "FastestVPN" {
		t.Fatal("fuzzy mode should keep exact matches before scattered matches")
	}
	if !strings.Contains(ansi.Strip(m.searchView()), "fuzzy · 22 results") || !strings.Contains(ansi.Strip(m.footerView()), "tab literal") {
		t.Fatal("fuzzy mode or literal toggle is not discoverable")
	}
	for range 20 {
		press(tea.KeyDown)
	}
	if m.offset == 0 {
		t.Fatal("fixture did not scroll")
	}
	press(tea.KeyTab)
	if m.searchFuzzy || len(m.items) != 1 || m.cursor != 0 || m.offset != 0 || m.searchInput.Value() != "TeSt" {
		t.Fatal("switching back changed the query or left selection outside the filtered results")
	}
	assertFrame(t, m.View().Content, m.width, m.height)
	press(tea.KeyTab)
	press(tea.KeyEnter)
	updated, _ := m.Update(RefreshMsg{})
	m = updated.(BrowserModel)
	if m.searching || !m.searchFuzzy || len(m.items) != 22 || m.searchInput.Value() != "TeSt" {
		t.Fatal("returning from an entry lost the committed query or mode")
	}
	m = startSearch(t, m, "test")
	if m.searchFuzzy || len(m.items) != 1 {
		t.Fatal("a new search inherited fuzzy mode")
	}
}
