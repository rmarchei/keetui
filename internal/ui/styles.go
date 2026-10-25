package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

const selectionCursor = "▸ "

// Styles are built from the active theme by ApplyTheme; the dark theme
// applies until the configuration names another.
var (
	// Colors
	primaryColor color.Color
	labelColor   color.Color
	mutedColor   color.Color
	successColor color.Color
	errorColor   color.Color
	warnColor    color.Color

	titleStyle lipgloss.Style // the app name in the header bar

	// List items
	itemStyle         lipgloss.Style
	selectedItemStyle lipgloss.Style // the selected row's main text
	selectedRowStyle  lipgloss.Style // the bar behind the selected row
	groupIconStyle    lipgloss.Style
	groupStyle        lipgloss.Style // group names
	entryStyle        lipgloss.Style
	secondaryStyle    lipgloss.Style // secondary columns: usernames

	// Detail view
	labelStyle  lipgloss.Style
	valueStyle  lipgloss.Style
	hiddenStyle lipgloss.Style

	successStyle  lipgloss.Style
	errorMsgStyle lipgloss.Style
	warnStyle     lipgloss.Style

	sectionStyle lipgloss.Style // section headings

	// Help and key hints
	helpStyle    lipgloss.Style
	hintKeyStyle lipgloss.Style

	focusedStyle lipgloss.Style // input field focused
	blurredStyle lipgloss.Style // input field blurred

	// Persistent status bar (bottom of screen)
	statusBarBgStyle       lipgloss.Style
	statusBarModifiedStyle lipgloss.Style
)

func init() { ApplyTheme(GetTheme("dark")) }
