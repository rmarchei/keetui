package ui

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rmarchei/keetui/internal/config"
)

// fakeNative stands in for the platform clipboard; tests never touch the
// desktop clipboard.
type fakeNative struct {
	local        bool
	marks        bool
	writeErr     error
	clearErr     error
	value        []byte // simulated clipboard contents
	owned        bool   // false once "another app" copied
	transient    bool
	alsoTerminal bool
	writes       int
	clears       int
	forwarded    bool            // route asks for the terminal copy too, as for a forwarded X display
	sessions     []remoteSession // what route was told, per copy
}

func (f *fakeNative) route(s remoteSession) nativeRoute {
	f.sessions = append(f.sessions, s)
	r := nativeRoute{ok: f.local, alsoTerminal: f.forwarded, forwarded: f.forwarded, dest: "the fake clipboard"}
	if f.forwarded {
		r.display = "localhost:10.0"
	}
	return r
}

func (f *fakeNative) write(value []byte, transient bool, _ nativeRoute) (bool, error) {
	if f.writeErr != nil {
		return false, f.writeErr
	}
	f.value, f.owned, f.transient = append([]byte(nil), value...), true, transient
	f.writes++
	return f.marks, nil
}

func (f *fakeNative) holds(matches func([]byte) bool, alsoTerminal bool) bool {
	f.alsoTerminal = alsoTerminal
	return f.owned && matches(f.value)
}

func (f *fakeNative) clear() error {
	if f.clearErr != nil {
		return f.clearErr
	}
	f.value, f.owned = nil, false
	f.clears++
	return nil
}

// fakeTerminal records terminal copies; clears are recorded as "".
type fakeTerminal struct {
	sent    []string
	err     error
	result  terminalResult
	outcome clearOutcome // what clear reports; clearRequested when unset
}

func (t *fakeTerminal) copy(value []byte) (terminalResult, error) {
	if t.err != nil {
		return terminalResult{}, t.err
	}
	t.sent = append(t.sent, string(value))
	return t.result, nil
}

func (t *fakeTerminal) clear(terminalResult) (clearOutcome, error) {
	if t.err != nil {
		return clearRequested, t.err
	}
	outcome := t.outcome
	if outcome == clearNothing {
		outcome = clearRequested
	}
	if outcome == clearRequested {
		t.sent = append(t.sent, "")
	}
	return outcome, nil
}

func newFakeClipboard(native *fakeNative) (*systemClipboard, *fakeTerminal) {
	term := &fakeTerminal{}
	c := &systemClipboard{terminal: terminalRoutes{tty: term}}
	if native != nil {
		c.native = native
	}
	return c, term
}

func TestClipboardRoutes(t *testing.T) {
	failing := errors.New("pbcopy failed")
	for _, tc := range []struct {
		name         string
		mode         string
		native       *fakeNative
		termErr      error
		wantNative   bool
		wantTerminal bool
		wantErr      bool
	}{
		{"auto local", config.OSC52Auto, &fakeNative{local: true, marks: true}, nil, true, false, false},
		{"auto remote", config.OSC52Auto, &fakeNative{}, nil, false, true, false},
		{"auto no platform backend", config.OSC52Auto, nil, nil, false, true, false},
		{"auto falls back when local fails", config.OSC52Auto, &fakeNative{local: true, writeErr: failing}, nil, false, true, false},
		{"always local", config.OSC52Always, &fakeNative{local: true, marks: true}, nil, true, true, false},
		{"always local, terminal fails", config.OSC52Always, &fakeNative{local: true, marks: true}, errors.New("no tty"), true, false, false},
		{"always remote", config.OSC52Always, &fakeNative{}, nil, false, true, false},
		{"never local", config.OSC52Never, &fakeNative{local: true, marks: true}, nil, true, false, false},
		{"never remote", config.OSC52Never, &fakeNative{}, nil, false, false, true},
		{"never local fails", config.OSC52Never, &fakeNative{local: true, writeErr: failing}, nil, false, false, true},
		{"auto remote, no tty", config.OSC52Auto, &fakeNative{}, errors.New("no tty"), false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, term := newFakeClipboard(tc.native)
			term.err = tc.termErr
			res, err := c.copy("synthetic-secret", copyOptions{osc52: tc.mode})
			if (err != nil) != tc.wantErr || res.native != tc.wantNative || res.terminal != tc.wantTerminal {
				t.Fatalf("copy = %+v, %v", res, err)
			}
			if tc.wantErr {
				if c.held != nil {
					t.Fatal("failed copy was recorded as held")
				}
				return
			}
			if res.native && !res.marked {
				t.Fatal("marked local copy reported as unmarked")
			}
			if got := len(term.sent) == 1 && term.sent[0] == "synthetic-secret"; got != tc.wantTerminal {
				t.Fatalf("terminal writes = %q", term.sent)
			}
		})
	}
}

func TestClipboardTransientFollowsClearTimer(t *testing.T) {
	native := &fakeNative{local: true, marks: true}
	c, _ := newFakeClipboard(native)
	for _, transient := range []bool{true, false} {
		if _, err := c.copy("x", copyOptions{transient: transient, osc52: config.OSC52Auto}); err != nil || native.transient != transient {
			t.Fatalf("transient = %v, want %v (%v)", native.transient, transient, err)
		}
	}
}

func TestClipboardReleaseOnlyClearsOwnCopy(t *testing.T) {
	native := &fakeNative{local: true, marks: true}
	c, term := newFakeClipboard(native)
	if outcome, err := outcomeOf(c.release()); outcome != clearNothing || err != nil {
		t.Fatalf("release without a copy = %v, %v", outcome, err)
	}

	_, _ = c.copy("mine", copyOptions{osc52: config.OSC52Auto})
	if outcome, err := outcomeOf(c.release()); outcome != clearDone || err != nil || native.clears != 1 {
		t.Fatalf("release of own copy = %v, %v (clears %d)", outcome, err, native.clears)
	}
	if outcome, _ := outcomeOf(c.release()); outcome != clearNothing || native.clears != 1 {
		t.Fatal("second release cleared again")
	}

	_, _ = c.copy("mine", copyOptions{osc52: config.OSC52Auto})
	native.owned = false // another app copied
	if outcome, _ := outcomeOf(c.release()); outcome != clearChanged || native.clears != 1 {
		t.Fatalf("release after a foreign copy = %v (clears %d)", outcome, native.clears)
	}

	// Content checks compare against this copy, not just any value.
	_, _ = c.copy("mine", copyOptions{osc52: config.OSC52Auto})
	native.value = []byte("theirs")
	if outcome, _ := outcomeOf(c.release()); outcome != clearChanged || native.clears != 1 {
		t.Fatalf("release of a different value = %v", outcome)
	}
	if len(term.sent) != 0 {
		t.Fatalf("local clears also wrote the terminal: %q", term.sent)
	}

	native.clearErr = errors.New("busy")
	_, _ = c.copy("mine", copyOptions{osc52: config.OSC52Auto})
	if _, err := c.release(); err == nil {
		t.Fatal("clear failure not reported")
	}
}

func TestClipboardReleaseTerminalOnly(t *testing.T) {
	c, term := newFakeClipboard(&fakeNative{})
	_, _ = c.copy("remote-secret", copyOptions{osc52: config.OSC52Auto})
	if outcome, err := outcomeOf(c.release()); outcome != clearRequested || err != nil {
		t.Fatalf("release = %v, %v", outcome, err)
	}
	if len(term.sent) != 2 || term.sent[1] != "" {
		t.Fatalf("terminal writes = %q, want an empty clearing copy", term.sent)
	}
}

// outcomeOf drops the terminal leg of a clear.
func outcomeOf(r clearResult, err error) (clearOutcome, error) { return r.outcome, err }

// With clipboard_osc52 always, the terminal copy made next to the local one
// is cleared after the local one, once, and never when the local clipboard
// changed meanwhile.
func TestClipboardAlwaysModeClearsTheTerminalLeg(t *testing.T) {
	native := &fakeNative{local: true, marks: true}
	c, term := newFakeClipboard(native)
	_, _ = c.copy("both", copyOptions{osc52: config.OSC52Always})
	r, err := c.release()
	if err != nil || r.outcome != clearDone || r.leg != clearRequested || !native.alsoTerminal {
		t.Fatalf("release = %+v, %v (alsoTerminal %v)", r, err, native.alsoTerminal)
	}
	if len(term.sent) != 2 || term.sent[1] != "" {
		t.Fatalf("terminal writes = %q, want the copy and one clear", term.sent)
	}
	if text, isErr := clearStatus(r, nil); text != "Clipboard cleared; terminal clear requested" || isErr {
		t.Fatalf("status = %q, %v", text, isErr)
	}

	// Copied over meanwhile: neither leg is touched.
	_, _ = c.copy("both", copyOptions{osc52: config.OSC52Always})
	native.value = []byte("copied later")
	if r, _ := c.release(); r.outcome != clearChanged || r.leg != clearNothing || len(term.sent) != 3 {
		t.Fatalf("release after a foreign copy = %+v (terminal %q)", r, term.sent)
	}

	// A terminal leg that can't be cleared, or whose clear fails, or that
	// tmux would drop now, is reported, never retried.
	for _, tc := range []struct {
		result  terminalResult
		outcome clearOutcome
		err     error
		leg     clearOutcome
		want    string
	}{
		{terminalResult{route: routeTmuxLoadBuffer, noClear: "allow-passthrough off"}, clearNothing, nil, clearUnsupported,
			"Clipboard cleared; tmux can't clear the terminal copy"},
		{terminalResult{route: routeTmuxPassthrough}, clearDeferred, nil, clearDeferred,
			"Clipboard cleared; terminal copy left: tmux would drop it"},
		{terminalResult{route: routeTmuxPassthrough}, clearDetached, nil, clearDetached,
			"Clipboard cleared; terminal copy left: its terminal detached"},
		{terminalResult{route: routeHerdr}, clearNoRemoteClient, nil, clearNoRemoteClient,
			"Clipboard cleared; terminal copy left: no herdr client over SSH"},
		{terminalResult{route: routeHerdr}, clearOverwritten, nil, clearOverwritten,
			"Clipboard cleared; terminal copy overwritten with a space"},
		{terminalResult{}, clearRequested, errors.New("terminal clipboard: no tty"), clearRequested,
			"Clipboard cleared; terminal clear failed: terminal clipboard: no tty"},
	} {
		term := &fakeTerminal{result: tc.result, outcome: tc.outcome}
		c := &systemClipboard{native: native, terminal: terminalRoutes{tty: term}}
		_, _ = c.copy("both", copyOptions{osc52: config.OSC52Always})
		term.err = tc.err
		r, err := c.release()
		if err != nil || r.outcome != clearDone || r.leg != tc.leg || c.held != nil {
			t.Fatalf("%+v: release = %+v, %v (held %v)", tc.result, r, err, c.held != nil)
		}
		if text, _ := clearStatus(r, nil); text != tc.want {
			t.Errorf("%+v: status = %q, want %q", tc.result, text, tc.want)
		}
		if r.leg != clearRequested && clearDetail(r) == "" {
			t.Errorf("%+v: no F1 detail", tc.result)
		}
	}
}

func TestClipboardFailedCopyKeepsPreviousCopy(t *testing.T) {
	native := &fakeNative{local: true, marks: true}
	c, _ := newFakeClipboard(native)
	_, _ = c.copy("first", copyOptions{osc52: config.OSC52Never})
	native.writeErr = errors.New("unavailable")
	if _, err := c.copy("second", copyOptions{osc52: config.OSC52Never}); err == nil {
		t.Fatal("expected copy failure")
	}
	native.writeErr = nil
	if outcome, _ := outcomeOf(c.release()); outcome != clearDone || native.clears != 1 {
		t.Fatalf("previous copy not released: %v", outcome)
	}
}

func TestHeldCopyDoesNotKeepValue(t *testing.T) {
	value := []byte("synthetic-secret-value")
	h := newHeldCopy(value, copyResult{native: true}, nil)
	if bytes.Contains(h.key, value) || bytes.Contains(h.sum, value) {
		t.Fatal("held copy stores the value")
	}
	if !h.matches(value) || h.matches([]byte("synthetic-secret-valuf")) {
		t.Fatal("keyed hash does not identify the copy")
	}
	if other := newHeldCopy(value, copyResult{}, nil); bytes.Equal(other.sum, h.sum) {
		t.Fatal("keyed hash is not per copy")
	}
}

func TestReleaseClipboardAfterExit(t *testing.T) {
	ReleaseClipboard() // nothing copied: a no-op
	native := &fakeNative{local: true, marks: true}
	c, _ := newFakeClipboard(native)
	prev := processClipboard.Swap(c)
	t.Cleanup(func() { processClipboard.Store(prev) })
	_, _ = c.copy("left-behind", copyOptions{osc52: config.OSC52Auto})
	ReleaseClipboard()
	if native.clears != 1 || c.held != nil {
		t.Fatal("ReleaseClipboard left keetui's copy on the clipboard")
	}
}

func TestCopyStatus(t *testing.T) {
	for _, tc := range []struct {
		res  copyResult
		want string
	}{
		{copyResult{native: true, marked: true}, "Copied password!"},
		{copyResult{native: true}, "Copied password (clipboard managers may keep it)"},
		{copyResult{native: true, marked: true, terminal: true}, "Copied password (also sent via OSC 52)"},
		{copyResult{terminal: true}, "Sent password to the terminal clipboard (OSC 52)"},
	} {
		if got := copyStatus("password", tc.res); got != tc.want {
			t.Errorf("copyStatus(%+v) = %q, want %q", tc.res, got, tc.want)
		}
	}
}

func TestCopyUsesConfiguredPolicy(t *testing.T) {
	m := createTestApp(t)
	clipboard := m.clipboard.(*testClipboard)
	m.cfg.ClipboardOSC52 = config.OSC52Never
	m = copyForCountdown(t, m, "synthetic")
	if clipboard.opts != (copyOptions{transient: true, osc52: config.OSC52Never}) {
		t.Fatalf("copy options = %+v", clipboard.opts)
	}
	m.cfg.ClipboardClearSeconds, m.cfg.ClipboardOSC52 = 0, ""
	copyForCountdown(t, m, "synthetic")
	if clipboard.opts != (copyOptions{osc52: config.OSC52Auto}) {
		t.Fatalf("copy options without a clear timer = %+v", clipboard.opts)
	}
}

func TestClearStatusReportsOutcome(t *testing.T) {
	m := copyForCountdown(t, createTestApp(t), "synthetic")
	updated, cmd := m.Update(clipboardClearMsg{gen: m.clipboardGen})
	m = updated.(AppModel)
	if msg := cmd().(StatusMsg); msg.Text != "Clipboard cleared" || msg.IsError {
		t.Fatalf("status = %+v", msg)
	}
	m = copyForCountdown(t, m, "synthetic")
	m.clipboard.(*testClipboard).value = "copied elsewhere"
	_, cmd = m.Update(clipboardClearMsg{gen: m.clipboardGen})
	if msg := cmd().(StatusMsg); !strings.Contains(msg.Text, "left untouched") {
		t.Fatalf("status = %+v", msg)
	}
}

func TestAutoLockLeavesForeignClipboardAlone(t *testing.T) {
	m := copyForCountdown(t, createTestApp(t), "synthetic")
	clipboard := m.clipboard.(*testClipboard)
	clipboard.value = "copied after keetui"
	m.lastActivity = m.lastActivity.Add(-10 * time.Minute)
	updated, _ := m.Update(autoLockMsg{gen: m.autoLockGen})
	if updated.(AppModel).state != viewUnlock {
		t.Fatal("database did not lock")
	}
	if clipboard.value != "copied after keetui" || clipboard.clears != 0 {
		t.Fatal("auto-lock wiped content keetui did not copy")
	}
}

// A copy that does not replace the previous one takes over its timed clear,
// so it clears what the previous one left: the local copy when the session
// turned remote, and the terminal copy when it turned local again. A local
// clipboard that another app has written since is left alone.
func TestCopyClearsWhatTheNextCopyDidNotReplace(t *testing.T) {
	native := &fakeNative{local: true, marks: true}
	c, term := newFakeClipboard(native)
	copyNow := func(value string) copyResult {
		t.Helper()
		res, err := c.copy(value, copyOptions{osc52: config.OSC52Auto})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	copyNow("synthetic-first")
	native.local = false // attached from elsewhere
	if res := copyNow("synthetic-second"); res.native || !res.terminal {
		t.Fatalf("remote copy = %+v", res)
	}
	if native.owned || native.clears != 1 {
		t.Fatalf("local copy left behind: %q (clears %d)", native.value, native.clears)
	}
	native.local = true // back at this machine
	if res := copyNow("synthetic-third"); !res.native || res.terminal {
		t.Fatalf("local copy = %+v", res)
	}
	if len(term.sent) != 2 || term.sent[0] != "synthetic-second" || term.sent[1] != "" {
		t.Fatalf("terminal got %q, want the remote copy cleared", term.sent)
	}

	// Another app copied after keetui: the remote copy leaves it be.
	native.owned, native.value = false, []byte("copied-elsewhere")
	native.local = false
	copyNow("synthetic-fourth")
	if string(native.value) != "copied-elsewhere" || native.clears != 1 {
		t.Fatalf("clipboard = %q (clears %d), want the other app's copy kept", native.value, native.clears)
	}
}

// A clear that fails keeps the copy tracked, so the next release (lock,
// quit, exit) clears it once the clipboard works again.
func TestFailedClearIsRetried(t *testing.T) {
	native := &fakeNative{local: true, marks: true, clearErr: errors.New("display unavailable")}
	c, _ := newFakeClipboard(native)
	if _, err := c.copy("synthetic-secret", copyOptions{osc52: config.OSC52Auto}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.release(); err == nil {
		t.Fatal("the clear should have failed")
	}
	native.clearErr = nil
	if r, err := c.release(); err != nil || r.outcome != clearDone || native.owned {
		t.Fatalf("retry = %+v, %v (still on the clipboard: %v)", r, err, native.owned)
	}
	if r, err := c.release(); err != nil || r.outcome != clearNothing {
		t.Fatalf("after the retry: %+v, %v, want nothing left to clear", r, err)
	}
}
