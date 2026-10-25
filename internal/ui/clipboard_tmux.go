package ui

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Inside tmux, an application's raw OSC 52 is dropped with set-clipboard
// external (the default) or off. With on, tmux keeps it in an automatic
// paste buffer that any pane can read back with an OSC 52 query. tmux also
// ignores empty OSC 52 data, so a raw clear does nothing. keetui therefore
// never sends a raw OSC 52 inside tmux. It asks the server how a copy can
// reach the terminal, once per copy and once per clear, and uses:
//
//   - passthrough (DCS tmux; ... ST): forwarded unchanged with no buffer,
//     and an empty copy clears. It needs allow-passthrough (always allowed
//     before 3.3), keetui's pane out of copy mode and, unless that is
//     "all", the pane on screen: its window current, no other pane zoomed.
//     tmux writes it to every client that is not suspended and shows the
//     pane ("all": whose session holds its window), so keetui lists them,
//     and a clear waits until it would reach the terminals that got the
//     copy.
//   - load-buffer -w (3.2+): the value goes on stdin into a randomly named
//     buffer, which tmux sends to one client whose terminal has the Ms
//     capability; the buffer is deleted right away. It cannot clear.

const (
	tmuxTimeout = 2 * time.Second
	// tmuxWaitDelay bounds the wait for tmux's output once it has exited or
	// been killed. A child that keeps the pipes open, such as one of a
	// wrapper script's, would otherwise hold up Update until it exits.
	tmuxWaitDelay = 500 * time.Millisecond
)

// tmuxRunner runs tmux with stdin (nil for none) and returns its stdout.
type tmuxRunner func(stdin []byte, args ...string) ([]byte, error)

// runTmux runs the tmux client on PATH, which finds the server through
// $TMUX. Secrets only ever travel on stdin.
func runTmux(stdin []byte, args ...string) ([]byte, error) {
	path, err := exec.LookPath("tmux")
	if err != nil {
		return nil, fmt.Errorf("tmux: %w", err)
	}
	return runTmuxAt(path, tmuxTimeout, stdin, args...)
}

// runTmuxAt runs the tmux client at path and returns within timeout plus
// tmuxWaitDelay.
func runTmuxAt(path string, timeout time.Duration, stdin []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.WaitDelay = tmuxWaitDelay
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if errors.Is(err, exec.ErrWaitDelay) {
		// tmux exited 0, and only a child it left holds the pipes. Failing
		// the run would skip deleting a buffer load-buffer filled (before
		// 3.4, the delete is a second run).
		err = nil
	}
	if err != nil {
		if msg := strings.Join(strings.Fields(stderr.String()), " "); msg != "" {
			return out, fmt.Errorf("tmux %s: %w: %s", args[0], err, msg)
		}
		return out, fmt.Errorf("tmux %s: %w", args[0], err)
	}
	return out, nil
}

// tmuxClipboard routes terminal copies through the tmux server in $TMUX.
type tmuxClipboard struct {
	run    tmuxRunner
	write  func(string) error // the controlling terminal
	settle func()             // lets a pending frame reach tmux first; nil in tests
	pane   string             // $TMUX_PANE; unset in popups
}

// passthroughSettle is a little over one frame at Bubble Tea's 60 fps.
// Bubble Tea ends each frame by leaving synchronized output (?2026l), on
// which tmux marks the pane for a redraw, and until that redraw it drops
// passthrough from the pane unless allow-passthrough is "all". While Update
// waits, the renderer flushes the frame it has pending and starts no other,
// so the copy or clear reaches tmux on its own rather than right behind a
// frame.
const passthroughSettle = 20 * time.Millisecond

func settlePassthrough() { time.Sleep(passthroughSettle) }

func (t tmuxClipboard) copy(value []byte) (terminalResult, error) {
	if len(value) == 0 {
		return terminalResult{}, errEmptyTmuxCopy
	}
	if t.settle != nil {
		t.settle()
	}
	p, err := t.probe()
	if err != nil {
		// tmux cannot be asked (not on PATH, socket gone): passthrough is
		// the one route that cannot leave a buffer behind.
		res := terminalResult{route: routeTmuxPassthrough, unverified: err.Error()}
		return res, t.write(passthroughOSC52(value))
	}
	res, err := planTmuxCopy(p)
	switch {
	case err != nil:
		return res, err
	case res.route == routeTmuxLoadBuffer:
		return res, t.loadBuffer(p, value)
	}
	return res, t.write(passthroughOSC52(value))
}

// clear never deletes buffers: passthrough creates none, and load-buffer
// copies delete theirs at once.
func (t tmuxClipboard) clear(last terminalResult) (clearOutcome, error) {
	if !last.clearable() {
		return clearUnsupported, nil
	}
	if t.settle != nil {
		t.settle()
	}
	outcome := clearRequested
	// A failed probe leaves the clear unverified, like the copy it follows.
	if p, err := t.probe(); err == nil {
		outcome = p.clearFor(last)
	}
	switch outcome {
	case clearRequested, clearReattached, clearPartial:
		return outcome, t.write(passthroughOSC52(nil))
	}
	return outcome, nil
}

// loadBuffer sends value to the probed client through a uniquely named
// buffer and deletes that buffer again.
func (t tmuxClipboard) loadBuffer(p tmuxProbe, value []byte) error {
	var id [8]byte
	_, _ = rand.Read(id[:])
	// A random name never replaces one of the user's buffers.
	name := "keetui-" + hex.EncodeToString(id[:])
	load := []string{"load-buffer", "-b", name, "-t", p.clientTTY, "-w", "-"}
	if p.atLeast(3, 4) {
		_, err := t.run(value, append(load, ";", "delete-buffer", "-b", name)...)
		return err
	}
	// Before 3.4, delete-buffer -b with a missing name deletes the newest
	// buffer, possibly the user's, and a failed load-buffer does not stop
	// a command list. Delete only after the load succeeded.
	if _, err := t.run(value, load...); err != nil {
		return err
	}
	if _, err := t.run(nil, "delete-buffer", "-b", name); err != nil {
		return fmt.Errorf("tmux kept the copy in buffer %s: %w", name, err)
	}
	return nil
}

// tmuxProbe is what tmux reports about keetui's pane, the client it picks
// for keetui's session, and the clients a passthrough write would reach.
type tmuxProbe struct {
	version       string
	major, minor  int    // 0.0 when the version is not a release number
	setClipboard  string // informational: neither route depends on it
	passthrough   string // allow-passthrough: off/on/all, 0/1 on 3.3, "" before 3.3
	clientTTY     string
	clientTerm    string   // client_termname: the client's TERM
	clientType    string   // client_termtype: the client terminal's XTVERSION reply
	clientMs      bool     // tmux knows the client's terminal takes OSC 52 (Ms)
	clientTTYs    []string // every attached client, suspended ones included; nil when none
	attached      int      // session_attached of keetui's session; lags behind a suspend
	windowActive  bool     // keetui's window is its session's current window
	windowClients int      // window_active_clients; 0 when unknown (3.0, 3.1)
	windowID      string
	windowZoomed  bool   // a pane of keetui's window is zoomed
	paneActive    bool   // keetui's pane is its window's active pane
	paneMode      string // the mode keetui's pane shows (copy-mode, ...); "" for none
	popup         bool   // keetui runs in a popup, where $TMUX_PANE is unset
	viewers       int    // clients awake whose session shows keetui's window
	holders       int    // clients awake whose session holds keetui's window, viewers included
	// The ttys of the clients awake in any session, of the viewers and of
	// the holders. Before 3.2 the counts come from session_attached, which
	// leaves out a client once tmux has noticed its suspend, and the lists
	// keep it.
	awakeTTYs, viewerTTYs, holderTTYs []string
	listed                            bool // the client list named them; else they hold at most the client tmux picks
}

// client_termtype comes last: it is the terminal's own reply and may hold
// the separator.
const tmuxProbeFormat = "keetui-probe|#{version}|#{set-clipboard}|#{allow-passthrough}|" +
	"#{client_tty}|#{client_termname}|#{session_attached}|#{window_active}|" +
	"#{window_active_clients}|#{window_zoomed_flag}|#{pane_active}|#{pane_mode}|" +
	"#{window_id}|#{client_termtype}"

const tmuxProbeFields = 14

// list-windows -a has a row for every session's link to every window, and
// list-clients one for every attached client, so the probe can count the
// clients tmux writes passthrough to. Neither format holds the separator:
// ids are $n and @n, and flags are comma-separated words.
const (
	tmuxWindowFormat = "keetui-window|#{session_id}|#{session_attached}|#{window_id}|#{window_active}"
	tmuxClientFormat = "keetui-client|#{client_tty}|#{session_id}|#{client_flags}"
)

func (t tmuxClipboard) probe() (tmuxProbe, error) {
	args := []string{"display-message", "-p"}
	if t.pane != "" {
		args = append(args, "-t", t.pane)
	}
	// show-messages -T lists each terminal's capabilities, which is where
	// Ms shows up whether it comes from terminfo, terminal-features or
	// terminal-overrides; #{client_termfeatures} misses the first and last.
	args = append(args, tmuxProbeFormat,
		";", "list-windows", "-a", "-F", tmuxWindowFormat,
		";", "list-clients", "-F", tmuxClientFormat,
		";", "show-messages", "-T")
	out, runErr := t.run(nil, args...)
	p, err := parseTmuxProbe(out)
	if err != nil {
		if runErr != nil {
			return p, runErr
		}
		return p, err
	}
	// Before 3.7, show-messages fails without a client ("no current
	// client") after display-message has answered; Ms then stays unknown.
	p.popup = t.pane == ""
	return p, nil
}

var (
	errTmuxProbe   = errors.New("tmux: unexpected display-message output")
	tmuxVersionRe  = regexp.MustCompile(`^(?:next-)?(\d+)\.(\d+)`)
	tmuxTerminalRe = regexp.MustCompile(`^Terminal \d+: (\S+) (?:for (\S+), flags=|\[)`)
	tmuxMsRe       = regexp.MustCompile(`^\s*\d+: Ms: (.*)$`)
)

func parseTmuxProbe(out []byte) (tmuxProbe, error) {
	var p tmuxProbe
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(nil, 1<<20)
	if !sc.Scan() {
		return p, errTmuxProbe
	}
	f := strings.SplitN(sc.Text(), "|", tmuxProbeFields)
	if len(f) != tmuxProbeFields || f[0] != "keetui-probe" {
		return p, errTmuxProbe
	}
	p.version, p.setClipboard, p.passthrough = f[1], f[2], f[3]
	p.clientTTY, p.clientTerm = f[4], f[5]
	p.attached, _ = strconv.Atoi(f[6])
	p.windowActive = f[7] == "1"
	p.windowClients, _ = strconv.Atoi(f[8])
	p.windowZoomed, p.paneActive, p.paneMode = f[9] == "1", f[10] == "1", f[11]
	p.windowID, p.clientType = f[12], f[13]
	if m := tmuxVersionRe.FindStringSubmatch(p.version); m != nil {
		p.major, _ = strconv.Atoi(m[1])
		p.minor, _ = strconv.Atoi(m[2])
	}
	links := map[string]*tmuxWindowLink{} // sessions holding keetui's window, by id
	var clients []tmuxClientRow
	var terminalTTYs []string
	// Terminal blocks name the client tty from 3.2; before, only the TERM.
	inClient := false
	for sc.Scan() {
		line := sc.Text()
		if row, ok := strings.CutPrefix(line, "keetui-window|"); ok {
			if f := strings.Split(row, "|"); len(f) == 4 && p.windowID != "" && f[2] == p.windowID {
				link := links[f[0]]
				if link == nil {
					link = &tmuxWindowLink{}
					links[f[0]] = link
				}
				link.attached, _ = strconv.Atoi(f[1])
				link.current = link.current || f[3] == "1"
			}
		} else if row, ok := strings.CutPrefix(line, "keetui-client|"); ok {
			if f := strings.Split(row, "|"); len(f) == 3 && f[0] != "" {
				suspended := slices.Contains(strings.Split(f[2], ","), "suspended")
				clients = append(clients, tmuxClientRow{tty: f[0], session: f[1], suspended: suspended})
			}
		} else if m := tmuxTerminalRe.FindStringSubmatch(line); m != nil {
			if m[2] != "" {
				terminalTTYs = append(terminalTTYs, m[2])
				inClient = p.clientTTY != "" && m[2] == p.clientTTY
			} else {
				inClient = p.clientTerm != "" && m[1] == p.clientTerm
			}
		} else if m := tmuxMsRe.FindStringSubmatch(line); m != nil && inClient {
			p.clientMs = m[1] != "[missing]"
		}
	}
	if err := sc.Err(); err != nil {
		return p, fmt.Errorf("tmux: %w", err)
	}
	// From 3.7, list-clients leaves out suspended clients, which
	// show-messages -T still names.
	for _, c := range clients {
		p.clientTTYs = append(p.clientTTYs, c.tty)
	}
	for _, tty := range terminalTTYs {
		if !slices.Contains(p.clientTTYs, tty) {
			p.clientTTYs = append(p.clientTTYs, tty)
		}
	}
	p.countClients(links, clients)
	return p, nil
}

// tmuxWindowLink is a session that holds keetui's window.
type tmuxWindowLink struct {
	attached int  // session_attached
	current  bool // keetui's window is the session's current window
}

type tmuxClientRow struct {
	tty, session string
	suspended    bool
}

// countClients lists and counts the clients tmux writes passthrough from
// keetui's pane to: those whose session shows keetui's window, and those
// whose session holds it at all, which "all" reaches. A suspended client
// gets nothing. client_flags names one from 3.2. Before, only
// session_attached leaves it out, and only once tmux has recounted after
// the suspend, which any tmux command does on exit.
func (p *tmuxProbe) countClients(links map[string]*tmuxWindowLink, clients []tmuxClientRow) {
	if len(links) == 0 {
		// No row for keetui's window: rely on the probe line, which names
		// the client tmux picks only.
		p.viewers, p.holders = p.windowClientsSeen(), max(p.attached, p.windowClients)
		if p.clientTTY != "" {
			p.awakeTTYs = []string{p.clientTTY}
			if p.viewers == 1 {
				p.viewerTTYs = []string{p.clientTTY}
			}
			if p.holders == 1 {
				p.holderTTYs = []string{p.clientTTY}
			}
		}
		return
	}
	// The client tmux picks need not show the window: it is the most
	// recently active client of the most recently active session holding
	// it. Recipients come from the client list instead.
	p.listed = true
	for _, c := range clients {
		if c.suspended {
			continue
		}
		p.awakeTTYs = append(p.awakeTTYs, c.tty)
		if link := links[c.session]; link != nil {
			p.holderTTYs = append(p.holderTTYs, c.tty)
			if link.current {
				p.viewerTTYs = append(p.viewerTTYs, c.tty)
			}
		}
	}
	p.viewers, p.holders = len(p.viewerTTYs), len(p.holderTTYs)
	if !p.atLeast(3, 2) {
		p.viewers, p.holders = 0, 0
		for _, link := range links {
			p.holders += link.attached
			if link.current {
				p.viewers += link.attached
			}
		}
	}
}

func (p tmuxProbe) atLeast(major, minor int) bool {
	return p.major > major || p.major == major && p.minor >= minor
}

func (p tmuxProbe) passthroughAllowed() bool {
	switch p.passthrough {
	case "on", "1", "all":
		return true
	case "":
		return !p.atLeast(3, 3) // no option before 3.3: always allowed
	}
	return false
}

// popupBlocked reports a popup in tmux 3.3 to 3.6, which ignores
// passthrough from popups.
func (p tmuxProbe) popupBlocked() bool {
	return p.popup && p.atLeast(3, 3) && !p.atLeast(3, 7)
}

// inMode reports keetui's pane in copy mode or another mode. tmux then
// writes the pane's output to no terminal, passthrough included, whatever
// allow-passthrough says (measured on 3.0a to 3.8). A popup has no pane:
// the probe then describes the pane under it, so this and zoomedAway do
// not apply.
func (p tmuxProbe) inMode() bool { return !p.popup && p.paneMode != "" }

// zoomedAway reports another pane of keetui's window zoomed, the zoomed
// pane being the active one. tmux then draws keetui's pane nowhere and,
// unless allow-passthrough is "all", drops its passthrough.
func (p tmuxProbe) zoomedAway() bool { return !p.popup && p.windowZoomed && !p.paneActive }

// windowClientsSeen estimates the clients showing keetui's window from the
// probe line alone: from 3.2 window_active_clients counts them across
// sessions, suspended ones included. Before, only keetui's session is
// known, whose clients all show its current window.
func (p tmuxProbe) windowClientsSeen() int {
	switch {
	case p.atLeast(3, 2):
		return p.windowClients
	case p.windowActive:
		return p.attached
	}
	return 0
}

// recipient is the tty of the one client a passthrough write reaches now;
// "" when it reaches several or the probe cannot name it.
func (p tmuxProbe) recipient() string {
	if ttys := p.passthroughTTYs(); p.passthroughClients() == 1 && len(ttys) == 1 {
		return ttys[0]
	}
	return ""
}

// passthroughTTYs lists the clients a passthrough write may reach now, by
// tty. For a popup, whose client nothing names, that is every client
// awake, the popup's own among them.
func (p tmuxProbe) passthroughTTYs() []string {
	switch {
	case p.passthroughClients() == 0:
		return nil
	case p.popup:
		return p.awakeTTYs
	case p.passthrough == "all":
		return p.holderTTYs
	}
	return p.viewerTTYs
}

// passthroughClients is how many clients a passthrough write reaches now.
// With "on" (and always before 3.4) tmux writes it only to clients that
// show keetui's pane; "all" reaches every client of a session holding its
// window. A popup's passthrough goes to the popup's own client only,
// which, typing in a popup, is awake.
func (p tmuxProbe) passthroughClients() int {
	switch {
	case !p.passthroughAllowed() || p.popupBlocked() || p.inMode():
		return 0
	case p.popup:
		return min(len(p.awakeTTYs), 1)
	case p.passthrough == "all":
		return p.holders
	case p.zoomedAway():
		return 0
	}
	return p.viewers
}

// detached reports that the client on tty, which got a copy, is no longer
// attached to tmux.
func (p tmuxProbe) detached(tty string) bool {
	return len(p.clientTTYs) > 0 && !slices.Contains(p.clientTTYs, tty)
}

// clearFor decides what a clear of the copy last describes does now. It
// goes out once tmux would write it to every terminal that got the copy
// and is still attached; it may reach others too.
func (p tmuxProbe) clearFor(last terminalResult) clearOutcome {
	if p.passthroughClients() == 0 {
		if p.passthroughAllowed() && !p.popupBlocked() {
			// keetui's pane is not on screen, or no client is attached
			// and awake: tmux would drop the clear.
			return clearDeferred
		}
		return clearUnsupported
	}
	away, gone, now := 0, 0, p.passthroughTTYs()
	for _, tty := range last.reached {
		switch {
		case p.detached(tty):
			gone++
		case p.listed && !slices.Contains(now, tty):
			// Attached, but suspended, or its session shows another
			// window (with "all", does not hold keetui's).
			away++
		}
	}
	switch {
	case away > 0:
		// Sent now, the clear would miss that terminal, and keetui could
		// not resend it without wiping whatever was copied since.
		return clearAway
	case gone == 0:
		return clearRequested
	case gone < len(last.reached):
		return clearPartial
	case p.sameTerminal(last):
		// The same kind of terminal on a new tty, as after an SSH
		// reconnect: most likely the one that got the copy.
		return clearReattached
	}
	// It would clear only terminals that never got the copy.
	return clearDetached
}

// clientID identifies the terminal of the client tmux picks across ttys: a
// terminal that reconnects over SSH comes back on a new one.
func (p tmuxProbe) clientID() string { return p.clientTerm + "\x00" + p.clientType }

// sameTerminal reports that a clear would reach one client only, the one
// tmux picks, and that it looks like the terminal that got the copy. A
// client that was already attached when keetui copied is another terminal.
func (p tmuxProbe) sameTerminal(last terminalResult) bool {
	return last.clientID != "" && p.clientTTY != "" && p.recipient() == p.clientTTY && p.clientID() == last.clientID &&
		!slices.Contains(last.attached, p.clientTTY)
}

// loadBufferReaches reports that load-buffer -w can send a copy to the
// client tmux picks, keetui's. In a popup that is known only with one client
// awake: nothing in a popup names its client, and keys typed there leave
// tmux's choice of client unchanged.
func (p tmuxProbe) loadBufferReaches() bool {
	return p.atLeast(3, 2) && p.clientTTY != "" && p.clientMs && (!p.popup || p.popupClientKnown())
}

func (p tmuxProbe) popupClientKnown() bool {
	return len(p.awakeTTYs) == 1 && p.awakeTTYs[0] == p.clientTTY
}

// nested reports a tmux client that itself runs in tmux (or screen). The
// outer multiplexer receives the copy as an application's raw OSC 52: it
// may keep it, and ignores the empty one that would clear it. A terminal
// that named itself (XTVERSION, 3.2+) settles it; otherwise the TERM does.
// Before 3.2, client_termtype is only a device class such as VT100.
func (p tmuxProbe) nested() bool {
	switch {
	case strings.HasPrefix(p.clientType, "tmux "):
		return true
	case p.clientType != "" && p.atLeast(3, 2):
		return false
	}
	return strings.HasPrefix(p.clientTerm, "tmux") || strings.HasPrefix(p.clientTerm, "screen")
}

// planTmuxCopy picks the route for a copy. It never chooses a raw OSC 52.
func planTmuxCopy(p tmuxProbe) (terminalResult, error) {
	res := terminalResult{nested: p.nested(), termName: p.clientTerm}
	clients := p.passthroughClients()
	switch {
	case clients > 1 && p.loadBufferReaches():
		// Passthrough would reach every terminal showing the window (with
		// "all", holding it), other people's in a shared session included.
		res.route, res.noClear = routeTmuxLoadBuffer, fmt.Sprintf("%d clients share the window", clients)
	case clients > 0:
		res.route = routeTmuxPassthrough
		if clients > 1 {
			res.clients = clients
		}
		// The terminals that got the copy, which its clear waits for.
		if tty := p.recipient(); tty != "" {
			res.reached = []string{tty}
			if tty == p.clientTTY {
				// The probe line describes this client's terminal.
				res.clientID, res.attached = p.clientID(), slices.Clone(p.clientTTYs)
			}
		} else if clients > 1 {
			res.reached = p.passthroughTTYs()
		}
		if res.nested {
			res.noClear = "nested tmux"
		}
	case p.loadBufferReaches():
		res.route, res.noClear = routeTmuxLoadBuffer, p.passthroughBlocker()
	default:
		reason := p.passthroughBlocker()
		if other := p.loadBufferBlocker(); other != reason {
			reason += " and " + other
		}
		return res, &tmuxRouteError{reason: reason, termName: p.clientTerm}
	}
	return res, nil
}

func (p tmuxProbe) passthroughBlocker() string {
	switch {
	case !p.passthroughAllowed():
		return "allow-passthrough off"
	case p.popupBlocked():
		return "tmux " + p.version + " ignores passthrough from popups"
	case p.popup && len(p.awakeTTYs) == 0, !p.popup && p.holders == 0:
		return "no client attached"
	case p.inMode():
		return "keetui's pane is in " + p.paneMode
	case p.viewers == 0:
		return "keetui's window is hidden"
	}
	return "another pane is zoomed"
}

func (p tmuxProbe) loadBufferBlocker() string {
	switch {
	case p.major == 0:
		return "unrecognized tmux version " + p.version
	case !p.atLeast(3, 2):
		return "tmux " + p.version + " has no load-buffer -w"
	case p.clientTTY == "", p.popup && len(p.awakeTTYs) == 0:
		return "no client attached"
	case p.popup && !p.popupClientKnown():
		return "tmux can't tell keetui which client shows its popup"
	case p.clientTerm == "":
		return "no clipboard support for the client's terminal"
	}
	return "no clipboard support for " + p.clientTerm
}

// tmuxRouteError explains why tmux cannot forward a copy. Nothing was sent.
type tmuxRouteError struct{ reason, termName string }

func (e *tmuxRouteError) Error() string {
	return "tmux can't reach the terminal clipboard: " + e.reason
}
