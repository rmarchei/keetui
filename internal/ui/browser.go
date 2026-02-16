package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/tobischo/gokeepasslib/v3"

	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/keepass"
)

// listItem represents either a group or entry in the browser list.
type listItem struct {
	isGroup   bool
	group     keepass.Group
	entry     keepass.Entry
	groupPath string // parent group path for search results (e.g., "Root > Email")
}

func (l listItem) name() string {
	if l.isGroup {
		return l.group.Name
	}
	return l.entry.Title
}

// stackFrame holds the state for one level of navigation.
type stackFrame struct {
	group  keepass.Group
	cursor int
	offset int
}

// sortMode determines how entries are ordered in the browser.
type sortMode int

const (
	sortByName     sortMode = iota // alphabetical (default)
	sortByModified                 // newest modification first
	sortByCreated                  // newest creation first
	sortModeCount                  // sentinel for cycling
)

func (s sortMode) String() string {
	switch s {
	case sortByModified:
		return "modified"
	case sortByCreated:
		return "created"
	default:
		return "name"
	}
}

// BrowserModel handles navigation through groups and entries.
type BrowserModel struct {
	db          *keepass.DB
	stack       []stackFrame
	items       []listItem
	cursor      int
	offset      int
	width       int
	height      int
	searching   bool
	searchInput textinput.Model
	searchQuery string
	allItems    []listItem         // unfiltered items for search
	cutEntry    *gokeepasslib.UUID // entry UUID in the clipboard for move
	cutGroup    *gokeepasslib.UUID // group UUID in the clipboard for move
	keys        config.KeyBindings
	readOnly    bool
	sortBy      sortMode
	tagFilter   string   // active tag filter (empty = no filter)
	allTags     []string // cached unique tags for cycling
}

// ViewEntryMsg requests viewing the detail of an entry.
type ViewEntryMsg struct {
	Entry keepass.Entry
}

// EditEntryMsg requests editing an entry.
type EditEntryMsg struct {
	Entry keepass.Entry
}

// NewEntryMsg requests creating a new entry in the current group.
type NewEntryMsg struct {
	GroupUUID gokeepasslib.UUID
}

// NewGroupMsg requests creating a new group in the current group.
type NewGroupMsg struct {
	ParentUUID gokeepasslib.UUID
}

// DeleteEntryMsg requests deleting an entry.
type DeleteEntryMsg struct {
	EntryUUID gokeepasslib.UUID
	Name      string
}

// DeleteGroupMsg requests deleting a group.
type DeleteGroupMsg struct {
	GroupUUID gokeepasslib.UUID
	Name      string
}

// PermanentlyDeleteEntryMsg requests permanent deletion of an entry.
type PermanentlyDeleteEntryMsg struct {
	EntryUUID gokeepasslib.UUID
	Name      string
}

// PermanentlyDeleteGroupMsg requests permanent deletion of a group.
type PermanentlyDeleteGroupMsg struct {
	GroupUUID gokeepasslib.UUID
	Name      string
}

// RestoreEntryMsg requests restoring an entry from recycle bin.
type RestoreEntryMsg struct {
	EntryUUID gokeepasslib.UUID
}

// RestoreGroupMsg requests restoring a group from recycle bin.
type RestoreGroupMsg struct {
	GroupUUID gokeepasslib.UUID
}

// EmptyRecycleBinMsg requests emptying the recycle bin.
type EmptyRecycleBinMsg struct{}

// RefreshMsg signals the browser to refresh its data from the DB.
type RefreshMsg struct{}

// StatusMsg updates the status bar.
type StatusMsg struct {
	Text    string
	IsError bool
}

func NewBrowserModel(db *keepass.DB, keys ...config.KeyBindings) BrowserModel {
	root := db.RootGroup()
	si := textinput.New()
	si.Prompt = "  / "
	si.PromptStyle = focusedStyle
	si.Width = 40
	si.Placeholder = "search..."

	var kb config.KeyBindings
	if len(keys) > 0 {
		kb = keys[0]
	} else {
		kb = config.DefaultKeyBindings()
	}

	m := BrowserModel{
		db:          db,
		searchInput: si,
		keys:        kb,
	}
	m.items = m.buildItemsWithDB(root)
	m.stack = []stackFrame{{group: root, cursor: 0, offset: 0}}
	return m
}

func buildItems(g keepass.Group, rbUUID gokeepasslib.UUID, mode sortMode, tagFilter string) []listItem {
	var items []listItem
	for _, sg := range g.Groups {
		items = append(items, listItem{isGroup: true, group: sg})
	}
	for _, e := range g.Entries {
		if tagFilter != "" && !entryHasTag(e, tagFilter) {
			continue
		}
		items = append(items, listItem{isGroup: false, entry: e})
	}
	sortItems(items, rbUUID, mode)
	return items
}

func (m BrowserModel) buildItemsWithDB(g keepass.Group) []listItem {
	return buildItems(g, m.db.RecycleBinUUID(), m.sortBy, m.tagFilter)
}

func sortItems(items []listItem, rbUUID gokeepasslib.UUID, mode sortMode) {
	sort.SliceStable(items, func(i, j int) bool {
		// Groups first, then entries
		if items[i].isGroup != items[j].isGroup {
			return items[i].isGroup
		}
		// Recycle bin group sorts last among groups
		if items[i].isGroup && items[j].isGroup {
			iIsRB := items[i].group.UUID.Compare(rbUUID)
			jIsRB := items[j].group.UUID.Compare(rbUUID)
			if iIsRB != jIsRB {
				return jIsRB // recycle bin goes last
			}
		}
		// Favorites first among entries
		if !items[i].isGroup && !items[j].isGroup {
			iFav := items[i].entry.IsFavorite()
			jFav := items[j].entry.IsFavorite()
			if iFav != jFav {
				return iFav
			}
		}
		// Sort entries by selected mode; groups always sort by name
		if !items[i].isGroup && !items[j].isGroup {
			switch mode {
			case sortByModified:
				ti := items[i].entry.ModificationTime
				tj := items[j].entry.ModificationTime
				if !ti.Equal(tj) {
					return ti.After(tj) // newest first
				}
			case sortByCreated:
				ti := items[i].entry.CreationTime
				tj := items[j].entry.CreationTime
				if !ti.Equal(tj) {
					return ti.After(tj) // newest first
				}
			}
		}
		return strings.ToLower(items[i].name()) < strings.ToLower(items[j].name())
	})
}

// countEntries recursively counts all entries within a group.
func countEntries(g keepass.Group) int {
	count := len(g.Entries)
	for _, sg := range g.Groups {
		count += countEntries(sg)
	}
	return count
}

// collectTags returns all unique, non-empty tags from every entry in the database.
func collectTags(g keepass.Group) []string {
	seen := map[string]bool{}
	var walk func(keepass.Group)
	walk = func(g keepass.Group) {
		for _, e := range g.Entries {
			for _, t := range strings.Split(e.Tags, ";") {
				t = strings.TrimSpace(t)
				if t != "" {
					seen[t] = true
				}
			}
		}
		for _, sg := range g.Groups {
			walk(sg)
		}
	}
	walk(g)
	var tags []string
	for t := range seen {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	return tags
}

// entryHasTag returns true if the entry's tags contain the given tag.
func entryHasTag(e keepass.Entry, tag string) bool {
	for _, t := range strings.Split(e.Tags, ";") {
		if strings.TrimSpace(t) == tag {
			return true
		}
	}
	return false
}

// collectAllEntries recursively gathers all entries from a group tree.
func collectAllEntries(g keepass.Group) []listItem {
	return collectAllEntriesWithPath(g, g.Name)
}

func collectAllEntriesWithPath(g keepass.Group, path string) []listItem {
	var items []listItem
	for _, e := range g.Entries {
		items = append(items, listItem{isGroup: false, entry: e, groupPath: path})
	}
	for _, sg := range g.Groups {
		subPath := path + " > " + sg.Name
		items = append(items, collectAllEntriesWithPath(sg, subPath)...)
	}
	return items
}

func (m BrowserModel) currentGroup() keepass.Group {
	return m.stack[len(m.stack)-1].group
}

// inRecycleBin returns true if the current group or any ancestor is the recycle bin.
func (m BrowserModel) inRecycleBin() bool {
	for _, f := range m.stack {
		if m.db.IsRecycleBin(f.group.UUID) {
			return true
		}
	}
	return false
}

func (m BrowserModel) breadcrumb() string {
	var parts []string
	for _, f := range m.stack {
		parts = append(parts, f.group.Name)
	}
	return strings.Join(parts, " > ")
}

func (m BrowserModel) Init() tea.Cmd {
	return nil
}

func (m BrowserModel) visibleHeight() int {
	// Reserve lines for header, breadcrumb, blank lines, help, status
	h := m.height - 8
	if m.searching {
		h -= 2 // search input line
	}
	if h < 5 {
		h = 5
	}
	return h
}

// fuzzyMatch returns true if all characters in pattern appear in s in order (case-insensitive).
func fuzzyMatch(s, pattern string) bool {
	if len(pattern) == 0 {
		return true
	}
	s = strings.ToLower(s)
	pi := 0
	for _, r := range s {
		if r == rune(pattern[pi]) {
			pi++
			if pi == len(pattern) {
				return true
			}
		}
	}
	return false
}

// entryMatchesSubstring returns true if query is a substring of any searchable field.
func entryMatchesSubstring(item listItem, query string) bool {
	if strings.Contains(strings.ToLower(item.name()), query) {
		return true
	}
	if item.isGroup {
		return false
	}
	e := item.entry
	if strings.Contains(strings.ToLower(e.UserName), query) ||
		strings.Contains(strings.ToLower(e.URL), query) ||
		strings.Contains(strings.ToLower(e.Notes), query) {
		return true
	}
	for _, cf := range e.CustomFields {
		if strings.Contains(strings.ToLower(cf.Value), query) ||
			strings.Contains(strings.ToLower(cf.Key), query) {
			return true
		}
	}
	return false
}

// entryMatchesFuzzy returns true if query fuzzy-matches any searchable field.
func entryMatchesFuzzy(item listItem, query string) bool {
	if fuzzyMatch(item.name(), query) {
		return true
	}
	if item.isGroup {
		return false
	}
	e := item.entry
	if fuzzyMatch(e.UserName, query) ||
		fuzzyMatch(e.URL, query) ||
		fuzzyMatch(e.Notes, query) {
		return true
	}
	for _, cf := range e.CustomFields {
		if fuzzyMatch(cf.Value, query) || fuzzyMatch(cf.Key, query) {
			return true
		}
	}
	return false
}

func (m *BrowserModel) applySearch() {
	query := strings.ToLower(m.searchInput.Value())
	m.searchQuery = query
	if query == "" {
		m.items = m.allItems
	} else {
		// Exact substring matches first, then fuzzy matches
		var exact, fuzzy []listItem
		for _, item := range m.allItems {
			if entryMatchesSubstring(item, query) {
				exact = append(exact, item)
			} else if len(query) > 1 && entryMatchesFuzzy(item, query) {
				fuzzy = append(fuzzy, item)
			}
		}
		m.items = append(exact, fuzzy...)
	}
	m.cursor = 0
	m.offset = 0
}

func (m BrowserModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case RefreshMsg:
		root := m.db.RootGroup()

		// Save selected item identity before rebuilding
		savedCursor := m.cursor
		var selectedEntryUUID gokeepasslib.UUID
		var selectedGroupUUID gokeepasslib.UUID
		if savedCursor < len(m.items) {
			if m.items[savedCursor].isGroup {
				selectedGroupUUID = m.items[savedCursor].group.UUID
			} else {
				selectedEntryUUID = m.items[savedCursor].entry.UUID
			}
		}

		// Rebuild navigation stack from old group UUIDs through fresh root
		oldUUIDs := make([]gokeepasslib.UUID, len(m.stack))
		for i, f := range m.stack {
			oldUUIDs[i] = f.group.UUID
		}
		m.stack = []stackFrame{{group: root, cursor: 0, offset: 0}}
		current := root
		for _, uuid := range oldUUIDs[1:] { // skip root (index 0)
			found := false
			for _, sg := range current.Groups {
				if sg.UUID == uuid {
					m.stack = append(m.stack, stackFrame{group: sg, cursor: 0, offset: 0})
					current = sg
					found = true
					break
				}
			}
			if !found {
				break // group was deleted, stop here
			}
		}

		if m.searchQuery != "" {
			// Re-run the search to preserve results
			m.allItems = collectAllEntries(root)
			sortItems(m.allItems, m.db.RecycleBinUUID(), m.sortBy)
			m.searchInput.SetValue(m.searchQuery)
			m.applySearch()
		} else {
			m.items = m.buildItemsWithDB(m.currentGroup())
			m.searching = false
			m.searchInput.SetValue("")
		}

		// Restore cursor to previously selected item
		restored := false
		for i, item := range m.items {
			if item.isGroup && item.group.UUID == selectedGroupUUID && selectedGroupUUID != (gokeepasslib.UUID{}) {
				m.cursor = i
				restored = true
				break
			}
			if !item.isGroup && item.entry.UUID == selectedEntryUUID && selectedEntryUUID != (gokeepasslib.UUID{}) {
				m.cursor = i
				restored = true
				break
			}
		}
		if !restored {
			if savedCursor < len(m.items) {
				m.cursor = savedCursor
			} else if len(m.items) > 0 {
				m.cursor = len(m.items) - 1
			} else {
				m.cursor = 0
			}
		}
		// Adjust scroll offset so cursor is visible
		vis := m.visibleHeight()
		if m.cursor < m.offset {
			m.offset = m.cursor
		} else if m.cursor >= m.offset+vis {
			m.offset = m.cursor - vis + 1
		}

		return m, nil

	case tea.MouseMsg:
		if !m.searching {
			switch msg.Button {
			case tea.MouseButtonLeft:
				if msg.Action != tea.MouseActionPress {
					break
				}
				// Calculate header lines: 1 newline + 1 title + 1 breadcrumb + 1 blank = 4
				headerLines := 4
				idx := msg.Y - headerLines + m.offset
				if idx >= 0 && idx < len(m.items) {
					if idx == m.cursor {
						// Click on already-selected item: open it
						item := m.items[m.cursor]
						if item.isGroup {
							m.stack[len(m.stack)-1].cursor = m.cursor
							m.stack[len(m.stack)-1].offset = m.offset
							m.stack = append(m.stack, stackFrame{group: item.group})
							m.items = m.buildItemsWithDB(item.group)
							m.cursor = 0
							m.offset = 0
						} else {
							return m, func() tea.Msg {
								return ViewEntryMsg{Entry: item.entry}
							}
						}
					} else {
						m.cursor = idx
					}
				}
			case tea.MouseButtonWheelUp:
				if m.cursor > 0 {
					m.cursor--
					if m.cursor < m.offset {
						m.offset = m.cursor
					}
				}
			case tea.MouseButtonWheelDown:
				if m.cursor < len(m.items)-1 {
					m.cursor++
					vis := m.visibleHeight()
					if m.cursor >= m.offset+vis {
						m.offset = m.cursor - vis + 1
					}
				}
			}
		}
		return m, nil

	case tea.KeyMsg:
		// Search mode key handling
		if m.searching {
			switch msg.String() {
			case "esc":
				m.searching = false
				m.searchInput.SetValue("")
				m.searchInput.Blur()
				m.items = m.buildItemsWithDB(m.currentGroup())
				m.cursor = 0
				m.offset = 0
				return m, nil
			case "enter":
				m.searching = false
				m.searchInput.Blur()
				return m, nil
			case "ctrl+c":
				return m, func() tea.Msg { return RequestQuitMsg{} }
			default:
				var cmd tea.Cmd
				m.searchInput, cmd = m.searchInput.Update(msg)
				m.applySearch()
				return m, cmd
			}
		}

		// Normal mode key handling
		key := msg.String()
		switch key {
		case m.keys.Quit:
			return m, func() tea.Msg { return RequestQuitMsg{} }
		case "ctrl+c":
			return m, func() tea.Msg { return RequestQuitMsg{} }

		case m.keys.Search:
			m.searching = true
			m.allItems = collectAllEntries(m.db.RootGroup())
			sortItems(m.allItems, m.db.RecycleBinUUID(), m.sortBy)
			m.searchInput.SetValue("")
			m.searchInput.Focus()
			m.items = m.allItems
			m.cursor = 0
			m.offset = 0
			return m, textinput.Blink
		}

		switch key {
		case "up", m.keys.NavUp:
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < m.offset {
					m.offset = m.cursor
				}
			}

		case "down", m.keys.NavDown:
			if m.cursor < len(m.items)-1 {
				m.cursor++
				vis := m.visibleHeight()
				if m.cursor >= m.offset+vis {
					m.offset = m.cursor - vis + 1
				}
			}

		case "enter", m.keys.Open, "right":
			if len(m.items) == 0 {
				break
			}
			item := m.items[m.cursor]
			if item.isGroup {
				// Push current state and drill into group
				m.stack[len(m.stack)-1].cursor = m.cursor
				m.stack[len(m.stack)-1].offset = m.offset
				m.stack = append(m.stack, stackFrame{group: item.group})
				m.items = m.buildItemsWithDB(item.group)
				m.cursor = 0
				m.offset = 0
			} else {
				// View entry detail
				return m, func() tea.Msg {
					return ViewEntryMsg{Entry: item.entry}
				}
			}

		case "esc", m.keys.Back, "left":
			if m.searchQuery != "" {
				// Clear search results and go back to normal browsing
				m.searchQuery = ""
				m.items = m.buildItemsWithDB(m.currentGroup())
				m.cursor = 0
				m.offset = 0
				return m, nil
			}
			if len(m.stack) > 1 {
				m.stack = m.stack[:len(m.stack)-1]
				frame := m.stack[len(m.stack)-1]
				m.items = m.buildItemsWithDB(frame.group)
				m.cursor = frame.cursor
				m.offset = frame.offset
			}

		case m.keys.NewEntry:
			if m.readOnly {
				return m, func() tea.Msg { return StatusMsg{Text: "Read-only mode", IsError: true} }
			}
			if m.inRecycleBin() {
				break
			}
			// New entry
			cg := m.currentGroup()
			return m, func() tea.Msg {
				return NewEntryMsg{GroupUUID: cg.UUID}
			}

		case m.keys.NewGroup:
			if m.readOnly {
				return m, func() tea.Msg { return StatusMsg{Text: "Read-only mode", IsError: true} }
			}
			if m.inRecycleBin() {
				break
			}
			// New group
			cg := m.currentGroup()
			return m, func() tea.Msg {
				return NewGroupMsg{ParentUUID: cg.UUID}
			}

		case m.keys.RenameGroup:
			if m.readOnly {
				return m, func() tea.Msg { return StatusMsg{Text: "Read-only mode", IsError: true} }
			}
			// Rename group
			if len(m.items) == 0 {
				break
			}
			item := m.items[m.cursor]
			if item.isGroup {
				return m, func() tea.Msg {
					return RenameGroupStartMsg{GroupUUID: item.group.UUID, CurrentName: item.group.Name}
				}
			}

		case m.keys.Duplicate:
			if m.readOnly {
				return m, func() tea.Msg { return StatusMsg{Text: "Read-only mode", IsError: true} }
			}
			// Duplicate entry
			if len(m.items) == 0 {
				break
			}
			item := m.items[m.cursor]
			if !item.isGroup {
				return m, func() tea.Msg {
					return DuplicateEntryMsg{EntryUUID: item.entry.UUID}
				}
			}

		case m.keys.Cut:
			if m.readOnly {
				return m, func() tea.Msg { return StatusMsg{Text: "Read-only mode", IsError: true} }
			}
			// Cut entry or group for move
			if len(m.items) == 0 {
				break
			}
			item := m.items[m.cursor]
			if item.isGroup {
				uuid := item.group.UUID
				m.cutGroup = &uuid
				m.cutEntry = nil
				return m, func() tea.Msg {
					return StatusMsg{Text: fmt.Sprintf("Cut group: %s (navigate to target and press %s to paste)", item.group.Name, m.keys.Paste)}
				}
			} else {
				uuid := item.entry.UUID
				m.cutEntry = &uuid
				m.cutGroup = nil
				return m, func() tea.Msg {
					return StatusMsg{Text: fmt.Sprintf("Cut: %s (navigate to target and press %s to paste)", item.entry.Title, m.keys.Paste)}
				}
			}

		case m.keys.Paste:
			if m.readOnly {
				return m, func() tea.Msg { return StatusMsg{Text: "Read-only mode", IsError: true} }
			}
			// Paste cut entry or group into current group
			if m.cutEntry != nil {
				entryUUID := *m.cutEntry
				cg := m.currentGroup()
				return m, func() tea.Msg {
					return PasteEntryMsg{EntryUUID: entryUUID, TargetGroupUUID: cg.UUID}
				}
			}
			if m.cutGroup != nil {
				groupUUID := *m.cutGroup
				cg := m.currentGroup()
				return m, func() tea.Msg {
					return PasteGroupMsg{GroupUUID: groupUUID, TargetGroupUUID: cg.UUID}
				}
			}

		case m.keys.Delete:
			if m.readOnly {
				return m, func() tea.Msg { return StatusMsg{Text: "Read-only mode", IsError: true} }
			}
			if len(m.items) == 0 {
				break
			}
			item := m.items[m.cursor]
			name := item.name()
			if m.inRecycleBin() {
				// In recycle bin: permanently delete
				if item.isGroup {
					return m, func() tea.Msg {
						return PermanentlyDeleteGroupMsg{GroupUUID: item.group.UUID, Name: name}
					}
				}
				return m, func() tea.Msg {
					return PermanentlyDeleteEntryMsg{EntryUUID: item.entry.UUID, Name: name}
				}
			}
			if item.isGroup {
				return m, func() tea.Msg {
					return DeleteGroupMsg{GroupUUID: item.group.UUID, Name: name}
				}
			}
			return m, func() tea.Msg {
				return DeleteEntryMsg{EntryUUID: item.entry.UUID, Name: name}
			}

		case m.keys.Restore:
			// Restore from recycle bin
			if !m.inRecycleBin() || len(m.items) == 0 {
				break
			}
			item := m.items[m.cursor]
			if item.isGroup {
				return m, func() tea.Msg {
					return RestoreGroupMsg{GroupUUID: item.group.UUID}
				}
			}
			return m, func() tea.Msg {
				return RestoreEntryMsg{EntryUUID: item.entry.UUID}
			}

		case m.keys.EmptyRecycleBin:
			// Empty recycle bin
			if !m.inRecycleBin() {
				break
			}
			return m, func() tea.Msg {
				return EmptyRecycleBinMsg{}
			}

		case m.keys.ChangePassword:
			if m.readOnly {
				return m, func() tea.Msg { return StatusMsg{Text: "Read-only mode", IsError: true} }
			}
			return m, func() tea.Msg {
				return ChangePasswordStartMsg{}
			}

		case m.keys.ShowStats:
			return m, func() tea.Msg {
				return ShowStatsMsg{}
			}

		case m.keys.ShowAudit:
			return m, func() tea.Msg {
				return ShowAuditMsg{}
			}

		case m.keys.ShowSettings:
			return m, func() tea.Msg {
				return ShowSettingsMsg{}
			}

		case "?":
			return m, func() tea.Msg { return ShowHelpMsg{} }

		case m.keys.Favorite:
			if m.readOnly {
				return m, func() tea.Msg { return StatusMsg{Text: "Read-only mode", IsError: true} }
			}
			if len(m.items) == 0 {
				break
			}
			item := m.items[m.cursor]
			if !item.isGroup {
				entryUUID := item.entry.UUID
				return m, func() tea.Msg {
					return ToggleFavoriteMsg{EntryUUID: entryUUID}
				}
			}

		case m.keys.CycleSort:
			m.sortBy = (m.sortBy + 1) % sortModeCount
			if m.searchQuery != "" {
				sortItems(m.items, m.db.RecycleBinUUID(), m.sortBy)
			} else {
				m.items = m.buildItemsWithDB(m.currentGroup())
			}
			m.cursor = 0
			m.offset = 0

		case m.keys.FilterTag:
			m.allTags = collectTags(m.db.RootGroup())
			if len(m.allTags) == 0 {
				return m, func() tea.Msg { return StatusMsg{Text: "No tags found"} }
			}
			if m.tagFilter == "" {
				m.tagFilter = m.allTags[0]
			} else {
				// Advance to next tag, or clear
				idx := -1
				for i, t := range m.allTags {
					if t == m.tagFilter {
						idx = i
						break
					}
				}
				if idx >= 0 && idx < len(m.allTags)-1 {
					m.tagFilter = m.allTags[idx+1]
				} else {
					m.tagFilter = ""
				}
			}
			m.items = m.buildItemsWithDB(m.currentGroup())
			m.cursor = 0
			m.offset = 0

		case m.keys.Save, "ctrl+s":
			if m.readOnly {
				return m, func() tea.Msg { return StatusMsg{Text: "Read-only mode", IsError: true} }
			}
			return m, func() tea.Msg {
				return SaveAsStartMsg{}
			}
		}
	}

	return m, nil
}

func (m BrowserModel) View() string {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString(titleStyle.Render(" keetui "))
	b.WriteString("\n")
	if m.searchQuery != "" && !m.searching {
		b.WriteString(breadcrumbStyle.Render(fmt.Sprintf("Search: %q", m.searchQuery)))
	} else {
		bc := m.breadcrumb()
		var indicators []string
		if m.sortBy != sortByName {
			indicators = append(indicators, "sort: "+m.sortBy.String())
		}
		if m.tagFilter != "" {
			indicators = append(indicators, "tag: "+m.tagFilter)
		}
		if len(indicators) > 0 {
			bc += hiddenStyle.Render("  [" + strings.Join(indicators, ", ") + "]")
		}
		b.WriteString(breadcrumbStyle.Render(bc))
	}
	b.WriteString("\n\n")

	if m.searching {
		b.WriteString(m.searchInput.View())
		b.WriteString("\n\n")
	}

	if len(m.items) == 0 {
		if m.searchQuery != "" {
			b.WriteString(itemStyle.Render(hiddenStyle.Render("(no matches)")))
		} else {
			b.WriteString(itemStyle.Render(hiddenStyle.Render("(empty)")))
		}
		b.WriteString("\n")
	} else {
		vis := m.visibleHeight()
		end := m.offset + vis
		if end > len(m.items) {
			end = len(m.items)
		}
		for i := m.offset; i < end; i++ {
			item := m.items[i]
			cursor := "  "
			if i == m.cursor {
				cursor = "> "
			}

			var line string
			if item.isGroup {
				icon := groupStyle.Render("[+] ")
				if m.db.IsRecycleBin(item.group.UUID) {
					icon = groupStyle.Render("[x] ")
				}
				name := item.name()
				count := countEntries(item.group)
				suffix := hiddenStyle.Render(fmt.Sprintf(" (%d)", count))
				if i == m.cursor {
					line = fmt.Sprintf("%s%s%s%s", cursor, icon, selectedItemStyle.Render(name), suffix)
				} else {
					line = fmt.Sprintf("%s%s%s%s", cursor, icon, groupStyle.Render(name), suffix)
				}
			} else {
				icon := "    "
				if item.entry.IsExpired() {
					icon = warnStyle.Render("[!] ")
				} else if item.entry.IsFavorite() {
					icon = warnStyle.Render("[*] ")
				}
				name := item.name()
				if name == "" {
					name = "(untitled)"
				}
				if item.groupPath != "" {
					suffix := hiddenStyle.Render(fmt.Sprintf("  (%s)", item.groupPath))
					if i == m.cursor {
						line = fmt.Sprintf("%s%s%s%s", cursor, icon, selectedItemStyle.Render(name), suffix)
					} else {
						line = fmt.Sprintf("%s%s%s%s", cursor, icon, entryStyle.Render(name), suffix)
					}
				} else {
					if i == m.cursor {
						line = fmt.Sprintf("%s%s%s", cursor, icon, selectedItemStyle.Render(name))
					} else {
						line = fmt.Sprintf("%s%s%s", cursor, icon, entryStyle.Render(name))
					}
				}
			}
			b.WriteString(line)
			b.WriteString("\n")
		}

		if len(m.items) > vis {
			b.WriteString(fmt.Sprintf("\n%s",
				helpStyle.Render(fmt.Sprintf("  %d/%d items", m.cursor+1, len(m.items)))))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	if m.searching {
		b.WriteString(helpStyle.Render("  enter: confirm  esc: cancel"))
	} else if m.readOnly {
		help := fmt.Sprintf("  %s/%s: nav  enter/%s: open  %s/esc: back  %s: search  %s: sort  %s: tag  %s: info  %s: audit  %s: quit",
			m.keys.NavDown, m.keys.NavUp, m.keys.Open, m.keys.Back,
			m.keys.Search, m.keys.CycleSort, m.keys.FilterTag, m.keys.ShowStats, m.keys.ShowAudit, m.keys.Quit)
		b.WriteString(helpStyle.Render(help))
	} else if m.inRecycleBin() {
		help := fmt.Sprintf("  %s/%s: nav  enter/%s: open  %s/esc: back  %s: delete permanently  %s: restore  %s: empty bin  %s: save  %s: quit",
			m.keys.NavDown, m.keys.NavUp, m.keys.Open, m.keys.Back,
			m.keys.Delete, m.keys.Restore, m.keys.EmptyRecycleBin,
			m.keys.Save, m.keys.Quit)
		b.WriteString(helpStyle.Render(help))
	} else {
		help := fmt.Sprintf("  %s/%s: nav  enter/%s: open  %s/esc: back  %s: search  %s: sort  %s: tag  %s: new  %s: group  %s: rename  %s: dup  %s: fav  %s: cut  %s: paste  %s: del  %s: info  %s: audit  %s: config  %s: save  %s: password  %s: quit",
			m.keys.NavDown, m.keys.NavUp, m.keys.Open, m.keys.Back,
			m.keys.Search, m.keys.CycleSort, m.keys.FilterTag, m.keys.NewEntry, m.keys.NewGroup,
			m.keys.RenameGroup, m.keys.Duplicate, m.keys.Favorite, m.keys.Cut, m.keys.Paste,
			m.keys.Delete, m.keys.ShowStats, m.keys.ShowAudit,
			m.keys.ShowSettings, m.keys.Save, m.keys.ChangePassword, m.keys.Quit)
		b.WriteString(helpStyle.Render(help))
	}
	b.WriteString("\n")

	return b.String()
}
