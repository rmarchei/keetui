package ui

import "github.com/charmbracelet/lipgloss"

// Theme defines a set of colors for the UI.
type Theme struct {
	Primary   string
	Secondary string
	Muted     string
	Success   string
	Error     string
	Warn      string
	Text      string
	TitleFg   string // foreground for title bar (over Primary background)
	StatusBg  string
	StatusFg  string
}

var builtinThemes = map[string]Theme{
	"dark": {
		Primary:   "#7D56F4",
		Secondary: "#AD58B4",
		Muted:     "#626262",
		Success:   "#04B575",
		Error:     "#FF4444",
		Warn:      "#FFAA00",
		Text:      "#FAFAFA",
		TitleFg:   "#000000",
		StatusBg:  "#3C3C3C",
		StatusFg:  "#FAFAFA",
	},
	"light": {
		Primary:   "#5B21B6",
		Secondary: "#7C3AED",
		Muted:     "#9CA3AF",
		Success:   "#059669",
		Error:     "#DC2626",
		Warn:      "#D97706",
		Text:      "#1F2937",
		TitleFg:   "#FFFFFF",
		StatusBg:  "#E5E7EB",
		StatusFg:  "#1F2937",
	},
	"high-contrast": {
		Primary:   "#FFFF00",
		Secondary: "#FF00FF",
		Muted:     "#AAAAAA",
		Success:   "#00FF00",
		Error:     "#FF0000",
		Warn:      "#FFAA00",
		Text:      "#FFFFFF",
		TitleFg:   "#000000",
		StatusBg:  "#000000",
		StatusFg:  "#FFFFFF",
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
	secondaryColor = lipgloss.Color(t.Secondary)
	mutedColor = lipgloss.Color(t.Muted)
	successColor = lipgloss.Color(t.Success)
	errorColor = lipgloss.Color(t.Error)
	warnColor = lipgloss.Color(t.Warn)

	textColor := lipgloss.Color(t.Text)
	titleFg := lipgloss.Color(t.TitleFg)

	titleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(titleFg).
		Background(primaryColor).
		Padding(0, 1)

	breadcrumbStyle = lipgloss.NewStyle().
		Foreground(mutedColor).
		Padding(0, 1)

	itemStyle = lipgloss.NewStyle().
		PaddingLeft(2)

	selectedItemStyle = lipgloss.NewStyle().
		PaddingLeft(2).
		Foreground(primaryColor).
		Bold(true)

	groupStyle = lipgloss.NewStyle().
		Foreground(warnColor)

	entryStyle = lipgloss.NewStyle().
		Foreground(textColor)

	labelStyle = lipgloss.NewStyle().
		Foreground(secondaryColor).
		Bold(true).
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

	helpStyle = lipgloss.NewStyle().
		Foreground(mutedColor)

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
