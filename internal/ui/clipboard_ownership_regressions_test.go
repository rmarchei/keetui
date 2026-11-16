package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rmarchei/keetui/internal/config"
)

// A fallback succeeded even though the native write failed: the new timed
// clear belongs to the terminal copy, so the previous local copy must be
// cleared now, while a value from another app must survive.
func TestClipboardFallbackRetiresOnlyOwnedNativeCopy(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		name := "owned"
		if foreign {
			name = "replaced by another app"
		}
		t.Run(name, func(t *testing.T) {
			native := &fakeNative{local: true, marks: true}
			c, terminal := newFakeClipboard(native)
			if _, err := c.copy("synthetic-local", copyOptions{osc52: config.OSC52Auto}); err != nil {
				t.Fatal(err)
			}
			if foreign {
				native.owned, native.value = false, []byte("another application's copy")
			}
			native.writeErr = errors.New("native clipboard temporarily unavailable")
			result, err := c.copy("synthetic-terminal", copyOptions{osc52: config.OSC52Auto})
			if err != nil || result.native || !result.terminal {
				t.Fatalf("fallback copy = %+v, %v", result, err)
			}
			if foreign {
				if string(native.value) != "another application's copy" || native.clears != 0 {
					t.Fatal("fallback cleared another application's copy")
				}
			} else if len(native.value) != 0 || native.clears != 1 {
				t.Fatal("fallback left the superseded native secret on the clipboard")
			}
			if result, err := c.release(); err != nil || result.outcome != clearRequested {
				t.Fatalf("release fallback = %+v, %v", result, err)
			}
			if len(terminal.sent) != 2 || terminal.sent[0] != "synthetic-terminal" || terminal.sent[1] != "" {
				t.Fatal("fallback's timed release did not clear its terminal copy")
			}
		})
	}
}

// Retaining ownership after a failed clear must not grant permission to
// clear whatever another app copied before the next lock or quit.
func TestClipboardFailedClearRechecksOwnershipBeforeRetry(t *testing.T) {
	for _, policy := range []string{config.OSC52Auto, config.OSC52Always} {
		t.Run(policy, func(t *testing.T) {
			native := &fakeNative{local: true, marks: true, clearErr: errors.New("temporarily busy")}
			c, terminal := newFakeClipboard(native)
			if _, err := c.copy("synthetic-secret", copyOptions{osc52: policy}); err != nil {
				t.Fatal(err)
			}
			if _, err := c.release(); err == nil {
				t.Fatal("fixture did not fail the first clear")
			}
			terminalWrites := len(terminal.sent)
			native.clearErr = nil
			native.owned, native.value = false, []byte("another application's copy")
			if result, err := c.release(); err != nil || result.outcome != clearChanged {
				t.Fatalf("release after replacement = %+v, %v", result, err)
			}
			if string(native.value) != "another application's copy" || native.clears != 0 || len(terminal.sent) != terminalWrites {
				t.Fatal("retry overwrote a clipboard that changed after the failure")
			}
			if result, err := c.release(); err != nil || result.outcome != clearNothing {
				t.Fatalf("later release = %+v, %v", result, err)
			}
		})
	}
}

func TestClipboardPairedCopyRetriesNativeBeforeClearingTerminal(t *testing.T) {
	native := &fakeNative{local: true, marks: true, clearErr: errors.New("temporarily busy")}
	c, terminal := newFakeClipboard(native)
	if _, err := c.copy("synthetic-secret", copyOptions{osc52: config.OSC52Always}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.release(); err == nil {
		t.Fatal("fixture did not fail the first clear")
	}
	if len(terminal.sent) != 1 || terminal.sent[0] != "synthetic-secret" {
		t.Fatal("terminal copy was cleared before its paired native clear succeeded")
	}
	native.clearErr = nil
	if result, err := c.release(); err != nil || result.outcome != clearDone || result.leg != clearRequested {
		t.Fatalf("paired retry = %+v, %v", result, err)
	}
	if native.clears != 1 || native.owned || len(terminal.sent) != 2 || terminal.sent[1] != "" {
		t.Fatal("successful retry did not clear both copies once")
	}
	if result, err := c.release(); err != nil || result.outcome != clearNothing || len(terminal.sent) != 2 {
		t.Fatalf("paired release was repeated: %+v, %v", result, err)
	}
}

func copyWithSupersededNativeClearFailure(t *testing.T) (*systemClipboard, *fakeNative, *fakeTerminal, error) {
	t.Helper()
	native := &fakeNative{local: true, marks: true}
	c, terminal := newFakeClipboard(native)
	if _, err := c.copy("synthetic-local", copyOptions{osc52: config.OSC52Auto}); err != nil {
		t.Fatal(err)
	}
	native.local = false
	failure := errors.New("native clipboard temporarily unavailable")
	native.clearErr = failure
	result, err := c.copy("synthetic-terminal", copyOptions{osc52: config.OSC52Auto})
	if err != nil || result.native || !result.terminal || !native.owned {
		t.Fatalf("route change = %+v, %v (old native copy owned: %v)", result, err, native.owned)
	}
	return c, native, terminal, failure
}

func TestClipboardSupersededNativeClearRetriesWithoutBlockingTerminalClear(t *testing.T) {
	c, native, terminal, failure := copyWithSupersededNativeClearFailure(t)
	result, err := c.release()
	if !errors.Is(err, failure) || result.outcome != clearRequested {
		t.Fatalf("release while native unavailable = %+v, %v", result, err)
	}
	if len(terminal.sent) != 2 || terminal.sent[0] != "synthetic-terminal" || terminal.sent[1] != "" {
		t.Fatal("old native clear failure blocked the current terminal clear")
	}
	if !native.owned || string(native.value) != "synthetic-local" {
		t.Fatal("fixture lost the old native copy before recovery")
	}
	native.clearErr = nil
	if result, err := c.release(); err != nil || result.outcome != clearDone || native.owned || native.clears != 1 {
		t.Fatalf("release after native recovery = %+v, %v (clears %d)", result, err, native.clears)
	}
	if result, err := c.release(); err != nil || result.outcome != clearNothing || len(terminal.sent) != 2 {
		t.Fatalf("completed clears were repeated: %+v, %v", result, err)
	}
}

func TestClipboardSupersededNativeRetryLeavesForeignCopyUntouched(t *testing.T) {
	c, native, terminal, _ := copyWithSupersededNativeClearFailure(t)
	native.clearErr = nil
	native.owned, native.value = false, []byte("another application's copy")
	if result, err := c.release(); err != nil || result.outcome != clearRequested {
		t.Fatalf("release after native replacement = %+v, %v", result, err)
	}
	if string(native.value) != "another application's copy" || native.clears != 0 || len(terminal.sent) != 2 || terminal.sent[1] != "" {
		t.Fatal("release failed to preserve the foreign copy and clear the terminal copy")
	}
	if result, err := c.release(); err != nil || result.outcome != clearNothing {
		t.Fatalf("foreign copy remained a cleanup target: %+v, %v", result, err)
	}
}

func TestClipboardNewNativeWriteReplacesPendingSupersededCopy(t *testing.T) {
	c, native, terminal, _ := copyWithSupersededNativeClearFailure(t)
	native.local, native.clearErr = true, nil
	result, err := c.copy("synthetic-new-local", copyOptions{osc52: config.OSC52Auto})
	if err != nil || !result.native || result.terminal {
		t.Fatalf("new native copy = %+v, %v", result, err)
	}
	if !native.owned || string(native.value) != "synthetic-new-local" || native.clears != 0 {
		t.Fatal("pending cleanup erased the successful new native write")
	}
	if len(terminal.sent) != 2 || terminal.sent[1] != "" {
		t.Fatal("new native copy did not retire the old terminal copy")
	}
	if result, err := c.release(); err != nil || result.outcome != clearDone || native.clears != 1 {
		t.Fatalf("release new native copy = %+v, %v (clears %d)", result, err, native.clears)
	}
}

func TestClipboardNewTerminalCopyRetriesPendingNativeClear(t *testing.T) {
	c, native, terminal, _ := copyWithSupersededNativeClearFailure(t)
	native.clearErr = nil
	if result, err := c.copy("synthetic-new-terminal", copyOptions{osc52: config.OSC52Auto}); err != nil || result.native || !result.terminal {
		t.Fatalf("new terminal copy = %+v, %v", result, err)
	}
	if native.owned || native.clears != 1 {
		t.Fatal("a later terminal copy did not retry the old native cleanup")
	}
	if result, err := c.release(); err != nil || result.outcome != clearRequested || len(terminal.sent) != 3 || terminal.sent[2] != "" {
		t.Fatalf("release latest terminal copy = %+v, %v (writes %q)", result, err, terminal.sent)
	}
}

func TestClipboardPreviousNativeErrorPreservesPendingTerminalRetries(t *testing.T) {
	for _, pending := range []struct {
		name    string
		outcome clearOutcome
	}{
		{"hidden terminal", clearDeferred},
		{"away terminal", clearAway},
	} {
		for _, recovery := range []string{"native recovers", "native stays unavailable"} {
			t.Run(pending.name+"/"+recovery, func(t *testing.T) {
				m := createTestApp(t)
				native := &fakeNative{local: true, marks: true}
				clipboard, terminal := newFakeClipboard(native)
				m.clipboard = clipboard
				updated, _ := m.Update(CopyToClipboardMsg{Field: "password", Value: "synthetic-local"})
				m = updated.(AppModel)
				native.local = false
				native.clearErr = errors.New("native temporarily unavailable")
				terminal.result = terminalResult{route: routeTmuxPassthrough}
				terminal.outcome = pending.outcome
				updated, _ = m.Update(CopyToClipboardMsg{Field: "password", Value: "synthetic-terminal"})
				m = updated.(AppModel)
				updated, cmd := m.Update(clipboardClearMsg{gen: m.clipboardGen})
				m = updated.(AppModel)
				status := statusOf(t, cmd)
				updated, _ = m.Update(status)
				m = updated.(AppModel)
				if !m.clipboardDeferred || !m.securityTickPending || status.IsError || !pendingClearStatus(status.Text) {
					t.Fatalf("native error stopped pending terminal retries: deferred=%v, tick=%v, status=%+v", m.clipboardDeferred, m.securityTickPending, status)
				}
				tick := func() {
					t.Helper()
					updated, _ := m.Update(securityTickMsg{gen: m.securityTickGen, at: m.securityNow.Add(time.Second)})
					m = updated.(AppModel)
				}
				tick()
				if !m.clipboardDeferred || !m.securityTickPending || m.statusError || len(terminal.sent) != 1 {
					t.Fatal("a pending terminal retry stopped or sent a premature clear")
				}
				if recovery == "native recovers" {
					native.clearErr = nil
				}
				terminal.outcome = clearRequested
				tick()
				if m.clipboardDeferred || len(terminal.sent) != 2 || terminal.sent[1] != "" {
					t.Fatal("reachable terminal was not cleared exactly once")
				}
				if recovery == "native recovers" {
					if native.owned || native.clears != 1 || m.statusError || m.statusText != "Clipboard clear requested" {
						t.Fatalf("recovered cleanup: clears=%d, status=%q, error=%v", native.clears, m.statusText, m.statusError)
					}
				} else {
					if !native.owned || !m.statusError || !strings.Contains(m.statusText, "native temporarily unavailable") {
						t.Fatalf("completed terminal clear hid the native error: %q", m.statusText)
					}
					native.clearErr = nil
					if result, err := clipboard.release(); err != nil || result.outcome != clearDone || native.owned {
						t.Fatalf("explicit native retry after terminal cleanup = %+v, %v", result, err)
					}
				}
				tick()
				if len(terminal.sent) != 2 {
					t.Fatal("completed terminal cleanup was sent again")
				}
			})
		}
	}
}

// twoClipboards is a backend with two clipboards, as Linux has Wayland's
// and each X display's: route picks the one in use, and a write to the
// other one leaves the last copy in place.
type twoClipboards struct {
	use, last int
	clip      [2]fakeNative
}

func newTwoClipboards() *twoClipboards {
	t := &twoClipboards{}
	t.clip[0].marks, t.clip[1].marks = true, true
	return t
}

func (t *twoClipboards) route(remoteSession) nativeRoute {
	return nativeRoute{ok: true, dest: "one of two clipboards"}
}

func (t *twoClipboards) write(value []byte, transient bool, r nativeRoute) (bool, error) {
	marked, err := t.clip[t.use].write(value, transient, r)
	if err == nil {
		t.last = t.use
	}
	return marked, err
}

func (t *twoClipboards) holds(matches func([]byte) bool, alsoTerminal bool) bool {
	return t.clip[t.last].holds(matches, alsoTerminal)
}

func (t *twoClipboards) clear() error { return t.clip[t.last].clear() }

func (t *twoClipboards) elsewhere(nativeRoute) nativeCopy {
	if t.use == t.last {
		return nil
	}
	return &t.clip[t.last]
}

// A local copy to another clipboard than the last one leaves that copy in
// place: it is cleared right away, after an ownership check, and a clear
// that fails keeps it tracked, so that the release reports the failure,
// never a clean clear, and retries it.
func TestClipboardSwitchClearsTheCopyLeftOnTheOtherClipboard(t *testing.T) {
	opts := copyOptions{transient: true, osc52: config.OSC52Never}
	t.Run("cleared", func(t *testing.T) {
		native := newTwoClipboards()
		c, _ := newFakeClipboard(nil)
		c.native = native
		if _, err := c.copy("synthetic-one", opts); err != nil {
			t.Fatal(err)
		}
		native.use = 1
		if res, err := c.copy("synthetic-two", opts); err != nil || !res.native {
			t.Fatalf("copy = %+v, %v", res, err)
		}
		if native.clip[0].owned || native.clip[0].clears != 1 || string(native.clip[1].value) != "synthetic-two" {
			t.Fatalf("clipboards hold %q and %q", native.clip[0].value, native.clip[1].value)
		}
		if r, err := c.release(); err != nil || r.outcome != clearDone || native.clip[1].owned {
			t.Fatalf("release = %+v, %v", r, err)
		}
		if r, err := c.release(); err != nil || r.outcome != clearNothing {
			t.Fatalf("later release = %+v, %v", r, err)
		}
	})
	t.Run("replaced by another app", func(t *testing.T) {
		native := newTwoClipboards()
		c, _ := newFakeClipboard(nil)
		c.native = native
		if _, err := c.copy("synthetic-one", opts); err != nil {
			t.Fatal(err)
		}
		native.clip[0].owned, native.clip[0].value = false, []byte("another application's copy")
		native.use = 1
		if _, err := c.copy("synthetic-two", opts); err != nil {
			t.Fatal(err)
		}
		if string(native.clip[0].value) != "another application's copy" || native.clip[0].clears != 0 {
			t.Fatal("the switch cleared another application's copy")
		}
	})
	t.Run("clear fails", func(t *testing.T) {
		native := newTwoClipboards()
		c, _ := newFakeClipboard(nil)
		c.native = native
		if _, err := c.copy("synthetic-one", opts); err != nil {
			t.Fatal(err)
		}
		failure := errors.New("clipboard temporarily unavailable")
		native.clip[0].clearErr = failure
		native.use = 1
		if _, err := c.copy("synthetic-two", opts); err != nil {
			t.Fatal(err)
		}
		if !native.clip[0].owned {
			t.Fatal("fixture cleared the first copy")
		}
		r, err := c.release()
		if text, isError := clearStatus(r, err); !errors.Is(err, failure) || !isError || native.clip[1].owned {
			t.Fatalf("release = %+v, %v (status %q)", r, err, text)
		}
		native.clip[0].clearErr = nil
		if r, err := c.release(); err != nil || r.outcome != clearDone || native.clip[0].owned {
			t.Fatalf("retry = %+v, %v", r, err)
		}
		if r, err := c.release(); err != nil || r.outcome != clearNothing || native.clip[0].clears != 1 {
			t.Fatalf("later release = %+v, %v", r, err)
		}
	})
	t.Run("clear fails, then a copy there", func(t *testing.T) {
		native := newTwoClipboards()
		c, _ := newFakeClipboard(nil)
		c.native = native
		if _, err := c.copy("synthetic-one", opts); err != nil {
			t.Fatal(err)
		}
		native.clip[0].clearErr = errors.New("clipboard temporarily unavailable")
		native.use = 1
		if _, err := c.copy("synthetic-two", opts); err != nil {
			t.Fatal(err)
		}
		// Back on the first clipboard: the new copy replaced the one left
		// there, and the second one is left in turn.
		native.use = 0
		if _, err := c.copy("synthetic-three", opts); err != nil {
			t.Fatal(err)
		}
		if string(native.clip[0].value) != "synthetic-three" || native.clip[1].owned {
			t.Fatalf("clipboards hold %q and %q", native.clip[0].value, native.clip[1].value)
		}
		native.clip[0].clearErr = nil
		if r, err := c.release(); err != nil || r.outcome != clearDone || native.clip[0].owned || native.clip[0].clears != 1 {
			t.Fatalf("release = %+v, %v", r, err)
		}
	})
}
