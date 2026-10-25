package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rmarchei/keetui/internal/config"
)

func TestInitAsksWhetherTheTerminalIsTmux(t *testing.T) {
	batch, ok := NewApp("/tmp/test.kdbx", "", config.Default()).Init()().(tea.BatchMsg)
	if !ok {
		t.Fatal("Init returned a single command")
	}
	asked := false
	for _, cmd := range batch {
		if cmd != nil && cmd() == tea.RequestTerminalVersion() {
			asked = true
		}
	}
	if !asked {
		t.Fatal("Init does not request XTVERSION")
	}
}

func TestTerminalVersionPicksTheTerminalRoute(t *testing.T) {
	for _, tc := range []struct {
		replies []string
		want    terminalKind
	}{
		{nil, terminalUnknown}, // no reply: no change
		{[]string{""}, terminalUnknown},
		{[]string{"xterm(390)"}, terminalOther},
		{[]string{"WezTerm 20240203"}, terminalOther},
		{[]string{"tmux 3.8"}, terminalTmux},
		{[]string{"tmux 3.3a", "xterm(390)"}, terminalTmux},
		{[]string{"xterm(390)", "tmux 3.3a"}, terminalTmux},
		{[]string{"tmuxinator"}, terminalOther},
	} {
		m := createTestApp(t)
		for _, name := range tc.replies {
			updated, cmd := m.Update(tea.TerminalVersionMsg{Name: name})
			m = updated.(AppModel)
			if cmd != nil {
				t.Fatal("version reply produced a command")
			}
		}
		m = copyForCountdown(t, m, "synthetic")
		if got := m.clipboard.(*testClipboard).opts.terminal; got != tc.want {
			t.Errorf("replies %q: terminal = %v, want %v", tc.replies, got, tc.want)
		}
	}
}

// statusOf returns the status a copy or clear reports. Only the status
// command runs: it comes first in a batch, before any timer.
func statusOf(t *testing.T, cmd tea.Cmd) StatusMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("no status command")
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		msg = batch[0]()
	}
	status, ok := msg.(StatusMsg)
	if !ok {
		t.Fatalf("command returned %T, want StatusMsg", msg)
	}
	return status
}

func TestUnclearableTerminalCopyShowsNoCountdown(t *testing.T) {
	m := copyForCountdown(t, createTestApp(t), "synthetic") // a live timer and display tick
	clipboard := m.clipboard.(*testClipboard)
	oldClear := m.clipboardGen
	clipboard.terminalOnly = true
	clipboard.via = terminalResult{route: routeTmuxLoadBuffer, noClear: "allow-passthrough off", termName: "xterm-256color"}
	updated, cmd := m.Update(CopyToClipboardMsg{Field: "password", Value: "synthetic"})
	m = updated.(AppModel)
	status := statusOf(t, cmd)
	if status.Text != "Sent password via tmux (OSC 52); can't auto-clear: allow-passthrough off" || status.IsError ||
		!strings.Contains(status.Detail, "set -g allow-passthrough on") {
		t.Fatalf("status = %+v", status)
	}
	if !m.clipboardClearAt.IsZero() || !m.clipboardHeld || m.clipboardGen == oldClear {
		t.Fatal("a copy nothing can clear kept a clear countdown")
	}
	if got := ansi.Strip(m.statusBar()); !strings.Contains(got, "clip:off") {
		t.Fatalf("status bar = %q", got)
	}
	updated, cmd = m.Update(clipboardClearMsg{gen: oldClear})
	if cmd != nil || clipboard.releases != 0 || updated.(AppModel).clipboardClearAt != m.clipboardClearAt {
		t.Fatal("the previous copy's timer acted on the new copy")
	}
}

func TestDeferredTerminalClearRetriesOnDisplayTick(t *testing.T) {
	m := createTestApp(t)
	clipboard := m.clipboard.(*testClipboard)
	clipboard.terminalOnly = true
	clipboard.via = terminalResult{route: routeTmuxPassthrough}
	clipboard.outcomes = []clearOutcome{clearDeferred, clearDeferred, clearRequested}
	m = copyForCountdown(t, m, "synthetic")
	updated, cmd := m.Update(clipboardClearMsg{gen: m.clipboardGen})
	m = updated.(AppModel)
	status := statusOf(t, cmd)
	if status.Text != "Terminal clipboard clears when keetui's tmux pane is visible" || !strings.Contains(status.Detail, "allow-passthrough all") {
		t.Fatalf("status = %+v", status)
	}
	updated, _ = m.Update(status)
	m = updated.(AppModel)
	if !m.clipboardDeferred || !m.securityTickPending || !clipboard.held {
		t.Fatal("deferred clear is not retried")
	}
	if got := ansi.Strip(m.statusBar()); !strings.Contains(got, "clip:wait") {
		t.Fatalf("status bar = %q", got)
	}

	activity := m.lastActivity
	tick := func() {
		t.Helper()
		updated, cmd := m.Update(securityTickMsg{gen: m.securityTickGen, at: m.securityNow.Add(time.Second)})
		m = updated.(AppModel)
		if m.lastActivity != activity {
			t.Fatal("retry counted as activity")
		}
		if m.clipboardDeferred && (cmd == nil || !m.securityTickPending) {
			t.Fatal("display tick stopped while the clear is deferred")
		}
	}
	tick()
	if !m.clipboardDeferred || clipboard.releases != 2 || m.statusText != deferredClearStatus {
		t.Fatalf("still-hidden retry changed state: releases %d, status %q", clipboard.releases, m.statusText)
	}
	tick()
	if m.clipboardDeferred || clipboard.clears != 1 || m.statusText != "Clipboard clear requested" || m.statusDetail != "" {
		t.Fatalf("visible retry: deferred %v, clears %d, status %q", m.clipboardDeferred, clipboard.clears, m.statusText)
	}
	tick()
	if clipboard.releases != 3 {
		t.Fatal("cleared copy released again")
	}
	if strings.Contains(ansi.Strip(m.statusBar()), "clip:") {
		t.Fatalf("cleared copy still shown: %q", ansi.Strip(m.statusBar()))
	}
}

// A clear that waits for the terminal that got the copy is retried on the
// display tick like a deferred one. Its notice follows why it still waits,
// and only its own notice is replaced.
func TestAwayClearWaitsForItsTerminal(t *testing.T) {
	m := createTestApp(t)
	clipboard := m.clipboard.(*testClipboard)
	clipboard.terminalOnly = true
	clipboard.via = terminalResult{route: routeTmuxPassthrough, reached: []string{"/dev/pts/7"}}
	clipboard.outcomes = []clearOutcome{clearAway, clearDeferred, clearAway, clearAway, clearRequested}
	m = copyForCountdown(t, m, "synthetic")
	updated, cmd := m.Update(clipboardClearMsg{gen: m.clipboardGen})
	m = updated.(AppModel)
	status := statusOf(t, cmd)
	if status.Text != "Terminal clipboard clear waits for the terminal that got the copy" || status.IsError ||
		!strings.Contains(status.Detail, "suspended (prefix C-z)") || !strings.Contains(status.Detail, "would miss that terminal") {
		t.Fatalf("status = %+v", status)
	}
	updated, _ = m.Update(status)
	m = updated.(AppModel)
	if !m.clipboardDeferred || !m.securityTickPending || !clipboard.held {
		t.Fatal("waiting clear is not retried")
	}
	if got := ansi.Strip(m.statusBar()); !strings.Contains(got, "clip:wait") {
		t.Fatalf("status bar = %q", got)
	}
	activity := m.lastActivity
	tick := func(want string) {
		t.Helper()
		updated, _ := m.Update(securityTickMsg{gen: m.securityTickGen, at: m.securityNow.Add(time.Second)})
		m = updated.(AppModel)
		if m.lastActivity != activity || m.statusText != want {
			t.Fatalf("tick: status %q, want %q (activity moved: %v)", m.statusText, want, m.lastActivity != activity)
		}
	}
	tick(deferredClearStatus)
	if !m.clipboardDeferred || !strings.Contains(m.statusDetail, "allow-passthrough all") {
		t.Fatalf("deferred retry: deferred %v, detail %q", m.clipboardDeferred, m.statusDetail)
	}
	tick(awayClearStatus)
	m.statusText = "Entry saved"
	tick("Entry saved")
	if !m.clipboardDeferred || clipboard.clears != 0 {
		t.Fatal("a waiting clear was sent")
	}
	m.statusText = awayClearStatus
	tick("Clipboard clear requested")
	if m.clipboardDeferred || clipboard.held || clipboard.clears != 1 || m.statusDetail != "" {
		t.Fatalf("clear once reachable: deferred %v, held %v, clears %d, detail %q", m.clipboardDeferred, clipboard.held, clipboard.clears, m.statusDetail)
	}
	if got := ansi.Strip(m.statusBar()); strings.Contains(got, "clip:") {
		t.Fatalf("cleared copy still shown: %q", got)
	}
}

func TestDeferredClearKeepsOtherStatusAndRetriesAfterLock(t *testing.T) {
	m := createTestApp(t)
	clipboard := m.clipboard.(*testClipboard)
	clipboard.terminalOnly = true
	clipboard.via = terminalResult{route: routeTmuxPassthrough}
	clipboard.outcomes = []clearOutcome{clearDeferred, clearRequested}
	m = copyForCountdown(t, m, "synthetic")
	m.lastActivity = time.Now().Add(-time.Hour)
	updated, _ := m.Update(autoLockMsg{gen: m.autoLockGen})
	m = updated.(AppModel)
	if m.state != viewUnlock || !m.clipboardDeferred || !m.securityTickPending {
		t.Fatal("lock abandoned a deferred terminal clear")
	}
	m.statusText = "Wrong password"
	updated, _ = m.Update(securityTickMsg{gen: m.securityTickGen, at: time.Now()})
	m = updated.(AppModel)
	if m.clipboardDeferred || clipboard.clears != 1 || m.statusText != "Wrong password" {
		t.Fatalf("retry after lock: deferred %v, clears %d, status %q", m.clipboardDeferred, clipboard.clears, m.statusText)
	}
	if m.securityTickPending {
		t.Fatal("display tick kept running on the unlock screen")
	}
}

func TestDeferredClearGivesUpWhenItsTerminalDetached(t *testing.T) {
	m := createTestApp(t)
	clipboard := m.clipboard.(*testClipboard)
	clipboard.terminalOnly = true
	clipboard.via = terminalResult{route: routeTmuxPassthrough, reached: []string{"/dev/pts/7"}}
	clipboard.outcomes = []clearOutcome{clearDeferred, clearDetached}
	m = copyForCountdown(t, m, "synthetic")
	updated, cmd := m.Update(clipboardClearMsg{gen: m.clipboardGen})
	updated, _ = updated.Update(statusOf(t, cmd))
	m = updated.(AppModel)
	updated, _ = m.Update(securityTickMsg{gen: m.securityTickGen, at: m.securityNow.Add(time.Second)})
	m = updated.(AppModel)
	if m.clipboardDeferred || clipboard.held || clipboard.clears != 0 {
		t.Fatal("a clear for a detached terminal is still pending or was sent")
	}
	if m.statusText != "Terminal clipboard not cleared: the terminal that got the copy detached" || !m.statusError {
		t.Fatalf("status = %q (error %v)", m.statusText, m.statusError)
	}
	if got := ansi.Strip(m.statusBar()); strings.Contains(got, "clip:") {
		t.Fatalf("status bar = %q", got)
	}
}

// The same kind of terminal reattached on another tty (an SSH reconnect):
// the retried clear goes out, and the status says where.
func TestDeferredClearReachesTheReattachedTerminal(t *testing.T) {
	m := createTestApp(t)
	clipboard := m.clipboard.(*testClipboard)
	clipboard.terminalOnly = true
	clipboard.via = terminalResult{route: routeTmuxPassthrough, reached: []string{"/dev/pts/7"}}
	clipboard.outcomes = []clearOutcome{clearDeferred, clearReattached}
	m = copyForCountdown(t, m, "synthetic")
	updated, cmd := m.Update(clipboardClearMsg{gen: m.clipboardGen})
	updated, _ = updated.Update(statusOf(t, cmd))
	m = updated.(AppModel)
	updated, _ = m.Update(securityTickMsg{gen: m.securityTickGen, at: m.securityNow.Add(time.Second)})
	m = updated.(AppModel)
	if m.clipboardDeferred || clipboard.held || clipboard.clears != 1 {
		t.Fatal("the clear for the reattached terminal was not sent")
	}
	if m.statusText != "Clipboard clear requested on the reattached terminal" || m.statusError ||
		!strings.Contains(m.statusDetail, "reconnects over SSH") {
		t.Fatalf("status = %q (error %v, detail %q)", m.statusText, m.statusError, m.statusDetail)
	}
	if got := ansi.Strip(m.statusBar()); strings.Contains(got, "clip:") {
		t.Fatalf("status bar = %q", got)
	}
}

func TestNewCopyReplacesDeferredClear(t *testing.T) {
	m := createTestApp(t)
	clipboard := m.clipboard.(*testClipboard)
	clipboard.terminalOnly = true
	clipboard.via = terminalResult{route: routeTmuxPassthrough}
	clipboard.outcomes = []clearOutcome{clearDeferred}
	m = copyForCountdown(t, m, "first")
	updated, _ := m.Update(clipboardClearMsg{gen: m.clipboardGen})
	m = copyForCountdown(t, updated.(AppModel), "second")
	if m.clipboardDeferred || m.clipboardClearAt.IsZero() {
		t.Fatal("new copy kept retrying the old clear instead of its own timer")
	}
}

func TestTmuxStatusDetailInHelp(t *testing.T) {
	m := createTestApp(t)
	clipboard := m.clipboard.(*testClipboard)
	clipboard.terminalOnly = true
	clipboard.via = terminalResult{route: routeTmuxPassthrough, termName: "foot\x1b[2J"}
	updated, cmd := m.Update(CopyToClipboardMsg{Field: "password", Value: "synthetic"})
	updated, _ = updated.Update(statusOf(t, cmd))
	updated, _ = updated.Update(tea.KeyPressMsg{Code: tea.KeyF1})
	m = updated.(AppModel)
	message := m.help.message
	if m.state != viewHelp || !strings.HasPrefix(message, "Sent password to the terminal clipboard via tmux (OSC 52)\n\n") ||
		!strings.Contains(message, "set -g allow-passthrough all") || strings.Contains(message, "\x1b") {
		t.Fatalf("help message = %q", message)
	}
	updated, _ = m.Update(CloseHelpMsg{})
	updated, _ = updated.Update(textKey("j"))
	if m := updated.(AppModel); m.statusText != "" || m.statusDetail != "" {
		t.Fatal("interaction left the tmux guidance behind")
	}

	// Errors carry the guidance too.
	m = createTestApp(t)
	m.clipboard.(*testClipboard).writeErr = &tmuxRouteError{reason: "allow-passthrough off and no clipboard support for screen", termName: "screen"}
	_, cmd = m.Update(CopyToClipboardMsg{Field: "password", Value: "synthetic"})
	if status := statusOf(t, cmd); !status.IsError || !strings.Contains(status.Detail, "',screen:clipboard'") {
		t.Fatalf("error status = %+v", status)
	}
}

func TestLongTmuxStatusKeepsOneRow(t *testing.T) {
	m := createTestApp(t)
	m.dirty = true
	m.undoStack.push(undoAction{description: "edit"})
	m = copyForCountdown(t, m, "synthetic")
	for _, text := range []string{
		copyStatus("history password", copyResult{terminal: true, via: terminalResult{route: routeTmuxLoadBuffer, nested: true, clients: 3, noClear: "3 clients share the window"}}),
		"Clipboard error: tmux can't reach the terminal clipboard: keetui's window is hidden and no clipboard support for xterm-256color",
		"Clipboard error: " + errTerminalNotTmux.Error(),
		deferredClearStatus,
		awayClearStatus,
		"Clipboard clear requested; a terminal that got the copy detached",
		"Terminal clipboard not cleared: the terminal that got the copy detached",
		"Clipboard clear requested on the reattached terminal",
		copyStatus("history password", copyResult{native: true, marked: true, terminal: true,
			via: terminalResult{route: routeTmuxPassthrough, nested: true, noClear: "nested tmux"}}),
		copyStatus("history password", copyResult{native: true, marked: true, terminal: true,
			via: terminalResult{route: routeTmuxPassthrough, clients: 12}}),
	} {
		m.statusText = text
		for _, width := range []int{20, 40, 80, 120} {
			m.width = width
			assertFrame(t, m.statusBar(), width, 1)
		}
	}
}

func TestClipboardDeferredReleaseKeepsCopy(t *testing.T) {
	c, term := newFakeClipboard(&fakeNative{})
	term.result = terminalResult{route: routeTmuxPassthrough}
	if _, err := c.copy("remote-secret", copyOptions{osc52: config.OSC52Auto}); err != nil {
		t.Fatal(err)
	}
	for _, pending := range []clearOutcome{clearDeferred, clearAway} {
		term.outcome = pending
		if outcome, err := c.release(); outcome != pending || err != nil || c.held == nil {
			t.Fatalf("pending release = %v, %v (held %v)", outcome, err, c.held != nil)
		}
	}
	// After the program exits, ReleaseClipboard tries once more.
	prev := processClipboard.Swap(c)
	t.Cleanup(func() { processClipboard.Store(prev) })
	term.outcome = clearRequested
	ReleaseClipboard()
	if c.held != nil || len(term.sent) != 2 || term.sent[1] != "" {
		t.Fatalf("ReleaseClipboard did not retry the clear: %q", term.sent)
	}

	term.outcome = clearUnsupported
	_, _ = c.copy("remote-secret", copyOptions{osc52: config.OSC52Auto})
	if outcome, _ := c.release(); outcome != clearUnsupported || c.held != nil {
		t.Fatalf("unsupported release = %v", outcome)
	}
}

func TestClipboardRoutesEachCopy(t *testing.T) {
	tty, remote := &fakeTerminal{}, &fakeTerminal{result: terminalResult{route: routeRemoteTmux, unverified: "remote"}}
	c := &systemClipboard{native: &fakeNative{}, terminal: terminalRoutes{tty: tty, remote: remote}}
	if _, err := c.copy("one", copyOptions{osc52: config.OSC52Auto}); err != nil || len(tty.sent) != 1 {
		t.Fatalf("plain terminal copy went elsewhere: %v", err)
	}
	res, err := c.copy("two", copyOptions{osc52: config.OSC52Auto, terminal: terminalTmux})
	if err != nil || res.via.route != routeRemoteTmux || len(remote.sent) != 1 {
		t.Fatalf("remote tmux copy = %+v, %v", res, err)
	}
	// The clear goes back the way the copy went.
	_, _ = c.release()
	if len(remote.sent) != 2 || len(tty.sent) != 1 {
		t.Fatalf("clear took another route: tty %q, remote %q", tty.sent, remote.sent)
	}

	// A local tmux leaves auto copies on the local clipboard (macOS)...
	tmux := &fakeTerminal{result: terminalResult{route: routeTmuxPassthrough}}
	c = &systemClipboard{native: &fakeNative{local: true, marks: true}, terminal: terminalRoutes{tty: tty, tmux: tmux}}
	if res, err := c.copy("local", copyOptions{osc52: config.OSC52Auto, terminal: terminalTmux}); err != nil || !res.native || res.terminal || len(tmux.sent) != 0 {
		t.Fatalf("auto copy in a local tmux = %+v, %v (tmux %q)", res, err, tmux.sent)
	}
	// ...and with always, the terminal leg is routed the same way.
	res, err = c.copy("three", copyOptions{osc52: config.OSC52Always})
	if err != nil || !res.native || res.via.route != routeTmuxPassthrough || len(tmux.sent) != 1 {
		t.Fatalf("always copy = %+v, %v", res, err)
	}
	tmux.err = &tmuxRouteError{reason: "allow-passthrough off"}
	res, err = c.copy("four", copyOptions{osc52: config.OSC52Always})
	if err != nil || !res.native || res.terminal || res.termErr == nil {
		t.Fatalf("always copy with a failing tmux leg = %+v, %v", res, err)
	}

	// $TMUX inherited by a terminal that named itself: nothing goes to the
	// terminal, raw or through tmux.
	tmux.err = nil
	c = &systemClipboard{native: &fakeNative{}, terminal: terminalRoutes{tty: tty, tmux: tmux}}
	before := len(tty.sent) + len(tmux.sent)
	_, err = c.copy("five", copyOptions{osc52: config.OSC52Auto, terminal: terminalOther})
	if !errors.Is(err, errTerminalNotTmux) || len(tty.sent)+len(tmux.sent) != before || c.held != nil {
		t.Fatalf("inherited $TMUX copy: %v (tty %q, tmux %q)", err, tty.sent, tmux.sent)
	}
	if status := "Clipboard error: " + err.Error(); status != "Clipboard error: $TMUX is set, but the terminal isn't tmux (unset TMUX outside tmux)" {
		t.Fatalf("status = %q", status)
	}
	c = &systemClipboard{native: &fakeNative{local: true, marks: true}, terminal: terminalRoutes{tty: tty, tmux: tmux}}
	res, err = c.copy("six", copyOptions{osc52: config.OSC52Always, terminal: terminalOther})
	if err != nil || !res.native || res.terminal || !errors.Is(res.termErr, errTerminalNotTmux) || len(tty.sent)+len(tmux.sent) != before {
		t.Fatalf("always copy with an inherited $TMUX = %+v, %v", res, err)
	}
}

func TestClipboardReportsBothFailures(t *testing.T) {
	c, term := newFakeClipboard(&fakeNative{local: true, writeErr: errors.New("pbcopy failed")})
	term.err = &tmuxRouteError{reason: "allow-passthrough off"}
	_, err := c.copy("synthetic", copyOptions{osc52: config.OSC52Auto})
	var routeErr *tmuxRouteError
	if err == nil || !strings.Contains(err.Error(), "pbcopy failed") || !errors.As(err, &routeErr) {
		t.Fatalf("copy error = %v", err)
	}
}

// With clipboard_osc52 always, the tmux leg of a copy that also went to the
// local clipboard keeps its warnings: the timed clear empties only the
// local clipboard.
func TestAlwaysModeTmuxLegKeepsItsWarnings(t *testing.T) {
	nested := "keetui-probe|3.5a|external|on|/dev/pts/7|tmux-256color|1|1|1|0|1||@1|tmux 3.5a\n" +
		"keetui-window|$1|1|@1|1\nkeetui-client|/dev/pts/7|$1|attached,focused,UTF-8\n"
	for _, tc := range []struct {
		probe    string
		probeErr error
		want     string
	}{
		{tmuxFixtures["3.0a two clients"], nil, "Copied password (also sent to 2 tmux clients via OSC 52)"},
		{tmuxFixtures["3.3a grouped"], nil, "Copied password (also sent via tmux OSC 52)"},
		{nested, nil, "Copied password (also sent via nested tmux OSC 52; outer tmux may keep a copy)"},
		{"", errors.New("tmux: no server running"), "Copied password (also sent via OSC 52 through tmux, unverified)"},
		{tmuxFixtures["3.8 two clients"], nil, "Copied password (also sent via tmux OSC 52)"}, // load-buffer to one client
	} {
		f := &fakeTmux{probe: tc.probe, probeErr: tc.probeErr}
		c := &systemClipboard{native: &fakeNative{local: true, marks: true},
			terminal: terminalRoutes{tty: &fakeTerminal{}, tmux: f.clipboard("%1")}}
		res, err := c.copy(tmuxSecret, copyOptions{transient: true, osc52: config.OSC52Always})
		if err != nil || !res.native || !res.terminal {
			t.Fatalf("copy with %q = %+v, %v", tc.probe, res, err)
		}
		if got := copyStatus("password", res); got != tc.want {
			t.Errorf("status = %q, want %q", got, tc.want)
		}
		if help := clipboardHelp(res, nil); tc.probeErr == nil && !strings.Contains(help, "local clipboard only") {
			t.Errorf("help = %q", help)
		}
	}
}
