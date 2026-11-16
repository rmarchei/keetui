package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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

	if m.revealedFields["builtin:password"] {
		t.Error("password should be hidden initially")
	}

	// Select password field (index 1)
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j', Text: string('j')})
	m = updated.(DetailModel)
	if m.selectedField != 1 {
		t.Fatalf("selectedField = %d, want 1 (password)", m.selectedField)
	}

	// Toggle password on selected field
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'p', Text: string('p')})
	m = updated.(DetailModel)
	if !m.revealedFields["builtin:password"] {
		t.Error("password should be visible after toggle")
	}

	// Toggle back
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'p', Text: string('p')})
	m = updated.(DetailModel)
	if m.revealedFields["builtin:password"] {
		t.Error("password should be hidden after second toggle")
	}

	// Toggle on non-protected field should do nothing
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'k', Text: string('k')}) // back to username
	m = updated.(DetailModel)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'p', Text: string('p')})
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
		updated, cmd := m.Update(tea.KeyPressMsg{Code: tt.key, Text: string(tt.key)})
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

func TestDetailRevealKeepsLayoutAndRawClipboardValue(t *testing.T) {
	entry := testEntry()
	entry.UserName = ""
	entry.Password = "p\r\n\x1b[2J\x85\xff"
	m := NewDetailModel(entry, config.Default())
	m.SetSize(80, 24)
	m.selectedField = 1
	wantLines := strings.Count(m.buildContent(), "\n")
	for i := range 4 {
		updated, _ := m.Update(tea.KeyPressMsg{Code: 'p', Text: string('p')})
		m = updated.(DetailModel)
		content := m.buildContent()
		if got := strings.Count(content, "\n"); got != wantLines {
			t.Fatalf("toggle %d: got %d content lines, want %d", i, got, wantLines)
		}
		if strings.Contains(content, entry.Password) || strings.Contains(content, "\x1b[2J") {
			t.Fatal("raw password control bytes reached the rendered content")
		}
		if i%2 == 0 && !strings.Contains(content, sanitizeDisplay(entry.Password)) {
			t.Error("revealed password should contain display-safe placeholders")
		}
		_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if cmd == nil {
			t.Fatal("enter on password should produce a copy command even with an empty username")
		}
		msg, ok := cmd().(CopyToClipboardMsg)
		if !ok || msg.Value != entry.Password {
			t.Fatal("display sanitization must not change the clipboard value")
		}
	}
}

func TestDetailEditMsg(t *testing.T) {
	entry := testEntry()
	m := NewDetailModel(entry, config.Default())
	m.SetSize(80, 24)

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'e', Text: string('e')})
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
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'Q', Text: string('Q')})
	m = updated.(DetailModel)
	if !m.showQR {
		t.Error("QR should be shown after Q")
	}

	// Close QR
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = updated.(DetailModel)
	if m.showQR {
		t.Error("QR should be hidden after esc")
	}
}

func TestDetailHIBPTrigger(t *testing.T) {
	entry := testEntry()
	m := NewDetailModel(entry, config.Default())
	m.SetSize(80, 24)

	updated, cmd := m.Update(textKey("b"))
	m = updated.(DetailModel)
	if cmd == nil {
		t.Fatal("b key should produce a command for HIBP check")
	}
	if !m.hibpChecking {
		t.Error("hibpChecking should be true after pressing b")
	}
}

func TestDetailBackMsg(t *testing.T) {
	entry := testEntry()
	m := NewDetailModel(entry, config.Default())
	m.SetSize(80, 24)

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
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
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: string('a')})
	m = updated.(DetailModel)
	if !m.attachMode {
		t.Error("should be in attachment mode after 'a'")
	}

	// Navigate
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: string('j')})
	m = updated.(DetailModel)
	if m.attachIdx != 1 {
		t.Errorf("attachIdx = %d, want 1", m.attachIdx)
	}

	// Exit attachment mode
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
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
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j', Text: string('j')})
	m = updated.(DetailModel)
	if m.selectedField != 1 {
		t.Errorf("selectedField after j = %d, want 1", m.selectedField)
	}

	// Move up
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'k', Text: string('k')})
	m = updated.(DetailModel)
	if m.selectedField != 0 {
		t.Errorf("selectedField after k = %d, want 0", m.selectedField)
	}

	// Should not go below 0
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'k', Text: string('k')})
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
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
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
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j', Text: string('j')})
	m = updated.(DetailModel)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: string('j')})
	m = updated.(DetailModel)
	if m.selectedField != 2 {
		t.Fatalf("selectedField = %d, want 2 (URL)", m.selectedField)
	}

	// Show QR
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'Q', Text: string('Q')})
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
	updated, _ := m.Update(tea.MouseClickMsg{
		X:      10,
		Y:      vpStart,
		Button: tea.MouseLeft,
	})
	m = updated.(DetailModel)
	if m.selectedField != 0 {
		t.Errorf("selectedField after click on username line = %d, want 0", m.selectedField)
	}

	// Click on Password line (viewport line 1)
	updated, _ = m.Update(tea.MouseClickMsg{
		X:      10,
		Y:      vpStart + 1,
		Button: tea.MouseLeft,
	})
	m = updated.(DetailModel)
	if m.selectedField != 1 {
		t.Errorf("selectedField after click on password line = %d, want 1", m.selectedField)
	}

	// Click on URL line (viewport line 2)
	updated, _ = m.Update(tea.MouseClickMsg{
		X:      10,
		Y:      vpStart + 2,
		Button: tea.MouseLeft,
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

// labelLine returns the content line on which label is drawn.
func labelLine(t *testing.T, m DetailModel, label string) int {
	t.Helper()
	for i, line := range strings.Split(ansi.Strip(m.buildContent()), "\n") {
		if strings.HasPrefix(strings.TrimLeft(line, " ▸"), label) {
			return i
		}
	}
	t.Fatalf("label %q not drawn", label)
	return -1
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

	// Each field maps to the line its label is drawn on.
	for i, label := range []string{"Username:", "Password:", "URL:", "Notes:", "field1:", "field2:"} {
		if got, want := m.lineForField(i), labelLine(t, m, label); got != want {
			t.Errorf("lineForField(%d) = %d, want %d (%s)", i, got, want, label)
		}
	}
}

func TestDetailView(t *testing.T) {
	entry := testEntry()
	m := NewDetailModel(entry, config.Default())
	m.SetSize(80, 24)

	view := m.View().Content
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
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'p', Text: string('p')})
	m = updated.(DetailModel)
	if !m.notesExpanded {
		t.Error("notes should be expanded after p")
	}

	// Press p to collapse
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'p', Text: string('p')})
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

// Enter on the TOTP row copies the code. With none (an invalid secret) it
// copies nothing, the secret above all, and says why; the QR keeps the
// secret.
func TestTOTPRowCopiesOnlyACode(t *testing.T) {
	for _, secret := range []string{"JBSWY3DPEHPK3PXP", "not-a-valid-base32-secret!!!"} {
		m := NewDetailModel(keepass.Entry{Title: "Synthetic", TOTP: secret}, config.Default())
		m.SetSize(80, 24)
		m.selectedField = 3 // username, password, URL, TOTP
		_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if cmd == nil {
			t.Fatalf("%s: enter did nothing", secret)
		}
		switch msg := cmd().(type) {
		case CopyToClipboardMsg:
			if m.totpCode == "" || msg.Value != m.totpCode {
				t.Errorf("%s: enter copied %q, the code is %q", secret, msg.Value, m.totpCode)
			}
		case StatusMsg:
			if m.totpCode != "" || !msg.IsError || !strings.Contains(msg.Text, "No TOTP code") {
				t.Errorf("%s: status %+v with code %q", secret, msg, m.totpCode)
			}
		default:
			t.Fatalf("%s: enter sent %T", secret, msg)
		}
		updated, _ := m.Update(textKey(m.keys.ShowQR))
		if m = updated.(DetailModel); !m.showQR || m.qrLabel != "TOTP" {
			t.Errorf("%s: the QR of the secret does not show", secret)
		}
	}
}

func TestLineForFieldBigNotes(t *testing.T) {
	m := NewDetailModel(bigNotesEntry(), config.Default())
	m.SetSize(80, 24)

	// Fields: username(0), password(1), URL(2), TOTP(3), Notes(4), cf1(5)
	for i, label := range []string{"TOTP:", "Notes:", "cf1:"} {
		if got, want := m.lineForField(i+3), labelLine(t, m, label); got != want {
			t.Errorf("lineForField(%d) = %d, want %d (%s)", i+3, got, want, label)
		}
	}

	// Collapsed notes take the label, 5 body lines and the hint.
	cfLine := m.lineForField(5)
	if notes := m.lineForField(4); cfLine <= notes+6 {
		t.Errorf("lineForField(5/cf1) = %d, should follow the collapsed notes at %d..%d", cfLine, notes, notes+6)
	}

	// Expanded, they take the label and all 20 body lines.
	m.notesExpanded = true
	cfLineExpanded := m.lineForField(5)
	if notes := m.lineForField(4); cfLineExpanded <= notes+20 {
		t.Errorf("lineForField(5/cf1) expanded = %d, should follow the notes at %d..%d", cfLineExpanded, notes, notes+20)
	}
	if got, want := cfLineExpanded, labelLine(t, m, "cf1:"); got != want {
		t.Errorf("expanded lineForField(5/cf1) = %d, drawn on %d", got, want)
	}
}

func TestFieldAtLineBigNotes(t *testing.T) {
	m := NewDetailModel(bigNotesEntry(), config.Default())
	m.SetSize(80, 24)

	totp, notes := labelLine(t, m, "TOTP:"), labelLine(t, m, "Notes:")
	if idx, ok := m.fieldAtLine(totp); !ok || idx != 3 {
		t.Errorf("fieldAtLine(%d) = (%d, %v), want (3, true) for TOTP", totp, idx, ok)
	}
	// The label, 5 body lines and the hint all select the notes.
	for line := notes; line <= notes+6; line++ {
		if idx, ok := m.fieldAtLine(line); !ok || idx != 4 {
			t.Errorf("fieldAtLine(%d) = (%d, %v), want (4, true) for Notes", line, idx, ok)
		}
	}
	// The gap before the notes selects nothing.
	if idx, ok := m.fieldAtLine(notes - 1); ok {
		t.Errorf("fieldAtLine(%d) = %d, want no field for the gap above the notes", notes-1, idx)
	}
}

func TestDetailTOTPHonorsDigits(t *testing.T) {
	entry := keepass.Entry{
		Title: "E",
		TOTP:  "otpauth://totp/x?secret=JBSWY3DPEHPK3PXP&digits=8",
	}
	m := NewDetailModel(entry, config.Default())
	m.refreshTOTP()
	if len(m.totpCode) != 8 {
		t.Errorf("TOTP code = %q (%d digits), want 8 digits per the otpauth URI", m.totpCode, len(m.totpCode))
	}
}
