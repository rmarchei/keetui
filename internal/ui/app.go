package ui

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
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
	viewPassword
	viewStats
	viewSettings
)

// clipboardClearMsg signals that the clipboard should be cleared.
type clipboardClearMsg struct {
	expected string // the value we expect to still be on the clipboard
}

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
	EntryUUID       gokeepasslib.UUID
	TargetGroupUUID gokeepasslib.UUID
}

// PasteGroupMsg requests pasting the cut group into the current group.
type PasteGroupMsg struct {
	GroupUUID       gokeepasslib.UUID
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

// RequestQuitMsg is sent by sub-views when the user presses q.
// The AppModel checks for unsaved changes before actually quitting.
type RequestQuitMsg struct{}

// quitMsg is sent when the user confirms quitting with unsaved changes.
type quitMsg struct{}

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
	password     PasswordModel
	stats        StatsModel
	settings     SettingsModel
	width        int
	height       int
	lastActivity time.Time // tracks last user interaction for auto-lock
	filePath     string    // remember file path for re-lock
	keyFilePath  string    // remember key file path for re-lock
	cfg          config.Config
	dirty        bool      // true when in-memory changes haven't been saved
	returnState  viewState // view to return to after confirm dialog
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
	d := m.cfg.AutoLockDuration()
	if d == 0 {
		return nil
	}
	lastActivity := m.lastActivity
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return autoLockMsg{at: lastActivity}
	})
}

func (m AppModel) scheduleClearClipboard(copiedValue string) tea.Cmd {
	d := m.cfg.ClipboardClearDuration()
	if d == 0 {
		return nil
	}
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return clipboardClearMsg{expected: copiedValue}
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
	case RequestQuitMsg:
		if m.dirty {
			m.returnState = m.state
			m.confirm = NewConfirmModel("You have unsaved changes. Quit anyway?", func() tea.Msg {
				return quitMsg{}
			})
			m.state = viewConfirm
			return m, nil
		}
		return m, tea.Quit

	case quitMsg:
		return m, tea.Quit

	case DatabaseOpenedMsg:
		return m.handleDatabaseOpened(msg)

	case ViewEntryMsg:
		m.detail = NewDetailModel(msg.Entry)
		if m.db != nil {
			m.detail.SetAttachments(m.db.EntryAttachments(msg.Entry.UUID))
		}
		m.detail.SetSize(m.width, m.height)
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
		m.dirty = true
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
		m.confirm = NewConfirmModel("Delete this entry?", func() tea.Msg {
			return confirmDeleteEntryMsg{UUID: msg.EntryUUID}
		})
		m.state = viewConfirm
		return m, nil

	case DeleteGroupMsg:
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
		m.dirty = true
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
		m.dirty = true
		m.state = viewBrowser
		return m, tea.Batch(
			func() tea.Msg { return RefreshMsg{} },
			statusCmd("Group deleted", false),
		)

	case DuplicateEntryMsg:
		if err := m.db.DuplicateEntry(msg.EntryUUID); err != nil {
			return m, statusCmd(fmt.Sprintf("Error duplicating: %s", err), true)
		}
		m.dirty = true
		return m, tea.Batch(
			func() tea.Msg { return RefreshMsg{} },
			statusCmd("Entry duplicated (use 's' to save to file)", false),
		)

	case PasteEntryMsg:
		if err := m.db.MoveEntry(msg.EntryUUID, msg.TargetGroupUUID); err != nil {
			return m, statusCmd(fmt.Sprintf("Error moving entry: %s", err), true)
		}
		m.dirty = true
		m.browser.cutEntry = nil
		return m, tea.Batch(
			func() tea.Msg { return RefreshMsg{} },
			statusCmd("Entry moved (use 's' to save to file)", false),
		)

	case PasteGroupMsg:
		if err := m.db.MoveGroup(msg.GroupUUID, msg.TargetGroupUUID); err != nil {
			return m, statusCmd(fmt.Sprintf("Error moving group: %s", err), true)
		}
		m.dirty = true
		m.browser.cutGroup = nil
		return m, tea.Batch(
			func() tea.Msg { return RefreshMsg{} },
			statusCmd("Group moved (use 's' to save to file)", false),
		)

	case ChangePasswordStartMsg:
		m.password = NewPasswordModel()
		m.state = viewPassword
		return m, m.password.Init()

	case ChangePasswordMsg:
		m.db.ChangePassword(msg.NewPassword)
		m.dirty = true
		m.state = viewBrowser
		return m, statusCmd("Master password changed (use 's' to save to file)", false)

	case ShowStatsMsg:
		m.stats = NewStatsModel(m.db.Stats())
		m.state = viewStats
		return m, nil

	case ShowSettingsMsg:
		m.settings = NewSettingsModel(m.cfg)
		m.state = viewSettings
		return m, m.settings.Init()

	case ConfigSavedMsg:
		m.cfg = msg.Cfg
		m.lastActivity = time.Now()
		m.state = viewBrowser
		return m, tea.Batch(
			statusCmd("Settings saved to "+config.Path(), false),
			m.scheduleAutoLock(),
		)

	case hibpResultMsg:
		m.detail.hibpResult = &msg
		m.detail.hibpChecking = false
		m.detail.RefreshContent()
		return m, nil

	case ExtractAttachmentMsg:
		if err := m.db.ExtractAttachment(msg.EntryUUID, msg.Name, msg.DestPath); err != nil {
			return m, statusCmd(fmt.Sprintf("Error extracting attachment: %s", err), true)
		}
		m.detail.copied = ""
		m.detail.RefreshContent()
		return m, statusCmd(fmt.Sprintf("Attachment saved to %s", msg.DestPath), false)

	case DeleteAttachmentMsg:
		m.confirm = NewConfirmModel(fmt.Sprintf("Delete attachment %q?", msg.Name), func() tea.Msg {
			return confirmDeleteAttachmentMsg{EntryUUID: msg.EntryUUID, Name: msg.Name}
		})
		m.returnState = viewDetail
		m.state = viewConfirm
		return m, nil

	case confirmDeleteAttachmentMsg:
		if err := m.db.DeleteAttachment(msg.EntryUUID, msg.Name); err != nil {
			m.state = viewDetail
			return m, statusCmd(fmt.Sprintf("Error deleting attachment: %s", err), true)
		}
		m.dirty = true
		// Refresh attachments in detail view
		m.detail.attachments = m.db.EntryAttachments(msg.EntryUUID)
		if m.detail.attachIdx >= len(m.detail.attachments) {
			m.detail.attachIdx = len(m.detail.attachments) - 1
			if m.detail.attachIdx < 0 {
				m.detail.attachIdx = 0
				m.detail.attachMode = false
			}
		}
		m.detail.RefreshContent()
		m.state = viewDetail
		return m, statusCmd("Attachment deleted (use 's' to save to file)", false)

	case AddAttachmentMsg:
		if err := m.db.AddAttachmentFile(msg.EntryUUID, msg.FilePath); err != nil {
			return m, statusCmd(fmt.Sprintf("Error adding attachment: %s", err), true)
		}
		m.dirty = true
		// Refresh attachments in detail view
		m.detail.attachments = m.db.EntryAttachments(msg.EntryUUID)
		m.detail.attachMode = true
		m.detail.attachIdx = len(m.detail.attachments) - 1
		m.detail.RefreshContent()
		return m, statusCmd("Attachment added (use 's' to save to file)", false)

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
		if m.returnState != 0 {
			m.state = m.returnState
			m.returnState = 0
		} else {
			m.state = viewBrowser
		}
		return m, nil

	case CopyToClipboardMsg:
		if err := copyToClipboard(msg.Value); err != nil {
			m.detail.copied = ""
			m.detail.RefreshContent()
			return m, statusCmd(fmt.Sprintf("Clipboard error: %s", err), true)
		}
		m.detail.copied = msg.Field
		m.detail.RefreshContent()
		return m, m.scheduleClearClipboard(msg.Value)

	case clipboardClearMsg:
		current, err := readClipboard()
		if err == nil && current == msg.expected {
			clearClipboard()
		}
		m.detail.copied = ""
		m.detail.RefreshContent()
		return m, nil

	case OpenURLMsg:
		if err := openURL(msg.URL); err != nil {
			return m, statusCmd(fmt.Sprintf("Error: %s", err), true)
		}
		return m, nil

	case autoLockMsg:
		// Only lock if no activity since the timer was set
		if m.state != viewUnlock && msg.at == m.lastActivity {
			if m.dirty && m.db != nil {
				_ = m.db.Save()
				m.dirty = false
			}
			clearClipboard()
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
	case viewPassword:
		var updated tea.Model
		updated, cmd = m.password.Update(msg)
		m.password = updated.(PasswordModel)
	case viewStats:
		var updated tea.Model
		updated, cmd = m.stats.Update(msg)
		m.stats = updated.(StatsModel)
	case viewSettings:
		var updated tea.Model
		updated, cmd = m.settings.Update(msg)
		m.settings = updated.(SettingsModel)
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
	case viewPassword:
		return m.password.View()
	case viewStats:
		return m.stats.View()
	case viewSettings:
		return m.settings.View()
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
	m.unlock.input.SetValue("")
	m.unlock.input.Reset()
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
	m.dirty = true
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
		m.dirty = true
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
	m.dirty = true
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
	m.dirty = false
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

type confirmDeleteAttachmentMsg struct {
	EntryUUID gokeepasslib.UUID
	Name      string
}

// RenameGroupStartMsg is sent to initiate group renaming.
type RenameGroupStartMsg struct {
	GroupUUID   gokeepasslib.UUID
	CurrentName string
}

// ChangePasswordStartMsg is sent to initiate master password change.
type ChangePasswordStartMsg struct{}

func copyToClipboard(text string) error {
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
			return fmt.Errorf("no clipboard utility found (install xclip, xsel, or wl-copy)")
		}
	default:
		return fmt.Errorf("clipboard not supported on %s", runtime.GOOS)
	}
	cmd.Stdin = nil
	pipe, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("clipboard: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("clipboard: %w", err)
	}
	if _, err := pipe.Write([]byte(text)); err != nil {
		return fmt.Errorf("clipboard write: %w", err)
	}
	pipe.Close()
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("clipboard: %w", err)
	}
	return nil
}

func readClipboard() (string, error) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbpaste")
	case "linux":
		if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard", "-o")
		} else if _, err := exec.LookPath("xsel"); err == nil {
			cmd = exec.Command("xsel", "--clipboard", "--output")
		} else if _, err := exec.LookPath("wl-paste"); err == nil {
			cmd = exec.Command("wl-paste", "--no-newline")
		} else {
			return "", fmt.Errorf("no clipboard utility found")
		}
	default:
		return "", fmt.Errorf("clipboard not supported on %s", runtime.GOOS)
	}
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("clipboard read: %w", err)
	}
	return string(out), nil
}

func clearClipboard() {
	_ = copyToClipboard("")
}

func openURL(rawURL string) error {
	if rawURL == "" {
		return nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("unsupported URL scheme %q: only http and https are allowed", scheme)
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "linux":
		cmd = exec.Command("xdg-open", rawURL)
	default:
		return fmt.Errorf("open URL not supported on %s", runtime.GOOS)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open URL: %w", err)
	}
	return nil
}
