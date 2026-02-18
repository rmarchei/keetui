package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rmarchei/keetui/internal/config"
)

const (
	settingClipboardClear = iota
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
	settingCount
)

var settingLabels = [settingCount]string{
	"Clipboard clear (sec)",
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
}

var settingDescriptions = [settingCount]string{
	"Seconds before clipboard is cleared after copying (0: disable, min: 15)",
	"Minutes of inactivity before database is locked (0: disable, min: 5)",
	"Default length for generated passwords (min: 8)",
	"Show password strength indicator (1: on, 0: off)",
	"Include uppercase letters in generated passwords (1: on, 0: off)",
	"Include lowercase letters in generated passwords (1: on, 0: off)",
	"Include digits in generated passwords (1: on, 0: off)",
	"Include symbols in generated passwords (1: on, 0: off)",
	"Default preset: PIN, Alphanumeric, Strong, Passphrase, Custom",
	"Color theme: dark, light, high-contrast",
	"Always confirm before quitting (1: on, 0: off)",
	"Create .kdbx.bak backup before each save (1: on, 0: off)",
}

// ShowSettingsMsg requests showing the settings view.
type ShowSettingsMsg struct{}

// ConfigSavedMsg is sent after settings are saved to disk.
type ConfigSavedMsg struct {
	Cfg config.Config
}

// SettingsModel handles viewing and editing application settings.
type SettingsModel struct {
	inputs  [settingCount]textinput.Model
	focused int
	cfg     config.Config
	width   int
	height  int
	status  string
	isError bool
}

func NewSettingsModel(cfg config.Config) SettingsModel {
	var inputs [settingCount]textinput.Model
	promptStyle := lipgloss.NewStyle().Foreground(secondaryColor).Bold(true)
	for i := range inputs {
		t := textinput.New()
		t.Prompt = fmt.Sprintf("  %-24s", settingLabels[i]+":")
		t.PromptStyle = promptStyle
		t.Width = 10
		t.CharLimit = 5
		inputs[i] = t
	}

	inputs[settingClipboardClear].SetValue(strconv.Itoa(cfg.ClipboardClearSeconds))
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

	return SettingsModel{
		inputs:  inputs,
		focused: 0,
		cfg:     cfg,
	}
}

func (m SettingsModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m SettingsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, func() tea.Msg { return RequestQuitMsg{} }
		case "esc":
			return m, func() tea.Msg { return BackToBrowserMsg{} }
		case "tab", "down":
			m.inputs[m.focused].Blur()
			m.focused = (m.focused + 1) % settingCount
			m.inputs[m.focused].Focus()
			return m, nil
		case "shift+tab", "up":
			m.inputs[m.focused].Blur()
			m.focused = (m.focused - 1 + settingCount) % settingCount
			m.inputs[m.focused].Focus()
			return m, nil
		case "ctrl+s", "enter":
			return m.save()
		}
	}

	// Update focused input
	var cmd tea.Cmd
	m.inputs[m.focused], cmd = m.inputs[m.focused].Update(msg)
	return m, cmd
}

func (m SettingsModel) save() (tea.Model, tea.Cmd) {
	clipSec, err := strconv.Atoi(strings.TrimSpace(m.inputs[settingClipboardClear].Value()))
	if err != nil || clipSec < 0 {
		m.status = "Invalid clipboard clear seconds (must be 0 or a positive number)"
		m.isError = true
		return m, nil
	}
	if clipSec != 0 && clipSec < 15 {
		m.status = "Clipboard clear must be 0 (disabled) or at least 15 seconds"
		m.isError = true
		return m, nil
	}

	lockMin, err := strconv.Atoi(strings.TrimSpace(m.inputs[settingAutoLock].Value()))
	if err != nil || lockMin < 0 {
		m.status = "Invalid auto-lock minutes (must be 0 or a positive number)"
		m.isError = true
		return m, nil
	}
	if lockMin != 0 && lockMin < 5 {
		m.status = "Auto-lock must be 0 (disabled) or at least 5 minutes"
		m.isError = true
		return m, nil
	}

	pwLen, err := strconv.Atoi(strings.TrimSpace(m.inputs[settingPasswordLength].Value()))
	if err != nil || pwLen < 1 {
		m.status = "Invalid password length (must be a positive number)"
		m.isError = true
		return m, nil
	}
	if pwLen < 8 {
		m.status = "Password length must be at least 8"
		m.isError = true
		return m, nil
	}

	strengthVal := strings.TrimSpace(m.inputs[settingPasswordStrength].Value())
	if strengthVal != "0" && strengthVal != "1" {
		m.status = "Password strength must be 1 (on) or 0 (off)"
		m.isError = true
		return m, nil
	}

	// Parse charset toggles
	parseBoolSetting := func(idx int, name string) (bool, bool) {
		v := strings.TrimSpace(m.inputs[idx].Value())
		if v != "0" && v != "1" {
			m.status = fmt.Sprintf("%s must be 1 (on) or 0 (off)", name)
			m.isError = true
			return false, false
		}
		return v == "1", true
	}

	upper, ok := parseBoolSetting(settingPWUppercase, "Uppercase")
	if !ok {
		return m, nil
	}
	lower, ok := parseBoolSetting(settingPWLowercase, "Lowercase")
	if !ok {
		return m, nil
	}
	digits, ok := parseBoolSetting(settingPWDigits, "Digits")
	if !ok {
		return m, nil
	}
	symbols, ok := parseBoolSetting(settingPWSymbols, "Symbols")
	if !ok {
		return m, nil
	}

	if !upper && !lower && !digits && !symbols {
		m.status = "At least one character set must be enabled"
		m.isError = true
		return m, nil
	}

	presetName := strings.TrimSpace(m.inputs[settingDefaultPreset].Value())
	validPreset := slices.Contains(PresetNames, presetName)
	if !validPreset {
		m.status = "Default preset must be one of: PIN, Alphanumeric, Strong, Passphrase, Custom"
		m.isError = true
		return m, nil
	}

	themeName := strings.TrimSpace(m.inputs[settingTheme].Value())
	validTheme := slices.Contains(ThemeNames(), themeName)
	if !validTheme {
		m.status = "Theme must be one of: dark, light, high-contrast"
		m.isError = true
		return m, nil
	}

	confirmQuit, ok := parseBoolSetting(settingConfirmQuit, "Confirm on quit")
	if !ok {
		return m, nil
	}

	backupOnSave, ok := parseBoolSetting(settingBackupOnSave, "Backup on save")
	if !ok {
		return m, nil
	}

	boolPtr := func(b bool) *bool { return &b }

	newCfg := config.Config{
		ClipboardClearSeconds: clipSec,
		AutoLockMinutes:       lockMin,
		PasswordLength:        pwLen,
		ShowPasswordStrength:  strengthVal == "1",
		PasswordUppercase:     boolPtr(upper),
		PasswordLowercase:     boolPtr(lower),
		PasswordDigits:        boolPtr(digits),
		PasswordSymbols:       boolPtr(symbols),
		DefaultPreset:         presetName,
		ThemeName:             themeName,
		ConfirmQuit:           boolPtr(confirmQuit),
		BackupOnSave:          boolPtr(backupOnSave),
	}

	if err := newCfg.Save(); err != nil {
		m.status = fmt.Sprintf("Error saving config: %s", err)
		m.isError = true
		return m, nil
	}

	ApplyTheme(GetTheme(themeName))
	m.cfg = newCfg
	m.inputs[settingClipboardClear].SetValue(strconv.Itoa(clipSec))
	m.inputs[settingAutoLock].SetValue(strconv.Itoa(lockMin))
	m.inputs[settingPasswordLength].SetValue(strconv.Itoa(pwLen))

	return m, func() tea.Msg { return ConfigSavedMsg{Cfg: newCfg} }
}

func (m SettingsModel) View() string {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString(titleStyle.Render(" Settings "))
	b.WriteString("\n\n")

	fmt.Fprintf(&b, "  %s %s\n\n",
		helpStyle.Render("Config file:"),
		valueStyle.Render(config.Path()))

	for i := range m.inputs {
		b.WriteString(m.inputs[i].View())
		b.WriteString("\n")
		fmt.Fprintf(&b, "  %s\n", hiddenStyle.Render(settingDescriptions[i]))
		if i < settingCount-1 {
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	if m.status != "" {
		if m.isError {
			fmt.Fprintf(&b, "  %s\n", errorMsgStyle.Render(m.status))
		} else {
			fmt.Fprintf(&b, "  %s\n", successStyle.Render(m.status))
		}
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  tab/shift+tab: navigate  enter/ctrl+s: save  esc: back"))
	b.WriteString("\n")

	return b.String()
}
