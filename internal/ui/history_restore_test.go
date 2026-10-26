package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rmarchei/keetui/internal/keepass"
)

// historyApp opens the history of an entry edited three times: versions,
// newest first, "second", "first" (with a protected PIN) and the original;
// current "third".
func historyApp(t *testing.T) (AppModel, keepass.Entry) {
	t.Helper()
	m := createTestApp(t)
	entry := m.db.RootGroup().Entries[0]
	for _, e := range []keepass.Entry{
		{Title: entry.Title, Password: "first", CustomFields: []keepass.CustomField{{Key: "PIN", Value: "1111", Protected: true}}},
		{Title: entry.Title, Password: "second"},
		{Title: entry.Title, Password: "third"},
	} {
		if err := m.db.UpdateEntry(entry.UUID, e); err != nil {
			t.Fatal(err)
		}
	}
	m, _ = lockStep(t, m, ShowHistoryMsg{EntryTitle: entry.Title, EntryUUID: entry.UUID})
	if m.state != viewHistory || len(m.history.history) != 3 {
		t.Fatalf("state %d, %d versions", m.state, len(m.history.history))
	}
	return m, entry
}

// Enter shows the selected version with what differs from the current
// entry, its protected values masked until p.
func TestHistoryShowsAVersion(t *testing.T) {
	m, _ := historyApp(t)
	for _, key := range []tea.KeyPressMsg{{Code: 'j', Text: "j"}, {Code: tea.KeyEnter}} {
		m, _ = lockStep(t, m, key)
	}
	if !m.history.previewing || m.history.history[m.history.cursor].Password != "first" {
		t.Fatalf("previewing %v, version %q", m.history.previewing, m.history.history[m.history.cursor].Password)
	}
	view := ansi.Strip(m.history.View().Content)
	if !strings.Contains(view, "PIN") || strings.Contains(view, "1111") || strings.Contains(view, "first") {
		t.Errorf("masked preview:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "Password") && !strings.Contains(line, "changed") {
			t.Errorf("the password differs from the current one: %q", line)
		}
		if strings.Contains(line, "Title") && strings.Contains(line, "changed") {
			t.Errorf("the title is the same: %q", line)
		}
	}
	m, _ = lockStep(t, m, tea.KeyPressMsg{Code: 'p', Text: "p"})
	if view := ansi.Strip(m.history.View().Content); !strings.Contains(view, "1111") || !strings.Contains(view, "first") {
		t.Errorf("p should show protected values:\n%s", view)
	}
}

// R asks before restoring; the restored entry shows, the database counts
// as modified, and Ctrl+Z puts the entry back as it was.
func TestHistoryRestoreAndUndo(t *testing.T) {
	m, entry := historyApp(t)
	m, cmd := lockStep(t, m, tea.KeyPressMsg{Code: 'R', Text: "R"}) // "second", the newest version
	m, _ = lockStep(t, m, cmd())
	if m.state != viewConfirm || !strings.Contains(m.confirm.message, "Restore the version of") {
		t.Fatalf("state %d, question %q", m.state, m.confirm.message)
	}
	m, cmd = lockStep(t, m, tea.KeyPressMsg{Code: 'y', Text: "y"})
	m, _ = lockStep(t, m, cmd())
	restored, err := m.db.GetEntry(entry.UUID)
	if err != nil || restored.Password != "second" || m.state != viewDetail || !m.dirty || m.detail.entry.Password != "second" {
		t.Fatalf("restored %q (%v), state %d, dirty %v, detail %q", restored.Password, err, m.state, m.dirty, m.detail.entry.Password)
	}
	if h := m.db.EntryHistory(entry.UUID); len(h) != 4 || h[0].Password != "third" {
		t.Errorf("history = %d versions, newest %q: want the replaced state kept", len(h), h[0].Password)
	}
	m, _ = lockStep(t, m, tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
	if back, _ := m.db.GetEntry(entry.UUID); back.Password != "third" || len(m.db.EntryHistory(entry.UUID)) != 3 {
		t.Errorf("after undo: %q with %d versions", back.Password, len(m.db.EntryHistory(entry.UUID)))
	}
}

// A read-only database restores nothing.
func TestHistoryRestoreReadOnly(t *testing.T) {
	m, entry := historyApp(t)
	m.readOnly = true
	m, cmd := lockStep(t, m, tea.KeyPressMsg{Code: 'R', Text: "R"})
	m, status := lockStep(t, m, cmd())
	if m.state != viewHistory || quickStatus(status) != "Read-only mode" {
		t.Fatalf("state %d, status %q", m.state, quickStatus(status))
	}
	if current, _ := m.db.GetEntry(entry.UUID); current.Password != "third" {
		t.Errorf("password = %q", current.Password)
	}
}
