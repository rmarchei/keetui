package ui

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rmarchei/keetui/internal/config"
)

// Clipboard copies prefer the local clipboard, where the platform backend can
// mark them as sensitive (excluded from clipboard history). The terminal
// clipboard (OSC 52) cannot be marked, and session recorders may keep it, so
// it is used according to clipboard_osc52, and routed so that tmux keeps no
// copy in its paste buffers (see clipboard_terminal.go). Whether the local
// clipboard is the user's is decided for each copy (see clipboard_remote.go).
//
// Backends remember their last copy (as a keyed hash, never the value), so
// clearing only ever removes keetui's own copy.

type copyOptions struct {
	transient bool         // an automatic clear is scheduled
	osc52     string       // config.OSC52Auto, OSC52Always or OSC52Never
	terminal  terminalKind // what the terminal answered to XTVERSION
}

type copyResult struct {
	native   bool           // written to the local clipboard
	marked   bool           // the local copy carries sensitive markers
	terminal bool           // sent to the terminal with OSC 52
	via      terminalResult // how the terminal copy went
	termErr  error          // why the terminal copy failed next to a local one

	// Where the copy went and why, for F1.
	session   remoteSession
	local     nativeRoute // the local clipboard's route for this copy
	nativeErr error       // why the local copy failed
}

// autoClear reports whether a timed clear can remove the copy.
func (r copyResult) autoClear() bool { return r.native || r.terminal && r.via.clearable() }

type clearOutcome int

const (
	clearNothing        clearOutcome = iota // no keetui copy was outstanding
	clearDone                               // keetui's copy was removed
	clearChanged                            // the clipboard holds something else; left untouched
	clearRequested                          // OSC 52 only: an empty copy was sent, unverifiable
	clearUnsupported                        // the terminal copy cannot be cleared (tmux)
	clearDeferred                           // tmux would drop the clear now; the copy is kept for a retry
	clearAway                               // as clearDeferred: a tmux client that got the copy is attached, but out of reach
	clearDetached                           // the tmux client that got the copy left; others would get the clear
	clearReattached                         // as clearRequested, sent to a terminal like the one that left
	clearPartial                            // as clearRequested, but some tmux clients that got the copy left
	clearOverwritten                        // herdr: a single space was copied over it, as herdr drops an empty copy
	clearNoRemoteClient                     // herdr: nothing sent, as no herdr client runs over SSH
	clearHerdrLocal                         // herdr: the terminal copy went to a local client, onto the local clipboard just cleared
)

// pending reports a clear that could not be sent yet: the copy is kept and
// the clear retried.
func (o clearOutcome) pending() bool { return o == clearDeferred || o == clearAway }

// clearResult is what a clear did.
type clearResult struct {
	outcome clearOutcome // keetui's copy: the local one, or a terminal-only one
	leg     clearOutcome // the terminal copy sent next to a local one; clearNothing for none
	legErr  error        // why the terminal leg's clear failed
	// repaint asks for a full redraw: the clear spoke to the local clipboard
	// in a remote session, an X display forwarded over SSH, and the ssh
	// client prints over the frame when that display has gone away.
	repaint bool
}

func (r clearResult) pending() bool { return r.outcome.pending() }

// Keep clipboard I/O injectable so timer regressions never access the user's
// real clipboard. A nil AppModel backend uses the process-wide one.
type clipboardAccess interface {
	copy(value string, opts copyOptions) (copyResult, error)
	// release clears the clipboard if it still holds the last copy, then
	// forgets that copy.
	release() (clearResult, error)
}

// nativeClipboard is the platform's local clipboard.
type nativeClipboard interface {
	// route decides whether and where a copy goes on the local clipboard,
	// which is the user's only while the user is at this machine.
	route(s remoteSession) nativeRoute
	write(value []byte, transient bool, r nativeRoute) (marked bool, err error)
	// holds reports whether the clipboard still holds the last write.
	// matches checks content where ownership cannot be tracked otherwise;
	// alsoTerminal means an OSC 52 copy of the same value may have replaced
	// the write.
	holds(matches func([]byte) bool, alsoTerminal bool) bool
	clear() error
}

// nativeCopy is a copy on the local clipboard, as holds and clear see it: a
// nativeClipboard's last write, or a copy that elsewhere returned.
type nativeCopy interface {
	holds(matches func([]byte) bool, alsoTerminal bool) bool
	clear() error
}

// nativeClipboards is a nativeClipboard with more than one clipboard
// (Linux: a Wayland compositor's, and each X display's), where a write to
// one leaves the copy on another in place.
type nativeClipboards interface {
	// elsewhere returns the last write's copy, to check and clear on its
	// own, when a write along r would leave it in place on another
	// clipboard; nil when the write would replace it.
	elsewhere(r nativeRoute) nativeCopy
}

// nativeRoute is where a copy goes on the local clipboard.
type nativeRoute struct {
	ok           bool     // the local clipboard is the user's
	alsoTerminal bool     // with auto, the terminal gets the copy too: the user may not be able to paste the local one
	dest         string   // where the copy goes, for F1
	why          string   // with !ok: why the local clipboard is not used, for F1
	display      string   // Linux: the X display the copy goes to; "" for Wayland
	authority    string   // Linux: the X authority file for display; "" for keetui's own
	forwarded    bool     // display is one that the SSH session forwards
	client       int      // Linux: the herdr client whose display the copy goes to (see localRoute); 0 for keetui's own
	env          []string // Linux: what the clipboard tools need set to reach the client's display
}

var errNoClipboard = errors.New("no clipboard available")

type systemClipboard struct {
	mu       sync.Mutex
	native   nativeClipboard      // nil when the platform has none
	terminal terminalRoutes       // OSC 52
	detect   func() remoteSession // nil: the user is local
	held     *heldCopy
	// Local copies that a later copy did not replace: the one a copy that
	// went only to the terminal left behind (the session turned remote), and
	// those a local copy left on another clipboard (Linux: Wayland, then a
	// forwarded X display). Each keeps its ownership check, independently of
	// the new copy, until its clear succeeds.
	previousNative []*heldCopy
}

type heldCopy struct {
	result   copyResult
	terminal terminalClipboard // the route that sent the terminal copy
	// native is the local copy once a write to another clipboard left it in
	// place (see nativeClipboards); nil while it is the backend's last write.
	native   nativeCopy
	key, sum []byte
}

func newHeldCopy(value []byte, result copyResult, terminal terminalClipboard) *heldCopy {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	return &heldCopy{result: result, terminal: terminal, key: key, sum: copyMAC(key, value)}
}

func copyMAC(key, value []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(value)
	return mac.Sum(nil)
}

func (h *heldCopy) matches(value []byte) bool {
	return hmac.Equal(copyMAC(h.key, value), h.sum)
}

func (c *systemClipboard) session() remoteSession {
	if c.detect == nil {
		return remoteSession{}
	}
	return c.detect()
}

func (c *systemClipboard) copy(value string, opts copyOptions) (copyResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Attachments change at any time, so each copy looks again.
	res := copyResult{session: c.session()}
	if c.native != nil {
		res.local = c.native.route(res.session)
	}
	useNative := res.local.ok
	useTerminal := opts.osc52 == config.OSC52Always ||
		opts.osc52 != config.OSC52Never && (!useNative || res.local.alsoTerminal)
	var err error
	var left nativeCopy // the last local copy, which this one leaves on another clipboard
	if useNative {
		if n, ok := c.native.(nativeClipboards); ok {
			left = n.elsewhere(res.local)
		}
		if res.marked, err = c.native.write([]byte(value), opts.transient, res.local); err == nil {
			res.native = true
		} else {
			res.nativeErr = err
			if opts.osc52 != config.OSC52Never {
				useTerminal = true
			}
		}
	}
	var terminal terminalClipboard
	if useTerminal {
		terminal = c.terminal.pick(opts.terminal, opts.osc52 == config.OSC52Always)
		via, terr := terminal.copy([]byte(value))
		switch {
		case terr == nil:
			res.terminal, res.via = true, via
		case res.native:
			res.termErr = terr
		case err != nil:
			err = fmt.Errorf("%w; %w", err, terr)
		default:
			err = terr
		}
	}
	if !res.native && !res.terminal {
		if err == nil {
			err = errNoClipboard
		}
		return copyResult{session: res.session, local: res.local, nativeErr: res.nativeErr}, err
	}
	prev := c.held
	c.held = newHeldCopy([]byte(value), res, terminal)
	if res.native {
		c.nativeWritten(prev, left)
	}
	c.clearSuperseded(prev, res)
	return res, nil
}

// nativeWritten accounts for a new local write. The earlier local copy that
// it replaced is forgotten: retrying its clear would now erase the new copy.
// One that it left on another clipboard (left) keeps a handle of its own,
// and is cleared next, like a copy left behind when the session turned
// remote; the backend's state now belongs to the new copy.
func (c *systemClipboard) nativeWritten(prev *heldCopy, left nativeCopy) {
	var kept []*heldCopy
	for _, h := range c.previousNative {
		if h.native == nil { // the backend's last write before this one
			if left == nil {
				continue
			}
			h.native = left
		}
		kept = append(kept, h)
	}
	if prev != nil && prev.result.native && left != nil {
		prev.native = left
		kept = append(kept, prev)
	}
	c.previousNative = kept
}

// clearSuperseded clears what a new copy did not replace of the previous
// one, whose timed clear the new copy's takes over: the local copy when the
// new one went only to the terminal (the session turned remote) or to
// another local clipboard (see nativeWritten), and the terminal copy, once,
// when the new one went only to the local clipboard.
func (c *systemClipboard) clearSuperseded(prev *heldCopy, next copyResult) {
	if prev != nil && prev.result.native && !next.native {
		c.previousNative = append(c.previousNative, prev)
	}
	_, _, _ = c.clearPreviousNative()
	if prev == nil {
		return
	}
	switch {
	case !prev.result.terminal || next.terminal:
	case prev.result.native:
		_, _ = prev.clearTerminalLeg()
	default:
		_, _ = prev.terminal.clear(prev.result.via)
	}
}

// clearPreviousNative clears the local copies that later copies did not
// replace, and keeps those whose clear failed. It rechecks ownership on
// every retry: a failed clear must not grant permission to erase something
// copied by another app later. repaint reports that it spoke to the local
// clipboard of a remote session (see clearResult.repaint).
func (c *systemClipboard) clearPreviousNative() (outcome clearOutcome, repaint bool, err error) {
	var kept []*heldCopy
	var errs []error
	for _, h := range c.previousNative {
		n := nativeCopy(c.native)
		if h.native != nil {
			n = h.native
		}
		repaint = repaint || h.result.session.remote
		if !n.holds(h.matches, h.result.terminal) {
			if outcome == clearNothing {
				outcome = clearChanged
			}
			continue
		}
		if cerr := n.clear(); cerr != nil {
			errs = append(errs, cerr)
			kept = append(kept, h)
			continue
		}
		outcome = clearDone
	}
	c.previousNative = kept
	if len(errs) > 0 {
		return outcome, repaint, fmt.Errorf("clear previous clipboard copy: %w", errors.Join(errs...))
	}
	return outcome, repaint, nil
}

func (c *systemClipboard) release() (clearResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	previousOutcome, previousRepaint, previousErr := c.clearPreviousNative()
	result, err := c.releaseHeld()
	result.repaint = result.repaint || previousRepaint
	if result.outcome == clearNothing {
		result.outcome = previousOutcome
	}
	if result.pending() && err == nil {
		// A pending terminal clear needs a successful result to keep its
		// existing display-tick retry armed. Defer the older native error
		// until that clear completes or fails; its ownership stays tracked
		// and is retried alongside each pending terminal release.
		return result, nil
	}
	return result, errors.Join(previousErr, err)
}

// releaseHeld clears the latest copy even when an older native clear failed.
// The caller holds c.mu.
func (c *systemClipboard) releaseHeld() (clearResult, error) {
	h := c.held
	if h == nil {
		return clearResult{}, nil
	}
	if !h.result.native {
		// The terminal clipboard cannot be read back to check ownership.
		outcome, err := h.terminal.clear(h.result.via)
		if !outcome.pending() || err != nil {
			c.held = nil
		}
		return clearResult{outcome: outcome}, err
	}
	c.held = nil
	repaint := h.result.session.remote
	if !c.native.holds(h.matches, h.result.terminal) {
		// Something else was copied since, which may have replaced the
		// terminal's copy as well: neither is touched.
		return clearResult{outcome: clearChanged, repaint: repaint}, nil
	}
	if err := c.native.clear(); err != nil {
		// Still keetui's copy: the next release (lock, quit, exit) tries
		// again.
		c.held = h
		return clearResult{repaint: repaint}, fmt.Errorf("clear clipboard: %w", err)
	}
	r := clearResult{outcome: clearDone, repaint: repaint}
	if h.result.terminal {
		r.leg, r.legErr = h.clearTerminalLeg()
	}
	return r, nil
}

// clearTerminalLeg clears the terminal copy made next to a local one, once
// the local one is cleared.
func (h *heldCopy) clearTerminalLeg() (clearOutcome, error) {
	switch s := h.result.session; {
	case !h.result.via.clearable():
		return clearUnsupported, nil
	case h.result.via.route == routeHerdr && s.herdr && !s.herdrSSH:
		// Every herdr client was local: the one that got the copy wrote
		// this machine's clipboard (or skipped the value already there),
		// which is cleared now. A space would go to a client over SSH
		// that attached since, which never got the copy.
		return clearHerdrLocal, nil
	}
	// Sent once, never retried: a later clear could wipe something copied
	// to the terminal since.
	return h.terminal.clear(h.result.via)
}

var (
	processClipboardOnce sync.Once
	processClipboard     atomic.Pointer[systemClipboard]
)

// platformClipboard returns the process-wide clipboard. It lives outside the
// value-typed AppModel so ReleaseClipboard can reach it after the program
// exits.
func platformClipboard() *systemClipboard {
	processClipboardOnce.Do(func() {
		// Tests install their own first.
		d := newSessionDetector(os.Getenv)
		processClipboard.CompareAndSwap(nil, &systemClipboard{
			native:   newNativeClipboard(),
			terminal: newTerminalRoutes(os.Getenv, d.detect),
			detect:   d.detect,
		})
	})
	return processClipboard.Load()
}

// nativeCloser is a native backend that must hand the clipboard over before
// keetui exits, because its copy ends with the process (X11).
type nativeCloser interface{ close() }

// releaseTimeout bounds ReleaseClipboard, so that an unresponsive display
// cannot hang the exit.
var releaseTimeout = 5 * time.Second

// ReleaseClipboard clears keetui's copy if it is still on the clipboard,
// then lets the backend hand the clipboard over. main calls it after the
// program exits, because signals and recovered panics end the program
// without the model's quit handling.
func ReleaseClipboard() {
	c := processClipboard.Load()
	if c == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = c.release()
		c.closeNative()
	}()
	select {
	case <-done:
	case <-time.After(releaseTimeout):
	}
}

func (c *systemClipboard) closeNative() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if n, ok := c.native.(nativeCloser); ok {
		n.close()
	}
}

// pipeTo runs a clipboard tool with input on stdin, never in argv. Output
// stays unattached: xclip, xsel and wl-copy fork a server that keeps serving
// the selection, and Wait must not block on it.
func pipeTo(env []string, input []byte, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(input)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// copyStatus describes where a copy went, and warns when clipboard history,
// tmux or another client may keep it.
func copyStatus(field string, r copyResult) string {
	switch {
	case !r.native:
		return terminalCopyStatus(field, r.via)
	case !r.marked:
		return fmt.Sprintf("Copied %s (clipboard managers may keep it)", field)
	case r.termErr != nil:
		return fmt.Sprintf("Copied %s (OSC 52 failed: %s)", field, r.termErr)
	case r.terminal && r.via.route.tmux():
		return fmt.Sprintf("Copied %s (also sent %s)", field, tmuxLegStatus(r.via))
	case r.terminal:
		return fmt.Sprintf("Copied %s (also sent via OSC 52)", field)
	}
	return fmt.Sprintf("Copied %s!", field)
}

func terminalCopyStatus(field string, v terminalResult) string {
	switch {
	case !v.route.tmux():
		return fmt.Sprintf("Sent %s to the terminal clipboard (OSC 52)", field)
	case v.unverified != "":
		// No tmux to ask: say what the copy needs to arrive, short enough
		// to show whole on an 80-column terminal.
		switch {
		case v.plain:
			return fmt.Sprintf("Sent %s unverified; tmux may keep a copy", field)
		case v.passthroughAlwaysAllowed():
			return fmt.Sprintf("Sent %s via tmux passthrough (OSC 52)", field)
		}
		return fmt.Sprintf("Sent %s unverified: tmux needs allow-passthrough", field)
	}
	switch {
	case v.nested:
		// The outer tmux keeps the copy (set-clipboard on) or drops it.
		return fmt.Sprintf("Sent %s via nested tmux (OSC 52, unverified); outer tmux may keep a copy", field)
	case v.clients > 1:
		return fmt.Sprintf("Sent %s to %d tmux clients' terminal clipboards (OSC 52)", field, v.clients)
	case v.route == routeTmuxPassthrough:
		return fmt.Sprintf("Sent %s to the terminal clipboard via tmux (OSC 52)", field)
	}
	return fmt.Sprintf("Sent %s via tmux (OSC 52); can't auto-clear: %s", field, v.noClear)
}

// tmuxLegStatus qualifies a terminal copy sent through tmux next to a local
// one.
func tmuxLegStatus(v terminalResult) string {
	switch {
	case v.unverified != "" && v.plain:
		return "via tmux, unverified; tmux may keep a copy"
	case v.unverified != "" && v.passthroughAlwaysAllowed():
		return "via tmux passthrough"
	case v.unverified != "":
		return "via tmux, unverified: needs allow-passthrough"
	case v.nested:
		return "via nested tmux OSC 52; outer tmux may keep a copy"
	case !v.clearable():
		return "via tmux; can't clear that"
	case v.clients > 1:
		return fmt.Sprintf("to %d tmux clients via OSC 52", v.clients)
	}
	return "via tmux OSC 52"
}

const (
	deferredClearStatus = "Terminal clipboard clears when keetui's tmux pane is visible"
	awayClearStatus     = "Terminal clipboard clear waits for the terminal that got the copy"
)

// pendingClearStatus reports the notice of a clear that is being retried.
func pendingClearStatus(text string) bool {
	return text == deferredClearStatus || text == awayClearStatus
}

// clearStatus describes the outcome of a timed or retried clear; "" for
// none.
func clearStatus(r clearResult, err error) (text string, isError bool) {
	if err == nil && r.outcome == clearDone && (r.leg != clearNothing || r.legErr != nil) {
		return legClearStatus(r)
	}
	switch outcome := r.outcome; {
	case err != nil:
		return fmt.Sprintf("Clipboard error: %s", err), true
	case outcome == clearDone:
		return "Clipboard cleared", false
	case outcome == clearChanged:
		return "Clipboard changed; left untouched", false
	case outcome == clearRequested:
		// OSC 52 has no acknowledgement; do not claim a verified erase.
		return "Clipboard clear requested", false
	case outcome == clearUnsupported:
		return "tmux can't clear the terminal clipboard", true
	case outcome == clearDeferred:
		return deferredClearStatus, false
	case outcome == clearAway:
		return awayClearStatus, false
	case outcome == clearDetached:
		return "Terminal clipboard not cleared: the terminal that got the copy detached", true
	case outcome == clearReattached:
		return "Clipboard clear requested on the reattached terminal", false
	case outcome == clearPartial:
		return "Clipboard clear requested; a terminal that got the copy detached", true
	case outcome == clearOverwritten:
		return "Terminal clipboard overwritten with a space", false
	case outcome == clearNoRemoteClient:
		return "Terminal clipboard not cleared: no herdr client over SSH", true
	}
	return "", false
}

// legClearStatus describes a local clear followed by the clear of the
// terminal copy made next to it.
func legClearStatus(r clearResult) (string, bool) {
	const done = "Clipboard cleared; "
	switch leg := r.leg; {
	case r.legErr != nil:
		return done + "terminal clear failed: " + r.legErr.Error(), true
	case leg == clearRequested, leg == clearReattached:
		return done + "terminal clear requested", false
	case leg == clearPartial:
		return done + "a terminal with the copy detached", true
	case leg == clearOverwritten:
		return done + "terminal copy overwritten with a space", false
	case leg == clearNoRemoteClient:
		return done + "terminal copy left: no herdr client over SSH", true
	case leg == clearHerdrLocal:
		return "Clipboard cleared", false
	case leg == clearUnsupported:
		return done + "tmux can't clear the terminal copy", true
	case leg == clearDetached:
		return done + "terminal copy left: its terminal detached", true
	case leg.pending():
		return done + "terminal copy left: tmux would drop it", true
	}
	return "Clipboard cleared", false
}

// clearDetail is the F1 guidance for a clear; "" for none.
func clearDetail(r clearResult) string {
	if r.outcome == clearDone && r.leg != clearNothing {
		return legClearDetail(r.leg)
	}
	switch r.outcome {
	case clearDeferred:
		return tmuxHelp("", false)
	case clearAway:
		return "A terminal that got the copy is still attached to tmux, but tmux would not pass the clear on to it now: " +
			"the client is suspended (prefix C-z), or its session shows another window " +
			"(with allow-passthrough all: does not hold keetui's window). " +
			"A clear sent now would miss that terminal, and keetui never sends a second one, which could wipe something copied since. " +
			"So keetui keeps the copy and sends the clear once tmux would deliver it to every terminal that got the copy " +
			"and is still attached, and tries once more when keetui exits."
	case clearDetached:
		return "The tmux client that got the copy detached (for a copy to several clients, all of them), " +
			"and the clear would not reach a single terminal that reports the same TERM and terminal type (XTVERSION) " +
			"as the one that got it, so keetui sent none: it would empty clipboards that never held the copy. " +
			"The copy may still be on the clipboard of the terminal that got it."
	case clearReattached:
		return "The tmux client that got the copy detached. The clear went to the one client attached now, " +
			"which reports the same TERM and terminal type (XTVERSION) and was not attached when keetui copied, " +
			"as when a terminal reconnects over SSH on a new tty."
	case clearPartial:
		return "The copy went to several tmux clients, and some of them detached before the clear. " +
			"The clear went to those still attached; the clipboards of the terminals that detached may still hold the copy."
	case clearOverwritten:
		return herdrOverwriteHelp
	case clearNoRemoteClient:
		return herdrNoClientHelp
	}
	return ""
}

// legClearDetail explains the clear of a terminal copy made next to a local
// one.
func legClearDetail(leg clearOutcome) string {
	const once = "The copy also went to the terminal (OSC 52). keetui clears that copy right after the local one, once: " +
		"it never sends a second clear, which could wipe something copied there since."
	switch leg {
	case clearHerdrLocal:
		return "The copy also went to herdr as OSC 52, and every herdr client was local: " +
			"herdr's client wrote it to this machine's clipboard (or left the same value there alone), " +
			"which keetui has just cleared. keetui sent herdr nothing more: " +
			"a client over SSH attached since would get a space it never needed."
	case clearOverwritten:
		return once + "\n\n" + herdrOverwriteHelp
	case clearNoRemoteClient:
		return once + "\n\n" + herdrNoClientHelp
	case clearDeferred, clearAway:
		return once + " tmux would have dropped the clear (keetui's pane not on screen, " +
			"or a terminal that got the copy suspended or showing another window), so keetui sent none: " +
			"the terminal copy stays until something replaces it."
	case clearDetached, clearPartial:
		return once + "\n\n" + clearDetail(clearResult{outcome: leg})
	case clearUnsupported:
		return "The copy also went to the terminal through tmux (load-buffer, or nested tmux), which cannot clear it: " +
			"it stays on that terminal's clipboard until something replaces it."
	}
	return ""
}

const (
	herdrCopyHelp = "keetui runs in herdr, which hands OSC 52 to the client that last typed. " +
		"A client running over SSH passes it on to its terminal; a local client writes this machine's clipboard " +
		"(on macOS with pbcopy, unmarked). herdr ignores an empty OSC 52, so the timed clear copies a single space over the copy, " +
		"and only while a herdr client runs over SSH."
	herdrOverwriteHelp = "keetui runs in herdr, which ignores an empty OSC 52, so keetui cleared the terminal copy by copying a single space over it " +
		"while a herdr client was attached over SSH. herdr hands it to the client that last typed. " +
		"Like any OSC 52 clear, it also replaces whatever was copied to that terminal's clipboard since."
	herdrNoClientHelp = "keetui runs in herdr, which ignores an empty OSC 52; keetui clears a terminal copy there by copying a single space over it. " +
		"herdr hands OSC 52 to the client that last typed, and a local client writes this machine's clipboard " +
		"(on macOS with pbcopy, unmarked). No herdr client was attached over SSH, so keetui sent nothing: " +
		"the copy may still be on the clipboard of the terminal that got it."
)

// clipboardHelp is the F1 guidance for a copy: where it went and why, and
// the tmux or herdr setup behind a terminal copy.
func clipboardHelp(r copyResult, err error) string {
	var parts []string
	for _, part := range []string{localHelp(r), terminalHelp(r, err)} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "\n\n")
}

// localHelp names the local clipboard a copy went to, or why it did not,
// and what keetui found out about the session.
func localHelp(r copyResult) string {
	var lines []string
	l := r.local
	switch {
	case r.native && l.forwarded && r.terminal:
		lines = append(lines, fmt.Sprintf("Copied to X display %s, which your SSH session forwards, and also to the terminal (OSC 52): "+
			"the X server at your end may be XQuartz or an untrusted ssh -X connection, whose copies cannot be pasted. "+
			"A terminal that applies OSC 52 to that same clipboard (kitty, foot, Alacritty, WezTerm or Ghostty on a Linux desktop) "+
			"replaces the marked copy with an unmarked one, which clipboard history may keep: "+
			"there, set clipboard_osc52 to never.", l.display))
	case r.native && l.dest != "":
		lines = append(lines, "Copied to "+l.dest+".")
	case r.nativeErr != nil && l.authority != "":
		lines = append(lines, fmt.Sprintf("Copying to %s failed: %s (X authority file %s, from the tmux session)", l.dest, r.nativeErr, l.authority))
	case r.nativeErr != nil && l.dest != "":
		lines = append(lines, fmt.Sprintf("Copying to %s failed: %s", l.dest, r.nativeErr))
	case !l.ok && l.why != "":
		lines = append(lines, l.why)
	}
	if l.client != 0 {
		lines = append(lines, fmt.Sprintf("That is the display of herdr client pid %d (%s), where you typed: "+
			"herdr panes keep the environment the herdr server started with, and keetui's names no display or another one.",
			l.client, strings.Join(l.env, " ")))
	}
	switch s := r.session; {
	case s.why == "":
	case s.remote:
		lines = append(lines, "Remote session: "+s.why+".")
	default:
		lines = append(lines, "Local session: "+s.why+".")
	}
	return strings.Join(lines, "\n")
}

// terminalHelp is the F1 guidance for a terminal copy routed through tmux
// or herdr.
func terminalHelp(r copyResult, err error) string {
	var routeErr *tmuxRouteError
	switch {
	case errors.Is(err, errTerminalNotTmux) || errors.Is(r.termErr, errTerminalNotTmux):
		return inheritedTmuxHelp
	case errors.As(err, &routeErr), errors.As(r.termErr, &routeErr):
		return tmuxHelp(routeErr.termName, false)
	case !r.terminal || r.via.route == routeTTY:
		return ""
	case r.via.route == routeHerdr:
		return herdrCopyHelp
	case r.via.unverified != "":
		return unverifiedTmuxHelp(r.via)
	case r.native && !r.via.clearable():
		return "The timed clear empties the local clipboard only: the terminal copy went through tmux (" + r.via.noClear +
			"), which cannot clear it, so it stays on that terminal's clipboard until something replaces it.\n\n" +
			tmuxHelp(r.via.termName, r.via.nested)
	case r.native:
		return "The timed clear empties the local clipboard, then sends the terminal clear once, " +
			"which reaches the terminals that show keetui's pane at that moment.\n\n" + tmuxHelp(r.via.termName, r.via.nested)
	}
	return tmuxHelp(r.via.termName, r.via.nested)
}

const inheritedTmuxHelp = "$TMUX names a tmux server, but the terminal answered XTVERSION with another name, " +
	"so keetui is not running directly in tmux. A terminal started from a tmux pane inherits $TMUX: " +
	"start keetui there with TMUX unset (env -u TMUX keetui ...).\n\n" +
	"keetui sends nothing in this case: a terminal multiplexer running in a tmux pane looks the same, " +
	"and could pass a plain OSC 52 on to tmux, which may keep it in a paste buffer."

// tmuxHelp explains the tmux options behind terminal copies.
func tmuxHelp(term string, nested bool) string {
	if term == "" {
		term = "TERM"
	}
	help := "tmux and the terminal clipboard (~/.tmux.conf):\n\n" +
		"set -g allow-passthrough on\n" +
		"  tmux forwards keetui's copies unchanged, keeps them out of its paste buffers, and lets keetui clear them. Always on before tmux 3.3. " +
		"tmux drops them while keetui's pane is not on screen (another window current, another pane zoomed), so a clear waits until it is. " +
		"It also drops them, unseen by keetui, while a tmux prompt, message, menu, popup or display-panes shows on the terminal.\n" +
		"set -g allow-passthrough all\n" +
		"  tmux 3.4+: copies and clears also reach the terminal while keetui's pane is not on screen or a tmux prompt, message or menu shows, " +
		"and reach every client of a session holding keetui's window. " +
		"No setting lets them out of copy mode or another tmux mode, or to a suspended client (prefix C-z): " +
		"a clear waits until keetui's pane leaves the mode or the client resumes.\n" +
		"set -as terminal-features '," + term + ":clipboard'\n" +
		"  Without passthrough, tmux 3.2+ sends copies only to a terminal it knows supports OSC 52. Those copies can't be cleared.\n\n"
	if nested {
		return help + "Nested tmux: this tmux runs inside another tmux (or screen), and hands the copy on to it as a plain OSC 52. " +
			"The outer tmux forwards it only with set -g set-clipboard on, and then keeps it in a paste buffer that keetui cannot clear."
	}
	return help + "set-clipboard needs no change: neither route depends on it."
}

// unverifiedTmuxHelp explains a copy sent through a tmux keetui cannot ask:
// one known only from XTVERSION, or one whose probe failed.
func unverifiedTmuxHelp(v terminalResult) string {
	var b strings.Builder
	if v.route == routeRemoteTmux {
		name := "tmux"
		if v.version != "" {
			name += " " + v.version
		}
		b.WriteString("The terminal answered XTVERSION as " + name + ", but $TMUX is unset, so keetui runs under a tmux it cannot ask: " +
			"over SSH from a tmux pane, or in a tmux pane after sudo or su. ")
	} else {
		b.WriteString("keetui could not ask tmux how to reach the terminal: " + v.unverified + ". ")
	}
	b.WriteString("It sent the copy through tmux passthrough, which leaves no paste buffer and can be cleared")
	if v.passthroughAlwaysAllowed() {
		// No option to set: allow-passthrough does not exist before 3.3.
		b.WriteString("; tmux before 3.3 always lets it through.")
		if v.plain {
			b.WriteString(" With clipboard_osc52 always, keetui also sent a plain OSC 52, which tmux forwards with set-clipboard on, " +
				"keeping a copy in a paste buffer that any pane can read back and keetui cannot clear; " +
				"set clipboard_osc52 to auto to send only the passthrough.")
		}
		return b.String()
	}
	b.WriteString(", but tmux 3.3 and later drop it unless that tmux allows passthrough.")
	if v.version == "" && v.route != routeRemoteTmux {
		b.WriteString(" (If this tmux is older than 3.2, which does not answer XTVERSION, passthrough is always allowed.)")
	}
	if v.plain {
		b.WriteString(" With clipboard_osc52 always, keetui also sent a plain OSC 52, which tmux forwards with set-clipboard on, " +
			"keeping a copy in a paste buffer that any pane can read back and keetui cannot clear." +
			"\n\nTo keep tmux from keeping copies:\n\n" +
			"set -g allow-passthrough on   (or all)\n" +
			"  in that tmux (~/.tmux.conf on the machine it runs on), and clipboard_osc52 auto in keetui's settings: " +
			"copies then arrive through passthrough alone, leave no paste buffer, and are cleared on the timer.")
		return b.String()
	}
	b.WriteString("\n\nTo make copies arrive, either:\n\n" +
		"set -g allow-passthrough on   (or all)\n" +
		"  in that tmux (~/.tmux.conf on the machine it runs on). Copies then arrive, leave no paste buffer, and are cleared on the timer.\n" +
		"clipboard_osc52 always\n" +
		"  in keetui's settings: keetui also sends a plain OSC 52, which tmux forwards with set -g set-clipboard on, " +
		"but keeps in a paste buffer that keetui cannot clear.")
	return b.String()
}

// envWith returns the environment with the given variables replaced.
func envWith(kv ...string) []string {
	env := os.Environ()
	for _, pair := range kv {
		name := pair[:strings.IndexByte(pair, '=')+1]
		env = slices.DeleteFunc(env, func(e string) bool { return strings.HasPrefix(e, name) })
		env = append(env, pair)
	}
	return env
}
