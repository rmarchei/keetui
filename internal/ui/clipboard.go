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
// copy in its paste buffers (see clipboard_terminal.go).
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
}

// autoClear reports whether a timed clear can remove the copy.
func (r copyResult) autoClear() bool { return r.native || r.terminal && r.via.clearable() }

type clearOutcome int

const (
	clearNothing     clearOutcome = iota // no keetui copy was outstanding
	clearDone                            // keetui's copy was removed
	clearChanged                         // the clipboard holds something else; left untouched
	clearRequested                       // OSC 52 only: an empty copy was sent, unverifiable
	clearUnsupported                     // the terminal copy cannot be cleared (tmux)
	clearDeferred                        // tmux would drop the clear now; the copy is kept for a retry
	clearAway                            // as clearDeferred: a tmux client that got the copy is attached, but out of reach
	clearDetached                        // the tmux client that got the copy left; others would get the clear
	clearReattached                      // as clearRequested, sent to a terminal like the one that left
	clearPartial                         // as clearRequested, but some tmux clients that got the copy left
)

// pending reports a clear that could not be sent yet: the copy is kept and
// the clear retried.
func (o clearOutcome) pending() bool { return o == clearDeferred || o == clearAway }

// Keep clipboard I/O injectable so timer regressions never access the user's
// real clipboard. A nil AppModel backend uses the process-wide one.
type clipboardAccess interface {
	copy(value string, opts copyOptions) (copyResult, error)
	// release clears the clipboard if it still holds the last copy, then
	// forgets that copy.
	release() (clearOutcome, error)
}

// nativeClipboard is the platform's local clipboard.
type nativeClipboard interface {
	// available reports whether the local clipboard is the user's clipboard
	// (for example, not a remote Mac reached over SSH).
	available() bool
	write(value []byte, transient bool) (marked bool, err error)
	// holds reports whether the clipboard still holds the last write.
	// matches checks content where ownership cannot be tracked otherwise;
	// alsoTerminal means an OSC 52 copy of the same value may have replaced
	// the write.
	holds(matches func([]byte) bool, alsoTerminal bool) bool
	clear() error
}

var errNoClipboard = errors.New("no clipboard available")

type systemClipboard struct {
	mu       sync.Mutex
	native   nativeClipboard // nil when the platform has none
	terminal terminalRoutes  // OSC 52
	held     *heldCopy
}

type heldCopy struct {
	result   copyResult
	terminal terminalClipboard // the route that sent the terminal copy
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

func (c *systemClipboard) copy(value string, opts copyOptions) (copyResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	useNative := c.native != nil && c.native.available()
	useTerminal := opts.osc52 == config.OSC52Always || opts.osc52 != config.OSC52Never && !useNative
	var res copyResult
	var err error
	if useNative {
		if res.marked, err = c.native.write([]byte(value), opts.transient); err == nil {
			res.native = true
		} else if opts.osc52 != config.OSC52Never {
			useTerminal = true
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
		return copyResult{}, err
	}
	c.held = newHeldCopy([]byte(value), res, terminal)
	return res, nil
}

func (c *systemClipboard) release() (clearOutcome, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	h := c.held
	if h == nil {
		return clearNothing, nil
	}
	if !h.result.native {
		// The terminal clipboard cannot be read back to check ownership.
		outcome, err := h.terminal.clear(h.result.via)
		if !outcome.pending() || err != nil {
			c.held = nil
		}
		return outcome, err
	}
	c.held = nil
	if !c.native.holds(h.matches, h.result.terminal) {
		return clearChanged, nil
	}
	if err := c.native.clear(); err != nil {
		return clearNothing, fmt.Errorf("clear clipboard: %w", err)
	}
	return clearDone, nil
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
		processClipboard.CompareAndSwap(nil, &systemClipboard{native: newNativeClipboard(), terminal: newTerminalRoutes(os.Getenv)})
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
	case r.terminal && r.via.route != routeTTY:
		return fmt.Sprintf("Copied %s (also sent %s)", field, tmuxLegStatus(r.via))
	case r.terminal:
		return fmt.Sprintf("Copied %s (also sent via OSC 52)", field)
	}
	return fmt.Sprintf("Copied %s!", field)
}

func terminalCopyStatus(field string, v terminalResult) string {
	switch {
	case v.route == routeTTY:
		return fmt.Sprintf("Sent %s to the terminal clipboard (OSC 52)", field)
	case v.unverified != "":
		// No tmux to ask: say what the copy needs to arrive.
		switch {
		case v.plain:
			return fmt.Sprintf("Sent %s via tmux passthrough and OSC 52; with set-clipboard on, tmux keeps a copy", field)
		case v.passthroughAlwaysAllowed():
			return fmt.Sprintf("Sent %s via tmux passthrough (OSC 52)", field)
		}
		return fmt.Sprintf("Sent %s via tmux passthrough (unverified; needs allow-passthrough on)", field)
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
// one. The timed clear then empties only the local clipboard.
func tmuxLegStatus(v terminalResult) string {
	switch {
	case v.unverified != "" && v.plain:
		return "via tmux passthrough and OSC 52; with set-clipboard on, tmux keeps a copy"
	case v.unverified != "" && v.passthroughAlwaysAllowed():
		return "via tmux passthrough"
	case v.unverified != "":
		return "via tmux passthrough, unverified; needs allow-passthrough on"
	case v.nested:
		return "via nested tmux OSC 52; outer tmux may keep a copy"
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
func clearStatus(outcome clearOutcome, err error) (text string, isError bool) {
	switch {
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
	}
	return "", false
}

// clearDetail is the F1 guidance for a clear outcome; "" for none.
func clearDetail(outcome clearOutcome) string {
	switch outcome {
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
	}
	return ""
}

// clipboardHelp is the F1 guidance for a terminal copy routed through tmux.
func clipboardHelp(r copyResult, err error) string {
	var routeErr *tmuxRouteError
	switch {
	case errors.Is(err, errTerminalNotTmux) || errors.Is(r.termErr, errTerminalNotTmux):
		return inheritedTmuxHelp
	case errors.As(err, &routeErr), errors.As(r.termErr, &routeErr):
		return tmuxHelp(routeErr.termName, false)
	case !r.terminal || r.via.route == routeTTY:
		return ""
	case r.via.unverified != "":
		return unverifiedTmuxHelp(r.via)
	case r.native:
		return "The timed clear empties the local clipboard only: other terminals tmux sent the copy to keep it.\n\n" +
			tmuxHelp(r.via.termName, r.via.nested)
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
		b.WriteString("; tmux before 3.3 always lets it through.")
	} else {
		b.WriteString(", but tmux 3.3 and later drop it unless that tmux allows passthrough.")
	}
	if v.plain {
		b.WriteString(" With clipboard_osc52 always, keetui also sent a plain OSC 52, which tmux forwards with set-clipboard on, " +
			"keeping a copy in a paste buffer that any pane can read back and keetui cannot clear.")
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
