package ui

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/keepass"
)

// PresetNames lists the valid preset names for use in settings validation.
var PresetNames = config.PresetNames

const (
	pickerModeList    = 0
	pickerModeCustom  = 1
	pickerModePreview = 2
)

// ShowPresetPickerMsg requests showing the preset picker from the editor.
type ShowPresetPickerMsg struct {
	CurrentPassword string
}

// GeneratedPasswordMsg carries the generated password back to the editor.
type GeneratedPasswordMsg struct {
	Password string
}

// CancelPresetMsg cancels the preset picker and returns to the editor.
type CancelPresetMsg struct{}

// SetDefaultPresetMsg asks the app to make a preset the default, where the
// default is set: in the database or in the config file.
type SetDefaultPresetMsg struct {
	Name string
}

type presetOption struct {
	name string
	desc string
}

// PresetPickerModel shows a list of password generation presets.
type PresetPickerModel struct {
	presets         []presetOption
	cursor          int
	mode            int // pickerModeList, pickerModeCustom, or pickerModePreview
	cfg             config.Config
	currentPassword string

	// Custom sub-screen fields
	lengthInput textinput.Model
	toggles     [4]bool // uppercase, lowercase, digits, symbols
	customFocus int     // 0=length, 1-4=toggles

	// Preview sub-screen fields
	generatedPassword string // the password being previewed
	passwordVisible   bool   // toggle for showing/hiding
	previewFrom       int    // mode to return to on esc (list or custom)
	lastPresetName    string // which preset generated the password, for refresh

	width  int
	height int
	err    error
}

func NewPresetPickerModel(cfg config.Config, currentPassword string) PresetPickerModel {
	presets := []presetOption{
		{"PIN", "6 digits"},
		{"Alphanumeric", "16 chars, A-Z a-z 0-9"},
		{"Strong", "20 chars, A-Z a-z 0-9 !@#"},
		{"Passphrase", "6 words, separated by -"},
	}
	if currentPassword != "" {
		presets = append(presets, presetOption{"Like current", describePassword(currentPassword)})
	}
	presets = append(presets, presetOption{"Custom", "configure length and character classes"})

	// Find the default preset index
	defaultPreset := cfg.Preset()
	cursor := 0
	for i, p := range presets {
		if p.name == defaultPreset {
			cursor = i
			break
		}
	}

	// Set up custom sub-screen
	li := newTextInput()
	li.Prompt = "  Length: "
	setInputPromptStyle(&li, labelStyle)
	li.SetWidth(8)
	li.CharLimit = 4
	li.SetValue(strconv.Itoa(cfg.PasswordLen()))

	return PresetPickerModel{
		presets:         presets,
		cursor:          cursor,
		mode:            pickerModeList,
		cfg:             cfg,
		currentPassword: currentPassword,
		lengthInput:     li,
		passwordVisible: true,
		toggles: [4]bool{
			cfg.PasswordHasUppercase(),
			cfg.PasswordHasLowercase(),
			cfg.PasswordHasDigits(),
			cfg.PasswordHasSymbols(),
		},
	}
}

func (m PresetPickerModel) Init() tea.Cmd {
	return nil
}

func (m PresetPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		sizeInput(&m.lengthInput, msg.Width)
		return m, nil

	case tea.KeyPressMsg:
		m.err = nil
		switch m.mode {
		case pickerModeList:
			return m.updateList(msg)
		case pickerModeCustom:
			return m.updateCustom(msg)
		case pickerModePreview:
			return m.updatePreview(msg)
		}
	}
	// Paste and cursor blink messages are not key presses in Bubble Tea v2.
	if m.mode == pickerModeCustom && m.customFocus == 0 {
		var cmd tea.Cmd
		m.lengthInput, cmd = m.lengthInput.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m PresetPickerModel) updateList(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, func() tea.Msg { return RequestQuitMsg{} }
	case "esc":
		return m, func() tea.Msg { return CancelPresetMsg{} }
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.presets)-1 {
			m.cursor++
		}
	case "d":
		name := m.presets[m.cursor].name
		if name == "Like current" {
			// "Like current" can't be a default
			return m, nil
		}
		return m, func() tea.Msg { return SetDefaultPresetMsg{Name: name} }
	case "enter":
		return m.selectPreset()
	}
	return m, nil
}

func (m PresetPickerModel) selectPreset() (tea.Model, tea.Cmd) {
	name := m.presets[m.cursor].name
	if name == "Custom" {
		m.mode = pickerModeCustom
		m.customFocus = 0
		m.lengthInput.Focus()
		return m, textinput.Blink
	}

	pw, err := m.generateForPreset(name)
	if err != nil {
		m.err = err
		return m, nil
	}
	m.generatedPassword = pw
	m.passwordVisible = true
	m.lastPresetName = name
	m.previewFrom = pickerModeList
	m.mode = pickerModePreview
	return m, nil
}

// generateForPreset generates a password for the given preset name.
func (m PresetPickerModel) generateForPreset(name string) (string, error) {
	switch name {
	case "PIN":
		return generatePassword(6, "0123456789")
	case "Alphanumeric":
		return generatePassword(16, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	case "Strong":
		return generatePassword(20, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()-_=+")
	case "Passphrase":
		return keepass.GeneratePassphrase(6, "-")
	case "Like current":
		return generateLikeCurrent(m.currentPassword)
	case "Custom":
		return m.generateCustomPassword()
	default:
		return "", fmt.Errorf("unknown preset: %s", name)
	}
}

// generateCustomPassword builds a password from the current custom settings.
func (m PresetPickerModel) generateCustomPassword() (string, error) {
	lengthStr := strings.TrimSpace(m.lengthInput.Value())
	length, err := strconv.Atoi(lengthStr)
	if err != nil || length < 1 {
		return "", fmt.Errorf("invalid length")
	}
	if length < 4 {
		return "", fmt.Errorf("length must be at least 4")
	}

	var charset string
	if m.toggles[1] { // lowercase
		charset += "abcdefghijklmnopqrstuvwxyz"
	}
	if m.toggles[0] { // uppercase
		charset += "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	}
	if m.toggles[2] { // digits
		charset += "0123456789"
	}
	if m.toggles[3] { // symbols
		charset += "!@#$%^&*()-_=+"
	}
	if charset == "" {
		return "", fmt.Errorf("enable at least one character class")
	}

	return generatePassword(length, charset)
}

func (m PresetPickerModel) updateCustom(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, func() tea.Msg { return RequestQuitMsg{} }
	case "esc":
		m.mode = pickerModeList
		m.lengthInput.Blur()
		return m, nil
	case "tab", "down", "j":
		m.customFocus = (m.customFocus + 1) % 5
		return m, m.updateCustomFocus()
	case "shift+tab", "up", "k":
		m.customFocus = (m.customFocus - 1 + 5) % 5
		return m, m.updateCustomFocus()
	case "space":
		if m.customFocus >= 1 && m.customFocus <= 4 {
			m.toggles[m.customFocus-1] = !m.toggles[m.customFocus-1]
		}
		return m, nil
	case "enter":
		return m.generateCustom()
	}

	// Pass key to length input when focused
	if m.customFocus == 0 {
		var cmd tea.Cmd
		m.lengthInput, cmd = m.lengthInput.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *PresetPickerModel) updateCustomFocus() tea.Cmd {
	if m.customFocus == 0 {
		return m.lengthInput.Focus()
	}
	m.lengthInput.Blur()
	return nil
}

func (m PresetPickerModel) generateCustom() (tea.Model, tea.Cmd) {
	pw, err := m.generateCustomPassword()
	if err != nil {
		m.err = err
		return m, nil
	}
	m.generatedPassword = pw
	m.passwordVisible = true
	m.lastPresetName = "Custom"
	m.previewFrom = pickerModeCustom
	m.mode = pickerModePreview
	m.lengthInput.Blur()
	return m, nil
}

func (m PresetPickerModel) updatePreview(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, func() tea.Msg { return RequestQuitMsg{} }
	case "esc":
		m.mode = m.previewFrom
		if m.mode == pickerModeCustom {
			m.customFocus = 0
			m.lengthInput.Focus()
			return m, textinput.Blink
		}
		return m, nil
	case "r":
		pw, err := m.generateForPreset(m.lastPresetName)
		if err != nil {
			m.err = err
			return m, nil
		}
		m.generatedPassword = pw
		return m, nil
	case "p", "ctrl+r":
		m.passwordVisible = !m.passwordVisible
		return m, nil
	case "enter":
		pw := m.generatedPassword
		return m, func() tea.Msg { return GeneratedPasswordMsg{Password: pw} }
	}
	return m, nil
}

func (m PresetPickerModel) View() tea.View {
	switch m.mode {
	case pickerModeCustom:
		return tea.NewView(m.viewCustom())
	case pickerModePreview:
		return tea.NewView(m.viewPreview())
	default:
		return tea.NewView(m.viewList())
	}
}

func (m PresetPickerModel) viewList() string {
	var b strings.Builder
	width, height := viewSize(m.width, m.height)
	b.WriteString(header(width, "", "Generate Password"))
	b.WriteString("\n\n")

	defaultPreset := m.cfg.Preset()
	nameWidth := 0
	for _, p := range m.presets {
		nameWidth = max(nameWidth, lipgloss.Width(p.name))
	}

	for i, p := range m.presets {
		cursor, style := "  ", valueStyle
		if i == m.cursor {
			cursor, style = selectionCursor, selectedItemStyle
		}

		marker := "  "
		if p.name == defaultPreset {
			marker = successStyle.Padding(0).Render("*") + " "
		}

		row := cursor + marker + style.Render(padCell(p.name, nameWidth)) + "  " + hiddenStyle.Render(p.desc)
		if i == m.cursor {
			row = selectedRow(row, width)
		}
		b.WriteString(row + "\n")
	}

	if m.err != nil {
		b.WriteString("\n")
		b.WriteString(errorMsgStyle.Render(fmt.Sprintf("  Error: %s", m.err)))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	footer := renderHints(width, "enter: select", "esc: cancel", "F1: help", "j/k: navigate", "d: set default")
	offset := followField(0, fieldRows{3 + m.cursor, 3 + m.cursor}, bodyHeight(height, footer))
	return screen(b.String(), footer, width, height, offset)
}

func (m PresetPickerModel) viewCustom() string {
	var b strings.Builder
	width, height := viewSize(m.width, m.height)
	sizeInput(&m.lengthInput, width)
	b.WriteString(header(width, "", "Generate Password", "Custom"))
	b.WriteString("\n\n")

	b.WriteString(m.lengthInput.View())
	b.WriteString("\n\n")

	type toggle struct {
		label string
		desc  string
	}
	toggleLabels := []toggle{
		{"A-Z", "Uppercase letters"},
		{"a-z", "Lowercase letters"},
		{"0-9", "Digits"},
		{"!@#", "Symbols"},
	}

	for i, tl := range toggleLabels {
		check := "[ ]"
		if m.toggles[i] {
			check = "[x]"
		}

		focus, checkStyle := "  ", valueStyle
		if m.customFocus == i+1 {
			focus, checkStyle = selectionCursor, selectedItemStyle
		}
		row := fmt.Sprintf("  %s%s %s   %s", focus, checkStyle.Render(check), tl.label, hiddenStyle.Render(tl.desc))
		if m.customFocus == i+1 {
			row = selectedRow(row, width)
		}
		b.WriteString(row + "\n")
	}

	if m.err != nil {
		b.WriteString("\n")
		b.WriteString(errorMsgStyle.Render(fmt.Sprintf("  Error: %s", m.err)))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	footer := renderHints(width, "enter: generate", "esc: back", "F1: help", "tab: next", "space: toggle")
	row := 3
	if m.customFocus > 0 {
		row = 4 + m.customFocus
	}
	offset := followField(0, fieldRows{row, row}, bodyHeight(height, footer))
	return screen(b.String(), footer, width, height, offset)
}

func (m PresetPickerModel) viewPreview() string {
	var b strings.Builder
	width, height := viewSize(m.width, m.height)
	b.WriteString(header(width, "", "Generate Password", m.lastPresetName))
	b.WriteString("\n\n")

	pw := m.generatedPassword
	if !m.passwordVisible {
		pw = maskedValue
	}
	fmt.Fprintf(&b, "  %s %s\n", labelStyle.Render("Password:"), valueStyle.Render(pw))

	if m.err != nil {
		b.WriteString("\n")
		b.WriteString(errorMsgStyle.Render(fmt.Sprintf("  Error: %s", m.err)))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	footer := renderHints(width, "enter: accept", "esc: back", "F1: help", "r: refresh", "p: show/hide")
	return screen(wrapContent(b.String(), width), footer, width, height, 0)
}

// describePassword analyzes a password and returns a human-readable description
// of its length and character classes.
func describePassword(pw string) string {
	strength := keepass.EvaluatePasswordStrength(pw)
	var classes []string
	if strength.HasUpper {
		classes = append(classes, "A-Z")
	}
	if strength.HasLower {
		classes = append(classes, "a-z")
	}
	if strength.HasDigit {
		classes = append(classes, "0-9")
	}
	if strength.HasSymbol {
		classes = append(classes, "!@#")
	}
	if len(classes) == 0 {
		classes = append(classes, "a-z")
	}
	return fmt.Sprintf("%d chars, %s", strength.Length, strings.Join(classes, " "))
}

// generateLikeCurrent generates a password that matches the character profile
// of the given password.
func generateLikeCurrent(pw string) (string, error) {
	if pw == "" {
		return "", fmt.Errorf("no current password")
	}

	length := len([]rune(pw))

	var hasLower, hasUpper, hasDigit, hasSymbol bool
	for _, r := range pw {
		switch {
		case unicode.IsLower(r) && r < 128:
			hasLower = true
		case unicode.IsUpper(r) && r < 128:
			hasUpper = true
		case unicode.IsDigit(r) && r < 128:
			hasDigit = true
		case r < 128:
			hasSymbol = true
		default:
			hasLower = true // treat unicode as lowercase for charset building
		}
	}

	var charset string
	if hasLower {
		charset += "abcdefghijklmnopqrstuvwxyz"
	}
	if hasUpper {
		charset += "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	}
	if hasDigit {
		charset += "0123456789"
	}
	if hasSymbol {
		charset += "!@#$%^&*()-_=+"
	}
	if charset == "" {
		charset = "abcdefghijklmnopqrstuvwxyz"
	}

	return generatePassword(length, charset)
}
