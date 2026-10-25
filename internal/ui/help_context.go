package ui

// helpContext selects an overview tab, or replaces it with the controls of
// the active screen/submode. Text forms deliberately list their input-safe
// shortcuts rather than the configurable printable browser commands.
func (m AppModel) helpContext() (int, *helpSection) {
	k := m.cfg.Keys()
	context := func(index int, title string, bindings ...helpBinding) (int, *helpSection) {
		return index, &helpSection{title, bindings}
	}
	navigate := helpBinding{k.NavDown + "/" + k.NavUp + "/↓/↑", "Navigate"}
	back := helpBinding{"esc/" + k.Back + "/←", "Back"}
	quit := helpBinding{k.Quit + "/ctrl+c", "Quit"}
	inputQuit := helpBinding{"ctrl+c", "Quit"}
	cancel := helpBinding{"esc", "Cancel"}
	fields := helpBinding{"tab/shift+tab", "Navigate fields"}
	switch m.state {
	case viewBrowser:
		if m.browser.searching {
			return context(helpBrowser, "Search", helpBinding{"↑/↓", "Select result"},
				helpBinding{"enter", "Open selected result"}, helpBinding{"tab", "Toggle literal/fuzzy"}, cancel, inputQuit)
		}
		if m.browser.inRecycleBin() {
			return helpRecycleBin, nil
		}
	case viewDetail:
		switch {
		case m.detail.addingAttachment, m.detail.extractingAttachment:
			return context(helpDetail, "File path", helpBinding{"enter", "Confirm path"}, cancel, inputQuit)
		case m.detail.attachMode:
			return context(helpDetail, "Attachments", navigate,
				helpBinding{"enter/" + k.Save, "Save to disk"}, helpBinding{k.Delete, "Delete attachment"},
				helpBinding{"+", "Add file"}, back, quit)
		case m.detail.showQR:
			return context(helpDetail, "QR", helpBinding{k.ShowQR, "Close QR"}, back, quit)
		}
		return helpDetail, nil
	case viewHistory:
		return context(helpDetail, "History", navigate, helpBinding{k.TogglePassword, "Reveal / hide passwords"},
			helpBinding{k.CopyPassword, "Copy selected password"}, back, quit)
	case viewStats:
		return context(helpBrowser, "Database info", navigate, helpBinding{"pgup/pgdown", "Scroll a page"},
			helpBinding{"home/end", "First / last page"}, back, quit)
	case viewAudit:
		return context(helpBrowser, "Audit", navigate, helpBinding{"enter/" + k.Open + "/→", "View selected entry"}, back, quit)
	case viewEditor:
		return helpEditor, nil
	case viewPresetPicker:
		switch m.presetPicker.mode {
		case pickerModeCustom:
			return context(helpEditor, "Custom password", fields, helpBinding{"↑/↓/k/j", "Navigate fields"},
				helpBinding{"space", "Toggle character class"}, helpBinding{"enter", "Generate preview"}, cancel, inputQuit)
		case pickerModePreview:
			return context(helpEditor, "Password preview", helpBinding{"enter", "Use password"},
				helpBinding{"r", "Regenerate"}, helpBinding{"p/ctrl+r", "Reveal / hide"}, cancel, inputQuit)
		default:
			return context(helpEditor, "Password presets", helpBinding{"↑/↓/k/j", "Select preset"},
				helpBinding{"enter", "Generate / configure"}, helpBinding{"d", "Set default preset"}, cancel, inputQuit)
		}
	case viewSettings:
		switch {
		case m.settings.editingKeys:
			return context(helpSettings, "Keybindings", helpBinding{"↑/↓/k/j/tab", "Select binding"},
				helpBinding{"enter/space", "Record shortcut"}, helpBinding{"backspace/del", "Reset to default"},
				helpBinding{"ctrl+s", "Save settings"}, cancel, inputQuit)
		case m.settings.picking:
			return context(helpSettings, "Choose setting", helpBinding{"↑/↓/k/j/tab", "Choose value"},
				helpBinding{"enter/space", "Select value"}, cancel, inputQuit)
		case m.settings.db.editing:
			return context(helpSettings, "Database settings", fields, helpBinding{"enter", "Next numeric field / change"},
				helpBinding{"space/←/→", "Cycle toggle / choice"}, helpBinding{"backspace/del", "Clear value to inherit"},
				helpBinding{"ctrl+s", "Save settings"}, cancel, inputQuit)
		}
		return helpSettings, nil
	case viewPathInput:
		bindings := []helpBinding{{"enter", "Save database"}, cancel, inputQuit}
		if m.pathInput.canUpgrade {
			bindings = append(bindings, fields, helpBinding{"space/←/→", "Change format when focused"})
		}
		return context(helpSettings, "Save as", bindings...)
	case viewPassword:
		return context(helpSettings, "Master password", fields, helpBinding{"enter", "Confirm password"}, cancel, inputQuit)
	case viewGroupInput:
		return context(helpSettings, "Group name", helpBinding{"enter", "Confirm name"}, cancel, inputQuit)
	case viewConfirm:
		return context(helpBrowser, "Confirm", helpBinding{"y/n", "Yes / no"}, helpBinding{"esc", "No"},
			helpBinding{"tab/←/→", "Switch button"}, helpBinding{"enter", "Choose selected button"}, inputQuit)
	}
	return helpBrowser, nil
}
