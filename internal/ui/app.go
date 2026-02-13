package ui

import (
	"fmt"
	"os/exec"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tobischo/gokeepasslib/v3"

	"github.com/rmarchei/keetui/internal/keepass"
)

type viewState int

const (
	viewUnlock viewState = iota
	viewBrowser
	viewDetail
	viewEditor
	viewGroupInput
	viewConfirm
)

// AppModel is the top-level model coordinating all views.
type AppModel struct {
	state       viewState
	db          *keepass.DB
	unlock      UnlockModel
	browser     BrowserModel
	detail      DetailModel
	editor      EditorModel
	groupInput  GroupInputModel
	confirm     ConfirmModel
	width       int
	height      int
	pendingDel  interface{} // stores the UUID for pending delete
}

// NewApp creates the application starting at the unlock screen.
func NewApp(filePath, keyFilePath string) AppModel {
	return AppModel{
		state:  viewUnlock,
		unlock: NewUnlockModel(filePath, keyFilePath),
	}
}

func (m AppModel) Init() tea.Cmd {
	return m.unlock.Init()
}

func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle window size globally
	if wsm, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = wsm.Width
		m.height = wsm.Height
	}

	// Handle cross-view messages first
	switch msg := msg.(type) {
	case DatabaseOpenedMsg:
		return m.handleDatabaseOpened(msg)

	case ViewEntryMsg:
		m.detail = NewDetailModel(msg.Entry)
		m.state = viewDetail
		return m, nil

	case EditEntryMsg:
		m.editor = NewEditorModel(&msg.Entry, gokeepasslib.UUID{})
		m.state = viewEditor
		return m, nil

	case NewEntryMsg:
		m.editor = NewEditorModel(nil, msg.GroupUUID)
		m.state = viewEditor
		return m, m.editor.Init()

	case NewGroupMsg:
		m.groupInput = NewGroupInputModel(msg.ParentUUID)
		m.state = viewGroupInput
		return m, m.groupInput.Init()

	case DeleteEntryMsg:
		m.pendingDel = msg.EntryUUID
		m.confirm = NewConfirmModel("Delete this entry?", func() tea.Msg {
			return confirmDeleteEntryMsg{UUID: msg.EntryUUID}
		})
		m.state = viewConfirm
		return m, nil

	case DeleteGroupMsg:
		m.pendingDel = msg.GroupUUID
		m.confirm = NewConfirmModel("Delete this group and all its contents?", func() tea.Msg {
			return confirmDeleteGroupMsg{UUID: msg.GroupUUID}
		})
		m.state = viewConfirm
		return m, nil

	case confirmDeleteEntryMsg:
		if err := m.db.DeleteEntry(msg.UUID); err != nil {
			m.state = viewBrowser
			return m, statusCmd(err.Error(), true)
		}
		m.state = viewBrowser
		return m, tea.Batch(
			func() tea.Msg { return RefreshMsg{} },
			statusCmd("Entry deleted", false),
		)

	case confirmDeleteGroupMsg:
		if err := m.db.DeleteGroup(msg.UUID); err != nil {
			m.state = viewBrowser
			return m, statusCmd(err.Error(), true)
		}
		m.state = viewBrowser
		return m, tea.Batch(
			func() tea.Msg { return RefreshMsg{} },
			statusCmd("Group deleted", false),
		)

	case SaveEntryMsg:
		return m.handleSaveEntry(msg)

	case SaveGroupMsg:
		return m.handleSaveGroup(msg)

	case SaveMsg:
		return m.handleSave()

	case BackToBrowserMsg:
		m.state = viewBrowser
		return m, func() tea.Msg { return RefreshMsg{} }

	case CancelEditMsg:
		m.state = viewBrowser
		return m, nil

	case CopyToClipboardMsg:
		copyToClipboard(msg.Value)
		m.detail.copied = msg.Field
		return m, nil
	}

	// Delegate to active view
	var cmd tea.Cmd
	switch m.state {
	case viewUnlock:
		var updated tea.Model
		updated, cmd = m.unlock.Update(msg)
		m.unlock = updated.(UnlockModel)
	case viewBrowser:
		var updated tea.Model
		updated, cmd = m.browser.Update(msg)
		m.browser = updated.(BrowserModel)
	case viewDetail:
		var updated tea.Model
		updated, cmd = m.detail.Update(msg)
		m.detail = updated.(DetailModel)
	case viewEditor:
		var updated tea.Model
		updated, cmd = m.editor.Update(msg)
		m.editor = updated.(EditorModel)
	case viewGroupInput:
		var updated tea.Model
		updated, cmd = m.groupInput.Update(msg)
		m.groupInput = updated.(GroupInputModel)
	case viewConfirm:
		var updated tea.Model
		updated, cmd = m.confirm.Update(msg)
		m.confirm = updated.(ConfirmModel)
	}

	return m, cmd
}

func (m AppModel) View() string {
	switch m.state {
	case viewUnlock:
		return m.unlock.View()
	case viewBrowser:
		return m.browser.View()
	case viewDetail:
		return m.detail.View()
	case viewEditor:
		return m.editor.View()
	case viewGroupInput:
		return m.groupInput.View()
	case viewConfirm:
		return m.confirm.View()
	}
	return ""
}

func (m AppModel) handleDatabaseOpened(msg DatabaseOpenedMsg) (tea.Model, tea.Cmd) {
	var db *keepass.DB
	var err error
	if msg.KeyFilePath != "" {
		db, err = keepass.OpenWithKeyFile(msg.FilePath, msg.Password, msg.KeyFilePath)
	} else {
		db, err = keepass.Open(msg.FilePath, msg.Password)
	}
	if err != nil {
		m.unlock.err = fmt.Errorf("failed to unlock: %w", err)
		return m, nil
	}
	m.db = db
	m.browser = NewBrowserModel(db)
	m.state = viewBrowser
	return m, nil
}

func (m AppModel) handleSaveEntry(msg SaveEntryMsg) (tea.Model, tea.Cmd) {
	if msg.IsNew {
		if err := m.db.AddEntry(msg.GroupUUID, msg.Entry); err != nil {
			m.state = viewBrowser
			return m, statusCmd(fmt.Sprintf("Error adding entry: %s", err), true)
		}
	} else {
		if err := m.db.UpdateEntry(msg.EntryUUID, msg.Entry); err != nil {
			m.state = viewBrowser
			return m, statusCmd(fmt.Sprintf("Error updating entry: %s", err), true)
		}
	}
	m.state = viewBrowser
	return m, tea.Batch(
		func() tea.Msg { return RefreshMsg{} },
		statusCmd("Entry saved (use 's' to save to file)", false),
	)
}

func (m AppModel) handleSaveGroup(msg SaveGroupMsg) (tea.Model, tea.Cmd) {
	if err := m.db.AddGroup(msg.ParentUUID, msg.Name); err != nil {
		m.state = viewBrowser
		return m, statusCmd(fmt.Sprintf("Error adding group: %s", err), true)
	}
	m.state = viewBrowser
	return m, tea.Batch(
		func() tea.Msg { return RefreshMsg{} },
		statusCmd("Group created (use 's' to save to file)", false),
	)
}

func (m AppModel) handleSave() (tea.Model, tea.Cmd) {
	if err := m.db.Save(); err != nil {
		return m, statusCmd(fmt.Sprintf("Error saving: %s", err), true)
	}
	return m, statusCmd("Database saved!", false)
}

func statusCmd(text string, isError bool) tea.Cmd {
	return func() tea.Msg {
		return StatusMsg{Text: text, IsError: isError}
	}
}

// Internal confirm messages
type confirmDeleteEntryMsg struct {
	UUID gokeepasslib.UUID
}

type confirmDeleteGroupMsg struct {
	UUID gokeepasslib.UUID
}

func copyToClipboard(text string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		// Try xclip first, then xsel, then wl-copy (Wayland)
		if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		} else if _, err := exec.LookPath("xsel"); err == nil {
			cmd = exec.Command("xsel", "--clipboard", "--input")
		} else if _, err := exec.LookPath("wl-copy"); err == nil {
			cmd = exec.Command("wl-copy")
		} else {
			return
		}
	default:
		return
	}
	cmd.Stdin = nil
	pipe, err := cmd.StdinPipe()
	if err != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		return
	}
	pipe.Write([]byte(text))
	pipe.Close()
	cmd.Wait()
}
