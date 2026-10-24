package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/rmarchei/keetui/internal/config"
)

const (
	settingClipboardClear = iota
	settingClipboardOSC52
	settingAutoLock
	settingPasswordLength
	settingPasswordStrength
	settingPWUppercase
	settingPWLowercase
	settingPWDigits
	settingPWSymbols
	settingDefaultPreset
	settingTheme
	settingConfirmQuit
	settingBackupOnSave
	settingKeyBindings
	settingCount
)

var settingLabels = [settingCount]string{
	"Clipboard clear (sec)",
	"Terminal clipboard",
	"Auto-lock (min)",
	"Password length",
	"Password strength",
	"Uppercase (A-Z)",
	"Lowercase (a-z)",
	"Digits (0-9)",
	"Symbols (!@#...)",
	"Default preset",
	"Theme",
	"Confirm on quit",
	"Backup on save",
	"Keybindings",
}

var settingDescriptions = [settingCount]string{
	"Seconds before clipboard is cleared after copying (0: disable, min: 15)",
	"Copy via OSC 52, which cannot be marked sensitive: auto (only without a local clipboard), always, never",
	"Minutes of inactivity before database is locked (0: disable, min: 5)",
	"Default length for generated passwords (min: 8)",
	"Show password strength indicator",
	"Include uppercase letters in generated passwords",
	"Include lowercase letters in generated passwords",
	"Include digits in generated passwords",
	"Include symbols in generated passwords",
	"Choose the default password generation preset",
	"Choose the color theme",
	"Confirm before quitting, even without unsaved changes",
	"Create a .kdbx.bak backup before each save",
	"Record shortcuts by pressing keys; conflicts are checked immediately",
}

// ShowSettingsMsg requests showing the settings view.
type ShowSettingsMsg struct{}

// ConfigSavedMsg is sent after settings are saved to disk.
type ConfigSavedMsg struct {
	Cfg config.Config
}

// SettingsModel handles viewing and editing application settings.
type SettingsModel struct {
	inputs       [settingCount]textinput.Model
	focused      int
	cfg          config.Config
	width        int
	height       int
	status       string
	isError      bool
	saveError    string // persists until a successful save, including in sub-editors
	offset       int
	fieldErrors  map[int]string
	picking      bool
	choice       int
	editingKeys  bool
	recording    bool
	bindings     config.KeyBindings
	bindingIndex int
	bindingError string
}

func NewSettingsModel(cfg config.Config) SettingsModel {
	var inputs [settingCount]textinput.Model
	promptStyle := lipgloss.NewStyle().Foreground(secondaryColor).Bold(true)
	for i := range inputs {
		t := newTextInput()
		t.Prompt = fmt.Sprintf("  %-24s", settingLabels[i]+":")
		setInputPromptStyle(&t, promptStyle)
		t.SetWidth(10)
		t.CharLimit = 5
		inputs[i] = t
	}

	inputs[settingClipboardClear].SetValue(strconv.Itoa(cfg.ClipboardClearSeconds))
	inputs[settingClipboardOSC52].CharLimit = 6
	inputs[settingClipboardOSC52].SetValue(cfg.OSC52Mode())
	inputs[settingAutoLock].SetValue(strconv.Itoa(cfg.AutoLockMinutes))
	inputs[settingPasswordLength].SetValue(strconv.Itoa(cfg.PasswordLength))
	if cfg.ShowPasswordStrength {
		inputs[settingPasswordStrength].SetValue("1")
	} else {
		inputs[settingPasswordStrength].SetValue("0")
	}
	inputs[settingPasswordStrength].CharLimit = 1

	boolToStr := func(b bool) string {
		if b {
			return "1"
		}
		return "0"
	}
	inputs[settingPWUppercase].SetValue(boolToStr(cfg.PasswordHasUppercase()))
	inputs[settingPWUppercase].CharLimit = 1
	inputs[settingPWLowercase].SetValue(boolToStr(cfg.PasswordHasLowercase()))
	inputs[settingPWLowercase].CharLimit = 1
	inputs[settingPWDigits].SetValue(boolToStr(cfg.PasswordHasDigits()))
	inputs[settingPWDigits].CharLimit = 1
	inputs[settingPWSymbols].SetValue(boolToStr(cfg.PasswordHasSymbols()))
	inputs[settingPWSymbols].CharLimit = 1

	inputs[settingDefaultPreset].CharLimit = 15
	inputs[settingDefaultPreset].SetValue(cfg.Preset())

	inputs[settingTheme].CharLimit = 15
	inputs[settingTheme].SetValue(cfg.Theme())

	inputs[settingConfirmQuit].SetValue(boolToStr(cfg.ConfirmOnQuit()))
	inputs[settingConfirmQuit].CharLimit = 1

	inputs[settingBackupOnSave].SetValue(boolToStr(cfg.BackupEnabled()))
	inputs[settingBackupOnSave].CharLimit = 1

	inputs[0].Focus()

	m := SettingsModel{
		inputs:   inputs,
		focused:  0,
		cfg:      cfg,
		bindings: cfg.Keys(),
	}
	m.syncLayout()
	return m
}

func (m SettingsModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m SettingsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := m.update(msg)
	next := updated.(SettingsModel)
	next.syncLayout()
	return next, cmd
}

func (m SettingsModel) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.editingKeys {
		return m.updateBindings(msg)
	}
	if m.picking {
		return m.updatePicker(msg)
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			return m.update(tea.KeyPressMsg{Code: tea.KeyUp})
		case tea.MouseWheelDown:
			return m.update(tea.KeyPressMsg{Code: tea.KeyDown})
		}

	case tea.KeyPressMsg:
		m.status, m.isError = "", false
		switch msg.String() {
		case "ctrl+c":
			return m, func() tea.Msg { return RequestQuitMsg{} }
		case "esc":
			return m, func() tea.Msg { return BackToBrowserMsg{} }
		case "tab", "down":
			return m.moveFocus(1), textinput.Blink
		case "shift+tab", "up":
			return m.moveFocus(-1), textinput.Blink
		case "ctrl+s":
			return m.save()
		case "space", "enter":
			if isToggleSetting(m.focused) {
				value := "1"
				if m.inputs[m.focused].Value() == "1" {
					value = "0"
				}
				m.inputs[m.focused].SetValue(value)
				return m, nil
			}
			if len(m.choices()) > 0 {
				m.picking = true
				m.choice = max(0, slices.Index(m.choices(), m.inputs[m.focused].Value()))
				return m, nil
			}
			if m.focused == settingKeyBindings {
				m.editingKeys = true
				return m, nil
			}
			if msg.String() == "enter" {
				return m.save()
			}
		}
	}

	if isToggleSetting(m.focused) || len(m.choices()) > 0 || m.focused == settingKeyBindings {
		return m, nil
	}
	// Update focused input
	var cmd tea.Cmd
	m.inputs[m.focused], cmd = m.inputs[m.focused].Update(msg)
	return m, cmd
}

func (m SettingsModel) save() (tea.Model, tea.Cmd) {
	cfg, errs := m.draftConfig()
	for i := range settingCount {
		if problem := errs[i]; problem != "" {
			m.editingKeys = i == settingKeyBindings
			m.inputs[m.focused].Blur()
			m.focused = i
			m.inputs[i].Focus()
			m.status, m.isError = problem, true
			m.syncLayout()
			return m, nil
		}
	}
	if err := cfg.Save(); err != nil {
		m.saveError = fmt.Sprintf("Settings not saved: %s", err)
		return m, nil
	}
	ApplyTheme(GetTheme(cfg.Theme()))
	m.cfg = cfg
	m.saveError, m.status, m.isError = "", "", false
	return m, func() tea.Msg { return ConfigSavedMsg{Cfg: cfg} }
}

// draftConfig validates without writing or changing the live configuration.
func (m SettingsModel) draftConfig() (config.Config, map[int]string) {
	cfg := m.cfg
	errs := make(map[int]string)
	number := func(i, minimum int, allowZero bool) int {
		n, err := strconv.Atoi(strings.TrimSpace(m.inputs[i].Value()))
		if err != nil || n < minimum && (!allowZero || n != 0) {
			errs[i] = fmt.Sprintf("Must be at least %d", minimum)
			if allowZero {
				errs[i] += " or 0 to disable"
			}
		}
		return n
	}
	cfg.ClipboardClearSeconds = number(settingClipboardClear, 15, true)
	cfg.ClipboardOSC52 = m.inputs[settingClipboardOSC52].Value()
	if !slices.Contains(config.OSC52Modes, cfg.ClipboardOSC52) {
		errs[settingClipboardOSC52] = "Choose a mode from the list"
	}
	cfg.AutoLockMinutes = number(settingAutoLock, 5, true)
	cfg.PasswordLength = number(settingPasswordLength, 8, false)
	toggle := func(i int) bool {
		v := m.inputs[i].Value()
		if v != "0" && v != "1" {
			errs[i] = "Choose On or Off"
		}
		return v == "1"
	}
	ptr := func(v bool) *bool { return &v }
	cfg.ShowPasswordStrength = toggle(settingPasswordStrength)
	cfg.PasswordUppercase = ptr(toggle(settingPWUppercase))
	cfg.PasswordLowercase = ptr(toggle(settingPWLowercase))
	cfg.PasswordDigits = ptr(toggle(settingPWDigits))
	cfg.PasswordSymbols = ptr(toggle(settingPWSymbols))
	cfg.ConfirmQuit = ptr(toggle(settingConfirmQuit))
	cfg.BackupOnSave = ptr(toggle(settingBackupOnSave))
	if !*cfg.PasswordUppercase && !*cfg.PasswordLowercase && !*cfg.PasswordDigits && !*cfg.PasswordSymbols {
		errs[settingPWSymbols] = "At least one character set must be enabled"
	}
	cfg.DefaultPreset = m.inputs[settingDefaultPreset].Value()
	if !slices.Contains(PresetNames, cfg.DefaultPreset) {
		errs[settingDefaultPreset] = "Choose a preset from the list"
	}
	cfg.ThemeName = m.inputs[settingTheme].Value()
	if !slices.Contains(ThemeNames(), cfg.ThemeName) {
		errs[settingTheme] = "Choose a theme from the list"
	}
	if err := m.bindings.Validate(); err != nil {
		errs[settingKeyBindings] = err.Error()
	}
	cfg.KeyBindingsPtr = m.bindings.Overrides()
	return cfg, errs
}

func (m *SettingsModel) syncLayout() {
	width, height := viewSize(m.width, m.height)
	for i := range m.inputs {
		sizeInput(&m.inputs[i], width)
	}
	_, m.fieldErrors = m.draftConfig()
	body, rows := m.body()
	available := bodyHeight(height, m.footer())
	m.offset = clampOffset(body, available, followField(m.offset, rows[m.focused], available))
}

func (m SettingsModel) body() (string, []fieldRows) {
	var b lineBuilder
	var rows []fieldRows
	width, _ := viewSize(m.width, m.height)

	b.WriteString("\n")
	b.WriteString(titleStyle.Render(" Settings "))
	b.WriteString("\n\n")

	b.WriteString(fmt.Sprintf("  %s %s\n\n",
		helpStyle.Render("Config file:"),
		pathLabel(config.Path(), max(1, width-15))))

	for i := range m.inputs {
		start := b.row
		if isToggleSetting(i) || isChoiceSetting(i) || i == settingKeyBindings {
			value := m.inputs[i].Value()
			switch {
			case isToggleSetting(i):
				value = "[ ] Off"
				if m.inputs[i].Value() == "1" {
					value = "[✓] On"
				}
			case i == settingKeyBindings:
				value = "[ Edit… ]"
			default:
				value = "[ " + value + " ▾ ]"
			}
			style := valueStyle
			if i == m.focused {
				style = focusedStyle.Bold(true)
			}
			line := labelStyle.Width(0).Render("  "+settingLabels[i]+": ") + style.Render(value)
			b.WriteString(wrapContent(line, width))
		} else {
			b.WriteString(m.inputs[i].View())
		}
		b.WriteString("\n")
		b.WriteString(wrapContent(hiddenStyle.Render("  "+settingDescriptions[i]), width) + "\n")
		if problem := m.fieldErrors[i]; problem != "" {
			b.WriteString(wrapContent(errorMsgStyle.Render(problem), width) + "\n")
		}
		rows = append(rows, fieldRows{start, b.row - 1})
		if i < settingCount-1 {
			b.WriteString("\n")
		}
	}

	return b.String(), rows
}

func (m SettingsModel) footer() string {
	width, _ := viewSize(m.width, m.height)
	var b strings.Builder
	if m.status != "" {
		if m.isError {
			fmt.Fprintf(&b, "%s\n", wrapContent(errorMsgStyle.Render(sanitizeDisplay(m.status)), width))
		} else {
			fmt.Fprintf(&b, "%s\n", wrapContent(successStyle.Render(sanitizeDisplay(m.status)), width))
		}
	}
	b.WriteString(renderHints(width, "ctrl+s: save", "esc: back", "F1: help", "space/enter: change", "tab/shift+tab: navigate"))
	return m.withSaveError(b.String())
}

func (m SettingsModel) View() tea.View {
	if m.editingKeys {
		return tea.NewView(m.bindingsView())
	}
	if m.picking {
		return tea.NewView(m.pickerView())
	}
	body, _ := m.body()
	return tea.NewView(screen(body, m.footer(), m.width, m.height, m.offset))
}
