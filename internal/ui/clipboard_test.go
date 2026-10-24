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
}

func (f *fakeNative) available() bool { return f.local }

func (f *fakeNative) write(value []byte, transient bool) (bool, error) {
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

type fakeTerminal struct {
	sent []string
	err  error
}

func (t *fakeTerminal) write(s string) error {
	if t.err != nil {
		return t.err
	}
	t.sent = append(t.sent, s)
	return nil
}

func newFakeClipboard(native *fakeNative) (*systemClipboard, *fakeTerminal) {
	term := &fakeTerminal{}
	c := &systemClipboard{terminal: term.write}
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
	if outcome, err := c.release(); outcome != clearNothing || err != nil {
		t.Fatalf("release without a copy = %v, %v", outcome, err)
	}

	_, _ = c.copy("mine", copyOptions{osc52: config.OSC52Auto})
	if outcome, err := c.release(); outcome != clearDone || err != nil || native.clears != 1 {
		t.Fatalf("release of own copy = %v, %v (clears %d)", outcome, err, native.clears)
	}
	if outcome, _ := c.release(); outcome != clearNothing || native.clears != 1 {
		t.Fatal("second release cleared again")
	}

	_, _ = c.copy("mine", copyOptions{osc52: config.OSC52Auto})
	native.owned = false // another app copied
	if outcome, _ := c.release(); outcome != clearChanged || native.clears != 1 {
		t.Fatalf("release after a foreign copy = %v (clears %d)", outcome, native.clears)
	}

	// Content checks compare against this copy, not just any value.
	_, _ = c.copy("mine", copyOptions{osc52: config.OSC52Auto})
	native.value = []byte("theirs")
	if outcome, _ := c.release(); outcome != clearChanged || native.clears != 1 {
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
	if outcome, err := c.release(); outcome != clearRequested || err != nil {
		t.Fatalf("release = %v, %v", outcome, err)
	}
	if len(term.sent) != 2 || term.sent[1] != "" {
		t.Fatalf("terminal writes = %q, want an empty clearing copy", term.sent)
	}
}

func TestClipboardAlwaysModeChecksContent(t *testing.T) {
	native := &fakeNative{local: true, marks: true}
	c, term := newFakeClipboard(native)
	_, _ = c.copy("both", copyOptions{osc52: config.OSC52Always})
	if outcome, _ := c.release(); outcome != clearDone || !native.alsoTerminal {
		t.Fatalf("release = %v, alsoTerminal = %v", outcome, native.alsoTerminal)
	}
	if len(term.sent) != 1 {
		t.Fatalf("local clear also sent an OSC 52 clear: %q", term.sent)
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
	if outcome, _ := c.release(); outcome != clearDone || native.clears != 1 {
		t.Fatalf("previous copy not released: %v", outcome)
	}
}

func TestHeldCopyDoesNotKeepValue(t *testing.T) {
	value := []byte("synthetic-secret-value")
	h := newHeldCopy(value, copyResult{native: true})
	if bytes.Contains(h.key, value) || bytes.Contains(h.sum, value) {
		t.Fatal("held copy stores the value")
	}
	if !h.matches(value) || h.matches([]byte("synthetic-secret-valuf")) {
		t.Fatal("keyed hash does not identify the copy")
	}
	if other := newHeldCopy(value, copyResult{}); bytes.Equal(other.sum, h.sum) {
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
