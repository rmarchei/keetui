package ui

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/tobischo/gokeepasslib/v3"

	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/keepass"
)

type viewState int

const (
	viewNone viewState = iota // no return target; never a renderable screen
	viewUnlock
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
	viewPresetPicker
)

// clipboardClearMsg signals that the clipboard should be cleared.
type clipboardClearMsg struct {
	gen int // a new copy invalidates even a timer for the same value
}

// autoLockMsg signals that the database should be locked due to inactivity.
type autoLockMsg struct {
	gen int // generation counter; stale ticks carry an old value and are dropped
}

// databaseOpenResultMsg carries the outcome of an asynchronous unlock.
type databaseOpenResultMsg struct {
	db  *keepass.DB
	err error
}

// saveResultMsg carries the outcome of an asynchronous save.
type saveResultMsg struct {
	err     error
	path    string
	upgrade bool
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

// Preserve the complete overlay context when a quit prompt temporarily
// replaces another confirmation (possibly underneath help).
type quitContext struct {
	state           viewState
	confirm         ConfirmModel
	help            HelpModel
	helpReturnState viewState
}

// AppModel is the top-level model coordinating all views.
type AppModel struct {
	state           viewState
	db              *keepass.DB
	unlock          UnlockModel
	browser         BrowserModel
	detail          DetailModel
	editor          EditorModel
	groupInput      GroupInputModel
	confirm         ConfirmModel
	password        PasswordModel
	stats           StatsModel
	settings        SettingsModel
	history         HistoryModel
	audit           AuditModel
	help            HelpModel
	pathInput       PathInputModel
	presetPicker    PresetPickerModel
	width           int
	height          int
	lastActivity    time.Time // tracks last user interaction for auto-lock
	autoLockGen     int       // current auto-lock timer generation; see autoLockMsg
	filePath        string    // remember file path for re-lock
	keyFilePath     string    // remember key file path for re-lock
	cfg             config.Config
	dirty           bool      // true when in-memory changes haven't been saved
	saving          bool      // true while a save runs in the background; input is gated
	lastCopied      string    // most recent keetui-copied clipboard value not yet cleared
	helpReturnState viewState // help must not overwrite a pending confirmation
	quitReturn      *quitContext
	entryCount      int       // cached entry count for status bar
	undoStack       undoStack // undo history
	readOnly        bool      // when true, all mutations are blocked
	statusText      string    // transient status message for status bar
	statusError     bool      // true if status message is an error

	autoLockCheckAt     time.Time // next armed check (may be deferred by a save/retry)
	clipboardGen        int
	clipboardClearAt    time.Time
	securityNow         time.Time // display clock, updated by input and a single 1s tick
	securityTickGen     int
	securityTickPending bool
	clipboard           clipboardAccess // nil uses the OS backend; tests inject an isolated clipboard
	healthScanGen       int
	healthScanView      viewState
	healthScanCancel    context.CancelFunc
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
		securityNow:  time.Now(),
		filePath:     filePath,
		keyFilePath:  keyFilePath,
		cfg:          cfg,
		readOnly:     ro,
	}
}

func (m AppModel) Init() tea.Cmd {
	return m.unlock.Init()
}

// scheduleAutoLock arms the auto-lock timer for the full configured interval.
// It invalidates any previously scheduled tick, so at most one timer chain is
// ever live. The receiver is a pointer: callers must return the mutated model.
func (m *AppModel) scheduleAutoLock() tea.Cmd {
	return m.scheduleAutoLockIn(m.cfg.AutoLockDuration())
}

func (m *AppModel) scheduleAutoLockIn(d time.Duration) tea.Cmd {
	m.autoLockGen++ // invalidate any pending tick, even when disabling
	m.autoLockCheckAt = time.Time{}
	if d <= 0 {
		return m.ensureSecurityTick()
	}
	m.securityNow = time.Now()
	m.autoLockCheckAt = m.securityNow.Add(d)
	gen := m.autoLockGen
	return tea.Batch(tea.Tick(d, func(t time.Time) tea.Msg {
		return autoLockMsg{gen: gen}
	}), m.ensureSecurityTick())
}

func (m *AppModel) scheduleClearClipboard() tea.Cmd {
	m.clipboardGen++
	m.clipboardClearAt = time.Time{}
	d := m.cfg.ClipboardClearDuration()
	if d == 0 {
		return m.ensureSecurityTick()
	}
	m.securityNow = time.Now()
	m.clipboardClearAt = m.securityNow.Add(d)
	gen := m.clipboardGen
	return tea.Batch(tea.Tick(d, func(t time.Time) tea.Msg {
		return clipboardClearMsg{gen: gen}
	}), m.ensureSecurityTick())
}

func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, tick := msg.(securityTickMsg); !tick {
		m.securityNow = time.Now()
	}
	updated, cmd := m.update(msg)
	next := updated.(AppModel)
	if next.healthScanCancel != nil && (next.db == nil || !slices.Contains(next.overlayStates(), next.healthScanView)) {
		next.cancelHealthScan()
	}
	if !next.needsSecurityTick() {
		next.stopSecurityTick()
	}
	// A new/returning screen needs dimensions immediately, even when the
	// terminal has not emitted another WindowSizeMsg.
	if next.state != m.state {
		updated, _ = next.updateActive(tea.WindowSizeMsg{Width: next.width, Height: next.viewHeight()})
		next = updated.(AppModel)
	}
	if next.state == viewConfirm {
		// A confirmation can be replaced without changing the active state.
		next.confirm.width, next.confirm.height = next.width, next.viewHeight()
	}
	return next, cmd
}

func (m AppModel) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if tick, ok := msg.(securityTickMsg); ok {
		if tick.gen != m.securityTickGen || !m.securityTickPending {
			return m, nil
		}
		m.securityTickPending = false
		// Timer messages can sit behind slow clipboard I/O or rendering. Use
		// processing time too, so a delayed tick never rewinds the countdown.
		now := time.Now()
		if tick.at.After(now) {
			now = tick.at
		}
		if now.After(m.securityNow) {
			m.securityNow = now
		}
		cmd := m.ensureSecurityTick()
		return m, cmd
	}
	// Handle window size globally
	if wsm, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = wsm.Width
		m.height = wsm.Height
		// Reduce height for sub-views to make room for the status bar
		if m.state != viewUnlock {
			msg = tea.WindowSizeMsg{Width: wsm.Width, Height: max(0, wsm.Height-1)}
		}
		// The long-lived browser model must track size even while another
		// view is active, or returning to it renders stale dimensions.
		if m.state != viewBrowser {
			m.browser.width = wsm.Width
			m.browser.height = wsm.Height - 1
		}
	}

	// While a save is in flight, freeze user input: mutating the database
	// tree would race with the encoding goroutine. Auto-lock ticks are
	// deferred instead of dropped so the timer stays armed.
	if m.saving {
		switch msg := msg.(type) {
		case tea.KeyPressMsg, tea.MouseMsg, tea.PasteMsg, tea.PasteStartMsg, tea.PasteEndMsg:
			return m.saveBusy()
		case autoLockMsg:
			if msg.gen != m.autoLockGen {
				return m, nil
			}
			cmd := m.scheduleAutoLock()
			return m, cmd
		}
	}

	// F1 works in forms without stealing printable characters from inputs.
	// Capture a long status message before normal activity clears it.
	if _, key := msg.(tea.KeyPressMsg); key && m.state == viewSettings && m.settings.recording {
		m.recordActivity()
		m.statusText = ""
		return m.updateActive(msg)
	}
	if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == "f1" && m.state != viewUnlock && m.state != viewHelp {
		m.recordActivity()
		return m.openHelp()
	}

	// Track user activity for auto-lock (key presses, mouse events, and pastes).
	if m.state != viewUnlock {
		switch msg.(type) {
		case tea.KeyPressMsg, tea.MouseMsg, tea.PasteMsg, tea.PasteStartMsg, tea.PasteEndMsg:
			m.recordActivity()
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
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok && keyMsg.String() == "ctrl+z" {
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
		if m.quitReturn != nil {
			if m.state == viewHelp && m.helpReturnState == viewConfirm {
				m.state = viewConfirm
			}
			return m, nil // already asking; do not replace the pending context
		}
		if m.dirty || m.cfg.ConfirmOnQuit() {
			m.quitReturn = &quitContext{
				state: m.state, confirm: m.confirm,
				help: m.help, helpReturnState: m.helpReturnState,
			}
			prompt := "Quit?"
			if m.dirty {
				prompt = "You have unsaved changes. Quit anyway?"
			}
			m.confirm = NewConfirmModel(m.state, prompt, func() tea.Msg { return quitMsg{} })
			m.state = viewConfirm
			return m, nil
		}
		m.clearClipboardOnQuit()
		m.cancelHealthScan()
		return m, tea.Quit

	case quitMsg:
		m.clearClipboardOnQuit()
		m.cancelHealthScan()
		return m, tea.Quit

	case DatabaseOpenedMsg:
		return m.handleDatabaseOpened(msg)

	case databaseOpenResultMsg:
		return m.handleDatabaseOpenResult(msg)

	case ViewEntryMsg:
		m.detail = NewDetailModel(msg.Entry, m.cfg)
		m.detail.readOnly = m.readOnly
		if m.db != nil {
			m.detail.SetAttachments(m.db.EntryAttachments(msg.Entry.UUID))
		}
		m.detail.SetSize(m.width, m.viewHeight())
		m.state = viewDetail
		tick := m.detail.startTOTPTick()
		return m, tick

	case EditEntryMsg:
		m.editor = NewEditorModel(&msg.Entry, gokeepasslib.UUID{}, m.cfg)
		m.state = viewEditor
		return m, m.editor.Init()

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
		m.confirm = NewConfirmModel(viewBrowser, fmt.Sprintf("Delete entry '%s'?", msg.Name), func() tea.Msg {
			return confirmDeleteEntryMsg{UUID: msg.EntryUUID}
		})
		m.state = viewConfirm
		return m, nil

	case DeleteGroupMsg:
		m.confirm = NewConfirmModel(viewBrowser, fmt.Sprintf("Delete group '%s' and all its contents?", msg.Name), func() tea.Msg {
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
		entry, _ := m.db.GetEntry(msg.UUID)
		recycleBin := m.db.RecycleBinEnabled()
		var snapshot *keepass.EntrySnapshot
		if !recycleBin {
			snapshot, _ = m.db.SnapshotEntry(msg.UUID)
		}

		if err := m.db.DeleteEntry(msg.UUID); err != nil {
			m.state = viewBrowser
			return m, statusCmd(err.Error(), true)
		}
		m.dirty = true
		m.refreshEntryCount()
		m.state = viewBrowser

		if recycleBin {
			m.undoStack.push(undoAction{
				actionType:  undoMoveEntry,
				description: fmt.Sprintf("Delete '%s'", entry.Title),
				entryUUID:   msg.UUID,
				sourceUUID:  parentUUID,
			})
		} else if snapshot != nil {
			m.undoStack.push(undoAction{
				actionType:    undoDeleteEntry,
				description:   fmt.Sprintf("Delete '%s'", entry.Title),
				entrySnapshot: snapshot,
				parentUUID:    parentUUID,
			})
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
		var snapshot *keepass.GroupSnapshot
		if !recycleBinG {
			snapshot, _ = m.db.SnapshotGroup(msg.UUID)
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
			} else if snapshot != nil {
				m.undoStack.push(undoAction{
					actionType:    undoDeleteGroup,
					description:   fmt.Sprintf("Delete group '%s'", groupName),
					groupSnapshot: snapshot,
					parentUUID:    parentUUID,
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
		m.confirm = NewConfirmModel(viewBrowser, fmt.Sprintf("Permanently delete entry '%s'?", msg.Name), func() tea.Msg {
			return confirmPermanentDeleteEntryMsg{UUID: msg.EntryUUID}
		})
		m.state = viewConfirm
		return m, nil

	case PermanentlyDeleteGroupMsg:
		m.confirm = NewConfirmModel(viewBrowser, fmt.Sprintf("Permanently delete group '%s' and all its contents?", msg.Name), func() tea.Msg {
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
		m.confirm = NewConfirmModel(viewBrowser, "Empty the recycle bin? This cannot be undone.", func() tea.Msg {
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
		err := m.db.ChangePassword(msg.NewPassword)
		// Clear password inputs to reduce sensitive data in memory.
		m.password.newInput.SetValue("")
		m.password.confirmInput.SetValue("")
		m.state = viewBrowser
		if err != nil {
			return m, statusCmd(fmt.Sprintf("Error changing password: %s", err), true)
		}
		m.dirty = true
		return m, statusCmd("Master password changed (use 's' to save to file)", false)

	case ShowAuditMsg:
		return m.startHealthScan(viewAudit)

	case healthScanResultMsg:
		if m.healthScanCancel == nil || msg.gen != m.healthScanGen || m.db == nil {
			return m, nil
		}
		m.cancelHealthScan()
		if msg.view == viewStats {
			m.stats = NewStatsModel(msg.stats)
			m.stats.width, m.stats.height = m.width, m.viewHeight()
		} else {
			m.audit = NewAuditModel(msg.audit)
			m.audit.width, m.audit.height = m.width, m.viewHeight()
		}
		return m, nil

	case ShowHelpMsg:
		return m.openHelp()

	case CloseHelpMsg:
		if m.state == viewHelp {
			m.state = m.helpReturnState
		}
		return m, nil

	case ShowStatsMsg:
		return m.startHealthScan(viewStats)

	case ShowSettingsMsg:
		m.settings = NewSettingsModel(m.cfg)
		m.state = viewSettings
		return m, m.settings.Init()

	case ShowPresetPickerMsg:
		m.presetPicker = NewPresetPickerModel(m.cfg, msg.CurrentPassword)
		m.presetPicker.width = m.width
		m.presetPicker.height = m.viewHeight()
		m.state = viewPresetPicker
		return m, m.presetPicker.Init()

	case GeneratedPasswordMsg:
		cmd := m.editor.SetGeneratedPassword(msg.Password)
		m.state = viewEditor
		return m, tea.Batch(cmd, statusCmd("Password generated", false))

	case CancelPresetMsg:
		m.state = viewEditor
		return m, nil

	case DefaultPresetChangedMsg:
		m.cfg = msg.Cfg
		return m, statusCmd("Default preset updated", false)

	case ConfigSavedMsg:
		m.cfg = msg.Cfg
		m.browser.keys = m.cfg.Keys()
		if m.db != nil {
			m.db.BackupOnSave = m.cfg.BackupEnabled()
		}
		// Styles captured at construction time don't see ApplyTheme's
		// global updates; re-pull them on the long-lived browser model.
		setInputPromptStyle(&m.browser.searchInput, focusedStyle)
		m.recordActivity()
		m.state = viewBrowser
		autoLockCmd := m.scheduleAutoLock()
		return m, tea.Batch(
			statusCmd("Settings saved to "+config.Path(), false),
			autoLockCmd,
		)

	case ShowHistoryMsg:
		history := m.db.EntryHistory(msg.EntryUUID)
		m.history = NewHistoryModel(msg.EntryTitle, history, m.cfg.Keys())
		m.history.width = m.width
		m.history.height = m.viewHeight()
		m.state = viewHistory
		return m, nil

	case BackToDetailMsg:
		m.state = viewDetail
		// The TOTP tick chain died while a sub-view was active; restart it.
		tick := m.detail.startTOTPTick()
		return m, tick

	case hibpResultMsg:
		m.detail.hibpChecking = false
		if msg.Err != nil {
			return m, statusCmd(fmt.Sprintf("HIBP check failed: %s", msg.Err), true)
		} else if msg.Breached {
			return m, statusCmd(fmt.Sprintf("WARNING: Password found in %d breaches!", msg.Count), true)
		}
		return m, statusCmd("Password not found in any known breaches", false)

	case ExtractAttachmentMsg:
		// Resolve relative paths against the database directory.
		destPath := msg.DestPath
		if !filepath.IsAbs(destPath) {
			destPath = filepath.Join(filepath.Dir(m.filePath), destPath)
		}
		destPath = filepath.Clean(destPath)
		if _, err := os.Stat(destPath); err == nil {
			// Destination exists: confirm before overwriting it.
			resolved := confirmExtractAttachmentMsg{
				EntryUUID: msg.EntryUUID,
				Name:      msg.Name,
				DestPath:  destPath,
			}
			m.confirm = NewConfirmModel(viewDetail, fmt.Sprintf("Overwrite existing file %q?", destPath), func() tea.Msg {
				return resolved
			})
			m.state = viewConfirm
			return m, nil
		}
		if err := m.db.ExtractAttachment(msg.EntryUUID, msg.Name, destPath); err != nil {
			return m, statusCmd(fmt.Sprintf("Error extracting attachment: %s", err), true)
		}
		return m, statusCmd(fmt.Sprintf("Attachment saved to %s", destPath), false)

	case confirmExtractAttachmentMsg:
		m.state = viewDetail
		tick := m.detail.startTOTPTick()
		if err := m.db.ExtractAttachment(msg.EntryUUID, msg.Name, msg.DestPath); err != nil {
			return m, tea.Batch(statusCmd(fmt.Sprintf("Error extracting attachment: %s", err), true), tick)
		}
		return m, tea.Batch(statusCmd(fmt.Sprintf("Attachment saved to %s", msg.DestPath), false), tick)

	case DeleteAttachmentMsg:
		m.confirm = NewConfirmModel(viewDetail, fmt.Sprintf("Delete attachment %q?", msg.Name), func() tea.Msg {
			return confirmDeleteAttachmentMsg(msg)
		})
		m.state = viewConfirm
		return m, nil

	case confirmDeleteAttachmentMsg:
		if m.readOnly {
			m.state = viewDetail
			tick := m.detail.startTOTPTick()
			return m, tea.Batch(statusCmd("Read-only mode", true), tick)
		}
		if err := m.db.DeleteAttachment(msg.EntryUUID, msg.Name); err != nil {
			m.state = viewDetail
			tick := m.detail.startTOTPTick()
			return m, tea.Batch(statusCmd(fmt.Sprintf("Error deleting attachment: %s", err), true), tick)
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
		tick := m.detail.startTOTPTick()
		return m, tea.Batch(statusCmd("Attachment deleted (use 's' to save to file)", false), tick)

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
		m.pathInput = NewPathInputModel(m.filePath, m.db.FormatVersion(), !m.db.IsKdbx4() && !m.readOnly)
		m.state = viewPathInput
		return m, m.pathInput.Init()

	case SaveAsMsg:
		if m.readOnly {
			m.state = viewBrowser
			return m, statusCmd("Read-only mode", true)
		}
		if target := filepath.Clean(msg.Path); target != filepath.Clean(m.filePath) {
			if _, err := os.Stat(target); err == nil {
				// The target is a different existing file: confirm before
				// clobbering it (it could be another vault).
				resolved := confirmSaveAsMsg(msg)
				m.confirm = NewConfirmModel(viewBrowser, fmt.Sprintf("Overwrite existing file %q?", target), func() tea.Msg {
					return resolved
				})
				m.state = viewConfirm
				return m, nil
			}
		}
		return m.handleSaveAs(msg.Path, msg.Upgrade)

	case confirmSaveAsMsg:
		return m.handleSaveAs(msg.Path, msg.Upgrade)

	case saveResultMsg:
		return m.handleSaveResult(msg)

	case BackToBrowserMsg:
		m.state = viewBrowser
		return m, func() tea.Msg { return RefreshMsg{} }

	case CancelEditMsg:
		if m.state == viewConfirm && m.quitReturn != nil {
			previous := m.quitReturn
			m.state, m.confirm = previous.state, previous.confirm
			m.help, m.helpReturnState = previous.help, previous.helpReturnState
			m.quitReturn = nil
			return m, nil
		}
		if m.state == viewConfirm && m.confirm.returnState != viewNone {
			m.state = m.confirm.returnState
			m.confirm = ConfirmModel{}
		} else {
			m.state = viewBrowser
		}
		if m.state == viewDetail {
			tick := m.detail.startTOTPTick()
			return m, tick
		}
		return m, nil

	case CopyToClipboardMsg:
		if err := m.clipboardBackend().write(msg.Value); err != nil {
			return m, statusCmd(fmt.Sprintf("Clipboard error: %s", err), true)
		}
		m.lastCopied = msg.Value
		clearCmd := m.scheduleClearClipboard()
		return m, tea.Batch(
			statusCmd(fmt.Sprintf("Copied %s!", msg.Field), false),
			clearCmd,
		)

	case clipboardClearMsg:
		if msg.gen != m.clipboardGen || m.clipboardClearAt.IsZero() {
			return m, nil
		}
		current, err := m.clipboardBackend().read()
		expected := m.lastCopied
		m.forgetCopiedValue()
		if err != nil {
			// Can't read clipboard (e.g. over SSH) — clear anyway so OSC 52 clear is sent.
			m.clipboardBackend().clear()
		} else if current == expected {
			m.clipboardBackend().clear()
		} else {
			return m, statusCmd("Clipboard changed; left untouched", false)
		}
		// OSC 52 has no acknowledgement; do not claim a verified erase.
		return m, statusCmd("Clipboard clear requested", false)

	case OpenURLMsg:
		if err := openURL(msg.URL); err != nil {
			return m, statusCmd(fmt.Sprintf("Error: %s", err), true)
		}
		return m, nil

	case autoLockMsg:
		if msg.gen != m.autoLockGen || m.state == viewUnlock || m.db == nil {
			return m, nil
		}
		d := m.cfg.AutoLockDuration()
		if d <= 0 {
			return m, nil
		}
		if idle := time.Since(m.lastActivity); idle < d {
			// Activity happened since the timer was armed; wait out the rest.
			cmd := m.scheduleAutoLockIn(d - idle)
			return m, cmd
		}
		if m.dirty && !m.readOnly {
			if err := m.db.Save(); err != nil {
				// Abort the lock rather than discard unsaved changes; keep the
				// database open, surface the error, and retry next interval.
				cmd := m.scheduleAutoLock()
				return m, tea.Batch(
					statusCmd(fmt.Sprintf("Auto-lock aborted: save failed: %s", err), true),
					cmd,
				)
			}
			m.dirty = false
		}
		m.clipboardBackend().clear()
		m.forgetCopiedValue()
		m.autoLockCheckAt = time.Time{}
		m.db = nil
		m.undoStack.clear()
		m.unlock = NewUnlockModel(m.filePath, m.keyFilePath)
		m.unlock.err = fmt.Errorf("locked due to inactivity")
		m.state = viewUnlock
		m.quitReturn = nil
		return m, m.unlock.Init()
	}

	return m.updateActive(msg)
}

func (m AppModel) openHelp() (tea.Model, tea.Cmd) {
	if m.state == viewHelp {
		return m, nil
	}
	m.helpReturnState = m.state
	m.help = NewHelpModel(m.cfg.Keys(), m.width, m.viewHeight())
	switch m.state {
	case viewDetail, viewHistory:
		m.help.section = helpDetail
	case viewEditor, viewPresetPicker:
		m.help.section = helpEditor
	case viewSettings, viewPathInput, viewPassword, viewGroupInput:
		m.help.section = helpSettings
	case viewBrowser:
		if m.browser.inRecycleBin() {
			m.help.section = helpRecycleBin
		}
	}
	if m.statusText != "" {
		m.help.message = sanitizeDisplay(m.statusText)
		m.help.section = helpMessage
	}
	if m.state == viewSettings && m.settings.saveError != "" {
		m.help.message = sanitizeDisplay(m.settings.saveError)
		m.help.section = helpMessage
	}
	m.help.resize()
	m.state = viewHelp
	return m, nil
}

func (m AppModel) updateActive(msg tea.Msg) (tea.Model, tea.Cmd) {
	states := []viewState{m.state}
	// Input belongs only to the active view. Other messages also go to the
	// screens under overlays, keeping their timers and dimensions current.
	switch msg.(type) {
	case tea.KeyPressMsg, tea.KeyReleaseMsg, tea.MouseMsg, tea.PasteMsg, tea.PasteStartMsg, tea.PasteEndMsg:
	default:
		states = m.overlayStates()
	}
	var commands []tea.Cmd
	for _, state := range states {
		updated, cmd := m.updateView(state, msg)
		m = updated.(AppModel)
		commands = append(commands, cmd)
	}
	return m, tea.Batch(commands...)
}

// overlayStates includes the active screen and every screen underneath it.
func (m AppModel) overlayStates() []viewState {
	states := []viewState{m.state}
	seen := map[viewState]bool{m.state: true}
	add := func(state viewState) {
		if state == viewNone {
			return
		}
		if !seen[state] {
			seen[state] = true
			states = append(states, state)
		}
	}
	for i := 0; i < len(states); i++ {
		switch states[i] {
		case viewHelp:
			add(m.helpReturnState)
		case viewConfirm:
			if m.quitReturn == nil {
				add(m.confirm.returnState)
			} else {
				add(m.quitReturn.state)
				if m.quitReturn.state == viewHelp {
					add(m.quitReturn.helpReturnState)
				}
				if m.quitReturn.state == viewConfirm || m.quitReturn.state == viewHelp && m.quitReturn.helpReturnState == viewConfirm {
					add(m.quitReturn.confirm.returnState)
				}
			}
		case viewPresetPicker:
			add(viewEditor)
		}
	}
	return states
}

func (m AppModel) updateView(state viewState, msg tea.Msg) (tea.Model, tea.Cmd) {
	// Delegate to active view
	var cmd tea.Cmd
	switch state {
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
	case viewPresetPicker:
		var updated tea.Model
		updated, cmd = m.presetPicker.Update(msg)
		m.presetPicker = updated.(PresetPickerModel)
	}

	return m, cmd
}

func (m AppModel) View() tea.View {
	view := tea.NewView(m.viewContent())
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	return view
}

func (m AppModel) viewContent() string {
	var view string
	switch m.state {
	case viewUnlock:
		return fitFrame(m.unlock.View().Content, m.width, m.height)
	case viewBrowser:
		view = m.browser.View().Content
	case viewDetail:
		view = m.detail.View().Content
	case viewEditor:
		view = m.editor.View().Content
	case viewGroupInput:
		view = m.groupInput.View().Content
	case viewConfirm:
		view = m.confirm.View().Content
	case viewPassword:
		view = m.password.View().Content
	case viewStats:
		view = m.stats.View().Content
	case viewSettings:
		view = m.settings.View().Content
	case viewHistory:
		view = m.history.View().Content
	case viewAudit:
		view = m.audit.View().Content
	case viewHelp:
		view = m.help.View().Content
	case viewPathInput:
		view = m.pathInput.View().Content
	case viewPresetPicker:
		view = m.presetPicker.View().Content
	}
	if m.height <= 1 {
		return fitFrame(m.statusBar(), m.width, m.height)
	}
	return fitFrame(view, m.width, m.viewHeight()) + "\n" + m.statusBar()
}

func (m *AppModel) refreshEntryCount() {
	if m.db != nil {
		m.entryCount = m.db.EntryCount()
	}
}

func (m AppModel) viewHeight() int {
	if m.state == viewUnlock {
		return m.height
	}
	return max(0, m.height-1)
}

func (m AppModel) handleUndo() (tea.Model, tea.Cmd) {
	action, ok := m.undoStack.pop()
	if !ok {
		return m, statusCmd("Nothing to undo", false)
	}

	var err error
	switch action.actionType {
	case undoDeleteEntry:
		err = m.db.InsertEntrySnapshot(action.parentUUID, action.entrySnapshot)
	case undoDeleteGroup:
		err = m.db.InsertGroupSnapshot(action.parentUUID, action.groupSnapshot)
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

// handleDatabaseOpened kicks off the actual open in a goroutine: KDBX key
// derivation (Argon2) deliberately takes on the order of a second, and
// running it inside Update would freeze the whole UI with no feedback.
func (m AppModel) handleDatabaseOpened(msg DatabaseOpenedMsg) (tea.Model, tea.Cmd) {
	m.unlock.unlocking = true
	m.unlock.err = nil
	return m, func() tea.Msg {
		var db *keepass.DB
		var err error
		if msg.KeyFilePath != "" {
			db, err = keepass.OpenWithKeyFile(msg.FilePath, msg.Password, msg.KeyFilePath)
		} else {
			db, err = keepass.Open(msg.FilePath, msg.Password)
		}
		return databaseOpenResultMsg{db: db, err: err}
	}
}

func (m AppModel) handleDatabaseOpenResult(msg databaseOpenResultMsg) (tea.Model, tea.Cmd) {
	m.unlock.unlocking = false
	if msg.err != nil {
		m.unlock.err = fmt.Errorf("failed to unlock: %w", msg.err)
		return m, nil
	}
	db := msg.db
	db.BackupOnSave = m.cfg.BackupEnabled()
	m.db = db
	if db.SuspectCorruption() {
		// Decoded protected values look like binary garbage (stream cipher
		// misalignment, e.g. an incompatible writer). Force read-only so the
		// corrupted values can never be written back to disk.
		m.readOnly = true
	}
	m.unlock.input.SetValue("")
	m.unlock.input.Reset()
	m.browser = NewBrowserModel(db, m.cfg.Keys())
	m.browser.readOnly = m.readOnly
	m.browser.width = m.width
	m.browser.height = m.viewHeight()
	m.state = viewBrowser
	m.recordActivity()
	m.refreshEntryCount()
	m.undoStack.clear()
	autoLockCmd := m.scheduleAutoLock()
	if m.db.SuspectCorruption() {
		return m, tea.Batch(autoLockCmd,
			statusCmd("Protected values look corrupted (incompatible writer?) - forced read-only, see README", true))
	}
	return m, autoLockCmd
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

// handleSaveAs converts (when upgrading) synchronously — it is a fast
// in-memory operation — then encrypts and writes in a goroutine so the
// Argon2 KDF does not freeze the UI. User input is gated on m.saving
// until the result arrives, so the tree cannot mutate mid-encode.
func (m AppModel) handleSaveAs(path string, upgrade bool) (tea.Model, tea.Cmd) {
	if upgrade {
		if err := m.db.ConvertToKDBX4(); err != nil {
			m.state = viewBrowser
			return m, statusCmd(fmt.Sprintf("Error upgrading: %s", err), true)
		}
	}
	m.state = viewBrowser
	m.saving = true
	m.statusText = "Saving…"
	m.statusError = false
	db := m.db
	return m, func() tea.Msg {
		return saveResultMsg{err: db.SaveAs(path), path: path, upgrade: upgrade}
	}
}

func (m AppModel) handleSaveResult(msg saveResultMsg) (tea.Model, tea.Cmd) {
	m.saving = false
	if msg.err != nil {
		return m, statusCmd(fmt.Sprintf("Error saving: %s", msg.err), true)
	}
	m.db.Path = msg.path
	m.filePath = msg.path
	m.dirty = false
	text := fmt.Sprintf("Database saved as %s", filepath.Base(msg.path))
	if msg.upgrade {
		text = fmt.Sprintf("Upgraded to KDBX 4 and saved as %s", filepath.Base(msg.path))
	}
	return m, statusCmd(text, false)
}

// Input stays gated until encryption finishes. Set feedback synchronously so
// ignored shortcuts (including stats/audit) cannot look like a silent no-op.
func (m AppModel) saveBusy() (tea.Model, tea.Cmd) {
	m.statusText, m.statusError = "Saving… please wait", false
	return m, nil
}

// clearClipboardOnQuit clears the clipboard if a keetui copy is still
// outstanding, so a copied secret does not outlive the process. The
// clipboard is left alone when the user has since copied something else.
func (m AppModel) clearClipboardOnQuit() {
	if m.lastCopied == "" {
		return
	}
	if current, err := m.clipboardBackend().read(); err == nil && current != m.lastCopied {
		return
	}
	m.clipboardBackend().clear()
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

type confirmExtractAttachmentMsg struct {
	EntryUUID gokeepasslib.UUID
	Name      string
	DestPath  string
}

type confirmSaveAsMsg struct {
	Path    string
	Upgrade bool
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
		_ = pipe.Close()
		return nil // OSC 52 already attempted
	}
	_, writeErr := pipe.Write([]byte(text))
	_ = pipe.Close()
	_ = cmd.Wait() // best-effort; OSC 52 already attempted
	_ = writeErr
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
