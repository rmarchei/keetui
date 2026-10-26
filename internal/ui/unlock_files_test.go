package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/keepass"
)

func TestFileProblemNamesWhatIsMissing(t *testing.T) {
	dir := t.TempDir()
	vault, key := filepath.Join(dir, "synthetic.kdbx"), filepath.Join(dir, "synthetic.key")
	for _, path := range []string{vault, key} {
		if err := os.WriteFile(path, []byte("synthetic"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	missing := filepath.Join(dir, "gone", "synthetic.kdbx")
	for _, tc := range []struct {
		name, db, key, want string
	}{
		{"both there", vault, key, ""},
		{"no key file", vault, "", ""},
		{"database missing", missing, key, "Database file not found"},
		{"database is a folder", dir, "", "Database file path is a folder"},
		{"key missing", vault, missing, "Key file not found"},
		{"key is a folder", vault, dir, "Key file path is a folder"},
		{"both missing", missing, missing, "Database file not found"},
	} {
		if got := fileProblem(tc.db, tc.key); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

// unlockFiles runs the file checks among cmd's messages through the app.
func unlockFiles(t *testing.T, m AppModel, cmd tea.Cmd) (AppModel, tea.Cmd) {
	t.Helper()
	var next tea.Cmd
	for _, msg := range quickMsgs(cmd) {
		if files, ok := msg.(unlockFilesMsg); ok {
			updated, c := m.Update(files)
			m, next = updated.(AppModel), c
		}
	}
	return m, next
}

func unlockText(m AppModel) string { return ansi.Strip(m.View().Content) }

// The unlock screen warns while the database file is missing, before any
// password is typed, and drops the warning once the file is back (a volume
// mounted again) without a key press.
func TestUnlockWarnsWhileTheDatabaseIsMissing(t *testing.T) {
	prev := fileCheckInterval
	fileCheckInterval = time.Millisecond
	t.Cleanup(func() { fileCheckInterval = prev })
	path := filepath.Join(t.TempDir(), "synthetic.kdbx")
	m := lockedApp(t)
	m.filePath, m.unlock.filePath = path, path

	m, next := unlockFiles(t, m, m.Init())
	if m.unlock.missing != "Database file not found" || !strings.Contains(unlockText(m), "Database file not found") {
		t.Fatalf("missing %q: the unlock screen does not warn about the missing file", m.unlock.missing)
	}
	if next == nil {
		t.Fatal("the check is not repeated while the unlock screen shows")
	}

	if _, err := keepass.Create(path, "synthetic"); err != nil {
		t.Fatal(err)
	}
	m, next = unlockFiles(t, m, next)
	if m.unlock.missing != "" || strings.Contains(unlockText(m), "not found") {
		t.Fatalf("missing %q: the warning stayed after the file came back", m.unlock.missing)
	}
	if next == nil {
		t.Fatal("the check stopped while the unlock screen still shows")
	}

	// A check started before the latest one is dropped, and ends.
	updated, cmd := m.Update(unlockFilesMsg{gen: m.fileCheckGen - 1, missing: "Database file not found"})
	if m = updated.(AppModel); m.unlock.missing != "" || cmd != nil {
		t.Fatal("a superseded check changed the screen or kept running")
	}
}

// Locking starts the check over: a vault on a volume unmounted while it was
// open is reported as soon as the unlock screen shows.
func TestLockChecksTheDatabaseFileAgain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic.kdbx")
	db, err := keepass.Create(path, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	m := NewApp(path, "", config.Default())
	m.clipboard = &testClipboard{}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updated.(AppModel)
	updated, _ = m.Update(databaseOpenResultMsg{db: db, gen: m.openGen})
	m = updated.(AppModel)
	if m.db == nil {
		t.Fatal("the database did not open")
	}
	stale := m.fileCheckGen
	if updated, cmd := m.Update(unlockFilesMsg{gen: stale}); cmd != nil || updated.(AppModel).unlock.missing != "" {
		t.Fatal("a file check ran on with the database open")
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	cmd := m.lockDatabase()
	if m.fileCheckGen == stale {
		t.Fatal("locking did not start a new file check")
	}
	m, _ = unlockFiles(t, m, cmd)
	if !strings.Contains(unlockText(m), "Database file not found") {
		t.Fatalf("after locking, the unlock screen does not warn: %q", m.unlock.missing)
	}
}

// Settings opened from the unlock screen pause the check; returning starts
// it over.
func TestUnlockSettingsRestartTheFileCheck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic.kdbx")
	m := lockedApp(t)
	m.filePath, m.unlock.filePath = path, path
	m = feed(t, m, ctrlO)
	if m.state != viewSettings {
		t.Fatalf("state %d, want the settings", m.state)
	}
	updated, cmd := m.Update(unlockFilesMsg{gen: m.fileCheckGen, missing: "Database file not found"})
	if m = updated.(AppModel); cmd != nil {
		t.Fatal("the check kept running under the settings")
	}
	gen := m.fileCheckGen
	updated, cmd = m.Update(BackToBrowserMsg{})
	m = updated.(AppModel)
	if m.state != viewUnlock || m.fileCheckGen == gen {
		t.Fatalf("state %d, gen %d: returning did not start the check over", m.state, m.fileCheckGen)
	}
	if m, _ = unlockFiles(t, m, cmd); m.unlock.missing != "Database file not found" {
		t.Fatalf("missing %q after returning from the settings", m.unlock.missing)
	}
}

// Enter on a missing file names it, not the open call's error chain.
func TestUnlockNamesTheMissingFile(t *testing.T) {
	dir := t.TempDir()
	vault, key := filepath.Join(dir, "synthetic.kdbx"), filepath.Join(dir, "synthetic.key")
	if _, err := keepass.Create(vault, "synthetic"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, db, key, want string
	}{
		{"database", filepath.Join(dir, "gone.kdbx"), "", "database file not found: " + filepath.Join(dir, "gone.kdbx")},
		{"key file", vault, key, "key file not found: " + key},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewApp(tc.db, tc.key, config.Default())
			m.clipboard = &testClipboard{}
			updated, cmd := m.Update(DatabaseOpenedMsg{FilePath: tc.db, Password: "synthetic", KeyFilePath: tc.key})
			m = updated.(AppModel)
			updated, _ = m.Update(cmd())
			m = updated.(AppModel)
			if m.unlock.err == nil || m.unlock.err.Error() != tc.want {
				t.Fatalf("error %v, want %q", m.unlock.err, tc.want)
			}
		})
	}
}
