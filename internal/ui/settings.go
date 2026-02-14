package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/rmarchei/keetui/internal/config"
)

const (
	settingClipboardClear = iota
	settingAutoLock
	settingPasswordLength
	settingPasswordStrength
	settingCount
)

var settingLabels = [settingCount]string{
	"Clipboard clear (sec)",
	"Auto-lock (min)",
	"Password length",
	"Password strength",
}

var settingDescriptions = [settingCount]string{
	"Seconds before clipboard is cleared after copying (0: disable, min: 15)",
	"Minutes of inactivity before database is locked (0: disable, min: 5)",
	"Default length for generated passwords (min: 8)",
	"Show password strength indicator (1: on, 0: off)",
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
	for i := range inputs {
		t := textinput.New()
		t.Prompt = fmt.Sprintf("  %-24s", settingLabels[i]+":")
		t.PromptStyle = labelStyle
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

	newCfg := config.Config{
		ClipboardClearSeconds: clipSec,
		AutoLockMinutes:       lockMin,
		PasswordLength:        pwLen,
		ShowPasswordStrength:  strengthVal == "1",
	}

	if err := newCfg.Save(); err != nil {
		m.status = fmt.Sprintf("Error saving config: %s", err)
		m.isError = true
		return m, nil
	}

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

	b.WriteString(fmt.Sprintf("  %s %s\n\n",
		helpStyle.Render("Config file:"),
		valueStyle.Render(config.Path())))

	for i := range m.inputs {
		b.WriteString(m.inputs[i].View())
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("  %s\n", hiddenStyle.Render(settingDescriptions[i])))
		if i < settingCount-1 {
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	if m.status != "" {
		if m.isError {
			b.WriteString(fmt.Sprintf("  %s\n", errorMsgStyle.Render(m.status)))
		} else {
			b.WriteString(fmt.Sprintf("  %s\n", successStyle.Render(m.status)))
		}
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  tab/shift+tab: navigate  enter/ctrl+s: save  esc: back"))
	b.WriteString("\n")

	return b.String()
}
