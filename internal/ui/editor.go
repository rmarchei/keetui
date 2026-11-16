package ui

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/tobischo/gokeepasslib/v3"

	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/keepass"
)

const (
	fieldTitle = iota
	fieldUserName
	fieldPassword
	fieldURL
	fieldTOTP
	fieldNotes
	fieldExpiry
	fixedFieldCount // number of built-in fields
)

var fieldLabels = [fixedFieldCount]string{
	"Title",
	"Username",
	"Password",
	"URL",
	"TOTP",
	"Notes",
	"Expiry",
}

// maskPasswordMsg is sent after a delay to re-mask a password field.
type maskPasswordMsg struct {
	seq int
}

// EditorModel handles creating or editing an entry.
type EditorModel struct {
	inputs        [fixedFieldCount]textinput.Model
	notesInput    textarea.Model // multi-line notes editor
	customInputs  []customFieldInput
	focused       int
	isNew         bool
	entryUUID     gokeepasslib.UUID
	groupUUID     gokeepasslib.UUID
	width         int
	height        int
	err           error
	generated     bool // true after password was auto-generated
	tags          string
	cfg           config.Config
	revealSeq     int  // incremented on each keystroke that triggers auto-reveal
	persistReveal bool // true when ctrl+r was used to persistently show password
	offset        int
	strength      keepass.StrengthResult // recomputed only when the password changes
	rows          []fieldRows
	loaded        [fixedFieldCount]loadedValue
	loadedNotes   loadedValue
}

type customFieldInput struct {
	key       textinput.Model
	value     textinput.Model
	protected bool
	loadedKey loadedValue
	loadedVal loadedValue
}

// loadedValue is a stored value and what its input shows of it. Inputs
// replace tabs and newlines and drop other control characters (the notes
// box writes each \r as a newline), so saving an unedited input would
// alter the value: a multi-line SSH key in a protected field would be
// flattened by a change to the title.
type loadedValue struct{ raw, shown string }

// keep returns the stored value while the input still shows it unedited.
func (v loadedValue) keep(current string) string {
	if current == v.shown {
		return v.raw
	}
	return current
}

// SaveEntryMsg signals that the entry should be saved.
type SaveEntryMsg struct {
	IsNew     bool
	EntryUUID gokeepasslib.UUID
	GroupUUID gokeepasslib.UUID
	Entry     keepass.Entry
}

// CancelEditMsg signals cancellation of editing.
type CancelEditMsg struct{}

func NewEditorModel(entry *keepass.Entry, groupUUID gokeepasslib.UUID, cfg config.Config) EditorModel {
	var inputs [fixedFieldCount]textinput.Model
	for i := range inputs {
		t := newTextInput()
		t.Prompt = fmt.Sprintf("  %s: ", fieldLabels[i])
		setInputPromptStyle(&t, labelStyle)
		setInputTextStyle(&t, valueStyle)
		t.SetWidth(50)
		inputs[i] = t
	}

	inputs[fieldPassword].EchoMode = textinput.EchoPassword
	inputs[fieldPassword].EchoCharacter = '*'
	inputs[fieldTOTP].EchoMode = textinput.EchoPassword
	inputs[fieldTOTP].EchoCharacter = '*'
	inputs[fieldTOTP].Placeholder = "base32 secret or otpauth:// URI"
	inputs[fieldExpiry].Placeholder = "YYYY-MM-DD (blank = no expiry)"

	// Notes uses a multi-line textarea instead of textinput.
	ta := textarea.New()
	ta.Prompt = "  "
	ta.ShowLineNumbers = false
	ta.SetHeight(4)
	ta.SetWidth(54)
	ta.CharLimit = 0 // no limit
	ta.MaxHeight = 0 // no limit on lines either: the default 99 cuts long notes
	// Disable Tab in textarea so our editor Tab navigation works.
	km := ta.KeyMap
	km.InsertNewline.SetEnabled(true)
	ta.KeyMap = km

	m := EditorModel{
		inputs:     inputs,
		notesInput: ta,
		groupUUID:  groupUUID,
		isNew:      entry == nil,
		cfg:        cfg,
	}

	if entry != nil {
		m.entryUUID = entry.UUID
		m.tags = entry.Tags
		for field, raw := range map[int]string{fieldTitle: entry.Title, fieldUserName: entry.UserName,
			fieldPassword: entry.Password, fieldURL: entry.URL, fieldTOTP: entry.TOTP} {
			inputs[field].SetValue(raw)
			m.loaded[field] = loadedValue{raw, inputs[field].Value()}
		}
		m.notesInput.SetValue(entry.Notes)
		m.loadedNotes = loadedValue{entry.Notes, m.notesInput.Value()}
		if entry.Expires && !entry.ExpiryTime.IsZero() {
			inputs[fieldExpiry].SetValue(entry.ExpiryTime.Local().Format("2006-01-02"))
		}
		m.inputs = inputs
		for _, cf := range entry.CustomFields {
			m.customInputs = append(m.customInputs, newCustomFieldInput(cf.Key, cf.Value, cf.Protected))
		}
	}

	m.inputs[0].Focus()
	setInputTextStyle(&m.inputs[0], selectedItemStyle)
	m.refreshStrength()
	m.syncLayout()

	return m
}

func newCustomFieldInput(key, value string, protected bool) customFieldInput {
	k := newTextInput()
	k.Prompt = "  Key: "
	setInputPromptStyle(&k, labelStyle)
	setInputTextStyle(&k, valueStyle)
	k.SetWidth(20)
	k.SetValue(key)

	v := newTextInput()
	v.Prompt = "  Value: "
	setInputPromptStyle(&v, labelStyle)
	setInputTextStyle(&v, valueStyle)
	v.SetWidth(40)
	v.SetValue(value)
	if protected {
		v.EchoMode = textinput.EchoPassword
		v.EchoCharacter = '*'
	}

	return customFieldInput{key: k, value: v, protected: protected,
		loadedKey: loadedValue{key, k.Value()}, loadedVal: loadedValue{value, v.Value()}}
}

func (m EditorModel) totalFields() int {
	return fixedFieldCount + len(m.customInputs)*2
}

func (m EditorModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m EditorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := m.update(msg)
	next := updated.(EditorModel)
	if next.inputs[fieldPassword].Value() != m.inputs[fieldPassword].Value() {
		next.refreshStrength()
	}
	next.syncLayout()
	return next, cmd
}

func (m EditorModel) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case maskPasswordMsg:
		if msg.seq == m.revealSeq && !m.persistReveal {
			m.remaskAll()
		}
		return m, nil

	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			_, height := viewSize(m.width, m.height)
			if msg.Y >= 0 && msg.Y < bodyHeight(height, m.footer()) {
				for i, row := range m.rows {
					if msg.Y+m.offset < row.start || msg.Y+m.offset > row.end {
						continue
					}
					m.focused = i
					return m, m.updateFocus()
				}
			}
		}
		return m, nil

	case tea.KeyPressMsg:
		// Clear stale errors on any keypress
		m.err = nil

		switch msg.String() {
		case "ctrl+c":
			return m, func() tea.Msg { return RequestQuitMsg{} }

		case "esc":
			return m, func() tea.Msg { return CancelEditMsg{} }

		case "tab", "down":
			// Inside the multi-line Notes field, up/down move the cursor
			// between lines; focus only leaves at the boundary lines.
			if msg.String() == "down" && m.focused == fieldNotes &&
				m.notesInput.Line() < m.notesInput.LineCount()-1 {
				break
			}
			m.focused = (m.focused + 1) % m.totalFields()
			return m, m.updateFocus()

		case "shift+tab", "up":
			if msg.String() == "up" && m.focused == fieldNotes &&
				m.notesInput.Line() > 0 {
				break
			}
			m.focused = (m.focused - 1 + m.totalFields()) % m.totalFields()
			return m, m.updateFocus()

		case "ctrl+s":
			updated, cmd := m.save()
			return updated, cmd

		case "ctrl+g":
			// Re-mask any auto-revealed field before leaving for the
			// picker: the pending re-mask tick is dropped while the
			// picker view is active, which would leave the secret
			// visible indefinitely.
			if !m.persistReveal {
				m.remaskAll()
			}
			pw := m.inputs[fieldPassword].Value()
			return m, func() tea.Msg {
				return ShowPresetPickerMsg{CurrentPassword: pw}
			}

		case "ctrl+r":
			// Toggle persistent visibility on password/TOTP or protected custom fields
			if m.focused == fieldPassword || m.focused == fieldTOTP {
				m.persistReveal = !m.persistReveal
				if m.persistReveal {
					m.inputs[m.focused].EchoMode = textinput.EchoNormal
				} else {
					m.inputs[m.focused].EchoMode = textinput.EchoPassword
				}
			} else if cf, ok := m.focusedProtectedCustomField(); ok {
				m.persistReveal = !m.persistReveal
				if m.persistReveal {
					cf.value.EchoMode = textinput.EchoNormal
				} else {
					cf.value.EchoMode = textinput.EchoPassword
				}
			}
			return m, nil

		case "ctrl+t":
			// Toggle protected (hidden) state on focused custom field
			if m.focused >= fixedFieldCount && len(m.customInputs) > 0 {
				cfIdx := (m.focused - fixedFieldCount) / 2
				if cfIdx < len(m.customInputs) {
					cf := &m.customInputs[cfIdx]
					cf.protected = !cf.protected
					if cf.protected {
						cf.value.EchoMode = textinput.EchoPassword
						cf.value.EchoCharacter = '*'
					} else {
						cf.value.EchoMode = textinput.EchoNormal
					}
				}
			}
			return m, nil

		case "ctrl+a":
			// Add a new custom field
			m.customInputs = append(m.customInputs, newCustomFieldInput("", "", false))
			m.focused = fixedFieldCount + (len(m.customInputs)-1)*2
			return m, m.updateFocus()

		case "ctrl+d":
			// Delete the custom field that is currently focused
			if m.focused >= fixedFieldCount && len(m.customInputs) > 0 {
				cfIdx := (m.focused - fixedFieldCount) / 2
				if cfIdx < len(m.customInputs) {
					m.customInputs = append(m.customInputs[:cfIdx], m.customInputs[cfIdx+1:]...)
					if m.focused >= m.totalFields() {
						m.focused = max(m.totalFields()-1, 0)
					}
					return m, m.updateFocus()
				}
			}
		}
	}

	// Update only the focused input, tracking value changes for auto-reveal
	var oldVal string
	isMaskedFixed := m.focused == fieldPassword || m.focused == fieldTOTP
	isMaskedCustom := false
	if isMaskedFixed {
		oldVal = m.inputs[m.focused].Value()
	}

	var cmd tea.Cmd
	if m.focused == fieldNotes {
		m.notesInput, cmd = m.notesInput.Update(msg)
	} else if m.focused < fixedFieldCount {
		m.inputs[m.focused], cmd = m.inputs[m.focused].Update(msg)
	} else {
		cfIdx := (m.focused - fixedFieldCount) / 2
		isValue := (m.focused-fixedFieldCount)%2 == 1
		if cfIdx < len(m.customInputs) {
			if isValue {
				if m.customInputs[cfIdx].protected {
					isMaskedCustom = true
					oldVal = m.customInputs[cfIdx].value.Value()
				}
				m.customInputs[cfIdx].value, cmd = m.customInputs[cfIdx].value.Update(msg)
			} else {
				m.customInputs[cfIdx].key, cmd = m.customInputs[cfIdx].key.Update(msg)
			}
		}
	}

	// Clear "Password generated!" when the password is manually edited
	if m.focused == fieldPassword && m.generated && m.inputs[fieldPassword].Value() != oldVal {
		m.generated = false
	}

	// Briefly reveal masked fields when typing, then re-mask after 1 second;
	// privacy mode reveals nothing unasked.
	if !m.persistReveal && !m.cfg.HidePasswords() {
		if isMaskedFixed && m.inputs[m.focused].Value() != oldVal {
			m.inputs[m.focused].EchoMode = textinput.EchoNormal
			return m, tea.Batch(cmd, m.scheduleAutoMask(m.focused, time.Second))
		}
		if isMaskedCustom {
			cfIdx := (m.focused - fixedFieldCount) / 2
			if m.customInputs[cfIdx].value.Value() != oldVal {
				m.customInputs[cfIdx].value.EchoMode = textinput.EchoNormal
				return m, tea.Batch(cmd, m.scheduleAutoMask(m.focused, time.Second))
			}
		}
	}

	return m, cmd
}

func (m *EditorModel) updateFocus() tea.Cmd {
	// Re-mask all protected fields and reset reveal state on focus change
	m.persistReveal = false
	m.remaskAll()

	var cmds []tea.Cmd
	for i := range m.inputs {
		if i == fieldNotes {
			continue // notes uses textarea, handled below
		}
		if i == m.focused {
			cmds = append(cmds, m.inputs[i].Focus())
			setInputTextStyle(&m.inputs[i], selectedItemStyle)
		} else {
			m.inputs[i].Blur()
			setInputTextStyle(&m.inputs[i], valueStyle)
		}
	}
	// Notes textarea focus
	if m.focused == fieldNotes {
		m.notesInput.Focus()
	} else {
		m.notesInput.Blur()
		m.notesInput.SetCursorColumn(0)
	}
	for ci := range m.customInputs {
		keyIdx := fixedFieldCount + ci*2
		valIdx := keyIdx + 1
		if keyIdx == m.focused {
			cmds = append(cmds, m.customInputs[ci].key.Focus())
			setInputTextStyle(&m.customInputs[ci].key, selectedItemStyle)
			m.customInputs[ci].value.Blur()
			setInputTextStyle(&m.customInputs[ci].value, valueStyle)
		} else if valIdx == m.focused {
			m.customInputs[ci].key.Blur()
			setInputTextStyle(&m.customInputs[ci].key, valueStyle)
			cmds = append(cmds, m.customInputs[ci].value.Focus())
			setInputTextStyle(&m.customInputs[ci].value, selectedItemStyle)
		} else {
			m.customInputs[ci].key.Blur()
			setInputTextStyle(&m.customInputs[ci].key, valueStyle)
			m.customInputs[ci].value.Blur()
			setInputTextStyle(&m.customInputs[ci].value, valueStyle)
		}
	}
	return tea.Batch(cmds...)
}

// remaskAll sets EchoPassword on all protected fields (password, TOTP, custom).
func (m *EditorModel) remaskAll() {
	m.inputs[fieldPassword].EchoMode = textinput.EchoPassword
	m.inputs[fieldTOTP].EchoMode = textinput.EchoPassword
	for i := range m.customInputs {
		if m.customInputs[i].protected {
			m.customInputs[i].value.EchoMode = textinput.EchoPassword
		}
	}
}

// scheduleAutoMask briefly reveals a masked field and schedules re-masking.
func (m *EditorModel) scheduleAutoMask(field int, delay time.Duration) tea.Cmd {
	m.revealSeq++
	seq := m.revealSeq
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return maskPasswordMsg{seq: seq}
	})
}

// focusedProtectedCustomField returns the custom field input if the currently
// focused field is a protected custom field value, along with a bool.
func (m *EditorModel) focusedProtectedCustomField() (*customFieldInput, bool) {
	if m.focused < fixedFieldCount {
		return nil, false
	}
	cfIdx := (m.focused - fixedFieldCount) / 2
	isValue := (m.focused-fixedFieldCount)%2 == 1
	if isValue && cfIdx < len(m.customInputs) && m.customInputs[cfIdx].protected {
		return &m.customInputs[cfIdx], true
	}
	return nil, false
}

// value is what saving stores for a fixed field.
func (m EditorModel) value(field int) string {
	return m.loaded[field].keep(m.inputs[field].Value())
}

func (m EditorModel) save() (tea.Model, tea.Cmd) {
	title := m.value(fieldTitle)
	if title != m.loaded[fieldTitle].raw {
		title = strings.TrimSpace(title) // edited
	}
	if strings.TrimSpace(title) == "" {
		m.err = fmt.Errorf("title cannot be empty")
		return m, nil
	}
	expiryStr := strings.TrimSpace(m.inputs[fieldExpiry].Value())
	var expiryTime time.Time
	var hasExpiry bool
	if expiryStr != "" {
		// Parse in local time: the field is displayed via Local() in
		// NewEditorModel, so parsing as UTC would shift the stored date
		// one day per edit for users west of UTC.
		t, err := time.ParseInLocation("2006-01-02", expiryStr, time.Local)
		if err != nil {
			m.err = fmt.Errorf("invalid expiry date (expected YYYY-MM-DD)")
			return m, nil
		}
		expiryTime = t
		hasExpiry = true
	}
	var custom []keepass.CustomField
	for _, cf := range m.customInputs {
		key := cf.loadedKey.keep(cf.key.Value())
		val := cf.loadedVal.keep(cf.value.Value())
		if key != "" {
			custom = append(custom, keepass.CustomField{
				Key:       key,
				Value:     val,
				Protected: cf.protected,
			})
		}
	}
	// Checked here as well as when saving, so that the form stays open.
	if err := keepass.CheckCustomFields(custom); err != nil {
		m.err = err
		return m, nil
	}
	return m, func() tea.Msg {
		entry := keepass.Entry{
			Title:        title,
			UserName:     m.value(fieldUserName),
			Password:     m.value(fieldPassword),
			URL:          m.value(fieldURL),
			Notes:        m.loadedNotes.keep(m.notesInput.Value()),
			TOTP:         m.value(fieldTOTP),
			Expires:      hasExpiry,
			ExpiryTime:   expiryTime,
			Tags:         m.tags,
			CustomFields: custom,
		}
		return SaveEntryMsg{
			IsNew:     m.isNew,
			EntryUUID: m.entryUUID,
			GroupUUID: m.groupUUID,
			Entry:     entry,
		}
	}
}

// SetGeneratedPassword sets the password field to the given value and
// reveals it briefly, unless in privacy mode. Returns a command to schedule
// re-masking.
func (m *EditorModel) SetGeneratedPassword(pw string) tea.Cmd {
	m.inputs[fieldPassword].SetValue(pw)
	m.refreshStrength()
	m.generated = true
	m.syncLayout()
	if m.cfg.HidePasswords() {
		return nil
	}
	m.inputs[fieldPassword].EchoMode = textinput.EchoNormal
	return m.scheduleAutoMask(fieldPassword, 2*time.Second)
}

func (m *EditorModel) refreshStrength() {
	if m.cfg.ShowPasswordStrength {
		m.strength = keepass.EvaluatePasswordStrength(m.inputs[fieldPassword].Value())
	}
}

func (m *EditorModel) syncLayout() {
	width, height := viewSize(m.width, m.height)
	for i := range m.inputs {
		sizeInput(&m.inputs[i], width)
	}
	// textinput pads to its width. Shrink a short password input before
	// deciding whether the annotation actually fits alongside it.
	if pw := m.inputs[fieldPassword].Value(); m.cfg.ShowPasswordStrength && pw != "" {
		input := m.inputs[fieldPassword]
		valueWidth := utf8.RuneCountInString(pw)
		if input.EchoMode == textinput.EchoNormal {
			valueWidth = lipgloss.Width(sanitizeDisplay(pw))
		}
		input.SetWidth(min(input.Width(), max(1, valueWidth+1)))
		if lipgloss.Width(input.View())+2+lipgloss.Width(renderStrengthDetail(m.strength)) <= width {
			m.inputs[fieldPassword] = input
		}
	}
	m.notesInput.SetWidth(max(1, width-1))
	m.notesInput.SetHeight(min(4, max(1, height-6)))
	for i := range m.customInputs {
		sizeInput(&m.customInputs[i].key, width)
		sizeInput(&m.customInputs[i].value, width)
	}
	body, rows := m.body()
	m.rows = rows
	m.focused = min(max(0, m.focused), len(rows)-1)
	visible := bodyHeight(height, m.footer())
	m.offset = clampOffset(body, visible, followField(m.offset, rows[m.focused], visible))
}

func (m EditorModel) body() (string, []fieldRows) {
	var b lineBuilder
	var rows []fieldRows
	width, _ := viewSize(m.width, m.height)

	action := "Edit Entry"
	if m.isNew {
		action = "New Entry"
	}
	b.WriteString(header(m.width, "", action))
	b.WriteString("\n\n")

	for i := range m.inputs {
		start := b.row
		if i == fieldNotes {
			content := fmt.Sprintf("  %s\n%s", labelStyle.Render("Notes:"), m.notesInput.View())
			b.WriteString(formRow(content, width, i == m.focused) + "\n")
			rows = append(rows, fieldRows{start, b.row - 1})
			continue
		}
		content := m.inputs[i].View()
		if i == fieldPassword && m.cfg.ShowPasswordStrength && m.inputs[i].Value() != "" {
			annotation := renderStrengthDetail(m.strength)
			if lipgloss.Width(content)+2+lipgloss.Width(annotation) <= width {
				content += "  "
			} else {
				content += "\n"
			}
			content += wrapContent(annotation, width)
		}
		b.WriteString(formRow(content, width, i == m.focused) + "\n")
		rows = append(rows, fieldRows{start, b.row - 1})
	}

	if len(m.customInputs) > 0 {
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("  Custom Fields:"))
		b.WriteString("\n")
		for i, cf := range m.customInputs {
			start := b.row
			b.WriteString(formRow(cf.key.View(), width, m.focused == fixedFieldCount+i*2))
			b.WriteString("\n")
			rows = append(rows, fieldRows{start, start})
			start++
			b.WriteString(formRow(cf.value.View(), width, m.focused == fixedFieldCount+i*2+1))
			b.WriteString("\n")
			rows = append(rows, fieldRows{start, start})
			if cf.protected {
				b.WriteString(hiddenStyle.Render("  [protected]") + "\n")
			}
		}
	}

	return b.String(), rows
}

func (m EditorModel) footer() string {
	width, _ := viewSize(m.width, m.height)
	var b strings.Builder
	if m.generated {
		b.WriteString("\n")
		b.WriteString(successStyle.Render("  Password generated!"))
		b.WriteString("\n")
	}

	if m.err != nil {
		b.WriteString("\n")
		b.WriteString(wrapContent(errorMsgStyle.Render("Error: "+sanitizeDisplay(m.err.Error())), width))
		b.WriteString("\n")
	}

	b.WriteString(renderHints(width, "ctrl+s: save", "esc: cancel", "F1: help", "tab/shift+tab: navigate", "ctrl+r: reveal", "ctrl+g: gen password", "ctrl+a: add field", "ctrl+t: protect", "ctrl+d: del field"))
	return b.String()
}

func (m EditorModel) View() tea.View {
	body, _ := m.body()
	return tea.NewView(screen(body, m.footer(), m.width, m.height, m.offset))
}

func generatePassword(length int, charset string) (string, error) {
	result := make([]byte, length)
	charLen := big.NewInt(int64(len(charset)))
	for i := range result {
		n, err := rand.Int(rand.Reader, charLen)
		if err != nil {
			return "", fmt.Errorf("random number generation failed: %w", err)
		}
		result[i] = charset[n.Int64()]
	}
	return string(result), nil
}
