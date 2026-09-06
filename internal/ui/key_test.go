package ui

import tea "charm.land/bubbletea/v2"

// textKey builds the same printable key event emitted by Bubble Tea v2.
func textKey(text string) tea.KeyPressMsg {
	var code rune
	for _, r := range text {
		code = r
		break
	}
	return tea.KeyPressMsg{Code: code, Text: text}
}
