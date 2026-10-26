package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/keepass"
)

var (
	keyY   = tea.KeyPressMsg{Code: 'y', Text: "y"}
	keyN   = tea.KeyPressMsg{Code: 'n', Text: "n"}
	keyEsc = tea.KeyPressMsg{Code: tea.KeyEscape}
)

// otherProgramAdds saves an entry into the vault file, as another program
// or a sync would, behind keetui's back.
func otherProgramAdds(t *testing.T, path, title string) {
	t.Helper()
	db, err := keepass.Open(path, "testmaster")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddEntry(db.RootGroup().UUID, keepass.Entry{Title: title}); err != nil {
		t.Fatal(err)
	}
	if err := db.Save(); err != nil {
		t.Fatal(err)
	}
}

// titlesOnDisk lists the entry titles in the vault file.
func titlesOnDisk(t *testing.T, path string) string {
	t.Helper()
	db, err := keepass.Open(path, "testmaster")
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	var walk func(g keepass.Group)
	walk = func(g keepass.Group) {
		for _, e := range g.Entries {
			titles = append(titles, e.Title)
		}
		for _, sg := range g.Groups {
			walk(sg)
		}
	}
	walk(db.RootGroup())
	return "," + strings.Join(titles, ",") + ","
}

// changedApp is the test app with an unsaved entry of its own, whose file
// another program has changed since.
func changedApp(t *testing.T) AppModel {
	t.Helper()
	m := createTestApp(t)
	if _, err := m.db.AddEntry(m.db.RootGroup().UUID, keepass.Entry{Title: "Ours"}); err != nil {
		t.Fatal(err)
	}
	m.dirty = true
	otherProgramAdds(t, m.filePath, "Theirs")
	return m
}

// answer sends a key to the question shown, then the message its command
// produces.
func answer(t *testing.T, m AppModel, key tea.KeyPressMsg) (AppModel, tea.Cmd) {
	t.Helper()
	m, cmd := lockStep(t, m, key)
	if cmd == nil {
		t.Fatalf("%s did nothing", key)
	}
	return lockStep(t, m, cmd())
}

// finishSave runs the save in flight and applies its status and refresh.
func finishSave(t *testing.T, m AppModel, save tea.Cmd) AppModel {
	t.Helper()
	if !m.saving || save == nil {
		t.Fatal("no save started")
	}
	m, cmd := lockStep(t, m, save())
	for _, msg := range quickMsgs(cmd) {
		switch msg.(type) {
		case StatusMsg, RefreshMsg:
			m, _ = lockStep(t, m, msg)
		}
	}
	return m
}

// saveNow saves to the database's own file and applies the result.
func saveNow(t *testing.T, m AppModel) AppModel {
	t.Helper()
	m, save := lockStep(t, m, SaveAsMsg{Path: m.filePath})
	return finishSave(t, m, save)
}

func TestSaveOverAChangedFileMergesOnYes(t *testing.T) {
	m := saveNow(t, changedApp(t))
	if m.state != viewConfirm || !strings.Contains(m.confirm.message, "changed by another program") {
		t.Fatalf("state %d, question %q: want the merge question", m.state, m.confirm.message)
	}
	if on := titlesOnDisk(t, m.filePath); strings.Contains(on, ",Ours,") || !strings.Contains(on, ",Theirs,") {
		t.Fatalf("the save went ahead before the answer: %s", on)
	}

	m, save := answer(t, m, keyY)
	m = finishSave(t, m, save)
	if m.state != viewBrowser || m.dirty || !strings.HasPrefix(m.statusText, "Merged the changes on disk (1 added) and saved") {
		t.Fatalf("state %d, dirty %v, status %q", m.state, m.dirty, m.statusText)
	}
	if on := titlesOnDisk(t, m.filePath); !strings.Contains(on, ",Ours,") || !strings.Contains(on, ",Theirs,") || !strings.Contains(on, ",Gmail,") {
		t.Fatalf("the merged file lacks a side's entries: %s", on)
	}
	if !strings.Contains(m.browser.View().Content, "Theirs") {
		t.Error("the browser does not show the merged entry")
	}
}

func TestSaveOverAChangedFileOverwritesOnlyWhenAsked(t *testing.T) {
	m := saveNow(t, changedApp(t))
	m, _ = answer(t, m, keyN)
	if m.state != viewConfirm || !m.confirm.destructive || !strings.Contains(m.confirm.message, "Overwrite") {
		t.Fatalf("No: state %d, question %q: want the overwrite question", m.state, m.confirm.message)
	}
	m, save := answer(t, m, keyY)
	m = finishSave(t, m, save)
	if m.dirty || m.statusError {
		t.Fatalf("dirty %v, status %q", m.dirty, m.statusText)
	}
	if on := titlesOnDisk(t, m.filePath); !strings.Contains(on, ",Ours,") || strings.Contains(on, ",Theirs,") {
		t.Fatalf("overwritten file: %s", on)
	}
}

// Esc cancels at either question, and No to overwriting does too: nothing
// is written and the changes stay unsaved.
func TestSaveOverAChangedFileCancels(t *testing.T) {
	for _, keys := range [][]tea.KeyPressMsg{{keyEsc}, {keyN, keyEsc}, {keyN, keyN}} {
		m := saveNow(t, changedApp(t))
		for _, key := range keys {
			m, _ = answer(t, m, key)
		}
		if m.state != viewBrowser || !m.dirty || m.saving {
			t.Fatalf("keys %v: state %d, dirty %v, saving %v", keys, m.state, m.dirty, m.saving)
		}
		if on := titlesOnDisk(t, m.filePath); strings.Contains(on, ",Ours,") || !strings.Contains(on, ",Theirs,") {
			t.Fatalf("keys %v: cancelled save wrote the file: %s", keys, on)
		}
	}
}

func TestSaveRecreatesAMissingFileWhenAsked(t *testing.T) {
	m := createTestApp(t)
	m.dirty = true
	if err := os.Remove(m.filePath); err != nil {
		t.Fatal(err)
	}
	m = saveNow(t, m)
	if m.state != viewConfirm || !strings.Contains(m.confirm.message, "no longer on disk") {
		t.Fatalf("state %d, question %q", m.state, m.confirm.message)
	}
	m, save := answer(t, m, keyY)
	m = finishSave(t, m, save)
	if m.dirty || !strings.Contains(titlesOnDisk(t, m.filePath), ",Gmail,") {
		t.Fatal("the missing file was not written again")
	}
}

func TestSaveStopsWhenTheFolderIsGone(t *testing.T) {
	m := createTestApp(t)
	m.dirty = true
	dir := filepath.Join(t.TempDir(), "volume")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	m = saveNow(t, m)
	moved := filepath.Join(dir, "test.kdbx")
	m, save := lockStep(t, m, SaveAsMsg{Path: moved})
	m = finishSave(t, m, save)
	if m.filePath != moved || m.db.Path != moved {
		t.Fatalf("save as did not move the database: %q", m.filePath)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	m.dirty = true
	m = saveNow(t, m)
	if m.state != viewBrowser || !m.statusError || !strings.Contains(m.statusText, "Not saved: the folder of") {
		t.Fatalf("state %d, status %q", m.state, m.statusText)
	}
}

// Ctrl+L with unsaved changes over a changed file merges when asked, then
// locks.
func TestLockMergesTheChangedFileWhenAsked(t *testing.T) {
	m := changedApp(t)
	m, _ = lockStep(t, m, ctrlL)
	m, save := answer(t, m, keyY) // save and lock?
	m = finishSave(t, m, save)
	if m.state != viewConfirm || !strings.Contains(m.confirm.message, "changed by another program") {
		t.Fatalf("state %d, question %q: want the merge question", m.state, m.confirm.message)
	}
	m, save = answer(t, m, keyY)
	m = finishSave(t, m, save)
	if m.state != viewUnlock || m.db != nil || m.unlock.notice != "Merged the changes on disk, saved and locked." {
		t.Fatalf("state %d, db open %v, notice %q", m.state, m.db != nil, m.unlock.notice)
	}
	if on := titlesOnDisk(t, m.filePath); !strings.Contains(on, ",Ours,") || !strings.Contains(on, ",Theirs,") {
		t.Fatalf("locked without both sides saved: %s", on)
	}
}

// Auto-lock has nobody to ask: it merges the changes on disk in, saves and
// locks.
func TestAutoLockMergesAChangedFile(t *testing.T) {
	m := changedApp(t)
	m.lastActivity = time.Now().Add(-time.Hour)
	m, _ = lockStep(t, m, autoLockMsg{gen: m.autoLockGen})
	if m.state != viewUnlock || m.db != nil || m.unlock.err == nil || !strings.Contains(m.unlock.err.Error(), "merged in") {
		t.Fatalf("state %d, db open %v, err %v", m.state, m.db != nil, m.unlock.err)
	}
	if on := titlesOnDisk(t, m.filePath); !strings.Contains(on, ",Ours,") || !strings.Contains(on, ",Theirs,") {
		t.Fatalf("auto-lock saved without both sides: %s", on)
	}
}

// Settings keetui saved in the file elsewhere take effect once merged in.
func TestMergedDatabaseSettingsTakeEffect(t *testing.T) {
	m := createTestApp(t)
	m.dirty = true
	other, err := keepass.Open(m.filePath, "testmaster")
	if err != nil {
		t.Fatal(err)
	}
	length := 42
	other.SetCustomData(config.OverridesKey, config.Overrides{PasswordLength: &length}.Encode())
	if err := other.Save(); err != nil {
		t.Fatal(err)
	}
	m = saveNow(t, m)
	m, save := answer(t, m, keyY)
	m = finishSave(t, m, save)
	if m.cfg.PasswordLength != 42 || m.dbOverrides.PasswordLength == nil {
		t.Fatalf("password length %d, status %q: want the merged database setting", m.cfg.PasswordLength, m.statusText)
	}
	if !strings.Contains(m.statusText, "1 database setting") {
		t.Errorf("status %q does not name the merged setting", m.statusText)
	}
}
