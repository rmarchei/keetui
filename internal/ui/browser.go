package ui

import (
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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
	inBin     bool   // true when the item lives inside the recycle bin (search results)
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
	searchFuzzy bool               // opt-in for the current query; new searches start literal
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
	Detail  string // guidance shown below the message in F1 help
}

func NewBrowserModel(db *keepass.DB, keys ...config.KeyBindings) BrowserModel {
	root := db.RootGroup()
	si := newTextInput()
	si.Prompt = "  / "
	setInputPromptStyle(&si, focusedStyle)
	si.SetWidth(40)
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
			for t := range strings.SplitSeq(e.Tags, ";") {
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
	for t := range strings.SplitSeq(e.Tags, ";") {
		if strings.TrimSpace(t) == tag {
			return true
		}
	}
	return false
}

// collectAllEntries recursively gathers all entries from a group tree.
func collectAllEntries(g keepass.Group, rbUUID gokeepasslib.UUID) []listItem {
	return collectAllEntriesWithPath(g, g.Name, rbUUID, false)
}

func collectAllEntriesWithPath(g keepass.Group, path string, rbUUID gokeepasslib.UUID, inBin bool) []listItem {
	var items []listItem
	for _, e := range g.Entries {
		items = append(items, listItem{isGroup: false, entry: e, groupPath: path, inBin: inBin})
	}
	for _, sg := range g.Groups {
		subPath := path + " > " + sg.Name
		items = append(items, collectAllEntriesWithPath(sg, subPath, rbUUID, inBin || sg.UUID == rbUUID)...)
	}
	return items
}

// itemInRecycleBin reports whether the item at the cursor is inside the
// recycle bin. During a search the flattened results span the whole tree,
// so membership comes from the item itself rather than the nav stack.
func (m BrowserModel) itemInRecycleBin(item listItem) bool {
	if m.searchQuery != "" {
		return item.inBin
	}
	return m.inRecycleBin()
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

// crumbs are the names of the groups from the root to the current one.
func (m BrowserModel) crumbs() []string {
	parts := make([]string, 0, len(m.stack))
	for _, f := range m.stack {
		parts = append(parts, f.group.Name)
	}
	return parts
}

// browserHeaderRows are the rows above the list: the header bar and a gap.
const browserHeaderRows = 2

func (m BrowserModel) Init() tea.Cmd {
	return nil
}

func (m BrowserModel) visibleHeight() int {
	_, height := viewSize(m.width, m.height)
	// Header rows, two position-counter rows, and measured hints.
	h := height - m.listStartRow() - 2 - lipgloss.Height(m.footerView())
	return max(1, h)
}

// fuzzyMatch returns true if all characters in pattern appear in s in order (case-insensitive).
func fuzzyMatch(s, pattern string) bool {
	if len(pattern) == 0 {
		return true
	}
	s = strings.ToLower(s)
	patRunes := []rune(strings.ToLower(pattern))
	pi := 0
	for _, r := range s {
		if r == patRunes[pi] {
			pi++
			if pi == len(patRunes) {
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
		// Literal substrings by default; opt-in fuzzy matches follow exact ones.
		var exact, fuzzy []listItem
		for _, item := range m.allItems {
			if entryMatchesSubstring(item, query) {
				exact = append(exact, item)
			} else if m.searchFuzzy && len(query) > 1 && entryMatchesFuzzy(item, query) {
				fuzzy = append(fuzzy, item)
			}
		}
		m.items = append(exact, fuzzy...)
	}
	m.cursor = 0
	m.offset = 0
}

func (m BrowserModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := m.update(msg)
	next := updated.(BrowserModel)
	next.cursor, next.offset = followList(next.cursor, next.offset, len(next.items), next.visibleHeight())
	return next, cmd
}

func (m BrowserModel) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		sizeInput(&m.searchInput, msg.Width)
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

		// Rebuild navigation stack from old group UUIDs through fresh root,
		// carrying over each frame's saved cursor/offset so popping back
		// out after a mutation returns to the same position.
		oldFrames := m.stack
		m.stack = []stackFrame{{group: root, cursor: oldFrames[0].cursor, offset: oldFrames[0].offset}}
		current := root
		for _, oldFrame := range oldFrames[1:] { // skip root (index 0)
			found := false
			for _, sg := range current.Groups {
				if sg.UUID == oldFrame.group.UUID {
					m.stack = append(m.stack, stackFrame{group: sg, cursor: oldFrame.cursor, offset: oldFrame.offset})
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
			m.allItems = collectAllEntries(root, m.db.RecycleBinUUID())
			sortItems(m.allItems, m.db.RecycleBinUUID(), m.sortBy)
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

		return m, nil

	case tea.MouseMsg:
		mouse := msg.Mouse()
		switch mouse.Button {
		case tea.MouseLeft:
			// The search input shifts the list, so clicks wait until the
			// search is committed.
			if _, click := msg.(tea.MouseClickMsg); !click || m.searching {
				break
			}
			start := m.listStartRow()
			idx := mouse.Y - start + m.offset
			if mouse.Y >= start && mouse.Y < start+m.visibleHeight() && idx >= 0 && idx < len(m.items) {
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
		case tea.MouseWheelUp:
			if m.cursor > 0 {
				m.cursor--
			}
		case tea.MouseWheelDown:
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		}
		return m, nil

	case tea.KeyPressMsg:
		// Search mode key handling
		if m.searching {
			switch msg.String() {
			case "tab":
				m.searchFuzzy = !m.searchFuzzy
				m.applySearch()
				return m, nil
			case "esc":
				// Fully leave search state: a stale searchQuery would
				// re-render the search header and resurrect the canceled
				// search on the next RefreshMsg.
				m.searching = false
				m.searchQuery = ""
				m.searchFuzzy = false
				m.searchInput.SetValue("")
				m.searchInput.Blur()
				m.items = m.buildItemsWithDB(m.currentGroup())
				m.cursor = 0
				m.offset = 0
				return m, nil
			case "enter":
				if strings.TrimSpace(m.searchInput.Value()) == "" {
					// Committing an empty search would strand the browser
					// in the flattened all-entries view; treat it as a
					// cancel instead.
					m.searching = false
					m.searchQuery = ""
					m.searchFuzzy = false
					m.searchInput.SetValue("")
					m.searchInput.Blur()
					m.items = m.buildItemsWithDB(m.currentGroup())
					m.cursor = 0
					m.offset = 0
					return m, nil
				}
				// Commit the search and open the selected result; back from
				// the entry returns to the committed results.
				m.searching = false
				m.searchInput.Blur()
				if len(m.items) == 0 {
					return m, nil
				}
				entry := m.items[m.cursor].entry
				return m, func() tea.Msg {
					return ViewEntryMsg{Entry: entry}
				}
			case "up":
				// Arrows move through the results while typing; printable
				// navigation keys (j/k) go to the query.
				if m.cursor > 0 {
					m.cursor--
				}
				return m, nil
			case "down":
				if m.cursor < len(m.items)-1 {
					m.cursor++
				}
				return m, nil
			case "ctrl+c":
				return m, func() tea.Msg { return RequestQuitMsg{} }
			default:
				cmd := m.updateSearchInput(msg)
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
			m.allItems = collectAllEntries(m.db.RootGroup(), m.db.RecycleBinUUID())
			sortItems(m.allItems, m.db.RecycleBinUUID(), m.sortBy)
			m.searchInput.SetValue("")
			m.searchQuery = ""
			m.searchFuzzy = false
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
			}

		case "down", m.keys.NavDown:
			if m.cursor < len(m.items)-1 {
				m.cursor++
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
				m.searchFuzzy = false
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
			if m.itemInRecycleBin(item) {
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
			if len(m.items) == 0 {
				break
			}
			item := m.items[m.cursor]
			if !m.itemInRecycleBin(item) {
				break
			}
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

		case m.keys.ShowSettings, "ctrl+o":
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

	// Forward paste results and cursor ticks as well as key presses.
	if m.searching {
		cmd := m.updateSearchInput(msg)
		return m, cmd
	}
	return m, nil
}

func (m *BrowserModel) updateSearchInput(msg tea.Msg) tea.Cmd {
	previous := m.searchInput.Value()
	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	// Cursor blinking should not rescan the entire database.
	if m.searchInput.Value() != previous {
		m.applySearch()
	}
	return cmd
}

func (m BrowserModel) View() tea.View {
	var b strings.Builder

	if m.searching || m.searchQuery != "" {
		b.WriteString(header(m.width, "", "Search"))
	} else {
		var indicators []string
		if m.sortBy != sortByName {
			indicators = append(indicators, "sort: "+m.sortBy.String())
		}
		if m.tagFilter != "" {
			indicators = append(indicators, "tag: "+m.tagFilter)
		}
		b.WriteString(header(m.width, strings.Join(indicators, "  "), m.crumbs()...))
	}
	b.WriteString("\n\n")

	if m.searching || m.searchQuery != "" {
		b.WriteString(m.searchView())
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
		end := min(m.offset+vis, len(m.items))
		cols := m.columns()
		for i := m.offset; i < end; i++ {
			b.WriteString(m.renderRow(i, cols))
			b.WriteString("\n")
		}

		if m.searchQuery != "" || len(m.items) > vis {
			position := fmt.Sprintf("  %d/%d items", m.cursor+1, len(m.items))
			if m.searchQuery != "" {
				position = fmt.Sprintf("  %d/%d", m.cursor+1, len(m.items))
				if field := searchMatchField(m.items[m.cursor], m.searchQuery, m.searchFuzzy); field != "" {
					position += " · matches " + field
				}
			}
			b.WriteString("\n" + helpStyle.Render(position) + "\n")
		}
	}

	return tea.NewView(screen(b.String(), m.footerView(), m.width, m.height, 0))
}

// browserColumns are the widths of a list row's columns, shared by every row
// so that they line up: title, then username (entry count for groups), then
// the URL's host or, in search results, the group path.
type browserColumns struct {
	title, user, extra int
}

func (m BrowserModel) columns() browserColumns {
	width, _ := viewSize(m.width, m.height)
	avail := width - 4 // selection marker and icon
	var title, user int
	for _, item := range m.items {
		title = max(title, lipgloss.Width(item.title()))
		user = max(user, lipgloss.Width(item.secondary()))
	}
	var cols browserColumns
	cols.title = min(title, max(12, avail*2/5))
	if rest := avail - cols.title - 2; rest >= 8 {
		cols.user = min(user, max(10, avail/4), rest)
		if extra := rest - cols.user - 2; cols.user > 0 && extra >= 10 {
			cols.extra = extra
		}
	}
	return cols
}

// renderRow draws item i: selection marker, icon, then the columns.
func (m BrowserModel) renderRow(i int, cols browserColumns) string {
	item := m.items[i]
	selected := i == m.cursor
	cursor := "  "
	if selected {
		cursor = selectionCursor
	}
	icon, nameStyle := "  ", entryStyle
	switch {
	case item.isGroup && m.db.IsRecycleBin(item.group.UUID):
		icon, nameStyle = groupIconStyle.Render("♲")+" ", groupStyle
	case item.isGroup:
		icon, nameStyle = groupIconStyle.Render("◆")+" ", groupStyle
	case item.entry.IsExpired():
		icon = errorMsgStyle.Padding(0).Render("!") + " "
	case item.entry.IsFavorite():
		icon = warnStyle.Padding(0).Render("★") + " "
	}
	if selected {
		nameStyle = selectedItemStyle
	}
	fuzzy := m.searchFuzzy && m.searchQuery != "" && !entryMatchesSubstring(item, m.searchQuery)
	row := cursor + icon + padCell(highlightSearch(item.title(), m.searchQuery, nameStyle, fuzzy), cols.title)
	if cols.user > 0 {
		style := secondaryStyle
		if item.isGroup {
			style = hiddenStyle
		}
		row += "  " + padCell(highlightSearch(item.secondary(), m.searchQuery, style, fuzzy), cols.user)
	}
	if cols.extra > 0 {
		extra := item.extra()
		if field := searchMatchField(item, m.searchQuery, m.searchFuzzy); field != "" && field != "title" && field != "username" {
			extra = "matches " + field + " · " + extra
		}
		row += "  " + hiddenStyle.Render(padCell(extra, cols.extra))
	}
	row = strings.TrimRight(row, " ")
	if selected {
		return selectedRow(row, m.width)
	}
	return row
}

// padCell fits s to exactly width cells.
func padCell(s string, width int) string {
	s = ansi.Truncate(s, width, "…")
	return s + strings.Repeat(" ", max(0, width-lipgloss.Width(s)))
}

func (i listItem) title() string {
	name := sanitizeDisplay(i.name())
	if name == "" && !i.isGroup {
		name = "(untitled)"
	}
	return name
}

// secondary is the username of an entry, or a group's entry count.
func (i listItem) secondary() string {
	if i.isGroup {
		n := countEntries(i.group)
		if n == 1 {
			return "1 entry"
		}
		return fmt.Sprintf("%d entries", n)
	}
	return sanitizeDisplay(i.entry.UserName)
}

// extra is the group path of a search result, or the host of an entry's URL.
func (i listItem) extra() string {
	if i.isGroup {
		return ""
	}
	if i.groupPath != "" {
		return sanitizeDisplay(i.groupPath)
	}
	return sanitizeDisplay(urlHost(i.entry.URL))
}

// urlHost shortens a URL to its host; text that is not a URL stays as is.
func urlHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return strings.TrimPrefix(u.Hostname(), "www.")
	}
	return raw
}

func (m BrowserModel) footerView() string {
	k := m.keys
	open := "enter/" + k.Open + ": open"
	back := k.Back + "/esc: back"
	nav := k.NavDown + "/" + k.NavUp + ": nav"
	var hints []string
	switch {
	case m.searching:
		mode := "fuzzy"
		if m.searchFuzzy {
			mode = "literal"
		}
		hints = []string{"enter: open", "esc: cancel", "F1: help", "tab: " + mode, "↑/↓: navigate"}
	case m.readOnly:
		hints = []string{"?: help", k.Search + ": search", open, back, k.ShowStats + ": info",
			k.ShowAudit + ": audit", k.Quit + ": quit", nav, k.CycleSort + ": sort", k.FilterTag + ": tag"}
	case m.inRecycleBin():
		hints = []string{"?: help", open, k.Restore + ": restore", k.Delete + ": delete permanently",
			k.EmptyRecycleBin + ": empty bin", back, k.Save + ": save", k.Quit + ": quit", nav}
	default:
		hints = []string{"?: help", k.Search + ": search", open, k.NewEntry + ": new", k.Delete + ": del",
			k.Cut + ": move", k.Save + ": save", k.Quit + ": quit", back, nav, k.Favorite + ": fav", k.NewGroup + ": group",
			k.ShowSettings + ": config", k.CycleSort + ": sort", k.FilterTag + ": tag", k.RenameGroup + ": rename",
			k.Duplicate + ": dup", k.ShowStats + ": info", k.ShowAudit + ": audit", k.ChangePassword + ": password"}
		if m.cutEntry != nil || m.cutGroup != nil {
			// Something is cut: pasting it is the next step.
			hints = slices.Insert(hints, 1, k.Paste+": paste here")
		}
	}
	return renderHints(m.width, hints...)
}
