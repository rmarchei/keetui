package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rmarchei/keetui/internal/config"
)

func TestF1ShowsActiveScreenControls(t *testing.T) {
	for _, tc := range []struct {
		title        string
		state        viewState
		prepare      func(*AppModel)
		want, absent string
	}{
		{"History", viewHistory, nil, "Copy selected password", "Copy selected field"},
		{"Database info", viewStats, nil, "Scroll a page", "New entry"},
		{"Audit", viewAudit, nil, "View selected entry", "New entry"},
		{"Save as", viewPathInput, nil, "Save database", "ctrl+s"},
		{"Master password", viewPassword, nil, "Confirm password", "ctrl+s"},
		{"Group name", viewGroupInput, nil, "Confirm name", "ctrl+s"},
		{"Password presets", viewPresetPicker, nil, "Set default preset", "ctrl+s"},
		{"Custom password", viewPresetPicker, func(m *AppModel) { m.presetPicker.mode = pickerModeCustom }, "Generate preview", "ctrl+s"},
		{"Password preview", viewPresetPicker, func(m *AppModel) { m.presetPicker.mode = pickerModePreview }, "Regenerate", "ctrl+s"},
		{"Attachments", viewDetail, func(m *AppModel) { m.detail.attachMode = true }, "Save to disk", "Copy selected field"},
		{"QR", viewDetail, func(m *AppModel) { m.detail.showQR = true }, "Close QR", "Copy selected field"},
		{"File path", viewDetail, func(m *AppModel) { m.detail.addingAttachment = true }, "Confirm path", "Copy selected field"},
		{"File path", viewDetail, func(m *AppModel) { m.detail.extractingAttachment = true }, "Confirm path", "Copy selected field"},
		{"Search", viewBrowser, func(m *AppModel) { m.browser.searching = true }, "Toggle literal/fuzzy", "New entry"},
		{"Choose setting", viewSettings, func(m *AppModel) { m.settings.picking = true; m.settings.focused = settingTheme }, "Select value", "ctrl+s"},
		{"Keybindings", viewSettings, func(m *AppModel) { m.settings.editingKeys = true }, "Record shortcut", "Cycle"},
		{"Database settings", viewSettings, func(m *AppModel) { m.settings.setDatabase(config.Overrides{}, false); m.settings.db.editing = true }, "Clear value to inherit", "Record shortcut"},
		{"Confirm", viewConfirm, nil, "Choose selected button", "New entry"},
	} {
		t.Run(tc.title, func(t *testing.T) {
			m := createTestApp(t)
			m.cfg = remappedConfig(t)
			m.state, m.statusText = tc.state, ""
			m.detail = NewDetailModel(testEntry(), m.cfg)
			m.detail.SetSize(80, 23)
			m.editor = NewEditorModel(nil, m.db.RootGroup().UUID, m.cfg)
			m.presetPicker = NewPresetPickerModel(m.cfg, "")
			m.settings = NewSettingsModel(m.cfg)
			m.pathInput = NewPathInputModel("synthetic.kdbx", "KDBX 3", true)
			m.password = NewPasswordModel()
			m.groupInput = NewGroupInputModel(m.db.RootGroup().UUID, "")
			m.confirm = NewConfirmModel(viewBrowser, "Synthetic confirmation", nil)
			if tc.prepare != nil {
				tc.prepare(&m)
			}
			updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyF1})
			next := updated.(AppModel)
			if next.state != viewHelp || next.help.sections()[next.help.section].title != tc.title {
				t.Fatal("F1 opened unrelated help")
			}
			content := ansi.Strip(next.help.buildContent())
			if !strings.Contains(content, tc.want) || strings.Contains(content, tc.absent) {
				t.Fatalf("incorrect context controls:\n%s", content)
			}
			if tc.state == viewHistory {
				for _, binding := range next.help.sections()[next.help.section].bindings {
					if binding.key == "enter" || binding.description == "Copy selected password" && binding.key != "h" {
						t.Fatal("history help must advertise remapped copy, not Enter")
					}
				}
			}
			if lipgloss.Height(next.help.buildContent()) > next.help.viewport.Height() {
				t.Fatal("context requires scrolling at 80x24")
			}
			assertFrame(t, next.help.View().Content, 80, 23)
			// Help closes via its message, preserving the originating screen.
			updated, closeCmd := next.Update(tea.KeyPressMsg{Code: tea.KeyF1})
			for _, msg := range quickMsgs(closeCmd) {
				if _, ok := msg.(CloseHelpMsg); ok {
					updated, _ = updated.Update(msg)
				}
			}
			if updated.(AppModel).state != tc.state {
				t.Fatal("closing help lost the origin")
			}
		})
	}
}
