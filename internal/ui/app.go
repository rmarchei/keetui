package ui

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
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
	viewHistory
	viewAudit
	viewHelp
	viewPathInput
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

// ToggleFavoriteMsg requests toggling the favorite status of an entry.
type ToggleFavoriteMsg struct {
	EntryUUID gokeepasslib.UUID
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
	history      HistoryModel
	audit        AuditModel
	help         HelpModel
	pathInput    PathInputModel
	width        int
	height       int
	lastActivity time.Time // tracks last user interaction for auto-lock
	filePath     string    // remember file path for re-lock
	keyFilePath  string    // remember key file path for re-lock
	cfg          config.Config
	dirty        bool      // true when in-memory changes haven't been saved
	returnState  viewState // view to return to after confirm dialog
	entryCount   int       // cached entry count for status bar
	undoStack    undoStack // undo history
	readOnly     bool      // when true, all mutations are blocked
	statusText   string    // transient status message for status bar
	statusError  bool      // true if status message is an error
}

// NewApp creates the application starting at the unlock screen.
func NewApp(filePath, keyFilePath string, cfg config.Config, readOnly ...bool) AppModel {
	ro := false
	if len(readOnly) > 0 {
		ro = readOnly[0]
	}
	ApplyTheme(GetTheme(cfg.Theme()))
	return AppModel{
		state:        viewUnlock,
		unlock:       NewUnlockModel(filePath, keyFilePath),
		lastActivity: time.Now(),
		filePath:     filePath,
		keyFilePath:  keyFilePath,
		cfg:          cfg,
		readOnly:     ro,
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
		// Reduce height for sub-views to make room for the status bar
		if m.state != viewUnlock {
			msg = tea.WindowSizeMsg{Width: wsm.Width, Height: wsm.Height - 1}
		}
	}

	// Track user activity for auto-lock (any key press or mouse event)
	if m.state != viewUnlock {
		switch msg.(type) {
		case tea.KeyMsg, tea.MouseMsg:
			m.lastActivity = time.Now()
			m.statusText = "" // clear transient status on interaction
		}
	}

	// Handle StatusMsg at the app level (show in status bar)
	if sm, ok := msg.(StatusMsg); ok {
		m.statusText = sm.Text
		m.statusError = sm.IsError
		return m, nil
	}

	// Handle undo (ctrl+z) in non-text-input views
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "ctrl+z" {
		if m.readOnly {
			return m, statusCmd("Read-only mode", true)
		}
		switch m.state {
		case viewBrowser, viewDetail, viewStats, viewHistory, viewAudit:
			return m.handleUndo()
		}
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
		if m.cfg.ConfirmOnQuit() {
			m.returnState = m.state
			m.confirm = NewConfirmModel("Quit?", func() tea.Msg {
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
		m.detail = NewDetailModel(msg.Entry, m.cfg)
		m.detail.readOnly = m.readOnly
		if m.db != nil {
			m.detail.SetAttachments(m.db.EntryAttachments(msg.Entry.UUID))
		}
		m.detail.SetSize(m.width, m.viewHeight())
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
		if m.readOnly {
			m.state = viewBrowser
			return m, statusCmd("Read-only mode", true)
		}
		rawGroup, _ := m.db.GetRawGroup(msg.GroupUUID)
		oldName := ""
		if rawGroup != nil {
			oldName = rawGroup.Name
		}
		if err := m.db.RenameGroup(msg.GroupUUID, msg.NewName); err != nil {
			m.state = viewBrowser
			return m, statusCmd(fmt.Sprintf("Error renaming group: %s", err), true)
		}
		if oldName != "" && oldName != msg.NewName {
			m.undoStack.push(undoAction{
				actionType:  undoRenameGroup,
				description: fmt.Sprintf("Rename group '%s' to '%s'", oldName, msg.NewName),
				groupUUID:   msg.GroupUUID,
				oldName:     oldName,
			})
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
		m.confirm = NewConfirmModel(fmt.Sprintf("Delete entry '%s'?", msg.Name), func() tea.Msg {
			return confirmDeleteEntryMsg{UUID: msg.EntryUUID}
		})
		m.state = viewConfirm
		return m, nil

	case DeleteGroupMsg:
		m.confirm = NewConfirmModel(fmt.Sprintf("Delete group '%s' and all its contents?", msg.Name), func() tea.Msg {
			return confirmDeleteGroupMsg{UUID: msg.GroupUUID}
		})
		m.state = viewConfirm
		return m, nil

	case confirmDeleteEntryMsg:
		if m.readOnly {
			m.state = viewBrowser
			return m, statusCmd("Read-only mode", true)
		}
		// Capture undo state before deletion
		parentUUID, _ := m.db.FindEntryParentUUID(msg.UUID)
		rawEntry, _ := m.db.GetRawEntry(msg.UUID)
		entry, _ := m.db.GetEntry(msg.UUID)
		recycleBin := m.db.RecycleBinEnabled()

		if err := m.db.DeleteEntry(msg.UUID); err != nil {
			m.state = viewBrowser
			return m, statusCmd(err.Error(), true)
		}
		m.dirty = true
		m.refreshEntryCount()
		m.state = viewBrowser

		if rawEntry != nil {
			if recycleBin {
				m.undoStack.push(undoAction{
					actionType:  undoMoveEntry,
					description: fmt.Sprintf("Delete '%s'", entry.Title),
					entryUUID:   msg.UUID,
					sourceUUID:  parentUUID,
				})
			} else {
				m.undoStack.push(undoAction{
					actionType:  undoDeleteEntry,
					description: fmt.Sprintf("Delete '%s'", entry.Title),
					rawEntry:    rawEntry,
					parentUUID:  parentUUID,
				})
			}
		}

		statusText := "Entry deleted"
		if recycleBin {
			statusText = "Moved to Recycle Bin"
		}
		return m, tea.Batch(
			func() tea.Msg { return RefreshMsg{} },
			statusCmd(statusText, false),
		)

	case confirmDeleteGroupMsg:
		if m.readOnly {
			m.state = viewBrowser
			return m, statusCmd("Read-only mode", true)
		}
		// Capture undo state before deletion
		parentUUID, _ := m.db.FindGroupParentUUID(msg.UUID)
		rawGroup, _ := m.db.GetRawGroup(msg.UUID)
		recycleBinG := m.db.RecycleBinEnabled()
		groupName := ""
		if rawGroup != nil {
			groupName = rawGroup.Name
		}

		if err := m.db.DeleteGroup(msg.UUID); err != nil {
			m.state = viewBrowser
			return m, statusCmd(err.Error(), true)
		}
		m.dirty = true
		m.refreshEntryCount()
		m.state = viewBrowser

		if rawGroup != nil {
			if recycleBinG {
				m.undoStack.push(undoAction{
					actionType:  undoMoveGroup,
					description: fmt.Sprintf("Delete group '%s'", groupName),
					groupUUID:   msg.UUID,
					sourceUUID:  parentUUID,
				})
			} else {
				m.undoStack.push(undoAction{
					actionType:  undoDeleteGroup,
					description: fmt.Sprintf("Delete group '%s'", groupName),
					rawGroup:    rawGroup,
					parentUUID:  parentUUID,
				})
			}
		}

		statusText := "Group deleted"
		if recycleBinG {
			statusText = "Moved to Recycle Bin"
		}
		return m, tea.Batch(
			func() tea.Msg { return RefreshMsg{} },
			statusCmd(statusText, false),
		)

	case PermanentlyDeleteEntryMsg:
		m.confirm = NewConfirmModel(fmt.Sprintf("Permanently delete entry '%s'?", msg.Name), func() tea.Msg {
			return confirmPermanentDeleteEntryMsg{UUID: msg.EntryUUID}
		})
		m.state = viewConfirm
		return m, nil

	case PermanentlyDeleteGroupMsg:
		m.confirm = NewConfirmModel(fmt.Sprintf("Permanently delete group '%s' and all its contents?", msg.Name), func() tea.Msg {
			return confirmPermanentDeleteGroupMsg{UUID: msg.GroupUUID}
		})
		m.state = viewConfirm
		return m, nil

	case confirmPermanentDeleteEntryMsg:
		if m.readOnly {
			m.state = viewBrowser
			return m, statusCmd("Read-only mode", true)
		}
		if err := m.db.PermanentlyDeleteEntry(msg.UUID); err != nil {
			m.state = viewBrowser
			return m, statusCmd(err.Error(), true)
		}
		m.dirty = true
		m.state = viewBrowser
		return m, tea.Batch(
			func() tea.Msg { return RefreshMsg{} },
			statusCmd("Entry permanently deleted", false),
		)

	case confirmPermanentDeleteGroupMsg:
		if m.readOnly {
			m.state = viewBrowser
			return m, statusCmd("Read-only mode", true)
		}
		if err := m.db.PermanentlyDeleteGroup(msg.UUID); err != nil {
			m.state = viewBrowser
			return m, statusCmd(err.Error(), true)
		}
		m.dirty = true
		m.state = viewBrowser
		return m, tea.Batch(
			func() tea.Msg { return RefreshMsg{} },
			statusCmd("Group permanently deleted", false),
		)

	case RestoreEntryMsg:
		if m.readOnly {
			return m, statusCmd("Read-only mode", true)
		}
		rootUUID := m.db.RootGroup().UUID
		if err := m.db.RestoreEntry(msg.EntryUUID, rootUUID); err != nil {
			return m, statusCmd(fmt.Sprintf("Error restoring: %s", err), true)
		}
		m.dirty = true
		return m, tea.Batch(
			func() tea.Msg { return RefreshMsg{} },
			statusCmd("Entry restored to root group", false),
		)

	case RestoreGroupMsg:
		if m.readOnly {
			return m, statusCmd("Read-only mode", true)
		}
		rootUUID := m.db.RootGroup().UUID
		if err := m.db.RestoreGroup(msg.GroupUUID, rootUUID); err != nil {
			return m, statusCmd(fmt.Sprintf("Error restoring: %s", err), true)
		}
		m.dirty = true
		return m, tea.Batch(
			func() tea.Msg { return RefreshMsg{} },
			statusCmd("Group restored to root group", false),
		)

	case EmptyRecycleBinMsg:
		m.confirm = NewConfirmModel("Empty the recycle bin? This cannot be undone.", func() tea.Msg {
			return confirmEmptyRecycleBinMsg{}
		})
		m.state = viewConfirm
		return m, nil

	case confirmEmptyRecycleBinMsg:
		if m.readOnly {
			m.state = viewBrowser
			return m, statusCmd("Read-only mode", true)
		}
		if err := m.db.EmptyRecycleBin(); err != nil {
			m.state = viewBrowser
			return m, statusCmd(err.Error(), true)
		}
		m.dirty = true
		m.refreshEntryCount()
		m.state = viewBrowser
		return m, tea.Batch(
			func() tea.Msg { return RefreshMsg{} },
			statusCmd("Recycle bin emptied", false),
		)

	case ToggleFavoriteMsg:
		if m.readOnly {
			return m, statusCmd("Read-only mode", true)
		}
		if err := m.db.ToggleFavorite(msg.EntryUUID); err != nil {
			return m, statusCmd(fmt.Sprintf("Error toggling favorite: %s", err), true)
		}
		m.undoStack.push(undoAction{
			actionType:  undoToggleFavorite,
			description: "Toggle favorite",
			entryUUID:   msg.EntryUUID,
		})
		m.dirty = true
		return m, tea.Batch(
			func() tea.Msg { return RefreshMsg{} },
			statusCmd("Favorite toggled", false),
		)

	case DuplicateEntryMsg:
		if m.readOnly {
			return m, statusCmd("Read-only mode", true)
		}
		newUUID, err := m.db.DuplicateEntry(msg.EntryUUID)
		if err != nil {
			return m, statusCmd(fmt.Sprintf("Error duplicating: %s", err), true)
		}
		m.undoStack.push(undoAction{
			actionType:  undoDuplicateEntry,
			description: "Duplicate entry",
			createdUUID: newUUID,
		})
		m.dirty = true
		m.refreshEntryCount()
		return m, tea.Batch(
			func() tea.Msg { return RefreshMsg{} },
			statusCmd("Entry duplicated (use 's' to save to file)", false),
		)

	case PasteEntryMsg:
		if m.readOnly {
			return m, statusCmd("Read-only mode", true)
		}
		sourceUUID, _ := m.db.FindEntryParentUUID(msg.EntryUUID)
		if err := m.db.MoveEntry(msg.EntryUUID, msg.TargetGroupUUID); err != nil {
			return m, statusCmd(fmt.Sprintf("Error moving entry: %s", err), true)
		}
		m.undoStack.push(undoAction{
			actionType:  undoMoveEntry,
			description: "Move entry",
			entryUUID:   msg.EntryUUID,
			sourceUUID:  sourceUUID,
		})
		m.dirty = true
		m.browser.cutEntry = nil
		return m, tea.Batch(
			func() tea.Msg { return RefreshMsg{} },
			statusCmd("Entry moved (use 's' to save to file)", false),
		)

	case PasteGroupMsg:
		if m.readOnly {
			return m, statusCmd("Read-only mode", true)
		}
		sourceUUID, _ := m.db.FindGroupParentUUID(msg.GroupUUID)
		if err := m.db.MoveGroup(msg.GroupUUID, msg.TargetGroupUUID); err != nil {
			return m, statusCmd(fmt.Sprintf("Error moving group: %s", err), true)
		}
		m.undoStack.push(undoAction{
			actionType:  undoMoveGroup,
			description: "Move group",
			groupUUID:   msg.GroupUUID,
			sourceUUID:  sourceUUID,
		})
		m.dirty = true
		m.browser.cutGroup = nil
		return m, tea.Batch(
			func() tea.Msg { return RefreshMsg{} },
			statusCmd("Group moved (use 's' to save to file)", false),
		)

	case ChangePasswordStartMsg:
		if m.readOnly {
			return m, statusCmd("Read-only mode", true)
		}
		m.password = NewPasswordModel()
		m.state = viewPassword
		return m, m.password.Init()

	case ChangePasswordMsg:
		m.db.ChangePassword(msg.NewPassword)
		// Clear password inputs to reduce sensitive data in memory.
		m.password.newInput.SetValue("")
		m.password.confirmInput.SetValue("")
		m.dirty = true
		m.state = viewBrowser
		return m, statusCmd("Master password changed (use 's' to save to file)", false)

	case ShowAuditMsg:
		result := m.db.Audit()
		m.audit = NewAuditModel(result)
		m.audit.width = m.width
		m.audit.height = m.viewHeight()
		m.state = viewAudit
		return m, nil

	case ShowHelpMsg:
		m.help = NewHelpModel(m.cfg.Keys(), m.width, m.viewHeight())
		m.state = viewHelp
		return m, nil

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
		if m.db != nil {
			m.db.BackupOnSave = m.cfg.BackupEnabled()
		}
		m.lastActivity = time.Now()
		m.state = viewBrowser
		return m, tea.Batch(
			statusCmd("Settings saved to "+config.Path(), false),
			m.scheduleAutoLock(),
		)

	case ShowHistoryMsg:
		history := m.db.EntryHistory(msg.EntryUUID)
		m.history = NewHistoryModel(msg.EntryTitle, history)
		m.history.width = m.width
		m.history.height = m.viewHeight()
		m.state = viewHistory
		return m, nil

	case BackToDetailMsg:
		m.state = viewDetail
		return m, nil

	case hibpResultMsg:
		m.detail.hibpResult = &msg
		m.detail.hibpChecking = false
		m.detail.RefreshContent()
		if msg.Err != nil {
			return m, statusCmd(fmt.Sprintf("HIBP check failed: %s", msg.Err), true)
		} else if msg.Breached {
			return m, statusCmd(fmt.Sprintf("WARNING: Password found in %d breaches!", msg.Count), true)
		}
		return m, statusCmd("Password not found in any known breaches", false)

	case ExtractAttachmentMsg:
		if err := m.db.ExtractAttachment(msg.EntryUUID, msg.Name, msg.DestPath); err != nil {
			return m, statusCmd(fmt.Sprintf("Error extracting attachment: %s", err), true)
		}
		m.detail.copied = ""
		m.detail.RefreshContent()
		return m, statusCmd(fmt.Sprintf("Attachment saved to %s", msg.DestPath), false)

	case DeleteAttachmentMsg:
		m.confirm = NewConfirmModel(fmt.Sprintf("Delete attachment %q?", msg.Name), func() tea.Msg {
			return confirmDeleteAttachmentMsg(msg)
		})
		m.returnState = viewDetail
		m.state = viewConfirm
		return m, nil

	case confirmDeleteAttachmentMsg:
		if m.readOnly {
			m.state = viewDetail
			return m, statusCmd("Read-only mode", true)
		}
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
		if m.readOnly {
			return m, statusCmd("Read-only mode", true)
		}
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
		if m.readOnly {
			m.state = viewBrowser
			return m, statusCmd("Read-only mode", true)
		}
		return m.handleSaveEntry(msg)

	case SaveGroupMsg:
		if m.readOnly {
			m.state = viewBrowser
			return m, statusCmd("Read-only mode", true)
		}
		return m.handleSaveGroup(msg)

	case SaveAsStartMsg:
		if m.readOnly {
			return m, statusCmd("Read-only mode", true)
		}
		m.pathInput = NewPathInputModel(m.filePath)
		m.state = viewPathInput
		return m, m.pathInput.Init()

	case SaveAsMsg:
		if m.readOnly {
			m.state = viewBrowser
			return m, statusCmd("Read-only mode", true)
		}
		return m.handleSaveAs(msg.Path)

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
		return m, tea.Batch(
			statusCmd(fmt.Sprintf("Copied %s!", msg.Field), false),
			m.scheduleClearClipboard(msg.Value),
		)

	case clipboardClearMsg:
		current, err := readClipboard()
		if err != nil {
			// Can't read clipboard (e.g. over SSH) — clear anyway so OSC 52 clear is sent.
			clearClipboard()
		} else if current == msg.expected {
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
		if m.state != viewUnlock && msg.at.Equal(m.lastActivity) {
			if m.dirty && m.db != nil && !m.readOnly {
				_ = m.db.Save()
				m.dirty = false
			}
			clearClipboard()
			m.db = nil
			m.undoStack.clear()
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
	case viewHistory:
		var updated tea.Model
		updated, cmd = m.history.Update(msg)
		m.history = updated.(HistoryModel)
	case viewAudit:
		var updated tea.Model
		updated, cmd = m.audit.Update(msg)
		m.audit = updated.(AuditModel)
	case viewHelp:
		var updated tea.Model
		updated, cmd = m.help.Update(msg)
		m.help = updated.(HelpModel)
	case viewPathInput:
		var updated tea.Model
		updated, cmd = m.pathInput.Update(msg)
		m.pathInput = updated.(PathInputModel)
	}

	return m, cmd
}

func (m AppModel) View() string {
	var view string
	switch m.state {
	case viewUnlock:
		return m.unlock.View()
	case viewBrowser:
		view = m.browser.View()
	case viewDetail:
		view = m.detail.View()
	case viewEditor:
		view = m.editor.View()
	case viewGroupInput:
		view = m.groupInput.View()
	case viewConfirm:
		view = m.confirm.View()
	case viewPassword:
		view = m.password.View()
	case viewStats:
		view = m.stats.View()
	case viewSettings:
		view = m.settings.View()
	case viewHistory:
		view = m.history.View()
	case viewAudit:
		view = m.audit.View()
	case viewHelp:
		view = m.help.View()
	case viewPathInput:
		view = m.pathInput.View()
	}
	return view + "\n" + m.statusBar()
}

func (m *AppModel) refreshEntryCount() {
	if m.db != nil {
		m.entryCount = m.db.Stats().TotalEntries
	}
}

func (m AppModel) statusBar() string {
	filename := filepath.Base(m.filePath)
	parts := []string{filename, fmt.Sprintf("%d entries", m.entryCount)}
	if m.readOnly {
		parts = append(parts, statusBarModifiedStyle.Render("[read-only]"))
	}
	if m.dirty {
		parts = append(parts, statusBarModifiedStyle.Render("[modified]"))
	}
	if m.undoStack.length() > 0 {
		parts = append(parts, helpStyle.Render(fmt.Sprintf("ctrl+z: undo (%d)", m.undoStack.length())))
	}
	if m.statusText != "" {
		if m.statusError {
			parts = append(parts, errorMsgStyle.Render(m.statusText))
		} else {
			parts = append(parts, successStyle.Render(m.statusText))
		}
	}
	content := strings.Join(parts, "  ")
	return statusBarBgStyle.Width(m.width).Render(content)
}

func (m AppModel) viewHeight() int {
	if m.state == viewUnlock {
		return m.height
	}
	return m.height - 1
}

func (m AppModel) handleUndo() (tea.Model, tea.Cmd) {
	action, ok := m.undoStack.pop()
	if !ok {
		return m, statusCmd("Nothing to undo", false)
	}

	var err error
	switch action.actionType {
	case undoDeleteEntry:
		err = m.db.InsertRawEntry(action.parentUUID, *action.rawEntry)
	case undoDeleteGroup:
		err = m.db.InsertRawGroup(action.parentUUID, *action.rawGroup)
	case undoEditEntry:
		err = m.db.UpdateEntry(action.entryUUID, *action.prevEntry)
	case undoRenameGroup:
		err = m.db.RenameGroup(action.groupUUID, action.oldName)
	case undoMoveEntry:
		err = m.db.MoveEntry(action.entryUUID, action.sourceUUID)
	case undoMoveGroup:
		err = m.db.MoveGroup(action.groupUUID, action.sourceUUID)
	case undoDuplicateEntry:
		err = m.db.PermanentlyDeleteEntry(action.createdUUID)
	case undoAddEntry:
		err = m.db.PermanentlyDeleteEntry(action.createdUUID)
	case undoAddGroup:
		err = m.db.PermanentlyDeleteGroup(action.createdUUID)
	case undoToggleFavorite:
		err = m.db.ToggleFavorite(action.entryUUID)
	}

	if err != nil {
		return m, statusCmd(fmt.Sprintf("Undo failed: %s", err), true)
	}

	m.dirty = true
	m.refreshEntryCount()
	m.state = viewBrowser

	return m, tea.Batch(
		func() tea.Msg { return RefreshMsg{} },
		statusCmd(fmt.Sprintf("Undone: %s", action.description), false),
	)
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
	db.BackupOnSave = m.cfg.BackupEnabled()
	m.db = db
	m.unlock.input.SetValue("")
	m.unlock.input.Reset()
	m.browser = NewBrowserModel(db, m.cfg.Keys())
	m.browser.readOnly = m.readOnly
	m.browser.width = m.width
	m.browser.height = m.viewHeight()
	m.state = viewBrowser
	m.lastActivity = time.Now()
	m.refreshEntryCount()
	m.undoStack.clear()
	return m, m.scheduleAutoLock()
}

func (m AppModel) handleSaveEntry(msg SaveEntryMsg) (tea.Model, tea.Cmd) {
	if msg.IsNew {
		newUUID, err := m.db.AddEntry(msg.GroupUUID, msg.Entry)
		if err != nil {
			m.state = viewBrowser
			return m, statusCmd(fmt.Sprintf("Error adding entry: %s", err), true)
		}
		m.undoStack.push(undoAction{
			actionType:  undoAddEntry,
			description: fmt.Sprintf("Add entry '%s'", msg.Entry.Title),
			createdUUID: newUUID,
		})
		m.refreshEntryCount()
	} else {
		// Capture previous state for undo
		prevEntry, _ := m.db.GetEntry(msg.EntryUUID)
		if err := m.db.UpdateEntry(msg.EntryUUID, msg.Entry); err != nil {
			m.state = viewBrowser
			return m, statusCmd(fmt.Sprintf("Error updating entry: %s", err), true)
		}
		m.undoStack.push(undoAction{
			actionType:  undoEditEntry,
			description: fmt.Sprintf("Edit '%s'", prevEntry.Title),
			entryUUID:   msg.EntryUUID,
			prevEntry:   &prevEntry,
		})
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
		// Capture old name for undo
		rawGroup, _ := m.db.GetRawGroup(msg.ParentUUID)
		oldName := ""
		if rawGroup != nil {
			oldName = rawGroup.Name
		}
		if err := m.db.RenameGroup(msg.ParentUUID, msg.Name); err != nil {
			m.state = viewBrowser
			return m, statusCmd(fmt.Sprintf("Error renaming group: %s", err), true)
		}
		if oldName != "" && oldName != msg.Name {
			m.undoStack.push(undoAction{
				actionType:  undoRenameGroup,
				description: fmt.Sprintf("Rename group '%s' to '%s'", oldName, msg.Name),
				groupUUID:   msg.ParentUUID,
				oldName:     oldName,
			})
		}
		m.dirty = true
		m.state = viewBrowser
		return m, tea.Batch(
			func() tea.Msg { return RefreshMsg{} },
			statusCmd("Group renamed (use 's' to save to file)", false),
		)
	}
	newUUID, err := m.db.AddGroup(msg.ParentUUID, msg.Name)
	if err != nil {
		m.state = viewBrowser
		return m, statusCmd(fmt.Sprintf("Error adding group: %s", err), true)
	}
	m.undoStack.push(undoAction{
		actionType:  undoAddGroup,
		description: fmt.Sprintf("Add group '%s'", msg.Name),
		createdUUID: newUUID,
	})
	m.dirty = true
	m.state = viewBrowser
	return m, tea.Batch(
		func() tea.Msg { return RefreshMsg{} },
		statusCmd("Group created (use 's' to save to file)", false),
	)
}

func (m AppModel) handleSaveAs(path string) (tea.Model, tea.Cmd) {
	if err := m.db.SaveAs(path); err != nil {
		m.state = viewBrowser
		return m, statusCmd(fmt.Sprintf("Error saving: %s", err), true)
	}
	m.db.Path = path
	m.filePath = path
	m.dirty = false
	m.state = viewBrowser
	return m, statusCmd(fmt.Sprintf("Database saved as %s", filepath.Base(path)), false)
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

type confirmPermanentDeleteEntryMsg struct {
	UUID gokeepasslib.UUID
}

type confirmPermanentDeleteGroupMsg struct {
	UUID gokeepasslib.UUID
}

type confirmEmptyRecycleBinMsg struct{}

// RenameGroupStartMsg is sent to initiate group renaming.
type RenameGroupStartMsg struct {
	GroupUUID   gokeepasslib.UUID
	CurrentName string
}

// ChangePasswordStartMsg is sent to initiate master password change.
type ChangePasswordStartMsg struct{}

// osc52Copy writes an OSC 52 escape sequence to set the terminal clipboard.
// This works over SSH because the escape sequence travels through the connection
// to the local terminal emulator. Writes directly to /dev/tty to avoid
// interfering with bubbletea's stdout rendering.
func osc52Copy(text string) {
	f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	b64 := base64.StdEncoding.EncodeToString([]byte(text))
	_, _ = fmt.Fprintf(f, "\033]52;c;%s\a", b64)
}

func copyToClipboard(text string) error {
	// Always attempt OSC 52 first — works over SSH when the terminal supports it,
	// and is silently ignored by terminals that don't.
	osc52Copy(text)

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
			// No clipboard utility available, but OSC 52 was already sent.
			return nil
		}
	default:
		// OSC 52 was already sent; don't fail on unsupported platforms.
		return nil
	}
	cmd.Stdin = nil
	pipe, err := cmd.StdinPipe()
	if err != nil {
		return nil // OSC 52 already attempted
	}
	if err := cmd.Start(); err != nil {
		return nil // OSC 52 already attempted
	}
	if _, err := pipe.Write([]byte(text)); err != nil {
		return nil // OSC 52 already attempted
	}
	_ = pipe.Close()
	_ = cmd.Wait() // best-effort; OSC 52 already attempted
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
	if parsed.Host == "" {
		return fmt.Errorf("invalid URL: missing host")
	}
	// Re-serialize the parsed URL to avoid passing through unexpected characters.
	sanitized := parsed.String()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", sanitized)
	case "linux":
		cmd = exec.Command("xdg-open", sanitized)
	default:
		return fmt.Errorf("open URL not supported on %s", runtime.GOOS)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open URL: %w", err)
	}
	return nil
}
