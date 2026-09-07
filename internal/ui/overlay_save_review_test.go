package ui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestSaveGateExplainsBlockedScans(t *testing.T) {
	for _, request := range []tea.Msg{textKey("i"), textKey("I"), ShowStatsMsg{}, ShowAuditMsg{}} {
		m := createTestApp(t)
		m.saving, m.statusError = true, true
		m.statusText = "an earlier message"
		updated, cmd := m.Update(request)
		m = updated.(AppModel)
		if cmd != nil || m.state != viewBrowser || m.healthScanCancel != nil || m.healthScanGen != 0 {
			t.Fatalf("%T bypassed the encryption input gate", request)
		}
		if m.statusError || !strings.Contains(ansi.Strip(m.statusBar()), "Saving… please wait") {
			t.Fatalf("%T was silently ignored: %q", request, m.statusText)
		}
	}
}

func TestScansAvailableAgainAfterSaveResult(t *testing.T) {
	for _, saveErr := range []error{nil, errors.New("synthetic save failure")} {
		m := createTestApp(t)
		m.saving = true
		updated, _ := m.Update(textKey("i"))
		updated, cmd := updated.Update(saveResultMsg{path: m.filePath, err: saveErr})
		updated, _ = updated.Update(cmd())
		m = updated.(AppModel)
		if m.saving || strings.Contains(m.statusText, "please wait") {
			t.Fatal("busy feedback outlived save")
		}
		updated, cmd = m.Update(textKey("i"))
		if cmd == nil {
			t.Fatal("stats shortcut still gated")
		}
		updated, cmd = updated.Update(cmd())
		m = updated.(AppModel)
		if m.state != viewStats || !m.stats.loading || cmd == nil {
			t.Fatal("scan did not start after save")
		}
		m.cancelHealthScan()
	}
}

func TestMissingOverlayTargetCannotBecomeUnlock(t *testing.T) {
	if viewNone != 0 || viewUnlock == 0 {
		t.Fatal("zero must not identify a live view")
	}
	for _, underHelp := range []bool{false, true} {
		m := createTestApp(t)
		m.state = viewConfirm
		m.confirm = ConfirmModel{} // malformed/absent parent, not a retired unlock view
		if underHelp {
			m.state, m.helpReturnState = viewHelp, viewConfirm
		}
		if states := m.overlayStates(); slices.Contains(states, viewUnlock) || slices.Contains(states, viewNone) {
			t.Fatalf("missing target treated as an active view: %v", states)
		}
		m.unlock.width, m.unlock.height = 17, 9
		updated, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
		m = updated.(AppModel)
		if m.unlock.width != 17 || m.unlock.height != 9 {
			t.Fatal("overlay forwarded resize to retired unlock model")
		}
	}
}

func TestConfirmationOwnsScanOriginThroughNestedQuit(t *testing.T) {
	for _, request := range []tea.Msg{ShowStatsMsg{}, ShowAuditMsg{}} {
		m := createTestApp(t)
		updated, scan := m.Update(request)
		m = updated.(AppModel)
		origin := m.state
		m.confirm = NewConfirmModel(origin, "Original confirmation", nil)
		m.state = viewConfirm
		updated, _ = m.Update(ShowHelpMsg{})
		updated, _ = updated.Update(RequestQuitMsg{})
		m = updated.(AppModel)
		if m.healthScanCancel == nil || !slices.Contains(m.overlayStates(), origin) {
			t.Fatal("nested quit lost scan origin")
		}
		updated, _ = m.Update(CancelEditMsg{})
		updated, _ = updated.Update(CloseHelpMsg{})
		m = updated.(AppModel)
		if m.confirm.returnState != origin || m.confirm.message != "Original confirmation" {
			t.Fatal("quit replaced original dialog target")
		}
		updated, _ = m.Update(CancelEditMsg{})
		m = updated.(AppModel)
		if m.state != origin || m.healthScanCancel == nil {
			t.Fatal("canceling dialog canceled the visible scan")
		}
		updated, _ = m.Update(scan())
		if updated.(AppModel).state != origin {
			t.Fatal("scan result changed view")
		}
	}
}
