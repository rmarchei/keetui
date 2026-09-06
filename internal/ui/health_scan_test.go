package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rmarchei/keetui/internal/keepass"
)

func TestBrowserConfirmationsForwardBackgroundMessages(t *testing.T) {
	for _, request := range []tea.Msg{
		DeleteEntryMsg{}, DeleteGroupMsg{}, PermanentlyDeleteEntryMsg{}, PermanentlyDeleteGroupMsg{}, EmptyRecycleBinMsg{},
	} {
		m := createTestApp(t)
		// A previously used returnState must not leak into a new dialog.
		m.returnState = viewDetail
		updated, _ := m.Update(request)
		m = updated.(AppModel)
		if m.returnState != viewBrowser {
			t.Fatalf("%T returns to %v", request, m.returnState)
		}
		root := m.db.RootGroup()
		if _, err := m.db.AddEntry(root.UUID, keepass.Entry{Title: "background refresh"}); err != nil {
			t.Fatal(err)
		}
		before := len(m.browser.items)
		updated, _ = m.Update(RefreshMsg{})
		m = updated.(AppModel)
		if len(m.browser.items) != before+1 {
			t.Fatalf("%T did not forward refresh to browser", request)
		}
		updated, _ = m.Update(CancelEditMsg{})
		if updated.(AppModel).state != viewBrowser {
			t.Fatalf("%T canceled to wrong screen", request)
		}
	}
}

func TestHealthScanReturnsAsynchronouslyWithoutLeavingOverlay(t *testing.T) {
	for _, request := range []tea.Msg{ShowStatsMsg{}, ShowAuditMsg{}} {
		m := createTestApp(t)
		updated, cmd := m.Update(request)
		m = updated.(AppModel)
		if cmd == nil || m.healthScanCancel == nil || !strings.Contains(ansi.Strip(m.View().Content), "Analyzing passwords") {
			t.Fatalf("%T did not show loading state", request)
		}
		updated, _ = m.Update(ShowHelpMsg{})
		updated, _ = updated.Update(RequestQuitMsg{})
		m = updated.(AppModel)
		if m.healthScanCancel == nil {
			t.Fatal("overlay canceled underlying scan")
		}
		updated, _ = m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
		updated, _ = updated.Update(cmd())
		m = updated.(AppModel)
		if m.state != viewConfirm || m.healthScanCancel != nil {
			t.Fatal("result stole focus or retained scan")
		}
		if m.stats.loading || m.audit.loading {
			t.Fatal("result did not clear loading state")
		}
		switch request.(type) {
		case ShowStatsMsg:
			if m.stats.stats.TotalEntries != 2 || m.stats.width != 40 || m.stats.height != 11 {
				t.Fatal("incorrect stats or dimensions")
			}
		case ShowAuditMsg:
			if m.audit.result.TotalIssues() == 0 || m.audit.width != 40 || m.audit.height != 11 {
				t.Fatal("incorrect audit or dimensions")
			}
		}
		updated, _ = m.Update(CancelEditMsg{})
		updated, _ = updated.Update(CloseHelpMsg{})
		if updated.(AppModel).state != m.healthScanView {
			t.Fatal("lost scan origin")
		}
		assertFrame(t, updated.View().Content, 40, 12)
	}
}

func TestHealthScanCanceledOnNavigationAndStaleResultsIgnored(t *testing.T) {
	for _, request := range []tea.Msg{ShowStatsMsg{}, ShowAuditMsg{}} {
		m := createTestApp(t)
		updated, cmd := m.Update(request)
		m = updated.(AppModel)
		stale := cmd() // finished worker; result has not yet reached Update
		updated, _ = m.Update(BackToBrowserMsg{})
		m = updated.(AppModel)
		if m.healthScanCancel != nil {
			t.Fatal("leaving view retained scan")
		}
		updated, _ = m.Update(request)
		m = updated.(AppModel)
		gen := m.healthScanGen
		updated, _ = m.Update(stale)
		m = updated.(AppModel)
		if m.healthScanGen != gen || m.healthScanCancel == nil {
			t.Fatal("stale result replaced new scan")
		}
		if !strings.Contains(ansi.Strip(m.View().Content), "Analyzing passwords") {
			t.Fatal("stale result replaced loading screen")
		}
		m.cancelHealthScan()
	}
}

func TestHealthScanCanceledOnLockAndQuit(t *testing.T) {
	for _, request := range []tea.Msg{autoLockMsg{gen: 7}, quitMsg{}} {
		m := createTestApp(t)
		m.autoLockGen = 7
		m.lastActivity = time.Now().Add(-10 * time.Minute)
		updated, cmd := m.Update(ShowAuditMsg{})
		updated, _ = updated.Update(request)
		m = updated.(AppModel)
		if m.healthScanCancel != nil {
			t.Fatalf("%T left worker active", request)
		}
		if result := cmd(); result != nil {
			t.Fatalf("canceled scan returned findings: %T", result)
		}
	}
}

func TestHealthScanSnapshotUnaffectedByLaterEdits(t *testing.T) {
	m := createTestApp(t)
	updated, cmd := m.Update(ShowStatsMsg{})
	m = updated.(AppModel)
	root := m.db.RootGroup()
	if _, err := m.db.AddEntry(root.UUID, keepass.Entry{Title: "added after snapshot"}); err != nil {
		t.Fatal(err)
	}
	result := cmd().(healthScanResultMsg)
	if result.stats.TotalEntries != 2 || m.db.EntryCount() != 3 {
		t.Fatal("worker read live DB instead of snapshot")
	}
	m.cancelHealthScan()
}

func TestHealthScanDoesNotStartDuringSaveOrWhileLocked(t *testing.T) {
	m := createTestApp(t)
	m.saving = true
	if updated, cmd := m.Update(ShowStatsMsg{}); cmd != nil || updated.(AppModel).healthScanCancel != nil {
		t.Fatal("scan raced with save")
	}
	m.saving, m.db, m.state = false, nil, viewUnlock
	if _, cmd := m.Update(ShowAuditMsg{}); cmd != nil {
		t.Fatal("scan started while locked")
	}
}
