package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rmarchei/keetui/internal/config"
)

func TestHelpSectionsFitStandardTerminal(t *testing.T) {
	m := NewHelpModel(config.DefaultKeyBindings(), 80, 23)
	for section := range helpMessage {
		m.section = section
		m.resize()
		if lipgloss.Height(m.buildContent()) > m.viewport.Height() {
			t.Fatalf("section %s needs scrolling at 80x24: %d content rows, %d available", m.sections()[section].title, lipgloss.Height(m.buildContent()), m.viewport.Height())
		}
		assertFrame(t, m.View().Content, 80, 23)
	}
	m.section = helpBrowser
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if updated.(HelpModel).section != helpDetail {
		t.Fatal("tab did not switch help context")
	}
}

func TestHelpOpensCurrentContext(t *testing.T) {
	m := createTestApp(t)
	updated, _ := m.Update(EditEntryMsg{Entry: testEntry()})
	m = updated.(AppModel)
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyF1})
	m = updated.(AppModel)
	if m.help.section != helpEditor || !strings.Contains(ansi.Strip(m.help.View().Content), "ctrl+s") {
		t.Fatal("editor help did not open directly")
	}
}

func TestCopyAndBreachActionsAreDistinct(t *testing.T) {
	m := NewDetailModel(testEntry(), config.Default())
	m.SetSize(80, 23)
	m.selectedField = 1
	if !strings.Contains(ansi.Strip(m.footerView()), "enter: copy password") {
		t.Fatal("copy target not discoverable")
	}
	for key, want := range map[string]string{"1": "username", "2": "password", "3": "url"} {
		_, cmd := m.Update(textKey(key))
		if cmd == nil {
			t.Fatalf("%s no longer copies", key)
		}
		msg, ok := cmd().(CopyToClipboardMsg)
		if !ok || msg.Field != want {
			t.Fatalf("%s not a copy action", key)
		}
	}
	updated, cmd := m.Update(textKey("5"))
	if cmd != nil || updated.(DetailModel).hibpChecking {
		t.Fatal("5 should not trigger a network action by default")
	}
	cfg := config.Default()
	cfg.KeyBindingsPtr = &config.KeyBindings{HIBPCheck: "5"}
	m = NewDetailModel(testEntry(), cfg)
	updated, cmd = m.Update(textKey("5"))
	if cmd == nil || !updated.(DetailModel).hibpChecking {
		t.Fatal("explicit legacy shortcut was lost")
	}
}

func TestBrowserUsesSharedUnicodeCursor(t *testing.T) {
	m := NewBrowserModel(createTestUIDB(t))
	m.width, m.height = 80, 23
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, selectionCursor) || !strings.Contains(view, "◇") || strings.Contains(view, "[+]") {
		t.Fatal("browser glyphs are inconsistent")
	}
}
