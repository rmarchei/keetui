package ui

import (
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// No test reads, overwrites, or clears the desktop clipboard.
type testClipboard struct {
	value         string // simulated clipboard contents
	copied        string // keetui's outstanding copy
	held          bool
	terminalOnly  bool           // an OSC 52-only session: clears cannot be verified
	via           terminalResult // the route of terminal-only copies
	outcomes      []clearOutcome // queued terminal clear outcomes; clearRequested after
	writeErr      error
	opts          copyOptions
	reads, clears int
	releases      int
}

func (c *testClipboard) copy(value string, opts copyOptions) (copyResult, error) {
	if c.writeErr != nil {
		return copyResult{}, c.writeErr
	}
	c.value, c.copied, c.held, c.opts = value, value, true, opts
	if c.terminalOnly {
		return copyResult{terminal: true, via: c.via}, nil
	}
	return copyResult{native: true, marked: true}, nil
}

func (c *testClipboard) release() (clearOutcome, error) {
	if !c.held {
		return clearNothing, nil
	}
	c.releases++
	c.held = false
	if c.terminalOnly {
		outcome := clearRequested
		if len(c.outcomes) > 0 {
			outcome, c.outcomes = c.outcomes[0], c.outcomes[1:]
		}
		// Like systemClipboard, a pending clear keeps the copy.
		c.held = outcome.pending()
		switch outcome {
		case clearRequested, clearReattached, clearPartial:
			c.clears++
		}
		return outcome, nil
	}
	c.reads++
	if c.value != c.copied {
		return clearChanged, nil
	}
	c.value = ""
	c.clears++
	return clearDone, nil
}

func copyForCountdown(t *testing.T, m AppModel, value string) AppModel {
	t.Helper()
	updated, cmd := m.Update(CopyToClipboardMsg{Field: "password", Value: value})
	if cmd == nil {
		t.Fatal("copy did not produce feedback/timer commands")
	}
	return updated.(AppModel)
}

func TestSecurityCountdownRounding(t *testing.T) {
	now := time.Unix(1000, 0)
	for _, tc := range []struct {
		after time.Duration
		want  string
	}{
		{15 * time.Second, "15s"}, {14001 * time.Millisecond, "15s"},
		{time.Millisecond, "1s"}, {0, "0s"}, {-time.Second, "0s"},
		{60 * time.Second, "1:00"}, {299 * time.Second, "4:59"},
	} {
		if got := countdown(now.Add(tc.after), now); got != tc.want {
			t.Errorf("countdown(%v) = %q, want %q", tc.after, got, tc.want)
		}
	}
}

func TestCopyCountdownAndDisplayTick(t *testing.T) {
	m := copyForCountdown(t, createTestApp(t), "synthetic-secret")
	if got := countdown(m.clipboardClearAt, m.securityNow); got != "15s" {
		t.Fatalf("new copy countdown = %q", got)
	}
	if !m.securityTickPending || m.clipboardGen == 0 {
		t.Fatal("copy did not arm the security timers")
	}
	m.statusText = "Copied password!"
	activity, gen := m.lastActivity, m.securityTickGen
	updated, cmd := m.Update(securityTickMsg{gen: gen, at: m.securityNow.Add(time.Second)})
	m = updated.(AppModel)
	if cmd == nil || !m.securityTickPending || m.securityTickGen == gen {
		t.Fatal("display tick did not rearm its single chain")
	}
	if m.lastActivity != activity || m.statusText != "Copied password!" {
		t.Fatal("countdown tick counted as activity or erased copy feedback")
	}
	if got := ansi.Strip(m.statusBar()); !strings.Contains(got, "clip:14s") || strings.Contains(got, "synthetic-secret") {
		t.Fatalf("invalid countdown status: %q", got)
	}
	updated, cmd = m.Update(securityTickMsg{gen: gen, at: m.securityNow.Add(time.Hour)})
	if cmd != nil || updated.(AppModel).securityNow != m.securityNow {
		t.Fatal("duplicate/stale display tick was accepted")
	}
}

func TestRepeatedCopyInvalidatesOldClearEvenForSameValue(t *testing.T) {
	for _, value := range []string{"first", "second"} {
		m := copyForCountdown(t, createTestApp(t), "first")
		oldClear, oldTick := m.clipboardGen, m.securityTickGen
		m = copyForCountdown(t, m, value)
		if m.securityTickGen != oldTick {
			t.Fatal("second copy created a duplicate display ticker")
		}
		clipboard := m.clipboard.(*testClipboard)
		updated, cmd := m.Update(clipboardClearMsg{gen: oldClear})
		m = updated.(AppModel)
		if cmd != nil || clipboard.reads != 0 || clipboard.clears != 0 || clipboard.value != value || m.clipboardClearAt.IsZero() {
			t.Fatal("old clear timer affected the newer copy")
		}
		updated, cmd = m.Update(clipboardClearMsg{gen: m.clipboardGen})
		m = updated.(AppModel)
		if cmd == nil || clipboard.clears != 1 || clipboard.value != "" || m.clipboardHeld || !m.clipboardClearAt.IsZero() {
			t.Fatal("current clear did not clear and reset the countdown")
		}
	}
}

func TestDelayedDisplayTickDoesNotRewindCountdown(t *testing.T) {
	m := copyForCountdown(t, createTestApp(t), "synthetic")
	before := m.securityNow
	updated, _ := m.Update(securityTickMsg{gen: m.securityTickGen, at: before.Add(-10 * time.Second)})
	m = updated.(AppModel)
	if m.securityNow.Before(before) || countdown(m.clipboardClearAt, m.securityNow) != "15s" {
		t.Fatal("delayed display tick rewound the newly copied value's countdown")
	}
}

func TestClipboardChangesAndFailures(t *testing.T) {
	m := copyForCountdown(t, createTestApp(t), "first")
	clipboard := m.clipboard.(*testClipboard)
	deadline, gen := m.clipboardClearAt, m.clipboardGen
	clipboard.writeErr = errors.New("unavailable")
	m = copyForCountdown(t, m, "second")
	if m.clipboardGen != gen || m.clipboardClearAt != deadline || clipboard.copied != "first" {
		t.Fatal("failed copy reset a live timer")
	}
	clipboard.value = "copied outside keetui"
	updated, _ := m.Update(clipboardClearMsg{gen: gen})
	m = updated.(AppModel)
	if clipboard.clears != 0 || clipboard.value != "copied outside keetui" || !m.clipboardClearAt.IsZero() {
		t.Fatal("clear timer overwrote a different clipboard value")
	}
	clipboard.writeErr = nil
	clipboard.terminalOnly = true
	m = copyForCountdown(t, m, "third")
	updated, _ = m.Update(clipboardClearMsg{gen: m.clipboardGen})
	if clipboard.clears != 1 || !updated.(AppModel).clipboardClearAt.IsZero() {
		t.Fatal("unreadable remote clipboard did not receive best-effort clear")
	}
}

func TestDisabledSecurityTimersDoNotTick(t *testing.T) {
	m := createTestApp(t)
	m.cfg.AutoLockMinutes, m.cfg.ClipboardClearSeconds = 0, 0
	m = copyForCountdown(t, m, "synthetic")
	if m.securityTickPending || !m.clipboardClearAt.IsZero() {
		t.Fatal("disabled timers scheduled a countdown")
	}
	if got := ansi.Strip(m.statusBar()); !strings.Contains(got, "clip:off") || !strings.Contains(got, "lock:off") {
		t.Fatalf("disabled protections not visible: %q", got)
	}
	m.cfg.ClipboardClearSeconds = 15
	m = copyForCountdown(t, m, "synthetic")
	if !m.securityTickPending {
		t.Fatal("clipboard-only countdown did not start")
	}
	updated, _ := m.Update(clipboardClearMsg{gen: m.clipboardGen})
	if updated.(AppModel).securityTickPending {
		t.Fatal("display ticker remained armed without active timers")
	}
}

func TestAutoLockCountdownTracksActivityAndRetry(t *testing.T) {
	m := createTestApp(t)
	m.scheduleAutoLock()
	if got := countdown(m.autoLockDeadline(), m.securityNow); got != "5:00" {
		t.Fatalf("initial auto-lock countdown = %q", got)
	}
	m.lastActivity = time.Now().Add(-4*time.Minute - 45*time.Second)
	m.autoLockCheckAt = m.lastActivity.Add(5 * time.Minute)
	m.securityNow = m.lastActivity.Add(4*time.Minute + 45*time.Second)
	if !strings.Contains(ansi.Strip(m.statusBar()), "lock:15s") {
		t.Fatal("near-expiry warning is missing")
	}
	updated, _ := m.Update(textKey("j"))
	m = updated.(AppModel)
	if got := countdown(m.autoLockDeadline(), m.securityNow); got != "5:00" {
		t.Fatalf("activity did not reset countdown: %q", got)
	}
	// A failed save retries later than the original idle deadline.
	m.lastActivity = time.Now().Add(-time.Hour)
	m.scheduleAutoLock()
	if got := countdown(m.autoLockDeadline(), m.securityNow); got != "5:00" {
		t.Fatalf("retry countdown is not the actual next check: %q", got)
	}
}

func TestCountdownRunsUnderHelpAndDuringSave(t *testing.T) {
	m := copyForCountdown(t, createTestApp(t), "synthetic")
	updated, _ := m.Update(ShowHelpMsg{})
	m = updated.(AppModel)
	m.saving = true
	activity := m.lastActivity
	updated, cmd := m.Update(securityTickMsg{gen: m.securityTickGen, at: m.securityNow.Add(2 * time.Second)})
	m = updated.(AppModel)
	if m.state != viewHelp || m.lastActivity != activity || cmd == nil || !strings.Contains(ansi.Strip(m.statusBar()), "clip:13s") {
		t.Fatal("overlay or save gate swallowed the display tick")
	}
	gen := m.autoLockGen
	updated, cmd = m.Update(autoLockMsg{gen: gen - 1})
	if cmd != nil || updated.(AppModel).autoLockGen != gen {
		t.Fatal("stale lock check rearmed during save")
	}
	updated, _ = m.Update(autoLockMsg{gen: gen})
	m = updated.(AppModel)
	if m.autoLockGen == gen || countdown(m.autoLockDeadline(), m.securityNow) != "5:00" {
		t.Fatal("deferred lock check and displayed deadline disagree")
	}
}

func TestLockInvalidatesClipboardAndDisplayTimers(t *testing.T) {
	m := copyForCountdown(t, createTestApp(t), "synthetic")
	oldClear, oldTick := m.clipboardGen, m.securityTickGen
	m.lastActivity = time.Now().Add(-time.Hour)
	updated, _ := m.Update(autoLockMsg{gen: m.autoLockGen})
	m = updated.(AppModel)
	clipboard := m.clipboard.(*testClipboard)
	if m.state != viewUnlock || m.securityTickPending || !m.clipboardClearAt.IsZero() || m.clipboardHeld {
		t.Fatal("lock did not reset countdown state")
	}
	clears := clipboard.clears
	updated, cmd := m.Update(clipboardClearMsg{gen: oldClear})
	if cmd != nil || clipboard.clears != clears {
		t.Fatal("stale clipboard timer cleared after lock")
	}
	_, cmd = updated.Update(securityTickMsg{gen: oldTick, at: time.Now()})
	if cmd != nil {
		t.Fatal("stale display ticker restarted after lock")
	}
}

func TestCountdownStatusFitsAndKeepsFeedback(t *testing.T) {
	m := copyForCountdown(t, createTestApp(t), "synthetic")
	m.statusText = "Copied password!"
	m.dirty = true
	m.entryCount = 3
	m.undoStack.push(undoAction{description: "edit"})
	for _, width := range []int{20, 40, 80, 110, 160} {
		m.width = width
		bar := m.statusBar()
		assertFrame(t, bar, width, 1)
		plain := ansi.Strip(bar)
		if width >= 40 {
			for _, want := range []string{"clip:15s", "lock:", "ctrl+z: undo"} {
				if !strings.Contains(plain, want) {
					t.Fatalf("%d columns hides %q: %q", width, want, plain)
				}
			}
		}
		if width >= 80 && (!strings.Contains(plain, "Copied password!") || !strings.Contains(plain, "3 entries")) {
			t.Fatalf("%d columns hides copy/count feedback: %q", width, plain)
		}
	}
}

func TestSecurityTimersRunAtDisplayedDeadlines(t *testing.T) {
	m := createTestApp(t)
	synctest.Test(t, func(t *testing.T) {
		m.recordActivity()
		messages := make(chan tea.Msg, 16)
		launch := func(cmd tea.Cmd) {
			if cmd == nil {
				return
			}
			go func() {
				if msg := cmd(); msg != nil {
					messages <- msg
				}
			}()
		}
		drain := func() {
			for {
				synctest.Wait()
				select {
				case msg := <-messages:
					if batch, ok := msg.(tea.BatchMsg); ok {
						for _, cmd := range batch {
							launch(cmd)
						}
					} else {
						updated, cmd := m.Update(msg)
						m = updated.(AppModel)
						if m.state != viewUnlock {
							launch(cmd) // the newly locked input's blink loop is outside this test
						}
					}
				default:
					return
				}
			}
		}
		launch(m.scheduleAutoLock())
		updated, cmd := m.Update(CopyToClipboardMsg{Field: "password", Value: "synthetic"})
		m = updated.(AppModel)
		launch(cmd)
		drain()
		clipboard := m.clipboard.(*testClipboard)
		for second := 1; second <= 300; second++ {
			time.Sleep(time.Second) // virtual time: no five-minute wall-clock wait
			drain()
			if second == 14 && (clipboard.clears != 0 || !strings.Contains(ansi.Strip(m.statusBar()), "clip:1s")) {
				t.Fatal("clipboard countdown/clear fired early")
			}
			if second == 15 && (clipboard.clears != 1 || !m.clipboardClearAt.IsZero()) {
				t.Fatal("clipboard was not cleared at the displayed deadline")
			}
			if second == 299 && (m.state != viewBrowser || !strings.Contains(ansi.Strip(m.statusBar()), "lock:1s")) {
				t.Fatal("auto-lock countdown/lock fired early")
			}
		}
		if m.state != viewUnlock || m.db != nil || m.securityTickPending {
			t.Fatal("database did not lock when its countdown expired")
		}
		// A display tick queued just before lock may still be waiting on its
		// timer. Let it arrive (without rearming it) before closing the bubble.
		time.Sleep(2 * time.Second)
		synctest.Wait()
	})
}

func TestSavingSettingsKeepsExistingCopyDeadline(t *testing.T) {
	m := copyForCountdown(t, createTestApp(t), "synthetic")
	deadline, clearGen, tickGen := m.clipboardClearAt, m.clipboardGen, m.securityTickGen
	cfg := m.cfg
	cfg.ClipboardClearSeconds, cfg.AutoLockMinutes = 0, 0
	updated, _ := m.Update(ConfigSavedMsg{Cfg: cfg})
	m = updated.(AppModel)
	if m.clipboardClearAt != deadline || m.clipboardGen != clearGen || m.securityTickGen != tickGen {
		t.Fatal("saving settings changed an already-scheduled clear")
	}
	if !m.autoLockCheckAt.IsZero() || !strings.Contains(ansi.Strip(m.statusBar()), "lock:off") {
		t.Fatal("disabled auto-lock did not update its indicator")
	}
	updated, _ = m.Update(clipboardClearMsg{gen: clearGen})
	m = updated.(AppModel)
	if m.securityTickPending {
		t.Fatal("last countdown did not stop after settings disabled timers")
	}
	cfg.AutoLockMinutes = 10
	updated, _ = m.Update(ConfigSavedMsg{Cfg: cfg})
	m = updated.(AppModel)
	if !m.securityTickPending || !strings.Contains(ansi.Strip(m.statusBar()), "lock:10:00") {
		t.Fatal("re-enabling auto-lock did not restart the display ticker")
	}
}
