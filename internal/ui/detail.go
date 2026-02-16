package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/boombuler/barcode/qr"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pquerna/otp/totp"
	"github.com/tobischo/gokeepasslib/v3"

	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/keepass"
)

// totpTickMsg is sent every second to refresh the TOTP code.
type totpTickMsg time.Time

// hibpResultMsg carries the result of a HIBP breach check.
type hibpResultMsg struct {
	Breached bool
	Count    int
	Err      error
}

// ExtractAttachmentMsg requests extracting an attachment to disk.
type ExtractAttachmentMsg struct {
	EntryUUID gokeepasslib.UUID
	Name      string
	DestPath  string
}

// DeleteAttachmentMsg requests deleting an attachment from an entry.
type DeleteAttachmentMsg struct {
	EntryUUID gokeepasslib.UUID
	Name      string
}

// AddAttachmentMsg requests adding a file attachment to an entry.
type AddAttachmentMsg struct {
	EntryUUID gokeepasslib.UUID
	FilePath  string
}

// DetailModel shows a read-only view of a single entry.
type DetailModel struct {
	entry          keepass.Entry
	revealedFields map[string]bool // per-field password visibility, keyed by field label
	showQR         bool
	qrLabel      string // label of the field currently shown as QR
	totpCode     string
	totpRemain   int // seconds remaining in current TOTP period
	width        int
	height       int
	copied       string
	strength     keepass.StrengthResult
	hibpChecking bool
	hibpResult   *hibpResultMsg
	attachments  []keepass.Attachment
	viewport     viewport.Model
	ready        bool // true after first size is set
	cfg          config.Config

	readOnly bool // when true, edit and attachment mutations are suppressed

	// Field selection
	selectedField int // index into selectableFields()

	// Attachment mode state
	attachMode       bool
	attachIdx        int
	addingAttachment bool            // true when file path input is shown
	attachPathInput  textinput.Model // input for file path when adding attachment
	keys             config.KeyBindings
}

// selectableField represents a field the user can highlight and act on.
type selectableField struct {
	label     string
	value     string // used for copy
	qrValue   string // used for QR; if empty, falls back to value
	protected bool   // true for password-type fields that can be revealed
}

// selectableFields returns the ordered list of fields the cursor can move through.
func (m DetailModel) selectableFields() []selectableField {
	var f []selectableField
	f = append(f, selectableField{label: "username", value: m.entry.UserName})
	f = append(f, selectableField{label: "password", value: m.entry.Password, protected: true})
	f = append(f, selectableField{label: "URL", value: m.entry.URL})
	if m.totpCode != "" {
		f = append(f, selectableField{label: "TOTP", value: m.totpCode, qrValue: m.entry.TOTP, protected: true})
	}
	if m.entry.Notes != "" {
		f = append(f, selectableField{label: "notes", value: m.entry.Notes})
	}
	for _, cf := range m.entry.CustomFields {
		f = append(f, selectableField{label: cf.Key, value: cf.Value, protected: cf.Protected})
	}
	return f
}

// fieldAtLine returns the selectable field index for a given content line number.
// Returns (index, true) if the line corresponds to a selectable field, or (0, false) otherwise.
func (m DetailModel) fieldAtLine(line int) (int, bool) {
	if line < 0 {
		return 0, false
	}
	fieldIdx := 0
	currentLine := 0

	// Username (1 line)
	if line == currentLine {
		return fieldIdx, true
	}
	fieldIdx++
	currentLine++

	// Password (1 line)
	if line == currentLine {
		return fieldIdx, true
	}
	fieldIdx++
	currentLine++

	// URL (1 line)
	if line == currentLine {
		return fieldIdx, true
	}
	fieldIdx++
	currentLine++

	// TOTP (1 line, optional)
	if m.totpCode != "" {
		if line == currentLine {
			return fieldIdx, true
		}
		fieldIdx++
		currentLine++
	}

	// Notes (1 label line + N body lines, optional)
	if m.entry.Notes != "" {
		bodyLines := len(strings.Split(m.entry.Notes, "\n"))
		totalNotesLines := 1 + bodyLines // label + body
		if line == currentLine {
			return fieldIdx, true // click on "Notes:" label
		}
		if line > currentLine && line < currentLine+totalNotesLines {
			return fieldIdx, true // click on notes body
		}
		fieldIdx++
		currentLine += totalNotesLines
	}

	// Non-selectable timestamp lines
	if !m.entry.CreationTime.IsZero() {
		currentLine++
	}
	if !m.entry.ModificationTime.IsZero() {
		currentLine++
	}
	if m.entry.Expires {
		currentLine++
	}

	// Custom fields (1 line each)
	for range m.entry.CustomFields {
		if line == currentLine {
			return fieldIdx, true
		}
		fieldIdx++
		currentLine++
	}

	return 0, false
}

// BackToBrowserMsg signals returning to the browser.
type BackToBrowserMsg struct{}

// BackToDetailMsg signals returning to the detail view from a sub-view.
type BackToDetailMsg struct{}

// CopyToClipboardMsg requests copying a value to clipboard.
type CopyToClipboardMsg struct {
	Field string
	Value string
}

func NewDetailModel(entry keepass.Entry, cfg config.Config) DetailModel {
	pi := textinput.New()
	pi.Prompt = "  File path: "
	pi.PromptStyle = labelStyle
	pi.Width = 50
	pi.Placeholder = "/path/to/file"

	m := DetailModel{
		entry:           entry,
		cfg:             cfg,
		keys:            cfg.Keys(),
		revealedFields:  make(map[string]bool),
		attachPathInput: pi,
	}
	if cfg.ShowPasswordStrength {
		m.strength = keepass.EvaluatePasswordStrength(entry.Password)
	}
	m.refreshTOTP()
	return m
}

// SetAttachments sets the attachment list for display.
func (m *DetailModel) SetAttachments(attachments []keepass.Attachment) {
	m.attachments = attachments
}

// SetSize initializes or resizes the viewport to match the terminal dimensions.
func (m *DetailModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.syncViewportSize()
}

// RefreshContent rebuilds the viewport content after external state changes.
func (m *DetailModel) RefreshContent() {
	if m.ready {
		m.viewport.SetContent(m.buildContent())
	}
}

// lineForField returns the content line number for the given selectable field index.
func (m DetailModel) lineForField(fieldIdx int) int {
	idx := 0
	line := 0

	// Username (1 line)
	if idx == fieldIdx {
		return line
	}
	idx++
	line++

	// Password (1 line)
	if idx == fieldIdx {
		return line
	}
	idx++
	line++

	// URL (1 line)
	if idx == fieldIdx {
		return line
	}
	idx++
	line++

	// TOTP (optional, 1 line)
	if m.totpCode != "" {
		if idx == fieldIdx {
			return line
		}
		idx++
		line++
	}

	// Notes (optional, 1 label line + N body lines)
	if m.entry.Notes != "" {
		if idx == fieldIdx {
			return line
		}
		idx++
		line += 1 + len(strings.Split(m.entry.Notes, "\n"))
	}

	// Timestamps (non-selectable)
	if !m.entry.CreationTime.IsZero() {
		line++
	}
	if !m.entry.ModificationTime.IsZero() {
		line++
	}
	if m.entry.Expires {
		line++
	}

	// Custom fields (1 line each)
	for range m.entry.CustomFields {
		if idx == fieldIdx {
			return line
		}
		idx++
		line++
	}

	return line
}

// refreshContentAndFollow rebuilds the viewport content and scrolls to keep
// the selected field visible.
func (m *DetailModel) refreshContentAndFollow() {
	if !m.ready {
		return
	}
	m.viewport.SetContent(m.buildContent())
	line := m.lineForField(m.selectedField)
	vpHeight := m.viewport.Height
	if line < m.viewport.YOffset {
		m.viewport.SetYOffset(line)
	} else if line >= m.viewport.YOffset+vpHeight {
		m.viewport.SetYOffset(line - vpHeight + 1)
	}
}

func (m DetailModel) Init() tea.Cmd {
	if m.entry.TOTP != "" {
		return tickTOTP()
	}
	return nil
}

func tickTOTP() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return totpTickMsg(t)
	})
}

func (m *DetailModel) refreshTOTP() {
	secret := keepass.TOTPSecret(m.entry.TOTP)
	if secret == "" {
		return
	}
	period := keepass.TOTPPeriod(m.entry.TOTP)
	now := time.Now()
	code, err := totp.GenerateCodeCustom(secret, now, totp.ValidateOpts{
		Period: period,
		Digits: 6,
	})
	if err != nil {
		m.totpCode = ""
		m.totpRemain = 0
		return
	}
	m.totpCode = code
	m.totpRemain = int(period) - int(now.Unix()%int64(period))
}

func (m *DetailModel) syncViewportSize() {
	// View() concatenates: headerView() + viewport.View() + "\n" + footerView()
	// Count newlines contributed by each section to compute viewport height.
	headerNewlines := strings.Count(m.headerView(), "\n")
	footerNewlines := strings.Count(m.footerView(), "\n")
	// Total lines = headerNewlines + vpHeight + 1 + footerNewlines
	// We need total lines = m.height
	vpHeight := m.height - headerNewlines - footerNewlines - 1
	if vpHeight < 1 {
		vpHeight = 1
	}
	if !m.ready {
		m.viewport = viewport.New(m.width, vpHeight)
		m.viewport.SetContent(m.buildContent())
		m.ready = true
	} else {
		m.viewport.Width = m.width
		m.viewport.Height = vpHeight
	}
}

func (m DetailModel) headerView() string {
	title := m.entry.Title
	if title == "" {
		title = "(untitled)"
	}
	return "\n" + titleStyle.Render(fmt.Sprintf(" %s ", title)) + "\n"
}

func (m DetailModel) footerView() string {
	if m.showQR {
		return helpStyle.Render(fmt.Sprintf("  QR: %s  esc: close  %s: close", m.qrLabel, m.keys.ShowQR))
	}
	if m.addingAttachment {
		return helpStyle.Render("  enter: add file  esc: cancel")
	}
	if m.attachMode {
		help := "  j/k: select  s: save to disk  d: delete  +: add file  esc: back"
		return helpStyle.Render(help)
	}
	// Show selected field name in footer when it's a custom field
	var fieldPrefix string
	fields := m.selectableFields()
	if m.selectedField < len(fields) {
		f := fields[m.selectedField]
		// Show full label for custom fields (which may have been truncated in the view)
		for _, cf := range m.entry.CustomFields {
			if cf.Key == f.label {
				fieldPrefix = fmt.Sprintf("  [%s]", f.label)
				break
			}
		}
	}

	var help string
	if m.readOnly {
		help = fmt.Sprintf("  j/k: select  enter: copy  %s: QR  %s: toggle pw  %s: url",
			m.keys.ShowQR, m.keys.TogglePassword, m.keys.OpenURL)
		if m.entry.HistoryCount > 0 {
			help += fmt.Sprintf("  %s: history", m.keys.ShowHistory)
		}
		help += fmt.Sprintf("  %s: HIBP  esc: back  %s: quit", m.keys.HIBPCheck, m.keys.Quit)
	} else {
		help = fmt.Sprintf("  j/k: select  enter: copy  %s: QR  %s: toggle pw  %s: edit  %s: url  %s: attach",
			m.keys.ShowQR, m.keys.TogglePassword, m.keys.Edit, m.keys.OpenURL, m.keys.Attachments)
		if m.entry.HistoryCount > 0 {
			help += fmt.Sprintf("  %s: history", m.keys.ShowHistory)
		}
		help += fmt.Sprintf("  %s: HIBP  esc: back  %s: quit", m.keys.HIBPCheck, m.keys.Quit)
	}
	return helpStyle.Render(fieldPrefix + help)
}

// truncateString truncates s to maxWidth runes, appending "…" if truncated.
func truncateString(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxWidth {
		return s
	}
	if maxWidth <= 1 {
		return "…"
	}
	return string(runes[:maxWidth-1]) + "…"
}

// truncateLabel shortens a field label to fit within the labelStyle fixed width.
// labelStyle has Width(12), so any label longer than that overflows and wraps.
// The full label is shown in the footer when the field is selected.
func truncateLabel(label string) string {
	// labelStyle uses a fixed Width — truncate label text to fit
	styleWidth := labelStyle.GetWidth()
	if styleWidth <= 0 {
		return label
	}
	return truncateString(label, styleWidth)
}

// buildContent returns the scrollable body content (everything between title and help bar).
func (m DetailModel) buildContent() string {
	var b strings.Builder

	fieldIdx := 0
	cursor := func() string {
		if fieldIdx == m.selectedField {
			fieldIdx++
			return selectedItemStyle.Render("▸ ")
		}
		fieldIdx++
		return "  "
	}

	b.WriteString(fmt.Sprintf("%s%s %s\n",
		cursor(),
		labelStyle.Render("Username:"),
		valueStyle.Render(m.entry.UserName)))

	pw := strings.Repeat("*", len(m.entry.Password))
	if m.revealedFields["password"] {
		pw = m.entry.Password
	}
	if m.cfg.ShowPasswordStrength {
		strengthStr := renderStrength(m.strength)
		b.WriteString(fmt.Sprintf("%s%s %s  %s\n",
			cursor(),
			labelStyle.Render("Password:"),
			hiddenStyle.Render(pw),
			strengthStr))
	} else {
		b.WriteString(fmt.Sprintf("%s%s %s\n",
			cursor(),
			labelStyle.Render("Password:"),
			hiddenStyle.Render(pw)))
	}

	b.WriteString(fmt.Sprintf("%s%s %s\n",
		cursor(),
		labelStyle.Render("URL:"),
		valueStyle.Render(m.entry.URL)))

	if m.totpCode != "" {
		if m.revealedFields["TOTP"] {
			b.WriteString(fmt.Sprintf("%s%s %s\n",
				cursor(),
				labelStyle.Render("TOTP:"),
				valueStyle.Render(m.entry.TOTP)))
		} else {
			bar := totpCountdownBar(m.totpRemain, int(keepass.TOTPPeriod(m.entry.TOTP)))
			b.WriteString(fmt.Sprintf("%s%s %s  %s\n",
				cursor(),
				labelStyle.Render("TOTP:"),
				valueStyle.Render(m.totpCode),
				helpStyle.Render(fmt.Sprintf("%s %ds", bar, m.totpRemain))))
		}
	}

	if m.entry.Notes != "" {
		b.WriteString(fmt.Sprintf("%s%s\n",
			cursor(),
			labelStyle.Render("Notes:")))
		for _, line := range strings.Split(m.entry.Notes, "\n") {
			b.WriteString(fmt.Sprintf("  %s%s\n",
				strings.Repeat(" ", 13),
				valueStyle.Render(line)))
		}
	}

	if !m.entry.CreationTime.IsZero() {
		b.WriteString(fmt.Sprintf("  %s %s\n",
			labelStyle.Render("Created:"),
			hiddenStyle.Render(m.entry.CreationTime.Local().Format("2006-01-02 15:04:05"))))
	}
	if !m.entry.ModificationTime.IsZero() {
		b.WriteString(fmt.Sprintf("  %s %s\n",
			labelStyle.Render("Modified:"),
			hiddenStyle.Render(m.entry.ModificationTime.Local().Format("2006-01-02 15:04:05"))))
	}
	if m.entry.Expires {
		expiryStr := m.entry.ExpiryTime.Local().Format("2006-01-02 15:04:05")
		if m.entry.IsExpired() {
			b.WriteString(fmt.Sprintf("  %s %s\n",
				labelStyle.Render("Expires:"),
				errorMsgStyle.Render(expiryStr+" (EXPIRED)")))
		} else {
			b.WriteString(fmt.Sprintf("  %s %s\n",
				labelStyle.Render("Expires:"),
				hiddenStyle.Render(expiryStr)))
		}
	}

	for _, cf := range m.entry.CustomFields {
		val := cf.Value
		if cf.Protected && !m.revealedFields[cf.Key] {
			val = strings.Repeat("*", len(cf.Value))
		}
		label := truncateLabel(cf.Key + ":")
		b.WriteString(fmt.Sprintf("%s%s %s\n",
			cursor(),
			labelStyle.Render(label),
			valueStyle.Render(val)))
	}

	// Attachments
	if len(m.attachments) > 0 {
		label := "Attachments:"
		if m.attachMode {
			label = "Attachments (select):"
		}
		b.WriteString(fmt.Sprintf("\n  %s\n", helpStyle.Render(label)))
		for i, a := range m.attachments {
			cursor := "  "
			nameStyle := valueStyle
			if m.attachMode && i == m.attachIdx {
				cursor = "> "
				nameStyle = selectedItemStyle
			}
			b.WriteString(fmt.Sprintf("  %s%s (%s)\n",
				cursor,
				nameStyle.Render(a.Name),
				hiddenStyle.Render(formatAttachmentSize(a.Size))))
		}
	}

	if m.addingAttachment {
		b.WriteString("\n")
		b.WriteString(m.attachPathInput.View())
		b.WriteString("\n")
	}

	// HIBP result
	if m.hibpChecking {
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("  Checking HIBP..."))
		b.WriteString("\n")
	} else if m.hibpResult != nil {
		b.WriteString("\n")
		if m.hibpResult.Err != nil {
			b.WriteString(errorMsgStyle.Render(fmt.Sprintf("  HIBP check failed: %s", m.hibpResult.Err)))
		} else if m.hibpResult.Breached {
			b.WriteString(errorMsgStyle.Render(fmt.Sprintf("  WARNING: Password found in %d data breaches!", m.hibpResult.Count)))
		} else {
			b.WriteString(successStyle.Render("  Password not found in any known breaches"))
		}
		b.WriteString("\n")
	}

	if m.copied != "" {
		b.WriteString("\n")
		b.WriteString(successStyle.Render(fmt.Sprintf("  Copied %s to clipboard!", m.copied)))
		b.WriteString("\n")
	}

	return b.String()
}

func (m DetailModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.syncViewportSize()
		return m, nil

	case totpTickMsg:
		m.refreshTOTP()
		if m.ready && !m.showQR {
			m.viewport.SetContent(m.buildContent())
		}
		return m, tickTOTP()

	case tea.MouseMsg:
		if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress && !m.showQR && !m.attachMode && !m.addingAttachment {
			// Map click Y to selectable field index.
			// The viewport content starts at the row after the header's last
			// visible line. strings.Count gives the number of newlines, which
			// equals the Y offset where viewport content begins.
			vpStartY := strings.Count(m.headerView(), "\n")
			vpRow := msg.Y - vpStartY + m.viewport.YOffset
			if idx, ok := m.fieldAtLine(vpRow); ok {
				m.selectedField = idx
				m.refreshContentAndFollow()
			}
			return m, nil
		}
		// Delegate other mouse events to viewport for scroll support
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		// File path input mode for adding attachments
		if m.addingAttachment {
			switch msg.String() {
			case "esc":
				m.addingAttachment = false
				m.attachPathInput.Blur()
				m.attachPathInput.SetValue("")
				if m.ready {
					m.viewport.SetContent(m.buildContent())
				}
				return m, nil
			case "enter":
				path := m.attachPathInput.Value()
				if path != "" {
					m.addingAttachment = false
					m.attachPathInput.Blur()
					m.attachPathInput.SetValue("")
					entryUUID := m.entry.UUID
					return m, func() tea.Msg {
						return AddAttachmentMsg{EntryUUID: entryUUID, FilePath: path}
					}
				}
				return m, nil
			default:
				var cmd tea.Cmd
				m.attachPathInput, cmd = m.attachPathInput.Update(msg)
				if m.ready {
					m.viewport.SetContent(m.buildContent())
				}
				return m, cmd
			}
		}

		// Attachment selection mode
		if m.attachMode {
			switch msg.String() {
			case "esc", "h", "left":
				m.attachMode = false
				if m.ready {
					m.viewport.SetContent(m.buildContent())
				}
				return m, nil
			case "j", "down":
				if m.attachIdx < len(m.attachments)-1 {
					m.attachIdx++
					if m.ready {
						m.viewport.SetContent(m.buildContent())
					}
				}
				return m, nil
			case "k", "up":
				if m.attachIdx > 0 {
					m.attachIdx--
					if m.ready {
						m.viewport.SetContent(m.buildContent())
					}
				}
				return m, nil
			case "s", "enter":
				if len(m.attachments) > 0 {
					a := m.attachments[m.attachIdx]
					entryUUID := m.entry.UUID
					return m, func() tea.Msg {
						return ExtractAttachmentMsg{
							EntryUUID: entryUUID,
							Name:      a.Name,
							DestPath:  filepath.Base(a.Name), // strip directory components for safety
						}
					}
				}
				return m, nil
			case "d":
				if len(m.attachments) > 0 {
					a := m.attachments[m.attachIdx]
					entryUUID := m.entry.UUID
					return m, func() tea.Msg {
						return DeleteAttachmentMsg{
							EntryUUID: entryUUID,
							Name:      a.Name,
						}
					}
				}
				return m, nil
			case "+":
				m.addingAttachment = true
				m.attachPathInput.SetValue("")
				m.attachPathInput.Focus()
				if m.ready {
					m.viewport.SetContent(m.buildContent())
				}
				return m, textinput.Blink
			case "q":
				return m, func() tea.Msg { return RequestQuitMsg{} }
			case "ctrl+c":
				return m, func() tea.Msg { return RequestQuitMsg{} }
			}
			return m, nil
		}

		// QR code display mode
		if m.showQR {
			switch msg.String() {
			case "esc", "Q":
				m.showQR = false
				m.viewport.SetContent(m.buildContent())
			}
			return m, nil
		}

		// Normal detail view key handling
		key := msg.String()
		switch key {
		case m.keys.Quit:
			return m, func() tea.Msg { return RequestQuitMsg{} }
		case "ctrl+c":
			return m, func() tea.Msg { return RequestQuitMsg{} }
		}

		fields := m.selectableFields()

		switch {
		case key == "j" || key == "down":
			if m.selectedField < len(fields)-1 {
				m.selectedField++
				m.refreshContentAndFollow()
			}
			return m, nil

		case key == "k" || key == "up":
			if m.selectedField > 0 {
				m.selectedField--
				m.refreshContentAndFollow()
			}
			return m, nil

		case key == "enter":
			// Copy selected field; if revealed and has a separate qrValue
			// (e.g. TOTP secret), copy that instead of the display value.
			if m.selectedField < len(fields) {
				f := fields[m.selectedField]
				val := f.value
				if f.qrValue != "" && m.revealedFields[f.label] {
					val = f.qrValue
				}
				if val != "" {
					return m, func() tea.Msg {
						return CopyToClipboardMsg{Field: f.label, Value: val}
					}
				}
			}
			return m, nil

		case key == "esc" || key == m.keys.Back || key == "left":
			return m, func() tea.Msg { return BackToBrowserMsg{} }

		case key == m.keys.TogglePassword:
			if m.selectedField < len(fields) {
				f := fields[m.selectedField]
				if f.protected {
					m.revealedFields[f.label] = !m.revealedFields[f.label]
					if m.ready {
						m.viewport.SetContent(m.buildContent())
					}
				}
			}
			return m, nil

		case key == m.keys.Edit:
			if m.readOnly {
				return m, func() tea.Msg { return StatusMsg{Text: "Read-only mode", IsError: true} }
			}
			return m, func() tea.Msg {
				return EditEntryMsg{Entry: m.entry}
			}

		case key == m.keys.Attachments:
			if m.readOnly {
				return m, func() tea.Msg { return StatusMsg{Text: "Read-only mode", IsError: true} }
			}
			// Enter attachment mode if there are attachments, or go to add mode
			if len(m.attachments) > 0 {
				m.attachMode = true
				m.attachIdx = 0
				if m.ready {
					m.viewport.SetContent(m.buildContent())
				}
			} else {
				// No attachments yet, go directly to add mode
				m.attachMode = true
				m.addingAttachment = true
				m.attachPathInput.SetValue("")
				m.attachPathInput.Focus()
				if m.ready {
					m.viewport.SetContent(m.buildContent())
				}
				return m, textinput.Blink
			}
			return m, nil

		case key == m.keys.CopyUsername:
			return m, func() tea.Msg {
				return CopyToClipboardMsg{Field: "username", Value: m.entry.UserName}
			}

		case key == m.keys.CopyPassword:
			return m, func() tea.Msg {
				return CopyToClipboardMsg{Field: "password", Value: m.entry.Password}
			}

		case key == m.keys.CopyURL:
			return m, func() tea.Msg {
				return CopyToClipboardMsg{Field: "url", Value: m.entry.URL}
			}

		case key == m.keys.CopyTOTP:
			if m.totpCode != "" {
				return m, func() tea.Msg {
					return CopyToClipboardMsg{Field: "TOTP code", Value: m.totpCode}
				}
			}

		case key == m.keys.HIBPCheck:
			// HIBP breach check
			if !m.hibpChecking && m.entry.Password != "" {
				m.hibpChecking = true
				m.hibpResult = nil
				if m.ready {
					m.viewport.SetContent(m.buildContent())
				}
				pw := m.entry.Password
				return m, func() tea.Msg {
					result := keepass.CheckHIBP(pw)
					return hibpResultMsg{
						Breached: result.Breached,
						Count:    result.Count,
						Err:      result.Err,
					}
				}
			}

		case key == m.keys.ShowQR:
			if m.selectedField < len(fields) {
				f := fields[m.selectedField]
				val := f.qrValue
				if val == "" {
					val = f.value
				}
				if val == "" {
					return m, nil
				}
				m.showQR = true
				m.qrLabel = f.label
				if m.ready {
					m.viewport.GotoTop()
					m.viewport.SetContent(m.renderQR(val, m.width, m.viewport.Height))
				}
			}
			return m, nil

		case key == m.keys.OpenURL:
			if m.entry.URL != "" {
				return m, func() tea.Msg {
					return OpenURLMsg{URL: m.entry.URL}
				}
			}

		case key == "?":
			return m, func() tea.Msg { return ShowHelpMsg{} }

		case key == m.keys.ShowHistory:
			if m.entry.HistoryCount > 0 {
				entryTitle := m.entry.Title
				entryUUID := m.entry.UUID
				return m, func() tea.Msg {
					return ShowHistoryMsg{EntryTitle: entryTitle, EntryUUID: entryUUID}
				}
			}
		}
	}

	// Delegate remaining messages to viewport (pgup/pgdn, mouse, etc.)
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m DetailModel) View() string {
	if !m.ready {
		return ""
	}
	return m.headerView() + m.viewport.View() + "\n" + m.footerView()
}

// renderStrength returns a styled strength label.
func renderStrength(s keepass.StrengthResult) string {
	label := fmt.Sprintf("[%s %.0f bits]", s.Label, s.Entropy)
	switch s.Score {
	case keepass.StrengthWeak:
		return errorMsgStyle.Render(label)
	case keepass.StrengthFair:
		return warnStyle.Render(label)
	case keepass.StrengthGood:
		return valueStyle.Render(label)
	case keepass.StrengthStrong, keepass.StrengthExcellent:
		return successStyle.Render(label)
	default:
		return hiddenStyle.Render(label)
	}
}

// renderStrengthDetail returns the strength label plus a character class breakdown.
// Missing classes are highlighted to guide the user.
func renderStrengthDetail(s keepass.StrengthResult) string {
	base := renderStrength(s)

	check := func(has bool, label string) string {
		if has {
			return successStyle.Render(label)
		}
		return hiddenStyle.Render(label)
	}

	classes := fmt.Sprintf(" %s %s %s %s  %s",
		check(s.HasLower, "a-z"),
		check(s.HasUpper, "A-Z"),
		check(s.HasDigit, "0-9"),
		check(s.HasSymbol, "!@#"),
		hiddenStyle.Render(fmt.Sprintf("len:%d", s.Length)))

	return base + classes
}

func formatAttachmentSize(bytes int) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
}

// renderQR renders the given text as a QR code using Unicode half-block characters.
func (m DetailModel) renderQR(text string, maxWidth, maxHeight int) string {
	qrCode, err := qr.Encode(text, qr.M, qr.Auto)
	if err != nil {
		return errorMsgStyle.Render(fmt.Sprintf("  QR encode error: %s", err))
	}

	bounds := qrCode.Bounds()
	w := bounds.Max.X - bounds.Min.X
	h := bounds.Max.Y - bounds.Min.Y

	// Add 1-module quiet zone on each side
	totalW := w + 2
	totalH := h + 2

	// Each character is 1 module wide, 2 modules tall (half-block)
	charH := (totalH + 1) / 2

	if totalW > maxWidth || charH > maxHeight {
		return errorMsgStyle.Render("  QR code too large for terminal")
	}

	// Check if a module is black (data); quiet zone pixels are white
	isBlack := func(x, y int) bool {
		qx := x - 1 + bounds.Min.X
		qy := y - 1 + bounds.Min.Y
		if qx < bounds.Min.X || qx >= bounds.Max.X || qy < bounds.Min.Y || qy >= bounds.Max.Y {
			return false
		}
		r, _, _, _ := qrCode.At(qx, qy).RGBA()
		return r == 0
	}

	blackFgWhiteBg := lipgloss.NewStyle().Foreground(lipgloss.Color("#000000")).Background(lipgloss.Color("#FFFFFF"))
	whiteFgBlackBg := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#000000"))
	whiteBg := lipgloss.NewStyle().Background(lipgloss.Color("#FFFFFF"))
	blackBg := lipgloss.NewStyle().Background(lipgloss.Color("#000000"))

	pad := ""
	if totalW < maxWidth {
		pad = strings.Repeat(" ", (maxWidth-totalW)/2)
	}

	var lines []string
	for row := 0; row < totalH; row += 2 {
		var line strings.Builder
		line.WriteString(pad)
		for col := 0; col < totalW; col++ {
			topBlack := isBlack(col, row)
			bottomBlack := row+1 < totalH && isBlack(col, row+1)

			switch {
			case topBlack && bottomBlack:
				line.WriteString(blackBg.Render(" "))
			case !topBlack && !bottomBlack:
				line.WriteString(whiteBg.Render(" "))
			case topBlack && !bottomBlack:
				line.WriteString(blackFgWhiteBg.Render("▀"))
			default: // !topBlack && bottomBlack
				line.WriteString(whiteFgBlackBg.Render("▀"))
			}
		}
		lines = append(lines, line.String())
	}

	return strings.Join(lines, "\n")
}

// totpCountdownBar renders a visual progress bar for the TOTP countdown.
func totpCountdownBar(remaining, period int) string {
	const width = 10
	filled := remaining * width / period
	if filled > width {
		filled = width
	}
	return "[" + strings.Repeat("#", filled) + strings.Repeat("-", width-filled) + "]"
}
