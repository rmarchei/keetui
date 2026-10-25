package ui

import (
	"context"
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
	"github.com/charmbracelet/x/ansi"
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
	gen int // the open it answers; see AppModel.openGen
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
	lastActivity    time.Time        // tracks last user interaction for auto-lock
	openGen         int              // the latest unlock attempt; older results are dropped
	autoLockGen     int              // current auto-lock timer generation; see autoLockMsg
	filePath        string           // remember file path for re-lock
	keyFilePath     string           // remember key file path for re-lock
	cfg             config.Config    // in effect: the global settings with the database's on top
	globalCfg       config.Config    // the config file's settings
	dbOverrides     config.Overrides // the open database's settings
	dirty           bool             // true when in-memory changes haven't been saved
	saving          bool             // true while a save runs in the background; input is gated
	clipboardHeld   bool             // a keetui copy may still be on the clipboard
	helpReturnState viewState        // help must not overwrite a pending confirmation
	quitReturn      *quitContext
	entryCount      int       // cached entry count for status bar
	undoStack       undoStack // undo history
	readOnly        bool      // when true, all mutations are blocked
	statusText      string    // transient status message for status bar
	statusError     bool      // true if status message is an error
	statusDetail    string    // F1 guidance for statusText

	autoLockCheckAt     time.Time // next armed check (may be deferred by a save/retry)
	clipboardGen        int
	clipboardClearAt    time.Time
	securityNow         time.Time // display clock, updated by input and a single 1s tick
	securityTickGen     int
	securityTickPending bool
	clipboard           clipboardAccess // nil uses the OS backend; tests inject an isolated clipboard
	clipboardDeferred   bool            // tmux would have dropped the clear; retried on the display tick
	terminalKind        terminalKind    // what the terminal answered to XTVERSION
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
	ApplyTheme(themeFor(cfg))
	return AppModel{
		state:        viewUnlock,
		unlock:       NewUnlockModel(filePath, keyFilePath),
		lastActivity: time.Now(),
		securityNow:  time.Now(),
		filePath:     filePath,
		keyFilePath:  keyFilePath,
		cfg:          cfg,
		globalCfg:    cfg,
		readOnly:     ro,
	}
}

func (m AppModel) Init() tea.Cmd {
	// A tmux reply with $TMUX unset means a tmux elsewhere (SSH from a tmux
	// pane), which needs terminal copies wrapped; another name with $TMUX
	// set means keetui is not directly in tmux. No reply changes nothing.
	return tea.Batch(m.unlock.Init(), tea.RequestTerminalVersion, tea.RequestBackgroundColor)
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

// cancelClearClipboard invalidates a pending clear without arming another.
func (m *AppModel) cancelClearClipboard() tea.Cmd {
	m.clipboardGen++
	m.clipboardClearAt = time.Time{}
	return m.ensureSecurityTick()
}

func (m *AppModel) scheduleClearClipboard() tea.Cmd {
	d := m.cfg.ClipboardClearDuration()
	if d == 0 {
		return m.cancelClearClipboard()
	}
	m.clipboardGen++
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
		if m.clipboardDeferred {
			m.retryDeferredClear()
		}
		cmd := m.ensureSecurityTick()
		return m, cmd
	}
	if version, ok := msg.(tea.TerminalVersionMsg); ok {
		switch {
		case strings.HasPrefix(version.Name, "tmux "):
			m.terminalKind = terminalKind{class: terminalTmux, tmux: strings.TrimSpace(strings.TrimPrefix(version.Name, "tmux "))}
		case version.Name != "" && m.terminalKind.class == terminalUnknown:
			// Once tmux, always tmux: its routes never send a plain OSC 52.
			m.terminalKind = terminalKind{class: terminalOther}
		}
		return m, nil
	}
	if bg, ok := msg.(tea.BackgroundColorMsg); ok {
		lightBackground, backgroundKnown = !bg.IsDark(), true
		if m.cfg.Theme() == "auto" {
			ApplyTheme(themeFor(m.cfg))
			setInputPromptStyle(&m.browser.searchInput, focusedStyle)
		}
		return m, nil
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
		m.statusText, m.statusDetail = "", ""
		return m.updateActive(msg)
	}
	if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == "f1" && m.state != viewUnlock && m.state != viewHelp {
		m.recordActivity()
		return m.openHelp()
	}
	// Ctrl+O opens the settings wherever they can be opened, including the
	// unlock screen, whose password field takes every printable key. The
	// password typed so far is not kept.
	if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == "ctrl+o" && m.state == viewUnlock && !m.unlock.unlocking {
		m.unlock.input.SetValue("")
		return m, func() tea.Msg { return ShowSettingsMsg{} }
	}

	// Track user activity for auto-lock (key presses, mouse events, and pastes).
	if m.state != viewUnlock {
		switch msg.(type) {
		case tea.KeyPressMsg, tea.MouseMsg, tea.PasteMsg, tea.PasteStartMsg, tea.PasteEndMsg:
			m.recordActivity()
			m.statusText, m.statusDetail = "", "" // clear transient status on interaction
		}
	}

	// Handle StatusMsg at the app level (show in status bar)
	if sm, ok := msg.(StatusMsg); ok {
		m.statusText, m.statusError, m.statusDetail = sm.Text, sm.IsError, sm.Detail
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
			if m.dirty {
				m.confirm = m.confirm.Destructive()
			}
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
		}).Destructive()
		m.state = viewConfirm
		return m, nil

	case DeleteGroupMsg:
		m.confirm = NewConfirmModel(viewBrowser, fmt.Sprintf("Delete group '%s' and all its contents?", msg.Name), func() tea.Msg {
			return confirmDeleteGroupMsg{UUID: msg.GroupUUID}
		}).Destructive()
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
		}).Destructive()
		m.state = viewConfirm
		return m, nil

	case PermanentlyDeleteGroupMsg:
		m.confirm = NewConfirmModel(viewBrowser, fmt.Sprintf("Permanently delete group '%s' and all its contents?", msg.Name), func() tea.Msg {
			return confirmPermanentDeleteGroupMsg{UUID: msg.GroupUUID}
		}).Destructive()
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
		}).Destructive()
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
			m.stats = NewStatsModel(msg.stats, m.cfg.Keys())
			m.stats.width, m.stats.height = m.width, m.viewHeight()
		} else {
			m.audit = NewAuditModel(msg.audit, m.cfg.Keys())
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
		m.settings = NewSettingsModel(m.globalCfg)
		if m.db != nil {
			m.settings.setDatabase(m.dbOverrides, m.readOnly)
		}
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

	case SetDefaultPresetMsg:
		return m.setDefaultPreset(msg.Name)

	case ConfigSavedMsg:
		m.globalCfg = msg.Cfg
		status := "Settings saved to " + config.Path()
		if msg.Overrides != nil && m.db != nil && !m.readOnly {
			if m.db.SetCustomData(config.OverridesKey, msg.Overrides.Encode()) {
				m.dirty = true
				status += "; save the database to keep its settings"
			}
			m.setOverrides(*msg.Overrides)
		} else {
			m.setOverrides(m.dbOverrides)
		}
		if m.db == nil {
			// Saved from the unlock screen: back to it, in the new theme.
			m.state = viewUnlock
			return m, m.unlock.Init()
		}
		m.browser.keys = m.cfg.Keys()
		// Styles captured at construction time don't see ApplyTheme's
		// global updates; re-pull them on the long-lived browser model.
		setInputPromptStyle(&m.browser.searchInput, focusedStyle)
		m.recordActivity()
		m.state = viewBrowser
		autoLockCmd := m.scheduleAutoLock()
		return m, tea.Batch(
			statusCmd(status, false),
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
			}).Destructive()
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
		}).Destructive()
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
				}).Destructive()
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
		if m.db == nil {
			// Settings opened from the unlock screen return to it.
			m.state = viewUnlock
			return m, m.unlock.Init()
		}
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
		res, err := m.clipboardBackend().copy(msg.Value, copyOptions{
			transient: m.cfg.ClipboardClearDuration() > 0,
			osc52:     m.cfg.OSC52Mode(),
			terminal:  m.terminalKind,
		})
		// Over SSH, a failed X11 connection may have made the ssh client
		// print its error over the frame: repaint all of it.
		var repaint tea.Cmd
		if res.session.remote && res.nativeErr != nil {
			repaint = tea.ClearScreen
		}
		if err != nil {
			return m, tea.Batch(statusDetailCmd(fmt.Sprintf("Clipboard error: %s", err), true, clipboardHelp(res, err)), repaint)
		}
		m.clipboardHeld = true
		m.clipboardDeferred = false // the new copy replaced the old one
		var clearCmd tea.Cmd
		if res.autoClear() {
			clearCmd = m.scheduleClearClipboard()
		} else {
			// Nothing can clear it (tmux load-buffer): show clip:off rather
			// than a countdown to a clear that cannot happen.
			clearCmd = m.cancelClearClipboard()
		}
		return m, tea.Batch(statusDetailCmd(copyStatus(msg.Field, res), false, clipboardHelp(res, nil)), clearCmd, repaint)

	case clipboardClearMsg:
		if msg.gen != m.clipboardGen || m.clipboardClearAt.IsZero() {
			return m, nil
		}
		result, err := m.clipboardBackend().release()
		m.forgetCopiedValue()
		m.clipboardDeferred = result.pending() && err == nil
		cmd := tea.Batch(m.ensureSecurityTick(), repaintAfter(result))
		text, isError := clearStatus(result, err)
		if text == "" {
			return m, cmd
		}
		detail := ""
		if err == nil {
			detail = clearDetail(result)
		}
		return m, tea.Batch(statusDetailCmd(text, isError, detail), cmd)

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
		result, err := m.clipboardBackend().release()
		m.forgetCopiedValue()
		m.clipboardDeferred = result.pending() && err == nil
		m.autoLockCheckAt = time.Time{}
		m.db = nil
		m.setOverrides(config.Overrides{})
		m.undoStack.clear()
		m.unlock = NewUnlockModel(m.filePath, m.keyFilePath)
		m.unlock.err = fmt.Errorf("locked due to inactivity")
		m.state = viewUnlock
		m.quitReturn = nil
		return m, tea.Batch(m.unlock.Init(), m.ensureSecurityTick(), repaintAfter(result))
	}

	return m.updateActive(msg)
}

func (m AppModel) openHelp() (tea.Model, tea.Cmd) {
	if m.state == viewHelp {
		return m, nil
	}
	m.helpReturnState = m.state
	m.help = NewHelpModel(m.cfg.Keys(), m.width, m.viewHeight())
	m.help.section, m.help.context = m.helpContext()
	m.help.contextIndex = m.help.section
	if m.statusText != "" {
		m.help.message = sanitizeDisplay(m.statusText)
		if m.statusDetail != "" {
			m.help.message += "\n\n" + strings.Join(sanitizeNoteLines(m.statusDetail), "\n")
		}
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
	content := m.viewContent()
	if canvas, ok := canvasStyle(m.cfg); ok {
		width, _ := viewSize(m.width, m.height)
		content = paintCanvas(content, width, canvas)
	}
	view := tea.NewView(content)
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	return view
}

func (m AppModel) viewContent() string {
	if m.state == viewUnlock {
		return fitFrame(m.unlock.View().Content, m.width, m.height)
	}
	if m.height <= 1 {
		return fitFrame(m.statusBar(), m.width, m.height)
	}
	return fitFrame(m.stateContent(m.state), m.width, m.viewHeight()) + "\n" + m.statusBar()
}

// confirmView draws the confirmation over the screen it returns to, dimmed,
// so the question stays next to what it is about.
func (m AppModel) confirmView() string {
	under := ""
	if s := m.confirm.returnState; s != viewConfirm && s != viewNone && s != viewUnlock {
		under = ansi.Strip(fitFrame(m.stateContent(s), m.width, m.viewHeight()))
	}
	d := m.confirm.dialog()
	return overlay(hiddenStyle.Render(fitFrame(under, m.width, m.viewHeight())), d.box, d.x, d.y)
}

// stateContent renders the view of state, above the status bar.
func (m AppModel) stateContent(state viewState) string {
	var view string
	switch state {
	case viewBrowser:
		view = m.browser.View().Content
	case viewDetail:
		view = m.detail.View().Content
	case viewEditor:
		view = m.editor.View().Content
	case viewGroupInput:
		view = m.groupInput.View().Content
	case viewConfirm:
		view = m.confirmView()
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
	return view
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
	if m.db != nil {
		return m, nil // already open: a late duplicate
	}
	m.unlock.unlocking = true
	m.unlock.err = nil
	m.openGen++
	gen := m.openGen
	return m, func() tea.Msg {
		var db *keepass.DB
		var err error
		if msg.KeyFilePath != "" {
			db, err = keepass.OpenWithKeyFile(msg.FilePath, msg.Password, msg.KeyFilePath)
		} else {
			db, err = keepass.Open(msg.FilePath, msg.Password)
		}
		return databaseOpenResultMsg{db: db, err: err, gen: gen}
	}
}

func (m AppModel) handleDatabaseOpenResult(msg databaseOpenResultMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.openGen || m.db != nil {
		// A superseded attempt, or one finishing after another opened the
		// database: replacing it would drop the edits made since.
		return m, nil
	}
	m.unlock.unlocking = false
	if msg.err != nil {
		m.unlock.err = fmt.Errorf("failed to unlock: %w", msg.err)
		return m, nil
	}
	db := msg.db
	m.db = db
	overrides, overridesErr := config.ParseOverrides(db.CustomData(config.OverridesKey))
	m.setOverrides(overrides)
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
	if overridesErr != nil {
		return m, tea.Batch(autoLockCmd,
			statusCmd(fmt.Sprintf("This database's settings are unreadable, using the global ones: %s", overridesErr), true))
	}
	return m, autoLockCmd
}

// setOverrides makes o the open database's settings: the settings in
// effect become the global ones with o on top.
func (m *AppModel) setOverrides(o config.Overrides) {
	m.dbOverrides = o
	m.cfg = m.globalCfg.Apply(o)
	if m.db != nil {
		m.db.BackupOnSave = m.cfg.BackupEnabled()
	}
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
	m.statusText, m.statusError, m.statusDetail = "Saving…", false, ""
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
	m.statusText, m.statusError, m.statusDetail = "Saving… please wait", false, ""
	return m, nil
}

// repaintAfter redraws the whole screen after a clear that may have made
// the ssh client print over the frame (see clearResult.repaint).
func repaintAfter(r clearResult) tea.Cmd {
	if r.repaint {
		return tea.ClearScreen
	}
	return nil
}

// clearClipboardOnQuit clears the clipboard if a keetui copy is still
// outstanding, so a copied secret does not outlive the process. The
// clipboard is left alone when the user has since copied something else.
func (m AppModel) clearClipboardOnQuit() {
	_, _ = m.clipboardBackend().release()
}

func statusCmd(text string, isError bool) tea.Cmd {
	return statusDetailCmd(text, isError, "")
}

// statusDetailCmd also carries guidance for F1 help.
func statusDetailCmd(text string, isError bool, detail string) tea.Cmd {
	return func() tea.Msg {
		return StatusMsg{Text: text, IsError: isError, Detail: detail}
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

// setDefaultPreset changes the default preset where it is set: in the
// database when the database sets one, else in the config file.
func (m AppModel) setDefaultPreset(name string) (tea.Model, tea.Cmd) {
	var status string
	switch {
	case m.dbOverrides.DefaultPreset != nil && m.readOnly:
		return m, statusCmd("This read-only database sets its own default preset", true)
	case m.dbOverrides.DefaultPreset != nil:
		overrides := m.dbOverrides
		overrides.DefaultPreset = &name
		if m.db.SetCustomData(config.OverridesKey, overrides.Encode()) {
			m.dirty = true
		}
		m.setOverrides(overrides)
		status = "Default preset set for this database; save the database to keep it"
	default:
		global := m.globalCfg
		global.DefaultPreset = name
		if err := global.Save(); err != nil {
			return m, statusCmd(fmt.Sprintf("Default preset not saved: %s", err), true)
		}
		m.globalCfg = global
		m.setOverrides(m.dbOverrides)
		status = "Default preset updated"
	}
	m.presetPicker.cfg = m.cfg
	return m, statusCmd(status, false)
}
