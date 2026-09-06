package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/rmarchei/keetui/internal/config"
)

func isToggleSetting(i int) bool {
	return i >= settingPasswordStrength && i <= settingPWSymbols || i == settingConfirmQuit || i == settingBackupOnSave
}

func (m SettingsModel) moveFocus(delta int) SettingsModel {
	m.inputs[m.focused].Blur()
	m.focused = (m.focused + delta + settingCount) % settingCount
	m.inputs[m.focused].Focus()
	m.status = ""
	return m
}

func (m SettingsModel) choices() []string {
	switch m.focused {
	case settingDefaultPreset:
		return PresetNames
	case settingTheme:
		return ThemeNames()
	default:
		return nil
	}
}

func (m SettingsModel) updatePicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.picking = false
		case "ctrl+c":
			return m, func() tea.Msg { return RequestQuitMsg{} }
		case "j", "down", "tab", "right":
			m.choice = (m.choice + 1) % len(m.choices())
		case "k", "up", "shift+tab", "left":
			m.choice = (m.choice + len(m.choices()) - 1) % len(m.choices())
		case "enter", "space":
			m.inputs[m.focused].SetValue(m.choices()[m.choice])
			m.picking = false
			m.status = ""
		}
	}
	return m, nil
}

func (m SettingsModel) pickerView() string {
	width, height := viewSize(m.width, m.height)
	var b strings.Builder
	b.WriteString(titleStyle.Render(" "+settingLabels[m.focused]+" ") + "\n\n")
	for i, choice := range m.choices() {
		cursor, style := "  ", valueStyle
		if i == m.choice {
			cursor, style = "▸ ", focusedStyle.Bold(true)
		}
		fmt.Fprintf(&b, "%s%s\n", cursor, style.Render(choice))
	}
	footer := renderHints(width, "enter/space: select", "esc: cancel", "↑/↓: choose")
	offset := followField(0, fieldRows{2 + m.choice, 2 + m.choice}, bodyHeight(height, footer))
	return screen(b.String(), footer, width, height, offset)
}

func (m SettingsModel) updateBindings(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		key := msg.String()
		if m.recording {
			if key == "esc" {
				m.recording, m.bindingError = false, ""
				return m, nil
			}
			if key == "" {
				return m, nil
			}
			id := m.bindings.Bindings()[m.bindingIndex].ID
			next, err := m.bindings.WithBinding(id, key)
			if err == nil {
				err = next.Validate()
			}
			if err != nil {
				m.bindingError = err.Error()
				return m, nil
			}
			m.bindings = next
			m.recording, m.bindingError = false, ""
			return m, nil
		}
		switch key {
		case "ctrl+c":
			return m, func() tea.Msg { return RequestQuitMsg{} }
		case "esc":
			m.editingKeys, m.bindingError = false, ""
		case "ctrl+s":
			return m.save()
		case "j", "down", "tab":
			m.bindingIndex = (m.bindingIndex + 1) % len(m.bindings.Bindings())
			m.bindingError = ""
		case "k", "up", "shift+tab":
			m.bindingIndex = (m.bindingIndex + len(m.bindings.Bindings()) - 1) % len(m.bindings.Bindings())
			m.bindingError = ""
		case "enter", "space":
			m.recording, m.bindingError = true, ""
		case "backspace", "delete":
			binding := m.bindings.Bindings()[m.bindingIndex]
			defaults := config.DefaultKeyBindings().Bindings()
			for _, def := range defaults {
				if def.ID != binding.ID {
					continue
				}
				next, err := m.bindings.WithBinding(binding.ID, def.Key)
				if err == nil {
					err = next.Validate()
				}
				if err != nil {
					m.bindingError = err.Error()
				} else {
					m.bindings, m.bindingError = next, ""
				}
				break
			}
		}
	}
	return m, nil
}

func (m SettingsModel) bindingsView() string {
	width, height := viewSize(m.width, m.height)
	var b strings.Builder
	b.WriteString(titleStyle.Render(" Keybindings ") + "\n\n")
	rows := make([]fieldRows, 0, len(m.bindings.Bindings()))
	for i, binding := range m.bindings.Bindings() {
		row := strings.Count(b.String(), "\n")
		cursor, style := "  ", valueStyle
		if i == m.bindingIndex {
			cursor, style = "▸ ", focusedStyle.Bold(true)
		}
		key := binding.Key
		if i == m.bindingIndex && m.recording {
			key = "press a key…"
		}
		line := cursor + binding.Label + ": " + style.Render(sanitizeDisplay(key))
		b.WriteString(wrapContent(line, width) + "\n")
		rows = append(rows, fieldRows{row, strings.Count(b.String(), "\n") - 1})
	}
	footer := renderHints(width, "enter: record key", "esc: back", "ctrl+s: save", "↑/↓: choose", "backspace: default")
	if m.recording {
		footer = renderHints(width, "press shortcut to assign", "esc: cancel recording")
	}
	if m.bindingError != "" {
		problem := wrapContent(errorMsgStyle.Render(m.bindingError), width)
		if lipgloss.Height(problem) > max(1, height-lipgloss.Height(footer)-3) {
			problem = wrapContent(errorMsgStyle.Render("Key already used or reserved; try another"), width)
		}
		footer = problem + "\n" + footer
	}
	available := max(1, height-lipgloss.Height(footer))
	offset := followField(0, rows[m.bindingIndex], available)
	return screen(b.String(), footer, width, height, offset)
}
