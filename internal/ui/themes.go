package ui

import "charm.land/lipgloss/v2"

// Theme defines a set of colors for the UI.
type Theme struct {
	Primary     string // accent: focus, selection, key hints, group icons
	Label       string // field labels and secondary columns
	Muted       string
	Success     string
	Error       string
	Warn        string
	Text        string
	TitleFg     string // foreground for title bar (over Primary background)
	StatusBg    string
	StatusFg    string
	SelectionBg string // the bar behind the selected row
	SelectionFg string
}

var builtinThemes = map[string]Theme{
	"dark": {
		Primary:     "#7D56F4",
		Label:       "#A1A1AA",
		Muted:       "#626262",
		Success:     "#04B575",
		Error:       "#FF4444",
		Warn:        "#FFAA00",
		Text:        "#FAFAFA",
		TitleFg:     "#000000",
		StatusBg:    "#3C3C3C",
		StatusFg:    "#FAFAFA",
		SelectionBg: "#2F2A47",
		SelectionFg: "#FFFFFF",
	},
	"light": {
		Primary:     "#5B21B6",
		Label:       "#6B7280",
		Muted:       "#9CA3AF",
		Success:     "#059669",
		Error:       "#DC2626",
		Warn:        "#D97706",
		Text:        "#1F2937",
		TitleFg:     "#FFFFFF",
		StatusBg:    "#E5E7EB",
		StatusFg:    "#1F2937",
		SelectionBg: "#EDE9FE",
		SelectionFg: "#1F2937",
	},
	"high-contrast": {
		Primary:     "#FFFF00",
		Label:       "#00FFFF",
		Muted:       "#AAAAAA",
		Success:     "#00FF00",
		Error:       "#FF0000",
		Warn:        "#FFAA00",
		Text:        "#FFFFFF",
		TitleFg:     "#000000",
		StatusBg:    "#000000",
		StatusFg:    "#FFFFFF",
		SelectionBg: "#444444",
		SelectionFg: "#FFFF00",
	},
}

// GetTheme returns a theme by name, falling back to "dark" for unknown names.
func GetTheme(name string) Theme {
	if t, ok := builtinThemes[name]; ok {
		return t
	}
	return builtinThemes["dark"]
}

// ThemeNames returns the list of available built-in theme names.
func ThemeNames() []string {
	return []string{"dark", "light", "high-contrast"}
}

// ApplyTheme updates all global style variables to use the given theme's colors.
func ApplyTheme(t Theme) {
	primaryColor = lipgloss.Color(t.Primary)
	labelColor = lipgloss.Color(t.Label)
	mutedColor = lipgloss.Color(t.Muted)
	successColor = lipgloss.Color(t.Success)
	errorColor = lipgloss.Color(t.Error)
	warnColor = lipgloss.Color(t.Warn)

	textColor := lipgloss.Color(t.Text)

	titleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(t.TitleFg)).
		Background(primaryColor).
		Padding(0, 1)

	breadcrumbStyle = lipgloss.NewStyle().
		Foreground(mutedColor).
		Padding(0, 1)

	itemStyle = lipgloss.NewStyle().
		PaddingLeft(2)

	selectedRowStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.SelectionFg)).
		Background(lipgloss.Color(t.SelectionBg))

	selectedItemStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.SelectionFg)).
		Bold(true)

	groupIconStyle = lipgloss.NewStyle().
		Foreground(primaryColor)

	groupStyle = lipgloss.NewStyle().
		Foreground(textColor).
		Bold(true)

	entryStyle = lipgloss.NewStyle().
		Foreground(textColor)

	secondaryStyle = lipgloss.NewStyle().
		Foreground(labelColor)

	labelStyle = lipgloss.NewStyle().
		Foreground(labelColor).
		Width(12)

	valueStyle = lipgloss.NewStyle().
		Foreground(textColor)

	hiddenStyle = lipgloss.NewStyle().
		Foreground(mutedColor)

	successStyle = lipgloss.NewStyle().
		Foreground(successColor).
		Padding(0, 1)

	errorMsgStyle = lipgloss.NewStyle().
		Foreground(errorColor).
		Padding(0, 1)

	warnStyle = lipgloss.NewStyle().
		Foreground(warnColor).
		Padding(0, 1)

	sectionStyle = lipgloss.NewStyle().
		Foreground(primaryColor).
		Bold(true)

	helpStyle = lipgloss.NewStyle().
		Foreground(mutedColor)

	hintKeyStyle = lipgloss.NewStyle().
		Foreground(primaryColor)

	focusedStyle = lipgloss.NewStyle().
		Foreground(primaryColor)

	blurredStyle = lipgloss.NewStyle().
		Foreground(mutedColor)

	statusBarBgStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.StatusFg)).
		Background(lipgloss.Color(t.StatusBg)).
		Padding(0, 1)

	statusBarModifiedStyle = lipgloss.NewStyle().
		Foreground(warnColor).
		Bold(true)
}
