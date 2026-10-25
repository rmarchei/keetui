package ui

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"

	"github.com/charmbracelet/x/ansi"
)

// Terminal copies use OSC 52. Outside tmux the sequence goes straight to the
// terminal. Inside tmux (see clipboard_tmux.go) it never goes raw: tmux would
// drop it or keep it in a paste buffer that any pane can read back.

// terminalRoute names how an OSC 52 copy travels to the terminal.
type terminalRoute int

const (
	routeTTY             terminalRoute = iota // raw OSC 52 on the controlling terminal
	routeTmuxPassthrough                      // wrapped in tmux passthrough (DCS tmux; ...)
	routeTmuxLoadBuffer                       // tmux load-buffer -w; the buffer is deleted at once
	routeRemoteTmux                           // passthrough to a tmux known only from XTVERSION
)

// terminalResult describes where a terminal copy went and who may keep it.
type terminalResult struct {
	route      terminalRoute
	noClear    string   // why a later empty copy cannot clear it; "" when it can
	unverified string   // why tmux could not confirm the route; "" when it did
	nested     bool     // the tmux client runs inside another tmux, which may keep a copy
	clients    int      // tmux clients the copy reached, when more than one
	reached    []string // ttys of the tmux clients a passthrough copy reached, as far as tmux named them
	termName   string   // TERM of the tmux client, for configuration hints
	clientID   string   // TERM and XTVERSION reply of the one client the copy reached, when known, to recognize it on another tty
	attached   []string // with clientID: ttys of every client attached at copy time, none of which is that terminal on a new tty
	plain      bool     // unverified: a plain OSC 52 followed the passthrough (clipboard_osc52 always)
	version    string   // unverified: the tmux version XTVERSION reported, if any
}

func (r terminalResult) clearable() bool { return r.noClear == "" }

// passthroughAlwaysAllowed reports a tmux that XTVERSION named as older
// than 3.3, which has no allow-passthrough option and lets passthrough by.
func (r terminalResult) passthroughAlwaysAllowed() bool {
	m := tmuxReleaseRe.FindStringSubmatch(r.version)
	if m == nil {
		return false
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	return major < 3 || major == 3 && minor < 3
}

// tmuxReleaseRe matches a release number; next-3.6 is a development
// version, newer than 3.5.
var tmuxReleaseRe = regexp.MustCompile(`^(\d+)\.(\d+)`)

// terminalClipboard sends copies to the terminal clipboard.
type terminalClipboard interface {
	copy(value []byte) (terminalResult, error)
	// clear asks the terminal to drop the copy that copy described as last.
	// A pending outcome means it could not be sent yet and should be retried.
	clear(last terminalResult) (clearOutcome, error)
}

// terminalClass is what the terminal answered to XTVERSION. Inside tmux the
// answer comes from tmux itself, or not at all before 3.2: tmux does not
// pass the query on to the terminal it runs in (measured on 3.0a to 3.8).
type terminalClass int

const (
	terminalUnknown terminalClass = iota // no answer, or none yet
	terminalTmux                         // a name starting with "tmux "
	terminalOther                        // any other name
)

type terminalKind struct {
	class terminalClass
	tmux  string // terminalTmux: the version tmux reported, such as 3.5a
}

// terminalRoutes picks the terminal clipboard for a copy. $TMUX names a
// tmux server keetui can query. Without it, a terminal that answers
// XTVERSION as tmux is a tmux elsewhere, typically on the other end of SSH.
type terminalRoutes struct {
	tty    terminalClipboard
	tmux   terminalClipboard // nil unless $TMUX is set
	remote terminalClipboard // nil to treat a remote tmux as a plain terminal
}

// configurable is a route whose copies depend on the copy's options.
type configurable interface {
	// with returns the route for a copy: plain asks for a plain OSC 52
	// next to passthrough that nothing confirms.
	with(kind terminalKind, plain bool) terminalClipboard
}

func newTerminalRoutes(getenv func(string) string) terminalRoutes {
	routes := terminalRoutes{
		tty:    ttyClipboard{write: writeTTY},
		remote: remoteTmuxClipboard{write: writeTTY, settle: settlePassthrough},
	}
	if getenv("TMUX") != "" {
		routes.tmux = tmuxClipboard{run: runTmux, write: writeTTY, settle: settlePassthrough, pane: getenv("TMUX_PANE")}
	}
	return routes
}

func (r terminalRoutes) pick(kind terminalKind, plain bool) terminalClipboard {
	route := r.tty
	switch {
	case r.tmux != nil && kind.class == terminalOther:
		// A terminal started from a tmux pane inherits $TMUX, but so does
		// a terminal multiplexer running inside the pane, which may hand a
		// plain OSC 52 on to tmux. keetui cannot tell them apart, and
		// tmux's queried state belongs to a pane keetui is not in.
		return refusedTerminal{errTerminalNotTmux}
	case r.tmux != nil:
		route = r.tmux
	case kind.class == terminalTmux && r.remote != nil:
		route = r.remote
	}
	if c, ok := route.(configurable); ok {
		return c.with(kind, plain)
	}
	return route
}

var errTerminalNotTmux = errors.New("$TMUX is set, but the terminal isn't tmux (unset TMUX outside tmux)")

// refusedTerminal sends nothing and reports why.
type refusedTerminal struct{ err error }

func (r refusedTerminal) copy([]byte) (terminalResult, error) { return terminalResult{}, r.err }

func (refusedTerminal) clear(terminalResult) (clearOutcome, error) { return clearNothing, nil }

// ttyClipboard sends a raw OSC 52 to the terminal.
type ttyClipboard struct{ write func(string) error }

func (t ttyClipboard) copy(value []byte) (terminalResult, error) {
	return terminalResult{}, t.write(ansi.SetSystemClipboard(string(value)))
}

func (t ttyClipboard) clear(terminalResult) (clearOutcome, error) {
	return clearRequested, t.write(ansi.SetSystemClipboard(""))
}

// remoteTmuxClipboard serves a terminal that answered XTVERSION as tmux
// while $TMUX is unset, such as SSH from a tmux pane, or sudo in one. That
// tmux cannot be queried. Passthrough is the only route that leaves no
// paste buffer behind in it, and nothing confirms that it is allowed; with
// clipboard_osc52 always, a plain OSC 52 follows, which tmux forwards with
// set-clipboard on, keeping a copy.
type remoteTmuxClipboard struct {
	write   func(string) error
	settle  func() // see passthroughSettle; nil in tests
	plain   bool
	version string
}

func (r remoteTmuxClipboard) with(kind terminalKind, plain bool) terminalClipboard {
	r.plain, r.version = plain, kind.tmux
	return r
}

func (r remoteTmuxClipboard) copy(value []byte) (terminalResult, error) {
	if len(value) == 0 {
		return terminalResult{}, errEmptyTmuxCopy
	}
	if r.settle != nil {
		r.settle()
	}
	res := terminalResult{route: routeRemoteTmux, unverified: "the terminal reports tmux, but $TMUX is unset",
		plain: r.plain, version: r.version}
	return res, r.write(unverifiedOSC52(value, r.plain))
}

func (r remoteTmuxClipboard) clear(last terminalResult) (clearOutcome, error) {
	if r.settle != nil {
		r.settle()
	}
	return clearRequested, r.write(unverifiedOSC52(nil, last.plain))
}

// tmux treats empty OSC 52 data as no copy at all, so an empty value could
// only clear the clipboard, or (load-buffer) leave nothing to delete.
var errEmptyTmuxCopy = errors.New("the value is empty; nothing sent through tmux")

// passthroughOSC52 wraps an OSC 52 copy (or clear, for an empty value) so
// tmux forwards it to the terminal unchanged instead of interpreting it.
func passthroughOSC52(value []byte) string {
	return ansi.TmuxPassthrough(ansi.SetSystemClipboard(string(value)))
}

// unverifiedOSC52 is a copy (or clear) for a tmux that cannot confirm
// passthrough: passthrough, then with plain a plain OSC 52.
func unverifiedOSC52(value []byte, plain bool) string {
	seq := passthroughOSC52(value)
	if plain {
		seq += ansi.SetSystemClipboard(string(value))
	}
	return seq
}

// writeTTY writes an escape sequence to the controlling terminal itself,
// synchronously, rather than queueing it with tea.Raw: Bubble Tea's
// TEA_TRACE log records everything queued that way, base64 secrets
// included, and drops its write errors. One writer also serves the running
// program and ReleaseClipboard after exit, where /dev/tty is the only way.
func writeTTY(seq string) error {
	f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("terminal clipboard: %w", err)
	}
	defer func() { _ = f.Close() }()
	// Fd puts the descriptor in blocking mode. Go's poller completes a
	// partial non-blocking write later, so a renderer frame written to
	// stdout meanwhile could split the sequence; Linux keeps a blocking tty
	// write whole under the tty's write lock.
	_ = f.Fd()
	if _, err := f.WriteString(seq); err != nil {
		return fmt.Errorf("terminal clipboard: %w", err)
	}
	return nil
}
