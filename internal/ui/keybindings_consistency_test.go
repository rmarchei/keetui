package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/keepass"
)

func remappedConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.KeyBindingsPtr = &config.KeyBindings{NavDown: "J", NavUp: "K", Back: "B", Open: "L", Quit: "X", CopyPassword: "h", Save: "w", Delete: "D"}
	if err := cfg.Keys().Validate(); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func requireQuitCommand(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	for _, msg := range quickMsgs(cmd) {
		if _, ok := msg.(RequestQuitMsg); ok {
			return
		}
	}
	t.Fatal("key did not request quit")
}

func TestRemappedQuitAcrossBrowsingScreens(t *testing.T) {
	cfg := remappedConfig(t)
	kb := cfg.Keys()
	attachment := NewDetailModel(testEntry(), cfg)
	attachment.attachMode = true
	qr := NewDetailModel(testEntry(), cfg)
	qr.showQR = true
	for name, model := range map[string]tea.Model{
		"browser": NewBrowserModel(createTestUIDB(t), kb),
		"detail":  NewDetailModel(testEntry(), cfg),
		"history": NewHistoryModel("Synthetic", nil, kb),
		"stats":   NewStatsModel(keepass.DBStats{}, kb),
		"audit":   NewAuditModel(keepass.AuditResult{}, kb),
		"help":    NewHelpModel(kb, 80, 24), "attachments": attachment, "QR": qr,
	} {
		t.Run(name, func(t *testing.T) {
			for _, key := range []tea.KeyPressMsg{textKey("X"), {Code: 'c', Mod: tea.ModCtrl}} {
				_, cmd := model.Update(key)
				requireQuitCommand(t, cmd)
			}
			_, cmd := model.Update(textKey("q"))
			if cmd != nil {
				t.Fatal("old quit binding is still active")
			}
		})
	}
}

func TestAttachmentPathQuitPreservesInput(t *testing.T) {
	for _, adding := range []bool{false, true} {
		m := NewDetailModel(testEntry(), remappedConfig(t))
		m.attachMode, m.addingAttachment, m.extractingAttachment = true, adding, !adding
		m.attachPathInput.Focus()
		m.attachPathInput.SetValue("synthetic-")
		updated, _ := m.Update(textKey("X"))
		m = updated.(DetailModel)
		if m.attachPathInput.Value() != "synthetic-X" {
			t.Fatal("printable Quit shortcut was stolen from the path input")
		}
		updated, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
		requireQuitCommand(t, cmd)
		next := updated.(DetailModel)
		if next.addingAttachment != adding || next.extractingAttachment == adding || next.attachPathInput.Value() != "synthetic-X" {
			t.Fatal("requesting quit discarded the pending attachment path")
		}
	}
}

func TestRemappedNavigationAndHistoryCopy(t *testing.T) {
	cfg := remappedConfig(t)
	kb := cfg.Keys()
	stats := NewStatsModel(keepass.DBStats{}, kb)
	stats.width, stats.height = 80, 8
	attachment := NewDetailModel(testEntry(), cfg)
	attachment.attachMode = true
	attachment.SetAttachments([]keepass.Attachment{{Name: "one"}, {Name: "two"}})
	position := func(m tea.Model) int {
		switch m := m.(type) {
		case BrowserModel:
			return m.cursor
		case DetailModel:
			if m.attachMode {
				return m.attachIdx
			}
			return m.selectedField
		case HistoryModel:
			return m.cursor
		case StatsModel:
			return m.offset
		case AuditModel:
			return m.cursor
		case HelpModel:
			return m.viewport.YOffset()
		}
		panic("unexpected screen")
	}
	for name, model := range map[string]tea.Model{
		"browser": NewBrowserModel(createTestUIDB(t), kb),
		"detail":  NewDetailModel(testEntry(), cfg), "attachments": attachment,
		"history": NewHistoryModel("Synthetic", []keepass.HistoryEntry{{Password: "one"}, {Password: "two"}}, kb),
		"stats":   stats, "help": NewHelpModel(kb, 80, 8),
		"audit": NewAuditModel(keepass.AuditResult{WeakEntries: []keepass.AuditEntry{{Entry: testEntry()}, {Entry: testEntry()}}}, kb),
	} {
		t.Run(name, func(t *testing.T) {
			initial := position(model)
			next, _ := model.Update(textKey("J"))
			if position(next) != initial+1 {
				t.Fatal("custom Down did not move one item")
			}
			next, _ = next.Update(textKey("K"))
			if position(next) != initial {
				t.Fatal("custom Up did not move back")
			}
			next, _ = next.Update(tea.KeyPressMsg{Code: tea.KeyDown})
			if position(next) != initial+1 {
				t.Fatal("arrow navigation stopped working")
			}
		})
	}
	history := NewHistoryModel("Synthetic", []keepass.HistoryEntry{{Password: "synthetic"}}, kb)
	_, cmd := history.Update(textKey("h"))
	if cmd == nil {
		t.Fatal("remapped copy was ignored")
	}
	if msg, ok := cmd().(CopyToClipboardMsg); !ok || msg.Value != "synthetic" {
		t.Fatal("old Back binding shadowed CopyPassword")
	}
	_, cmd = history.Update(textKey("B"))
	if cmd == nil {
		t.Fatal("remapped Back was ignored")
	}
	if _, ok := cmd().(BackToDetailMsg); !ok {
		t.Fatal("remapped Back did not return to detail")
	}
	updated, _ := attachment.Update(textKey("B"))
	if updated.(DetailModel).attachMode {
		t.Fatal("remapped Back did not leave attachments")
	}
	updated, _ = attachment.Update(textKey("w"))
	if !updated.(DetailModel).extractingAttachment {
		t.Fatal("remapped Save did not open the destination input")
	}
	_, cmd = attachment.Update(textKey("D"))
	if cmd == nil {
		t.Fatal("remapped Delete was ignored")
	}
	if _, ok := cmd().(DeleteAttachmentMsg); !ok {
		t.Fatal("remapped Delete did not request deletion")
	}
}

func TestHealthScanKeepsRemappedKeysWhileLoadingAndAfterResults(t *testing.T) {
	for _, view := range []viewState{viewStats, viewAudit} {
		m := createTestApp(t)
		m.cfg = remappedConfig(t)
		updated, scan := m.startHealthScan(view)
		m = updated.(AppModel)
		_, cmd := m.Update(textKey("X"))
		requireQuitCommand(t, cmd)
		updated, _ = m.Update(scan())
		m = updated.(AppModel)
		_, cmd = m.Update(textKey("X"))
		requireQuitCommand(t, cmd)
		_, cmd = m.Update(textKey("B"))
		found := false
		for _, msg := range quickMsgs(cmd) {
			if _, ok := msg.(BackToBrowserMsg); ok {
				found = true
			}
		}
		if !found {
			t.Fatal("remapped Back did not return from health results")
		}
	}
}
