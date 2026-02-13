package ui

import (
	"fmt"
	"os/exec"
	"runtime"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tobischo/gokeepasslib/v3"

	"github.com/rmarchei/keetui/internal/config"
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

// clipboardClearMsg signals that the clipboard should be cleared.
type clipboardClearMsg struct{}

// autoLockMsg signals that the database should be locked due to inactivity.
type autoLockMsg struct {
	at time.Time
}

// DuplicateEntryMsg requests duplicating an entry.
type DuplicateEntryMsg struct {
	EntryUUID gokeepasslib.UUID
}

// MoveEntryMsg requests cutting an entry to be pasted elsewhere.
type MoveEntryMsg struct {
	EntryUUID gokeepasslib.UUID
}

// PasteEntryMsg requests pasting the cut entry into the current group.
type PasteEntryMsg struct {
	EntryUUID     gokeepasslib.UUID
	TargetGroupUUID gokeepasslib.UUID
}

// RenameGroupMsg requests renaming a group.
type RenameGroupMsg struct {
	GroupUUID gokeepasslib.UUID
	NewName   string
}

// OpenURLMsg requests opening a URL in the system browser.
type OpenURLMsg struct {
	URL string
}

// AppModel is the top-level model coordinating all views.
type AppModel struct {
	state        viewState
	db           *keepass.DB
	unlock       UnlockModel
	browser      BrowserModel
	detail       DetailModel
	editor       EditorModel
	groupInput   GroupInputModel
	confirm      ConfirmModel
	width        int
	height       int
	pendingDel   interface{} // stores the UUID for pending delete
	lastActivity time.Time   // tracks last user interaction for auto-lock
	filePath     string      // remember file path for re-lock
	keyFilePath  string      // remember key file path for re-lock
	cfg          config.Config
}

// NewApp creates the application starting at the unlock screen.
func NewApp(filePath, keyFilePath string, cfg config.Config) AppModel {
	return AppModel{
		state:        viewUnlock,
		unlock:       NewUnlockModel(filePath, keyFilePath),
		lastActivity: time.Now(),
		filePath:     filePath,
		keyFilePath:  keyFilePath,
		cfg:          cfg,
	}
}

func (m AppModel) Init() tea.Cmd {
	return m.unlock.Init()
}

func (m AppModel) scheduleAutoLock() tea.Cmd {
	lastActivity := m.lastActivity
	return tea.Tick(m.cfg.AutoLockDuration(), func(t time.Time) tea.Msg {
		return autoLockMsg{at: lastActivity}
	})
}

func (m AppModel) scheduleClearClipboard() tea.Cmd {
	return tea.Tick(m.cfg.ClipboardClearDuration(), func(t time.Time) tea.Msg {
		return clipboardClearMsg{}
	})
}

func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle window size globally
	if wsm, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = wsm.Width
		m.height = wsm.Height
	}

	// Track user activity for auto-lock (any key press)
	if _, ok := msg.(tea.KeyMsg); ok && m.state != viewUnlock {
		m.lastActivity = time.Now()
	}

	// Handle cross-view messages first
	switch msg := msg.(type) {
	case DatabaseOpenedMsg:
		return m.handleDatabaseOpened(msg)

	case ViewEntryMsg:
		m.detail = NewDetailModel(msg.Entry)
		m.state = viewDetail
		return m, m.detail.Init()

	case EditEntryMsg:
		m.editor = NewEditorModel(&msg.Entry, gokeepasslib.UUID{}, m.cfg)
		m.state = viewEditor
		return m, nil

	case NewEntryMsg:
		m.editor = NewEditorModel(nil, msg.GroupUUID, m.cfg)
		m.state = viewEditor
		return m, m.editor.Init()

	case NewGroupMsg:
		m.groupInput = NewGroupInputModel(msg.ParentUUID, "")
		m.state = viewGroupInput
		return m, m.groupInput.Init()

	case RenameGroupMsg:
		if err := m.db.RenameGroup(msg.GroupUUID, msg.NewName); err != nil {
			m.state = viewBrowser
			return m, statusCmd(fmt.Sprintf("Error renaming group: %s", err), true)
		}
		m.state = viewBrowser
		return m, tea.Batch(
			func() tea.Msg { return RefreshMsg{} },
			statusCmd("Group renamed (use 's' to save to file)", false),
		)

	case RenameGroupStartMsg:
		m.groupInput = NewGroupInputModel(msg.GroupUUID, msg.CurrentName)
		m.groupInput.renaming = true
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

	case DuplicateEntryMsg:
		if err := m.db.DuplicateEntry(msg.EntryUUID); err != nil {
			return m, statusCmd(fmt.Sprintf("Error duplicating: %s", err), true)
		}
		return m, tea.Batch(
			func() tea.Msg { return RefreshMsg{} },
			statusCmd("Entry duplicated (use 's' to save to file)", false),
		)

	case PasteEntryMsg:
		if err := m.db.MoveEntry(msg.EntryUUID, msg.TargetGroupUUID); err != nil {
			return m, statusCmd(fmt.Sprintf("Error moving entry: %s", err), true)
		}
		m.browser.cutEntry = nil
		return m, tea.Batch(
			func() tea.Msg { return RefreshMsg{} },
			statusCmd("Entry moved (use 's' to save to file)", false),
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
		return m, m.scheduleClearClipboard()

	case clipboardClearMsg:
		clearClipboard()
		m.detail.copied = ""
		return m, nil

	case OpenURLMsg:
		openURL(msg.URL)
		return m, nil

	case autoLockMsg:
		// Only lock if no activity since the timer was set
		if m.state != viewUnlock && msg.at == m.lastActivity {
			m.db = nil
			m.unlock = NewUnlockModel(m.filePath, m.keyFilePath)
			m.unlock.err = fmt.Errorf("locked due to inactivity")
			m.state = viewUnlock
			return m, m.unlock.Init()
		}
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
	m.lastActivity = time.Now()
	return m, m.scheduleAutoLock()
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
	if msg.Rename {
		if err := m.db.RenameGroup(msg.ParentUUID, msg.Name); err != nil {
			m.state = viewBrowser
			return m, statusCmd(fmt.Sprintf("Error renaming group: %s", err), true)
		}
		m.state = viewBrowser
		return m, tea.Batch(
			func() tea.Msg { return RefreshMsg{} },
			statusCmd("Group renamed (use 's' to save to file)", false),
		)
	}
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

// RenameGroupStartMsg is sent to initiate group renaming.
type RenameGroupStartMsg struct {
	GroupUUID   gokeepasslib.UUID
	CurrentName string
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

func clearClipboard() {
	copyToClipboard("")
}

func openURL(u string) {
	if u == "" {
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "linux":
		cmd = exec.Command("xdg-open", u)
	default:
		return
	}
	cmd.Start()
}
