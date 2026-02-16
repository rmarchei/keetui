package ui

import "github.com/charmbracelet/lipgloss"

var (
	// Colors
	primaryColor   = lipgloss.Color("#7D56F4")
	secondaryColor = lipgloss.Color("#AD58B4")
	mutedColor     = lipgloss.Color("#626262")
	successColor   = lipgloss.Color("#04B575")
	errorColor     = lipgloss.Color("#FF4444")
	warnColor      = lipgloss.Color("#FFAA00")

	// Title bar
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#000000")).
			Background(primaryColor).
			Padding(0, 1)

	// Breadcrumb / path
	breadcrumbStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			Padding(0, 1)

	// List items
	itemStyle = lipgloss.NewStyle().
			PaddingLeft(2)

	selectedItemStyle = lipgloss.NewStyle().
				PaddingLeft(2).
				Foreground(primaryColor).
				Bold(true)

	// Group icon styling
	groupStyle = lipgloss.NewStyle().
			Foreground(warnColor)

	entryStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FAFAFA"))

	// Detail view
	labelStyle = lipgloss.NewStyle().
			Foreground(secondaryColor).
			Bold(true).
			Width(12)

	valueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FAFAFA"))

	hiddenStyle = lipgloss.NewStyle().
			Foreground(mutedColor)

	successStyle = lipgloss.NewStyle().
			Foreground(successColor).
			Padding(0, 1)

	errorMsgStyle = lipgloss.NewStyle().
			Foreground(errorColor).
			Padding(0, 1)

	// Warning
	warnStyle = lipgloss.NewStyle().
			Foreground(warnColor).
			Padding(0, 1)

	// Help
	helpStyle = lipgloss.NewStyle().
			Foreground(mutedColor)

	// Input field focused
	focusedStyle = lipgloss.NewStyle().
			Foreground(primaryColor)

	// Input field blurred
	blurredStyle = lipgloss.NewStyle().
			Foreground(mutedColor)

	// Persistent status bar (bottom of screen)
	statusBarBgStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FAFAFA")).
				Background(lipgloss.Color("#3C3C3C")).
				Padding(0, 1)

	statusBarModifiedStyle = lipgloss.NewStyle().
				Foreground(warnColor).
				Bold(true)
)
