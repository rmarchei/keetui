package ui

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/rmarchei/keetui/internal/keepass"
)

// softLockApp is the test app with an unsaved entry, idle past its
// auto-lock.
func softLockApp(t *testing.T) AppModel {
	t.Helper()
	m := createTestApp(t)
	if _, err := m.db.AddEntry(m.db.RootGroup().UUID, keepass.Entry{Title: "Unsaved", Password: "synthetic-unsaved"}); err != nil {
		t.Fatal(err)
	}
	m.dirty = true
	m.lastActivity = time.Now().Add(-time.Hour)
	return m
}

// runLockSave runs the commands an auto-lock returned in the background,
// and sends the outcome of its save on the channel. The test delivers it
// when it chooses: until then, the save runs as far as the app knows.
// Timers are left running.
func runLockSave(cmd tea.Cmd) <-chan lockSaveMsg {
	saved := make(chan lockSaveMsg, 1)
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		go func() {
			switch msg := c().(type) {
			case tea.BatchMsg:
				for _, sub := range msg {
					run(sub)
				}
			case lockSaveMsg:
				saved <- msg
			}
		}()
	}
	run(cmd)
	return saved
}

func awaitLockSave(t *testing.T, saved <-chan lockSaveMsg) lockSaveMsg {
	t.Helper()
	select {
	case msg := <-saved:
		return msg
	case <-time.After(time.Minute):
		t.Fatal("the auto-lock's save did not end")
	}
	return lockSaveMsg{}
}

// softLocked auto-locks m, whose save fails, and applies the outcome.
func softLocked(t *testing.T, m AppModel) AppModel {
	t.Helper()
	m.db.Path = filepath.Join(t.TempDir(), "missing", "test.kdbx")
	m, cmd := lockStep(t, m, autoLockMsg{gen: m.autoLockGen})
	m, _ = lockStep(t, m, awaitLockSave(t, runLockSave(cmd)))
	if m.held.db == nil || m.held.err == nil {
		t.Fatal("the failed save did not leave the database held")
	}
	return m
}

// typePassword types password on the unlock screen, presses enter and
// applies the check the unlock runs.
func typePassword(t *testing.T, m AppModel, password string) AppModel {
	t.Helper()
	m.unlock.input.SetValue("") // what an earlier attempt left
	for _, r := range password {
		m, _ = lockStep(t, m, textKey(string(r)))
	}
	m, cmd := lockStep(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter did not unlock")
	}
	m, cmd = lockStep(t, m, cmd())
	if cmd == nil {
		t.Fatal("the unlock started no check")
	}
	m, cmd = lockStep(t, m, cmd())
	return withStatus(t, m, cmd)
}

// withStatus applies the status among cmd's messages.
func withStatus(t *testing.T, m AppModel, cmd tea.Cmd) AppModel {
	t.Helper()
	for _, msg := range quickMsgs(cmd) {
		if status, ok := msg.(StatusMsg); ok {
			m, _ = lockStep(t, m, status)
		}
	}
	return m
}

func hasEntry(db *keepass.DB, title string) bool {
	for _, e := range db.RootGroup().Entries {
		if e.Title == title {
			return true
		}
	}
	return false
}

// A save that does not answer neither keeps the database on screen nor
// freezes it: the screen locks at once, and the password typed meanwhile,
// checked against the database held, opens it once the save ends, saved
// or not.
func TestAutoLockSaveThatHangsLocksAtOnce(t *testing.T) {
	for _, fails := range []bool{false, true} {
		m := softLockApp(t)
		if fails {
			m.db.Path = filepath.Join(t.TempDir(), "missing", "test.kdbx")
		}
		before, err := os.ReadFile(m.filePath)
		if err != nil {
			t.Fatal(err)
		}
		m, cmd := lockStep(t, m, autoLockMsg{gen: m.autoLockGen})
		if m.state != viewUnlock || m.db != nil || m.browser.db != nil || !m.held.saving {
			t.Fatalf("fails %v: state %d, db open %v, holding %v", fails, m.state, m.db != nil, m.held.saving)
		}
		if after, err := os.ReadFile(m.filePath); err != nil || !bytes.Equal(before, after) {
			t.Fatalf("fails %v: the save ran inside Update", fails)
		}
		if !strings.Contains(unlockText(m), "Saving your changes") {
			t.Errorf("fails %v: the unlock screen does not say the changes are being saved", fails)
		}

		saved := runLockSave(cmd)
		// Checked while the save runs (the race detector watches).
		m = typePassword(t, m, "wrong")
		if m.state != viewUnlock || m.unlock.err == nil || m.unlock.unlocking || m.held.unlock {
			t.Fatalf("fails %v: a wrong password: state %d, err %v", fails, m.state, m.unlock.err)
		}
		m = typePassword(t, m, "testmaster")
		if m.state != viewUnlock || m.db != nil || !m.unlock.unlocking || !m.held.unlock {
			t.Fatalf("fails %v: the right password during the save: state %d, db open %v", fails, m.state, m.db != nil)
		}
		if !strings.Contains(unlockText(m), "unlocks when the save ends") {
			t.Errorf("fails %v: the unlock screen does not say the unlock waits for the save", fails)
		}

		m, cmd = lockStep(t, m, awaitLockSave(t, saved))
		m = withStatus(t, m, cmd)
		if m.state != viewBrowser || m.db == nil || m.held.db != nil || m.unlock.unlocking || !hasEntry(m.db, "Unsaved") {
			t.Fatalf("fails %v: after the save: state %d, db open %v", fails, m.state, m.db != nil)
		}
		if m.dirty != fails || m.statusError != fails {
			t.Errorf("fails %v: dirty %v, status %q", fails, m.dirty, m.statusText)
		}
		if !fails && !strings.Contains(titlesOnDisk(t, m.filePath), ",Unsaved,") {
			t.Error("the saved file lacks the change")
		}
	}
}

// A save that succeeds, with no password typed meanwhile, ends the soft
// lock: the database is closed as by any lock.
func TestAutoLockSaveThatSucceedsLocksFully(t *testing.T) {
	m := softLockApp(t)
	m, cmd := lockStep(t, m, autoLockMsg{gen: m.autoLockGen})
	m, _ = lockStep(t, m, awaitLockSave(t, runLockSave(cmd)))
	if m.state != viewUnlock || m.db != nil || m.held.db != nil || m.dirty || m.unlock.held != "" {
		t.Fatalf("state %d, held %v, dirty %v, notice %q", m.state, m.held.db != nil, m.dirty, m.unlock.held)
	}
	if strings.Contains(unlockText(m), "Saving") {
		t.Error("the unlock screen still says the changes are being saved")
	}
	m = typePassword(t, m, "testmaster")
	if m.state != viewBrowser || m.dirty || !hasEntry(m.db, "Unsaved") {
		t.Fatalf("reopened: state %d, dirty %v", m.state, m.dirty)
	}
}

// While the database is held, messages that act on it or show it are
// dropped as when it is closed.
func TestSoftLockDropsDatabaseMessages(t *testing.T) {
	m := softLockApp(t)
	entry := m.db.RootGroup().Groups[0].Entries[0]
	m = softLocked(t, m)
	held := m.held.db
	target := filepath.Join(t.TempDir(), "must-not-be-written.kdbx")
	for _, msg := range []tea.Msg{
		ViewEntryMsg{Entry: entry},
		ShowHistoryMsg{EntryTitle: entry.Title, EntryUUID: entry.UUID},
		CopyToClipboardMsg{Field: "Password", Value: entry.Password},
		confirmDeleteEntryMsg{UUID: entry.UUID},
		confirmPermanentDeleteEntryMsg{UUID: entry.UUID},
		SaveAsMsg{Path: target}, confirmSaveAsMsg{Path: target},
		confirmLockMsg{}, softLockMsg{}, diskOverwriteMsg{},
		autoLockMsg{gen: m.autoLockGen},
	} {
		var cmd tea.Cmd
		m, cmd = lockStep(t, m, msg)
		if m.state != viewUnlock || m.db != nil || m.held.db != held || cmd != nil {
			t.Fatalf("%T acted on the database held: state %d, command %v", msg, m.state, cmd != nil)
		}
	}
	if _, err := held.GetEntry(entry.UUID); err != nil {
		t.Error("a dropped message deleted an entry of the database held")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("a dropped save wrote its destination: %v", err)
	}
}

// Quitting a soft lock loses its changes: esc and ctrl+c ask first, and No
// goes back to the unlock screen.
func TestSoftLockAsksBeforeQuitting(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{keyEsc, {Code: 'c', Mod: tea.ModCtrl}} {
		m := softLocked(t, softLockApp(t))
		m, cmd := lockStep(t, m, key)
		if m.state != viewConfirm || !m.confirm.destructive || !strings.Contains(m.confirm.message, "quitting loses them") {
			t.Fatalf("%s: state %d, question %q", key, m.state, m.confirm.message)
		}
		if cmd != nil {
			t.Fatalf("%s quit without asking", key)
		}
		m, _ = answer(t, m, keyN)
		if m.state != viewUnlock || m.held.db == nil {
			t.Fatalf("%s, No: state %d, held %v", key, m.state, m.held.db != nil)
		}
		m, _ = lockStep(t, m, key)
		if _, cmd = lockStep(t, m, keyY); cmd == nil {
			t.Fatalf("%s, Yes did nothing", key)
		}
		if _, quits := cmd().(quitMsg); !quits {
			t.Fatalf("%s, Yes did not quit", key)
		}
	}
}

// A database that opens while the quit question shows (the save ended
// after the right password) keeps the question, now over the database.
func TestSoftLockQuitQuestionOutlivesTheUnlock(t *testing.T) {
	m := softLockApp(t)
	m, cmd := lockStep(t, m, autoLockMsg{gen: m.autoLockGen})
	saved := runLockSave(cmd)
	m = typePassword(t, m, "testmaster")
	m, _ = lockStep(t, m, keyEsc)
	if m.state != viewConfirm || m.quitReturn == nil {
		t.Fatalf("state %d: want the quit question", m.state)
	}
	m, _ = lockStep(t, m, awaitLockSave(t, saved))
	if m.db == nil || m.state != viewConfirm || m.quitReturn == nil || m.confirm.returnState != viewBrowser {
		t.Fatalf("db open %v, state %d: want the question over the database", m.db != nil, m.state)
	}
	m, _ = answer(t, m, keyN)
	if m.state != viewBrowser || m.quitReturn != nil || !m.lockable() {
		t.Fatalf("No: state %d, question pending %v", m.state, m.quitReturn != nil)
	}
}
