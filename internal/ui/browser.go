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

// BrowserModel handles navigation through groups and entries.
type BrowserModel struct {
	db          *keepass.DB
	stack       []stackFrame
	items       []listItem
	cursor      int
	offset      int
	width       int
	height      int
	status      string
	searching   bool
	searchInput textinput.Model
	searchQuery string
	allItems    []listItem         // unfiltered items for search
	cutEntry    *gokeepasslib.UUID // entry UUID in the clipboard for move
	cutGroup    *gokeepasslib.UUID // group UUID in the clipboard for move
	keys        config.KeyBindings
	readOnly    bool
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
}

// DeleteGroupMsg requests deleting a group.
type DeleteGroupMsg struct {
	GroupUUID gokeepasslib.UUID
}

// PermanentlyDeleteEntryMsg requests permanent deletion of an entry.
type PermanentlyDeleteEntryMsg struct {
	EntryUUID gokeepasslib.UUID
}

// PermanentlyDeleteGroupMsg requests permanent deletion of a group.
type PermanentlyDeleteGroupMsg struct {
	GroupUUID gokeepasslib.UUID
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

// SaveMsg requests saving the database.
type SaveMsg struct{}

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

func buildItems(g keepass.Group) []listItem {
	var items []listItem
	for _, sg := range g.Groups {
		items = append(items, listItem{isGroup: true, group: sg})
	}
	for _, e := range g.Entries {
		items = append(items, listItem{isGroup: false, entry: e})
	}
	sortItems(items)
	return items
}

// recycleBinUUID is set during sort when a DB reference is available.
var recycleBinUUID gokeepasslib.UUID

func (m BrowserModel) buildItemsWithDB(g keepass.Group) []listItem {
	recycleBinUUID = m.db.RecycleBinUUID()
	return buildItems(g)
}

func sortItems(items []listItem) {
	sort.SliceStable(items, func(i, j int) bool {
		// Groups first, then entries; alphabetical within each category
		if items[i].isGroup != items[j].isGroup {
			return items[i].isGroup
		}
		// Recycle bin group sorts last among groups
		if items[i].isGroup && items[j].isGroup {
			iIsRB := items[i].group.UUID.Compare(recycleBinUUID)
			jIsRB := items[j].group.UUID.Compare(recycleBinUUID)
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
		return strings.ToLower(items[i].name()) < strings.ToLower(items[j].name())
	})
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

func (m *BrowserModel) applySearch() {
	query := strings.ToLower(m.searchInput.Value())
	m.searchQuery = query
	if query == "" {
		m.items = m.allItems
	} else {
		var filtered []listItem
		for _, item := range m.allItems {
			n := strings.ToLower(item.name())
			u := ""
			if !item.isGroup {
				u = strings.ToLower(item.entry.UserName)
			}
			if strings.Contains(n, query) || strings.Contains(u, query) {
				filtered = append(filtered, item)
			}
		}
		m.items = filtered
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
		// Rebuild the stack from UUIDs
		m.stack = []stackFrame{{group: root, cursor: 0, offset: 0}}
		if m.searchQuery != "" {
			// Re-run the search to preserve results
			m.allItems = collectAllEntries(root)
			sortItems(m.allItems)
			m.searchInput.SetValue(m.searchQuery)
			m.applySearch()
		} else {
			m.items = m.buildItemsWithDB(root)
			m.cursor = 0
			m.offset = 0
			m.searching = false
			m.searchInput.SetValue("")
		}
		return m, nil

	case StatusMsg:
		if msg.IsError {
			m.status = errorMsgStyle.Render(msg.Text)
		} else {
			m.status = successStyle.Render(msg.Text)
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
			sortItems(m.allItems)
			m.searchInput.SetValue("")
			m.searchInput.Focus()
			m.items = m.allItems
			m.cursor = 0
			m.offset = 0
			return m, textinput.Blink
		}

		switch {
		case key == "up" || key == m.keys.NavUp:
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < m.offset {
					m.offset = m.cursor
				}
			}

		case key == "down" || key == m.keys.NavDown:
			if m.cursor < len(m.items)-1 {
				m.cursor++
				vis := m.visibleHeight()
				if m.cursor >= m.offset+vis {
					m.offset = m.cursor - vis + 1
				}
			}

		case key == "enter" || key == m.keys.Open || key == "right":
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

		case key == "esc" || key == m.keys.Back || key == "left":
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

		case key == m.keys.NewEntry:
			if m.readOnly {
				m.status = errorMsgStyle.Render("Read-only mode")
				break
			}
			if m.inRecycleBin() {
				break
			}
			// New entry
			cg := m.currentGroup()
			return m, func() tea.Msg {
				return NewEntryMsg{GroupUUID: cg.UUID}
			}

		case key == m.keys.NewGroup:
			if m.readOnly {
				m.status = errorMsgStyle.Render("Read-only mode")
				break
			}
			if m.inRecycleBin() {
				break
			}
			// New group
			cg := m.currentGroup()
			return m, func() tea.Msg {
				return NewGroupMsg{ParentUUID: cg.UUID}
			}

		case key == m.keys.RenameGroup:
			if m.readOnly {
				m.status = errorMsgStyle.Render("Read-only mode")
				break
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

		case key == m.keys.Duplicate:
			if m.readOnly {
				m.status = errorMsgStyle.Render("Read-only mode")
				break
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

		case key == m.keys.Cut:
			if m.readOnly {
				m.status = errorMsgStyle.Render("Read-only mode")
				break
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
				m.status = successStyle.Render(fmt.Sprintf("Cut group: %s (navigate to target group and press %s to paste)", item.group.Name, m.keys.Paste))
			} else {
				uuid := item.entry.UUID
				m.cutEntry = &uuid
				m.cutGroup = nil
				m.status = successStyle.Render(fmt.Sprintf("Cut: %s (navigate to target group and press %s to paste)", item.entry.Title, m.keys.Paste))
			}

		case key == m.keys.Paste:
			if m.readOnly {
				m.status = errorMsgStyle.Render("Read-only mode")
				break
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

		case key == m.keys.Delete:
			if m.readOnly {
				m.status = errorMsgStyle.Render("Read-only mode")
				break
			}
			if len(m.items) == 0 {
				break
			}
			item := m.items[m.cursor]
			if m.inRecycleBin() {
				// In recycle bin: permanently delete
				if item.isGroup {
					return m, func() tea.Msg {
						return PermanentlyDeleteGroupMsg{GroupUUID: item.group.UUID}
					}
				}
				return m, func() tea.Msg {
					return PermanentlyDeleteEntryMsg{EntryUUID: item.entry.UUID}
				}
			}
			if item.isGroup {
				return m, func() tea.Msg {
					return DeleteGroupMsg{GroupUUID: item.group.UUID}
				}
			}
			return m, func() tea.Msg {
				return DeleteEntryMsg{EntryUUID: item.entry.UUID}
			}

		case key == m.keys.Restore:
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

		case key == m.keys.EmptyRecycleBin:
			// Empty recycle bin
			if !m.inRecycleBin() {
				break
			}
			return m, func() tea.Msg {
				return EmptyRecycleBinMsg{}
			}

		case key == m.keys.ChangePassword:
			if m.readOnly {
				m.status = errorMsgStyle.Render("Read-only mode")
				break
			}
			return m, func() tea.Msg {
				return ChangePasswordStartMsg{}
			}

		case key == m.keys.ShowStats:
			return m, func() tea.Msg {
				return ShowStatsMsg{}
			}

		case key == m.keys.ShowAudit:
			return m, func() tea.Msg {
				return ShowAuditMsg{}
			}

		case key == m.keys.ShowSettings:
			return m, func() tea.Msg {
				return ShowSettingsMsg{}
			}

		case key == "?":
			return m, func() tea.Msg { return ShowHelpMsg{} }

		case key == m.keys.Favorite:
			if m.readOnly {
				m.status = errorMsgStyle.Render("Read-only mode")
				break
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

		case key == m.keys.Save || key == "ctrl+s":
			if m.readOnly {
				m.status = errorMsgStyle.Render("Read-only mode")
				break
			}
			return m, func() tea.Msg {
				return SaveMsg{}
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
		b.WriteString(breadcrumbStyle.Render(m.breadcrumb()))
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
				if i == m.cursor {
					line = fmt.Sprintf("%s%s%s", cursor, icon, selectedItemStyle.Render(name))
				} else {
					line = fmt.Sprintf("%s%s%s", cursor, icon, groupStyle.Render(name))
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
	if m.status != "" {
		b.WriteString(m.status)
		b.WriteString("\n")
	}
	if m.searching {
		b.WriteString(helpStyle.Render("  enter: confirm  esc: cancel"))
	} else if m.readOnly {
		help := fmt.Sprintf("  %s/%s: nav  enter/%s: open  %s/esc: back  %s: search  %s: info  %s: audit  %s: quit",
			m.keys.NavDown, m.keys.NavUp, m.keys.Open, m.keys.Back,
			m.keys.Search, m.keys.ShowStats, m.keys.ShowAudit, m.keys.Quit)
		b.WriteString(helpStyle.Render(help))
	} else if m.inRecycleBin() {
		help := fmt.Sprintf("  %s/%s: nav  enter/%s: open  %s/esc: back  %s: delete permanently  %s: restore  %s: empty bin  %s: save  %s: quit",
			m.keys.NavDown, m.keys.NavUp, m.keys.Open, m.keys.Back,
			m.keys.Delete, m.keys.Restore, m.keys.EmptyRecycleBin,
			m.keys.Save, m.keys.Quit)
		b.WriteString(helpStyle.Render(help))
	} else {
		help := fmt.Sprintf("  %s/%s: nav  enter/%s: open  %s/esc: back  %s: search  %s: new  %s: group  %s: rename  %s: dup  %s: fav  %s: cut  %s: paste  %s: del  %s: info  %s: audit  %s: config  %s: save  %s: password  %s: quit",
			m.keys.NavDown, m.keys.NavUp, m.keys.Open, m.keys.Back,
			m.keys.Search, m.keys.NewEntry, m.keys.NewGroup,
			m.keys.RenameGroup, m.keys.Duplicate, m.keys.Favorite, m.keys.Cut, m.keys.Paste,
			m.keys.Delete, m.keys.ShowStats, m.keys.ShowAudit,
			m.keys.ShowSettings, m.keys.Save, m.keys.ChangePassword, m.keys.Quit)
		b.WriteString(helpStyle.Render(help))
	}
	b.WriteString("\n")

	return b.String()
}
