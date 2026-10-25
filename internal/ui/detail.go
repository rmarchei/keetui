package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/boombuler/barcode/qr"
	"github.com/charmbracelet/x/ansi"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"github.com/tobischo/gokeepasslib/v3"

	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/keepass"
)

// maxNoteLines is the number of note body lines shown before truncation.
const maxNoteLines = 5

// maskedValue is the constant-width placeholder for hidden secrets. A
// length-proportional mask would reveal the secret's exact length to
// anyone viewing the screen.
const maskedValue = "********"

// totpTickMsg is sent every second to refresh the TOTP code. The sequence
// number identifies the tick chain; ticks from a superseded chain are
// dropped instead of re-armed.
type totpTickMsg struct {
	seq int
}

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
	qrLabel        string // label of the field currently shown as QR
	totpCode       string
	totpRemain     int // seconds remaining in current TOTP period
	totpSeq        int // current TOTP tick-chain sequence; see totpTickMsg
	width          int
	height         int
	strength       keepass.StrengthResult
	hibpChecking   bool
	attachments    []keepass.Attachment
	viewport       viewport.Model
	ready          bool // true after first size is set
	cfg            config.Config

	readOnly      bool // when true, edit and attachment mutations are suppressed
	notesExpanded bool // true when long notes are fully shown

	// Field selection
	selectedField int // index into selectableFields()

	// Attachment mode state
	attachMode           bool
	attachIdx            int
	addingAttachment     bool            // true when file path input is shown for adding
	extractingAttachment bool            // true when file path input is shown for extracting
	extractAttachName    string          // name of attachment being extracted
	attachPathInput      textinput.Model // input for file path (add or extract)
	keys                 config.KeyBindings
}

// selectableField represents a field the user can highlight and act on.
type selectableField struct {
	label     string
	value     string // used for copy
	qrValue   string // used for QR; if empty, falls back to value
	protected bool   // true for password-type fields that can be revealed
	revealKey string // namespaced revealedFields key; "" for non-protected fields
}

// selectableFields returns the ordered list of fields the cursor can move through.
func (m DetailModel) selectableFields() []selectableField {
	var f []selectableField
	f = append(f, selectableField{label: "username", value: m.entry.UserName})
	// revealKey is namespaced so a custom field literally named "password"
	// or "TOTP" can never share reveal state with the built-in fields.
	f = append(f, selectableField{label: "password", value: m.entry.Password, protected: true, revealKey: "builtin:password"})
	f = append(f, selectableField{label: "URL", value: m.entry.URL})
	if m.entry.TOTP != "" {
		val := m.totpCode
		if val == "" {
			val = m.entry.TOTP // fallback to raw secret when code generation fails
		}
		f = append(f, selectableField{label: "TOTP", value: val, qrValue: m.entry.TOTP, protected: true, revealKey: "builtin:totp"})
	}
	if m.entry.Notes != "" {
		f = append(f, selectableField{label: "Notes", value: m.entry.Notes})
	}
	for _, cf := range m.entry.CustomFields {
		f = append(f, selectableField{label: cf.Key, value: cf.Value, protected: cf.Protected, revealKey: "custom:" + cf.Key})
	}
	return f
}

// fieldAtLine returns the selectable field index for a given content line number.
func (m DetailModel) fieldAtLine(line int) (int, bool) {
	_, rows := m.layout()
	for i, r := range rows {
		if line >= r.start && line <= r.end {
			return i, true
		}
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
	pi := newTextInput()
	pi.Prompt = "  File path: "
	setInputPromptStyle(&pi, labelStyle.UnsetWidth())
	pi.SetWidth(50)
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

// notesBodyLines returns the number of visible body lines rendered for the notes
// field, including the hint line when notes are truncated.
func (m DetailModel) notesBodyLines() int {
	if m.entry.Notes == "" {
		return 0
	}
	noteLines := strings.Split(m.entry.Notes, "\n")
	bodyLines := len(noteLines)
	if len(noteLines) > maxNoteLines && !m.notesExpanded {
		bodyLines = maxNoteLines + 1 // truncated lines + "(N more lines)" hint
	}
	return bodyLines
}

// lineForField returns the content line number for the given selectable field index.
func (m DetailModel) lineForField(fieldIdx int) int {
	_, rows := m.layout()
	if fieldIdx < 0 || fieldIdx >= len(rows) {
		return 0
	}
	return rows[fieldIdx].start
}

// refreshContentAndFollow rebuilds the viewport content and scrolls to keep
// the selected field visible.
func (m *DetailModel) refreshContentAndFollow() {
	if !m.ready {
		return
	}
	m.syncViewportSize()
	m.viewport.SetContent(m.buildContent())
	line := m.lineForField(m.selectedField)
	vpHeight := m.viewport.Height()
	if line < m.viewport.YOffset() {
		m.viewport.SetYOffset(line)
	} else if line >= m.viewport.YOffset()+vpHeight {
		m.viewport.SetYOffset(line - vpHeight + 1)
	}
}

func (m DetailModel) Init() tea.Cmd {
	return nil
}

// totpTickSeq issues a unique sequence number per tick-chain start. The UI
// runs on a single goroutine, so a plain counter is safe.
var totpTickSeq int

// startTOTPTick starts (or restarts) the once-per-second TOTP refresh
// chain. Each start takes a fresh sequence number, so any tick still
// pending from an earlier chain — including one belonging to a previous
// DetailModel instance — is recognized as stale and dropped instead of
// spawning a duplicate chain.
func (m *DetailModel) startTOTPTick() tea.Cmd {
	if m.entry.TOTP == "" {
		return nil
	}
	totpTickSeq++
	m.totpSeq = totpTickSeq
	return tickTOTP(m.totpSeq)
}

func tickTOTP(seq int) tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return totpTickMsg{seq: seq}
	})
}

func (m *DetailModel) refreshTOTP() {
	secret := keepass.TOTPSecret(m.entry.TOTP)
	if secret == "" {
		return
	}
	period := m.entry.TOTPPeriod()
	now := time.Now()
	code, err := totp.GenerateCodeCustom(secret, now, totp.ValidateOpts{
		Period:    period,
		Digits:    otp.Digits(m.entry.TOTPDigits()),
		Algorithm: totpAlgorithm(m.entry.TOTPAlgorithm()),
	})
	if err != nil {
		m.totpCode = ""
		m.totpRemain = 0
		return
	}
	m.totpCode = code
	m.totpRemain = int(period) - int(now.Unix()%int64(period))
}

// totpAlgorithm maps a normalized algorithm name from the keepass layer to
// the otp library's constant.
func totpAlgorithm(name string) otp.Algorithm {
	switch name {
	case "SHA256":
		return otp.AlgorithmSHA256
	case "SHA512":
		return otp.AlgorithmSHA512
	default:
		return otp.AlgorithmSHA1
	}
}

func (m *DetailModel) syncViewportSize() {
	sizeInput(&m.attachPathInput, m.width)
	// View() concatenates: headerView() + viewport.View() + "\n" + footerView()
	// Count newlines contributed by each section to compute viewport height.
	headerNewlines := strings.Count(m.headerView(), "\n")
	footerNewlines := strings.Count(m.footerView(), "\n")
	// Total lines = headerNewlines + vpHeight + 1 + footerNewlines
	// We need total lines = m.height
	vpHeight := max(m.height-headerNewlines-footerNewlines-1, 1)
	if !m.ready {
		m.viewport = viewport.New(viewport.WithWidth(m.width), viewport.WithHeight(vpHeight))
		m.viewport.FillHeight = true
		m.viewport.SetContent(m.buildContent())
		m.ready = true
	} else {
		m.viewport.SetWidth(m.width)
		m.viewport.SetHeight(vpHeight)
	}
}

func (m DetailModel) headerView() string {
	title := sanitizeDisplay(m.entry.Title)
	if title == "" {
		title = "(untitled)"
	}
	return header(m.width, "", title) + "\n\n"
}

func (m DetailModel) footerView() string {
	if m.showQR {
		return renderHints(m.width, "esc: close", m.keys.ShowQR+": close", "QR: "+sanitizeDisplay(m.qrLabel))
	}
	if m.addingAttachment || m.extractingAttachment {
		return renderHints(m.width, "enter: confirm path", "esc: cancel", "F1: help")
	}
	if m.attachMode {
		help := "  j/k: select  s: save to disk  d: delete  +: add file  esc: back"
		return renderHints(m.width, append([]string{"F1: help", "esc: back"}, strings.Split(strings.TrimSpace(help), "  ")...)...)
	}
	// Show selected field name in footer
	var fieldPrefix string
	fields := m.selectableFields()
	if m.selectedField < len(fields) {
		f := fields[m.selectedField]
		fieldPrefix = fmt.Sprintf("  [%s]", sanitizeDisplay(f.label))
	}

	var help string
	if m.readOnly {
		help = fmt.Sprintf("  j/k: select  %s: QR  %s: toggle pw  %s: url",
			m.keys.ShowQR, m.keys.TogglePassword, m.keys.OpenURL)
		if m.entry.HistoryCount > 0 {
			help += fmt.Sprintf("  %s: history", m.keys.ShowHistory)
		}
		help += fmt.Sprintf("  esc: back  %s: quit", m.keys.Quit)
	} else {
		help = fmt.Sprintf("  j/k: select  %s: QR  %s: toggle pw  %s: edit  %s: url  %s: attach",
			m.keys.ShowQR, m.keys.TogglePassword, m.keys.Edit, m.keys.OpenURL, m.keys.Attachments)
		if m.entry.HistoryCount > 0 {
			help += fmt.Sprintf("  %s: history", m.keys.ShowHistory)
		}
		help += fmt.Sprintf("  esc: back  %s: quit", m.keys.Quit)
	}
	copyHint := "enter: copy field"
	if m.selectedField < len(fields) {
		copyHint = "enter: copy " + truncateString(sanitizeDisplay(fields[m.selectedField].label), max(1, m.width-12))
	}
	return renderHints(m.width, append([]string{copyHint, "?: help", "esc: back", m.keys.HIBPCheck + ": check breaches", strings.TrimSpace(fieldPrefix)}, strings.Split(strings.TrimSpace(help), "  ")...)...)
}

// truncateString truncates to terminal cells, preserving wide graphemes.
func truncateString(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	return ansi.Truncate(s, maxWidth, "…")
}

// buildContent returns the scrollable body content (everything between title and help bar).
func (m DetailModel) buildContent() string {
	content, _ := m.layout()
	return content
}

// layout renders the body in sections (credentials, notes, custom fields,
// attachments, then timestamps) and records the rows of every selectable
// field, so that clicks and scrolling follow the drawn layout.
func (m DetailModel) layout() (string, []fieldRows) {
	var b lineBuilder
	var rows []fieldRows
	labelWidth := m.labelWidth()
	label := func(text string) string {
		return labelStyle.Width(labelWidth).Render(truncateString(text, labelWidth))
	}

	// field writes a selectable field's first row: the selected one gets the
	// marker and the selection bar. Rows written before the next field, such
	// as the body of the notes, belong to it too.
	field := func(format string, args ...any) {
		row := fmt.Sprintf(format, args...)
		if len(rows) == m.selectedField {
			row = selectedRow(selectionCursor+row, m.width)
		} else {
			row = "  " + row
		}
		rows = append(rows, fieldRows{b.row, b.row})
		b.WriteString(row + "\n")
	}
	extendField := func() { rows[len(rows)-1].end = b.row - 1 }

	field("%s %s", label("Username:"), valueStyle.Render(sanitizeDisplay(m.entry.UserName)))

	pw := maskedValue
	if m.revealedFields["builtin:password"] {
		pw = sanitizeDisplay(m.entry.Password)
	}
	if m.cfg.ShowPasswordStrength {
		field("%s %s  %s", label("Password:"), hiddenStyle.Render(pw), renderStrength(m.strength))
	} else {
		field("%s %s", label("Password:"), hiddenStyle.Render(pw))
	}

	field("%s %s", label("URL:"), valueStyle.Render(sanitizeDisplay(m.entry.URL)))

	if m.entry.TOTP != "" {
		switch {
		case m.revealedFields["builtin:totp"]:
			field("%s %s", label("TOTP:"), valueStyle.Render(sanitizeDisplay(m.entry.TOTP)))
		case m.totpCode != "":
			field("%s %s  %s", label("TOTP:"), valueStyle.Render(groupDigits(m.totpCode)),
				totpCountdown(m.totpRemain, int(m.entry.TOTPPeriod())))
		default:
			// TOTP secret exists but code generation failed; show masked
			field("%s %s", label("TOTP:"), hiddenStyle.Render(maskedValue))
		}
	}

	if m.entry.Expires {
		expiry := m.entry.ExpiryTime.Local().Format("2006-01-02 15:04")
		value := hiddenStyle.Render(expiry)
		if m.entry.IsExpired() {
			value = errorMsgStyle.Padding(0).Render(expiry + " (EXPIRED)")
		}
		b.WriteString("  " + label("Expires:") + " " + value + "\n")
	}

	if m.entry.Notes != "" {
		b.WriteString("\n")
		field("%s", label("Notes:"))
		noteLines := sanitizeNoteLines(m.entry.Notes)
		show := noteLines
		if len(noteLines) > maxNoteLines && !m.notesExpanded {
			show = noteLines[:maxNoteLines]
		}
		for _, line := range show {
			b.WriteString("    " + valueStyle.Render(line) + "\n")
		}
		if len(noteLines) > maxNoteLines && !m.notesExpanded {
			b.WriteString("    " + helpStyle.Render(fmt.Sprintf("(%d more lines — %s to expand)",
				len(noteLines)-maxNoteLines, m.keys.TogglePassword)) + "\n")
		}
		extendField()
	}

	if len(m.entry.CustomFields) > 0 {
		b.WriteString("\n" + sectionStyle.Render("  Custom fields") + "\n")
		for _, cf := range m.entry.CustomFields {
			val := sanitizeDisplay(cf.Value)
			if cf.Protected && !m.revealedFields["custom:"+cf.Key] {
				val = maskedValue
			}
			field("%s %s", label(sanitizeDisplay(cf.Key)+":"), valueStyle.Render(val))
		}
	}

	if len(m.attachments) > 0 {
		heading := "Attachments"
		if m.attachMode {
			heading = "Attachments (select)"
		}
		b.WriteString("\n" + sectionStyle.Render("  "+heading) + "\n")
		for i, a := range m.attachments {
			name := valueStyle.Render(sanitizeDisplay(a.Name))
			size := hiddenStyle.Render(formatAttachmentSize(a.Size))
			if m.attachMode && i == m.attachIdx {
				b.WriteString(selectedRow("  "+selectionCursor+selectedItemStyle.Render(sanitizeDisplay(a.Name))+"  "+size, m.width) + "\n")
			} else {
				b.WriteString("    " + name + "  " + size + "\n")
			}
		}
	}

	var stamps []string
	if !m.entry.CreationTime.IsZero() {
		stamps = append(stamps, "Created "+m.entry.CreationTime.Local().Format("2006-01-02 15:04"))
	}
	if !m.entry.ModificationTime.IsZero() {
		stamps = append(stamps, "Modified "+m.entry.ModificationTime.Local().Format("2006-01-02 15:04"))
	}
	if len(stamps) > 0 {
		b.WriteString("\n" + hiddenStyle.Render("  "+strings.Join(stamps, " · ")) + "\n")
	}

	if m.addingAttachment || m.extractingAttachment {
		b.WriteString("\n")
		b.WriteString(m.attachPathInput.View())
		b.WriteString("\n")
	}

	return b.String(), rows
}

// labelWidth fits the longest label shown, within limits: longer custom
// field names are cut, and the footer names the selected field in full.
func (m DetailModel) labelWidth() int {
	width := lipgloss.Width("Username:")
	if m.entry.Expires {
		width = max(width, lipgloss.Width("Expires:"))
	}
	for _, cf := range m.entry.CustomFields {
		width = max(width, lipgloss.Width(sanitizeDisplay(cf.Key)+":"))
	}
	return min(width, max(10, min(24, m.width/3)))
}

func (m DetailModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.syncViewportSize()
		return m, nil

	case totpTickMsg:
		if msg.seq != m.totpSeq {
			return m, nil
		}
		m.refreshTOTP()
		if m.ready && !m.showQR {
			m.viewport.SetContent(m.buildContent())
		}
		return m, tickTOTP(m.totpSeq)

	case tea.MouseMsg:
		if click, ok := msg.(tea.MouseClickMsg); ok && click.Button == tea.MouseLeft && !m.showQR && !m.attachMode && !m.addingAttachment {
			// Map click Y to selectable field index.
			// The viewport content starts at the row after the header's last
			// visible line. strings.Count gives the number of newlines, which
			// equals the Y offset where viewport content begins.
			vpStartY := strings.Count(m.headerView(), "\n")
			if click.Y < vpStartY || click.Y >= vpStartY+m.viewport.Height() {
				return m, nil
			}
			vpRow := click.Y - vpStartY + m.viewport.YOffset()
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

	case tea.KeyPressMsg:
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
				path := cleanInputPath(m.attachPathInput.Value())
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

		// File path input mode for extracting attachments
		if m.extractingAttachment {
			switch msg.String() {
			case "esc":
				m.extractingAttachment = false
				m.attachPathInput.Blur()
				m.attachPathInput.SetValue("")
				if m.ready {
					m.viewport.SetContent(m.buildContent())
				}
				return m, nil
			case "enter":
				path := cleanInputPath(m.attachPathInput.Value())
				if path != "" {
					m.extractingAttachment = false
					m.attachPathInput.Blur()
					m.attachPathInput.SetValue("")
					entryUUID := m.entry.UUID
					name := m.extractAttachName
					return m, func() tea.Msg {
						return ExtractAttachmentMsg{EntryUUID: entryUUID, Name: name, DestPath: path}
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
					m.extractingAttachment = true
					m.extractAttachName = a.Name
					m.attachPathInput.Prompt = "  Save to: "
					m.attachPathInput.Placeholder = "/path/to/save"
					m.attachPathInput.SetValue(filepath.Base(a.Name))
					m.attachPathInput.CursorEnd()
					m.attachPathInput.Focus()
					if m.ready {
						m.viewport.SetContent(m.buildContent())
					}
					return m, textinput.Blink
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
			case "esc", m.keys.ShowQR:
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

		switch key {
		case "j", "down":
			// When notes are expanded, scroll within the notes body before
			// moving to the next field so the user can read all content.
			if m.notesExpanded && m.selectedField < len(fields) && fields[m.selectedField].label == "Notes" {
				_, rows := m.layout()
				notesEnd := rows[m.selectedField].end + 1
				if m.viewport.YOffset()+m.viewport.Height() < notesEnd {
					m.viewport.ScrollDown(1)
					return m, nil
				}
			}
			if m.selectedField < len(fields)-1 {
				m.selectedField++
				m.refreshContentAndFollow()
			}
			return m, nil

		case "k", "up":
			// When notes are expanded, scroll within the notes body before
			// moving to the previous field.
			if m.notesExpanded && m.selectedField < len(fields) && fields[m.selectedField].label == "Notes" {
				notesStart := m.lineForField(m.selectedField)
				if m.viewport.YOffset() > notesStart {
					m.viewport.ScrollUp(1)
					return m, nil
				}
			}
			if m.selectedField > 0 {
				m.selectedField--
				m.refreshContentAndFollow()
			}
			return m, nil

		case "enter":
			// Copy selected field; if revealed and has a separate qrValue
			// (e.g. TOTP secret), copy that instead of the display value.
			if m.selectedField < len(fields) {
				f := fields[m.selectedField]
				val := f.value
				if f.qrValue != "" && m.revealedFields[f.revealKey] {
					val = f.qrValue
				}
				if val != "" {
					return m, func() tea.Msg {
						return CopyToClipboardMsg{Field: f.label, Value: val}
					}
				}
			}
			return m, nil

		case "esc", m.keys.Back, "left":
			return m, func() tea.Msg { return BackToBrowserMsg{} }

		case m.keys.TogglePassword:
			if m.selectedField < len(fields) {
				f := fields[m.selectedField]
				if f.label == "Notes" {
					m.notesExpanded = !m.notesExpanded
					m.refreshContentAndFollow()
				} else if f.protected {
					m.revealedFields[f.revealKey] = !m.revealedFields[f.revealKey]
					m.refreshContentAndFollow()
				}
			}
			return m, nil

		case m.keys.Edit:
			if m.readOnly {
				return m, func() tea.Msg { return StatusMsg{Text: "Read-only mode", IsError: true} }
			}
			return m, func() tea.Msg {
				return EditEntryMsg{Entry: m.entry}
			}

		case m.keys.Attachments:
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

		case m.keys.CopyUsername:
			return m, func() tea.Msg {
				return CopyToClipboardMsg{Field: "username", Value: m.entry.UserName}
			}

		case m.keys.CopyPassword:
			return m, func() tea.Msg {
				return CopyToClipboardMsg{Field: "password", Value: m.entry.Password}
			}

		case m.keys.CopyURL:
			return m, func() tea.Msg {
				return CopyToClipboardMsg{Field: "url", Value: m.entry.URL}
			}

		case m.keys.CopyTOTP:
			if m.totpCode != "" {
				return m, func() tea.Msg {
					return CopyToClipboardMsg{Field: "TOTP code", Value: m.totpCode}
				}
			}

		case m.keys.HIBPCheck:
			// HIBP breach check
			if !m.hibpChecking && m.entry.Password != "" {
				m.hibpChecking = true
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

		case m.keys.ShowQR:
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
					m.viewport.SetContent(m.renderQR(val, m.width, m.viewport.Height()))
				}
			}
			return m, nil

		case m.keys.OpenURL:
			if m.entry.URL != "" {
				return m, func() tea.Msg {
					return OpenURLMsg{URL: m.entry.URL}
				}
			}

		case "?":
			return m, func() tea.Msg { return ShowHelpMsg{} }

		case m.keys.ShowHistory:
			if m.entry.HistoryCount > 0 {
				entryTitle := m.entry.Title
				entryUUID := m.entry.UUID
				return m, func() tea.Msg {
					return ShowHistoryMsg{EntryTitle: entryTitle, EntryUUID: entryUUID}
				}
			}
		}
	}

	// Paste results and cursor ticks must reach the active path input too.
	if m.addingAttachment || m.extractingAttachment {
		var cmd tea.Cmd
		m.attachPathInput, cmd = m.attachPathInput.Update(msg)
		if m.ready {
			m.viewport.SetContent(m.buildContent())
		}
		return m, cmd
	}
	// Delegate remaining messages to viewport (pgup/pgdn, mouse, etc.)
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m DetailModel) View() tea.View {
	if !m.ready {
		return tea.NewView("")
	}
	m.syncViewportSize()
	return tea.NewView(fitFrame(m.headerView()+m.viewport.View()+"\n"+m.footerView(), m.width, m.height))
}

// renderStrength returns a strength meter and label, colored by level.
func renderStrength(s keepass.StrengthResult) string {
	if s.Unavailable {
		return warnStyle.Padding(0).Render("Not rated: invalid text")
	}
	label := fmt.Sprintf("%s ~%.0f guess bits", s.Label, s.GuessBits)
	if s.AnalyzedLength < s.Length {
		label += fmt.Sprintf("; first %d chars", s.AnalyzedLength)
	}
	style := hiddenStyle
	switch s.Score {
	case keepass.StrengthWeak:
		style = errorMsgStyle.Padding(0)
	case keepass.StrengthFair:
		style = warnStyle.Padding(0)
	case keepass.StrengthGood:
		style = valueStyle
	case keepass.StrengthStrong, keepass.StrengthExcellent:
		style = successStyle.Padding(0)
	}
	const segments = 5
	filled := min(max(int(s.Score)+1, 1), segments)
	return style.Render(strings.Repeat("▰", filled)) +
		hiddenStyle.Render(strings.Repeat("▱", segments-filled)) + " " + style.Render(label)
}

// renderStrengthDetail returns the strength label plus a character class breakdown.
// Classes describe the password; they do not award strength points.
func renderStrengthDetail(s keepass.StrengthResult) string {
	check := func(has bool, label string) string {
		if has {
			return successStyle.Padding(0).Render(label)
		}
		return hiddenStyle.Render(label)
	}
	return renderStrength(s) + "  " + strings.Join([]string{
		check(s.HasLower, "a-z"),
		check(s.HasUpper, "A-Z"),
		check(s.HasDigit, "0-9"),
		check(s.HasSymbol, "!@#"),
		hiddenStyle.Render(fmt.Sprintf("len:%d", s.Length)),
	}, " ")
}

// cleanInputPath strips surrounding quotes and backslash escapes that terminals
// insert when a file is dragged into the window (e.g. '/path/to/file' or
// /path/to/my\ file.txt), then trims whitespace.
func cleanInputPath(raw string) string {
	s := strings.TrimSpace(raw)
	// Strip matching quotes
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			s = s[1 : len(s)-1]
		}
	}
	// Remove backslash escapes (e.g. "my\ file" -> "my file")
	s = strings.ReplaceAll(s, "\\ ", " ")
	return strings.TrimSpace(s)
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
		for col := range totalW {
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

// groupDigits splits an even-length code in two halves for reading:
// 026242 shows as 026 242. Copies use the code itself.
func groupDigits(code string) string {
	if len(code) < 6 || len(code)%2 != 0 {
		return code
	}
	return code[:len(code)/2] + " " + code[len(code)/2:]
}

// totpCountdown draws the time left on a TOTP code as a bar that turns
// yellow, then red, as the code nears expiry.
func totpCountdown(remaining, period int) string {
	const width = 12
	filled := min(max(remaining*width/max(period, 1), 0), width)
	style := successStyle.Padding(0)
	switch {
	case remaining <= 5:
		style = errorMsgStyle.Padding(0)
	case remaining <= 10:
		style = warnStyle.Padding(0)
	}
	return style.Render(strings.Repeat("━", filled)) +
		hiddenStyle.Render(strings.Repeat("─", width-filled)) + " " + style.Render(fmt.Sprintf("%ds", remaining))
}
