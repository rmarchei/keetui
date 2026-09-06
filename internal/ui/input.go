package ui

import (
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

// newTextInput keeps the original input appearance and virtual cursor. The
// cursor is rendered within the string, so nested views need no cursor offsets.
func newTextInput() textinput.Model {
	input := textinput.New()
	styles := input.Styles()
	styles.Focused.Prompt = lipgloss.NewStyle()
	styles.Blurred = styles.Focused
	styles.Cursor.Color = nil
	input.SetStyles(styles)
	input.SetVirtualCursor(true)
	return input
}

func setInputPromptStyle(input *textinput.Model, style lipgloss.Style) {
	styles := input.Styles()
	styles.Focused.Prompt = style
	styles.Blurred.Prompt = style
	input.SetStyles(styles)
}

func setInputTextStyle(input *textinput.Model, style lipgloss.Style) {
	styles := input.Styles()
	styles.Focused.Text = style
	styles.Blurred.Text = style
	input.SetStyles(styles)
}
