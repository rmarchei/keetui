package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/rmarchei/keetui/internal/keepass"
)

func TestHistoryNavigation(t *testing.T) {
	history := []keepass.HistoryEntry{
		{Password: "pass1", ModificationTime: time.Now()},
		{Password: "pass2", ModificationTime: time.Now()},
		{Password: "pass3", ModificationTime: time.Now()},
	}
	m := NewHistoryModel("Test", history)
	m.width, m.height = 80, 24

	if m.cursor != 0 {
		t.Errorf("initial cursor = %d, want 0", m.cursor)
	}

	// Move down
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j', Text: string('j')})
	m = updated.(HistoryModel)
	if m.cursor != 1 {
		t.Errorf("cursor after j = %d, want 1", m.cursor)
	}

	// Move up
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'k', Text: string('k')})
	m = updated.(HistoryModel)
	if m.cursor != 0 {
		t.Errorf("cursor after k = %d, want 0", m.cursor)
	}
}

func TestHistoryTogglePassword(t *testing.T) {
	history := []keepass.HistoryEntry{
		{Password: "secret", ModificationTime: time.Now()},
	}
	m := NewHistoryModel("Test", history)
	m.width, m.height = 80, 24

	if m.showPw {
		t.Error("showPw should be false initially")
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'p', Text: string('p')})
	m = updated.(HistoryModel)
	if !m.showPw {
		t.Error("showPw should be true after p")
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'p', Text: string('p')})
	m = updated.(HistoryModel)
	if m.showPw {
		t.Error("showPw should be false after second p")
	}
}

func TestHistoryBack(t *testing.T) {
	m := NewHistoryModel("Test", nil)
	m.width, m.height = 80, 24

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(BackToDetailMsg); !ok {
		t.Errorf("expected BackToDetailMsg, got %T", msg)
	}
}

func TestHistoryEmpty(t *testing.T) {
	m := NewHistoryModel("Test", nil)
	m.width, m.height = 80, 24

	view := m.View().Content
	if !strings.Contains(view, "(no history)") {
		t.Error("View() should contain '(no history)' for empty history")
	}
}

func TestHistoryView(t *testing.T) {
	history := []keepass.HistoryEntry{
		{Password: "oldpass", ModificationTime: time.Now()},
	}
	m := NewHistoryModel("Test Entry", history)
	m.width, m.height = 80, 24

	view := m.View().Content
	if view == "" {
		t.Error("View() should return non-empty string")
	}
	if !strings.Contains(view, "Test Entry") {
		t.Error("View() should contain entry title")
	}
}

// Custom field names come from the database, and the preview labels its
// rows with them: control and bidi characters, which XML 1.0 allows in the
// C1 range, must not reach the terminal.
func TestHistoryPreviewSanitizesFieldNames(t *testing.T) {
	version := keepass.Entry{Title: "t", CustomFields: []keepass.CustomField{
		{Key: "a\u009d0;x\u009cb", Value: "v"},
		{Key: "c\u009b31md", Value: "v"},
		{Key: "e\u202ef", Value: "v"},
	}}
	m := NewHistoryModel("t", []keepass.HistoryEntry{{Entry: version}})
	m.current = keepass.Entry{Title: "t", CustomFields: []keepass.CustomField{{Key: "g\x1b]0;x\x07h", Value: "v"}}}
	m.width, m.height, m.previewing = 100, 40, true
	view := m.View().Content
	for _, raw := range []string{"\u009b", "\u009c", "\u009d", "\u202e", "\x07", "\x1b]0;x"} {
		if strings.Contains(view, raw) {
			t.Errorf("preview renders %q raw", raw)
		}
	}
	for _, want := range []string{"a\ufffd0;x\ufffdb", "c\ufffd31md", "e\ufffdf", "g\ufffd]0;x\ufffdh"} {
		if !strings.Contains(view, want) {
			t.Errorf("preview lacks %q:\n%s", want, view)
		}
	}
}
