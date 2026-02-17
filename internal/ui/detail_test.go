package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/keepass"
)

func testEntry() keepass.Entry {
	return keepass.Entry{
		Title:    "Test Entry",
		UserName: "testuser",
		Password: "testpassword123",
		URL:      "https://example.com",
		Notes:    "Some notes",
	}
}

func TestDetailPasswordToggle(t *testing.T) {
	entry := testEntry()
	m := NewDetailModel(entry, config.Default())
	m.SetSize(80, 24)

	if m.revealedFields["password"] {
		t.Error("password should be hidden initially")
	}

	// Select password field (index 1)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(DetailModel)
	if m.selectedField != 1 {
		t.Fatalf("selectedField = %d, want 1 (password)", m.selectedField)
	}

	// Toggle password on selected field
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(DetailModel)
	if !m.revealedFields["password"] {
		t.Error("password should be visible after toggle")
	}

	// Toggle back
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(DetailModel)
	if m.revealedFields["password"] {
		t.Error("password should be hidden after second toggle")
	}

	// Toggle on non-protected field should do nothing
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}) // back to username
	m = updated.(DetailModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(DetailModel)
	if m.revealedFields["username"] {
		t.Error("username is not protected, toggle should have no effect")
	}
}

func TestDetailCopyCommands(t *testing.T) {
	entry := testEntry()
	m := NewDetailModel(entry, config.Default())
	m.SetSize(80, 24)

	tests := []struct {
		key   rune
		field string
		value string
	}{
		{'1', "username", "testuser"},
		{'2', "password", "testpassword123"},
		{'3', "url", "https://example.com"},
	}

	for _, tt := range tests {
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{tt.key}})
		m = updated.(DetailModel)
		if cmd == nil {
			t.Errorf("key %c should produce a command", tt.key)
			continue
		}
		msg := cmd()
		copyMsg, ok := msg.(CopyToClipboardMsg)
		if !ok {
			t.Errorf("key %c: expected CopyToClipboardMsg, got %T", tt.key, msg)
			continue
		}
		if copyMsg.Field != tt.field {
			t.Errorf("key %c: field = %q, want %q", tt.key, copyMsg.Field, tt.field)
		}
		if copyMsg.Value != tt.value {
			t.Errorf("key %c: value = %q, want %q", tt.key, copyMsg.Value, tt.value)
		}
	}
}

func TestDetailEditMsg(t *testing.T) {
	entry := testEntry()
	m := NewDetailModel(entry, config.Default())
	m.SetSize(80, 24)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if cmd == nil {
		t.Fatal("e key should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(EditEntryMsg); !ok {
		t.Errorf("expected EditEntryMsg, got %T", msg)
	}
}

func TestDetailQRToggle(t *testing.T) {
	entry := testEntry()
	m := NewDetailModel(entry, config.Default())
	m.SetSize(80, 24)

	// Show QR
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Q'}})
	m = updated.(DetailModel)
	if !m.showQR {
		t.Error("QR should be shown after Q")
	}

	// Close QR
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(DetailModel)
	if m.showQR {
		t.Error("QR should be hidden after esc")
	}
}

func TestDetailHIBPTrigger(t *testing.T) {
	entry := testEntry()
	m := NewDetailModel(entry, config.Default())
	m.SetSize(80, 24)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	m = updated.(DetailModel)
	if cmd == nil {
		t.Fatal("5 key should produce a command for HIBP check")
	}
	if !m.hibpChecking {
		t.Error("hibpChecking should be true after pressing 5")
	}
}

func TestDetailBackMsg(t *testing.T) {
	entry := testEntry()
	m := NewDetailModel(entry, config.Default())
	m.SetSize(80, 24)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(BackToBrowserMsg); !ok {
		t.Errorf("expected BackToBrowserMsg, got %T", msg)
	}
}

func TestDetailAttachmentMode(t *testing.T) {
	entry := testEntry()
	m := NewDetailModel(entry, config.Default())
	m.SetSize(80, 24)

	// Set attachments
	m.attachments = []keepass.Attachment{
		{Name: "file.txt", Size: 100},
		{Name: "doc.pdf", Size: 2048},
	}
	m.RefreshContent()

	// Enter attachment mode
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = updated.(DetailModel)
	if !m.attachMode {
		t.Error("should be in attachment mode after 'a'")
	}

	// Navigate
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(DetailModel)
	if m.attachIdx != 1 {
		t.Errorf("attachIdx = %d, want 1", m.attachIdx)
	}

	// Exit attachment mode
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(DetailModel)
	if m.attachMode {
		t.Error("should exit attachment mode on esc")
	}
}

func TestDetailFieldNavigation(t *testing.T) {
	entry := testEntry()
	m := NewDetailModel(entry, config.Default())
	m.SetSize(80, 24)

	// Starts on field 0 (username)
	if m.selectedField != 0 {
		t.Errorf("initial selectedField = %d, want 0", m.selectedField)
	}

	// Move down
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(DetailModel)
	if m.selectedField != 1 {
		t.Errorf("selectedField after j = %d, want 1", m.selectedField)
	}

	// Move up
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(DetailModel)
	if m.selectedField != 0 {
		t.Errorf("selectedField after k = %d, want 0", m.selectedField)
	}

	// Should not go below 0
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(DetailModel)
	if m.selectedField != 0 {
		t.Errorf("selectedField should not go below 0, got %d", m.selectedField)
	}
}

func TestDetailEnterCopiesSelected(t *testing.T) {
	entry := testEntry()
	m := NewDetailModel(entry, config.Default())
	m.SetSize(80, 24)

	// Field 0 = username
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should produce a copy command")
	}
	msg := cmd()
	copyMsg, ok := msg.(CopyToClipboardMsg)
	if !ok {
		t.Fatalf("expected CopyToClipboardMsg, got %T", msg)
	}
	if copyMsg.Field != "username" {
		t.Errorf("field = %q, want %q", copyMsg.Field, "username")
	}
	if copyMsg.Value != "testuser" {
		t.Errorf("value = %q, want %q", copyMsg.Value, "testuser")
	}
}

func TestDetailQRSelectedField(t *testing.T) {
	entry := testEntry()
	m := NewDetailModel(entry, config.Default())
	m.SetSize(80, 24)

	// Move to URL field (index 2)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(DetailModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(DetailModel)
	if m.selectedField != 2 {
		t.Fatalf("selectedField = %d, want 2 (URL)", m.selectedField)
	}

	// Show QR
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Q'}})
	m = updated.(DetailModel)
	if !m.showQR {
		t.Error("QR should be shown")
	}
	if m.qrLabel != "URL" {
		t.Errorf("qrLabel = %q, want %q", m.qrLabel, "URL")
	}
}

func TestDetailMouseSelectField(t *testing.T) {
	entry := testEntry()
	m := NewDetailModel(entry, config.Default())
	m.SetSize(80, 24)

	// Starts on field 0 (username)
	if m.selectedField != 0 {
		t.Fatalf("initial selectedField = %d, want 0", m.selectedField)
	}

	// headerView() = "\n" + title + "\n" has 2 newlines.
	// Viewport content starts at Y = strings.Count(header, "\n") = 2.
	vpStart := 2

	// Click on Username line (viewport line 0)
	updated, _ := m.Update(tea.MouseMsg{
		X:      10,
		Y:      vpStart,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = updated.(DetailModel)
	if m.selectedField != 0 {
		t.Errorf("selectedField after click on username line = %d, want 0", m.selectedField)
	}

	// Click on Password line (viewport line 1)
	updated, _ = m.Update(tea.MouseMsg{
		X:      10,
		Y:      vpStart + 1,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = updated.(DetailModel)
	if m.selectedField != 1 {
		t.Errorf("selectedField after click on password line = %d, want 1", m.selectedField)
	}

	// Click on URL line (viewport line 2)
	updated, _ = m.Update(tea.MouseMsg{
		X:      10,
		Y:      vpStart + 2,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = updated.(DetailModel)
	if m.selectedField != 2 {
		t.Errorf("selectedField after click on URL line = %d, want 2", m.selectedField)
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		maxWidth int
		want     string
	}{
		{"no truncation", "hello", 10, "hello"},
		{"exact fit", "hello", 5, "hello"},
		{"truncate", "hello world", 8, "hello w…"},
		{"very short", "hello", 2, "h…"},
		{"width 1", "hello", 1, "…"},
		{"width 0", "hello", 0, ""},
		{"empty string", "", 5, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateString(tt.s, tt.maxWidth)
			if got != tt.want {
				t.Errorf("truncateString(%q, %d) = %q, want %q", tt.s, tt.maxWidth, got, tt.want)
			}
		})
	}
}

func TestLineForField(t *testing.T) {
	entry := keepass.Entry{
		Title:    "Test",
		UserName: "user",
		Password: "pass",
		URL:      "https://example.com",
		Notes:    "line1\nline2",
		CustomFields: []keepass.CustomField{
			{Key: "field1", Value: "val1"},
			{Key: "field2", Value: "val2"},
		},
	}
	m := NewDetailModel(entry, config.Default())
	m.SetSize(80, 24)

	// username=0, password=1, URL=2, notes=3, field1=4, field2=5
	// Lines: username(0), password(1), URL(2), notes label(3), note body(4,5),
	//        timestamps may add lines, then custom fields

	if line := m.lineForField(0); line != 0 {
		t.Errorf("lineForField(0) = %d, want 0 (username)", line)
	}
	if line := m.lineForField(1); line != 1 {
		t.Errorf("lineForField(1) = %d, want 1 (password)", line)
	}
	if line := m.lineForField(2); line != 2 {
		t.Errorf("lineForField(2) = %d, want 2 (URL)", line)
	}
	if line := m.lineForField(3); line != 3 {
		t.Errorf("lineForField(3) = %d, want 3 (notes)", line)
	}
	// After notes: 1 label + 2 body lines = 3 lines (lines 3-5)
	// Custom fields should be after notes + any timestamps
	line4 := m.lineForField(4)
	line5 := m.lineForField(5)
	if line5 <= line4 {
		t.Errorf("lineForField(5)=%d should be > lineForField(4)=%d", line5, line4)
	}
}

func TestDetailView(t *testing.T) {
	entry := testEntry()
	m := NewDetailModel(entry, config.Default())
	m.SetSize(80, 24)

	view := m.View()
	if view == "" {
		t.Error("View() should return non-empty string")
	}
	if !strings.Contains(view, "Test Entry") {
		t.Error("View() should contain entry title")
	}
}

// bigNotesEntry returns an entry with more than maxNoteLines lines of notes.
func bigNotesEntry() keepass.Entry {
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = strings.Repeat("x", 10)
	}
	return keepass.Entry{
		Title:    "Big Notes",
		UserName: "user",
		Password: "pass",
		URL:      "https://example.com",
		Notes:    strings.Join(lines, "\n"),
		TOTP:     "JBSWY3DPEHPK3PXP",
		CustomFields: []keepass.CustomField{
			{Key: "cf1", Value: "val1"},
		},
	}
}

func TestNotesBodyLinesTruncated(t *testing.T) {
	m := NewDetailModel(bigNotesEntry(), config.Default())
	m.SetSize(80, 24)

	// Collapsed: maxNoteLines body + 1 hint line
	want := maxNoteLines + 1
	if got := m.notesBodyLines(); got != want {
		t.Errorf("notesBodyLines() collapsed = %d, want %d", got, want)
	}

	// Expanded: all 20 lines
	m.notesExpanded = true
	if got := m.notesBodyLines(); got != 20 {
		t.Errorf("notesBodyLines() expanded = %d, want 20", got)
	}
}

func TestNotesTruncationInContent(t *testing.T) {
	m := NewDetailModel(bigNotesEntry(), config.Default())
	m.SetSize(80, 24)

	content := m.buildContent()

	if !strings.Contains(content, "15 more lines") {
		t.Error("truncated notes should show '15 more lines' hint")
	}
	if !strings.Contains(content, "p to expand") {
		t.Error("truncated notes should show 'p to expand' hint")
	}

	// Expand
	m.notesExpanded = true
	content = m.buildContent()
	if strings.Contains(content, "more lines") {
		t.Error("expanded notes should not show truncation hint")
	}
}

func TestNotesExpandToggle(t *testing.T) {
	m := NewDetailModel(bigNotesEntry(), config.Default())
	m.SetSize(80, 24)

	// Navigate to Notes field: username(0), password(1), URL(2), TOTP(3), Notes(4)
	fields := m.selectableFields()
	notesIdx := -1
	for i, f := range fields {
		if f.label == "Notes" {
			notesIdx = i
			break
		}
	}
	if notesIdx == -1 {
		t.Fatal("Notes field not found in selectableFields")
	}
	m.selectedField = notesIdx

	if m.notesExpanded {
		t.Error("notes should start collapsed")
	}

	// Press p to expand
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(DetailModel)
	if !m.notesExpanded {
		t.Error("notes should be expanded after p")
	}

	// Press p to collapse
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(DetailModel)
	if m.notesExpanded {
		t.Error("notes should be collapsed after second p")
	}
}

func TestTOTPVisibleWithBigNotes(t *testing.T) {
	m := NewDetailModel(bigNotesEntry(), config.Default())
	m.SetSize(80, 24)

	// TOTP field should appear in selectableFields
	fields := m.selectableFields()
	found := false
	for _, f := range fields {
		if f.label == "TOTP" {
			found = true
			break
		}
	}
	if !found {
		t.Error("TOTP should be in selectableFields when entry has TOTP secret")
	}

	// TOTP should appear in rendered content
	content := m.buildContent()
	if !strings.Contains(content, "TOTP:") {
		t.Error("buildContent should include TOTP field")
	}
}

func TestTOTPVisibleWhenCodeGenerationFails(t *testing.T) {
	entry := keepass.Entry{
		Title:    "Bad TOTP",
		UserName: "user",
		Password: "pass",
		URL:      "https://example.com",
		TOTP:     "not-a-valid-base32-secret!!!", // will fail code generation
	}
	m := NewDetailModel(entry, config.Default())
	m.SetSize(80, 24)

	// TOTP code generation should have failed
	if m.totpCode != "" {
		t.Fatalf("expected totpCode to be empty for invalid secret, got %q", m.totpCode)
	}

	// But the field should still appear
	fields := m.selectableFields()
	found := false
	for _, f := range fields {
		if f.label == "TOTP" {
			found = true
			break
		}
	}
	if !found {
		t.Error("TOTP should be in selectableFields even when code generation fails")
	}

	content := m.buildContent()
	if !strings.Contains(content, "TOTP:") {
		t.Error("buildContent should include TOTP field even when code generation fails")
	}
}

func TestLineForFieldBigNotes(t *testing.T) {
	m := NewDetailModel(bigNotesEntry(), config.Default())
	m.SetSize(80, 24)

	// Fields: username(0), password(1), URL(2), TOTP(3), Notes(4), cf1(5)
	// Lines when collapsed:
	//   0: username, 1: password, 2: URL, 3: TOTP, 4: Notes label,
	//   5-9: 5 body lines, 10: hint line
	//   then timestamps, then custom fields

	if line := m.lineForField(3); line != 3 {
		t.Errorf("lineForField(3/TOTP) = %d, want 3", line)
	}
	if line := m.lineForField(4); line != 4 {
		t.Errorf("lineForField(4/Notes) = %d, want 4", line)
	}

	// Notes takes 1 label + 5 body + 1 hint = 7 lines (4..10)
	// Custom field should be after notes + timestamps
	cfLine := m.lineForField(5)
	if cfLine <= 10 {
		t.Errorf("lineForField(5/cf1) = %d, should be > 10 (after truncated notes)", cfLine)
	}

	// After expanding, notes take 1 label + 20 body = 21 lines (4..24)
	m.notesExpanded = true
	cfLineExpanded := m.lineForField(5)
	if cfLineExpanded <= 24 {
		t.Errorf("lineForField(5/cf1) expanded = %d, should be > 24", cfLineExpanded)
	}
	if cfLineExpanded <= cfLine {
		t.Errorf("custom field line should increase after expanding notes: collapsed=%d expanded=%d", cfLine, cfLineExpanded)
	}
}

func TestFieldAtLineBigNotes(t *testing.T) {
	m := NewDetailModel(bigNotesEntry(), config.Default())
	m.SetSize(80, 24)

	// Line 3 = TOTP
	if idx, ok := m.fieldAtLine(3); !ok || idx != 3 {
		t.Errorf("fieldAtLine(3) = (%d, %v), want (3, true) for TOTP", idx, ok)
	}

	// Line 4 = Notes label
	if idx, ok := m.fieldAtLine(4); !ok || idx != 4 {
		t.Errorf("fieldAtLine(4) = (%d, %v), want (4, true) for Notes label", idx, ok)
	}

	// Lines 5-9 = Notes body (should also map to Notes field)
	for line := 5; line <= 9; line++ {
		if idx, ok := m.fieldAtLine(line); !ok || idx != 4 {
			t.Errorf("fieldAtLine(%d) = (%d, %v), want (4, true) for Notes body", line, idx, ok)
		}
	}

	// Line 10 = hint line (inside notes total)
	if idx, ok := m.fieldAtLine(10); !ok || idx != 4 {
		t.Errorf("fieldAtLine(10) = (%d, %v), want (4, true) for Notes hint", idx, ok)
	}
}
