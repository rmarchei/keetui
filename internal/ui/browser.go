package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tobischo/gokeepasslib/v3"

	"github.com/rmarchei/keetui/internal/keepass"
)

// listItem represents either a group or entry in the browser list.
type listItem struct {
	isGroup bool
	group   keepass.Group
	entry   keepass.Entry
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
	db     *keepass.DB
	stack  []stackFrame
	items  []listItem
	cursor int
	offset int
	width  int
	height int
	status string
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

// SaveMsg requests saving the database.
type SaveMsg struct{}

// RefreshMsg signals the browser to refresh its data from the DB.
type RefreshMsg struct{}

// StatusMsg updates the status bar.
type StatusMsg struct {
	Text    string
	IsError bool
}

func NewBrowserModel(db *keepass.DB) BrowserModel {
	root := db.RootGroup()
	m := BrowserModel{
		db: db,
	}
	m.items = buildItems(root)
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
	return items
}

func (m BrowserModel) currentGroup() keepass.Group {
	return m.stack[len(m.stack)-1].group
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
	if h < 5 {
		h = 5
	}
	return h
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
		m.items = buildItems(root)
		m.cursor = 0
		m.offset = 0
		return m, nil

	case StatusMsg:
		if msg.IsError {
			m.status = errorMsgStyle.Render(msg.Text)
		} else {
			m.status = successStyle.Render(msg.Text)
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < m.offset {
					m.offset = m.cursor
				}
			}

		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
				vis := m.visibleHeight()
				if m.cursor >= m.offset+vis {
					m.offset = m.cursor - vis + 1
				}
			}

		case "enter", "l", "right":
			if len(m.items) == 0 {
				break
			}
			item := m.items[m.cursor]
			if item.isGroup {
				// Push current state and drill into group
				m.stack[len(m.stack)-1].cursor = m.cursor
				m.stack[len(m.stack)-1].offset = m.offset
				m.stack = append(m.stack, stackFrame{group: item.group})
				m.items = buildItems(item.group)
				m.cursor = 0
				m.offset = 0
			} else {
				// View entry detail
				return m, func() tea.Msg {
					return ViewEntryMsg{Entry: item.entry}
				}
			}

		case "esc", "h", "left":
			if len(m.stack) > 1 {
				m.stack = m.stack[:len(m.stack)-1]
				frame := m.stack[len(m.stack)-1]
				m.items = buildItems(frame.group)
				m.cursor = frame.cursor
				m.offset = frame.offset
			}

		case "n":
			// New entry
			cg := m.currentGroup()
			return m, func() tea.Msg {
				return NewEntryMsg{GroupUUID: cg.UUID}
			}

		case "g":
			// New group
			cg := m.currentGroup()
			return m, func() tea.Msg {
				return NewGroupMsg{ParentUUID: cg.UUID}
			}

		case "d":
			if len(m.items) == 0 {
				break
			}
			item := m.items[m.cursor]
			if item.isGroup {
				return m, func() tea.Msg {
					return DeleteGroupMsg{GroupUUID: item.group.UUID}
				}
			}
			return m, func() tea.Msg {
				return DeleteEntryMsg{EntryUUID: item.entry.UUID}
			}

		case "s", "ctrl+s":
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
	b.WriteString(breadcrumbStyle.Render(m.breadcrumb()))
	b.WriteString("\n\n")

	if len(m.items) == 0 {
		b.WriteString(itemStyle.Render(hiddenStyle.Render("(empty)")))
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
				name := item.name()
				if i == m.cursor {
					line = fmt.Sprintf("%s%s%s", cursor, icon, selectedItemStyle.Render(name))
				} else {
					line = fmt.Sprintf("%s%s%s", cursor, icon, groupStyle.Render(name))
				}
			} else {
				icon := "    "
				name := item.name()
				if name == "" {
					name = "(untitled)"
				}
				if i == m.cursor {
					line = fmt.Sprintf("%s%s%s", cursor, icon, selectedItemStyle.Render(name))
				} else {
					line = fmt.Sprintf("%s%s%s", cursor, icon, entryStyle.Render(name))
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
	b.WriteString(helpStyle.Render("  j/k: navigate  enter/l: open  h/esc: back  n: new entry  g: new group  d: delete  s: save  q: quit"))
	b.WriteString("\n")

	return b.String()
}
