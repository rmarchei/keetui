//go:build linux

package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rmarchei/keetui/internal/x11clip"
)

// linuxClipboard uses wl-copy on Wayland, where wl-clipboard 2.3.0 and later
// can mark copies as sensitive (x-kde-passwordManagerHint=secret). On X11 it
// owns CLIPBOARD in-process (internal/x11clip), which offers the same marker
// and tracks ownership through the X server; xclip and xsel, which cannot
// mark copies, are fallbacks. Tool copies are checked by content.
//
// In a remote session (see clipboard_remote.go) WAYLAND_DISPLAY names the
// remote host's compositor, and only an X display that sshd forwards can be
// the user's (see route).
type linuxClipboard struct {
	getenv func(string) string
	has    func(string) bool
	// run executes wl-copy, runX xclip and xsel, and output the tools that
	// read the clipboard; tests replace them, so they never reach a real
	// clipboard.
	run    func(env []string, input []byte, name string, args ...string) error
	runX   func(env []string, input []byte, name string, args ...string) error
	output func(env []string, name string, args ...string) ([]byte, error)
	newX11 func(display, authority string) x11Owner
	// listeners returns the owners of the TCP sockets listening on a port
	// at a loopback or unspecified address (listenerUIDs); tests replace
	// it, so they need no sockets.
	listeners func(port int) ([]uint32, error)
	uid       uint32 // keetui's effective uid

	tool      string   // backend of the last write: wl-copy, x11, xclip or xsel
	env       []string // the route's env for the last write, for the tools that check and clear it
	x11       *x11Slot // the owner of the last X11 copy
	owners    []*x11Slot
	byContent bool // holds matched another client's copy of the value
	gone      bool // holds found the forwarded X server gone, with keetui's copy

	sensitiveOnce sync.Once
	sensitive     bool // wl-copy understands --sensitive
}

// x11Owner is the in-process CLIPBOARD owner, *x11clip.Owner. A Copy that
// fails blanks the earlier copy, so a value copied elsewhere instead (a tool
// or OSC 52) never leaves the previous secret on the clipboard.
type x11Owner interface {
	Copy(value []byte, marked bool) error
	Owns() bool
	Lost() bool
	Blank()
	Read() ([]byte, error)
	Close() error
}

// x11Slot is keetui's owner on one X display. keetui keeps every owner it
// made until it exits: one whose display the session no longer uses keeps
// its clipboard, empty, so that managers which ignore the marker have no
// ownerless clipboard to refill from their history.
type x11Slot struct {
	owner     x11Owner
	display   string
	authority string // X authority file; "" for keetui's own
	forwarded bool   // display is forwarded by the SSH session
}

// x11Name names keetui's owner window, so that clipboard managers which
// filter by window (clipmenu, greenclip) can skip keetui's copies.
const x11Name = "keetui"

// handOffTimeout bounds the exit hand-off to xclip.
const handOffTimeout = 2 * time.Second

// x11ToolTimeout bounds the part of an xclip or xsel run that keetui waits
// for: connecting to the X server, reading the input and taking the
// clipboard. Both then leave a forked process holding the copy, which keetui
// does not wait for. Tests shorten it.
var x11ToolTimeout = 2 * time.Second

// errToolTimeout reports an xclip or xsel run that was still waiting for the
// X server after x11ToolTimeout.
var errToolTimeout = errors.New("timed out waiting for the X server")

func newNativeClipboard() nativeClipboard {
	return &linuxClipboard{
		getenv: os.Getenv,
		has: func(name string) bool {
			_, err := exec.LookPath(name)
			return err == nil
		},
		run:    pipeTo,
		runX:   pipeDetached,
		output: outputBounded,
		newX11: func(display, authority string) x11Owner {
			return &x11clip.Owner{Name: x11Name, Display: display, Authority: authority}
		},
		listeners: listenerUIDs,
		uid:       uint32(os.Geteuid()),
	}
}

// linuxTool picks the clipboard for the session. X11 clients reach Wayland
// apps only on some compositors (not wlroots or Hyprland without an X11
// window in focus), so wl-copy comes first.
func linuxTool(getenv func(string) string, has func(string) bool) string {
	if getenv("WAYLAND_DISPLAY") != "" && has("wl-copy") && has("wl-paste") {
		return "wl-copy"
	}
	if getenv("DISPLAY") != "" {
		return "x11"
	}
	return ""
}

// x11Fallback picks the tool for copies the in-process owner cannot make.
func x11Fallback(has func(string) bool) string {
	for _, tool := range []string{"xclip", "xsel"} {
		if has(tool) {
			return tool
		}
	}
	return ""
}

// route picks the clipboard for a copy. Locally that is linuxTool's choice.
// In a remote session, WAYLAND_DISPLAY belongs to the remote host, and so
// does an X display that sshd did not forward (:0 of the remote desktop, a
// VNC server's :1). A forwarded display (localhost:10.0) may be the user's,
// but on an XQuartz server or through an untrusted ssh -X connection its
// copies cannot be pasted, so auto also sends the copy to the terminal.
// Inside tmux, panes keep the DISPLAY they started with, so the session's
// DISPLAY, which tmux refreshes on attach, takes precedence, local or
// remote, and so does its XAUTHORITY: a tmux started on a desktop keeps the
// display manager's in its panes, while sshd writes the forwarded display's
// cookie to ~/.Xauthority. A display on this machine's loopback, as sshd
// forwards it, must be served by keetui's user (see displayRefusal).
func (l *linuxClipboard) route(s remoteSession) nativeRoute {
	if !s.remote {
		return l.localRoute(s)
	}
	display, from, authority := l.getenv("DISPLAY"), "DISPLAY", ""
	if v, a, known := l.sessionDisplay(s.tmux); known {
		display, from, authority = v, "the tmux session's DISPLAY", a
	}
	wayland := ""
	if l.getenv("WAYLAND_DISPLAY") != "" {
		wayland = " WAYLAND_DISPLAY belongs to this host's compositor."
	}
	switch {
	case display == "":
		return nativeRoute{why: "No clipboard of yours here: no X display is forwarded." + wayland}
	case !forwardedDisplay(display):
		return nativeRoute{dest: "X display " + display, why: fmt.Sprintf(
			"Not copied to X display %s (%s): sshd did not forward it, so it belongs to this host, not to you.%s",
			display, from, wayland)}
	}
	if why := l.displayRefusal(display); why != "" {
		return nativeRoute{dest: "X display " + display, why: fmt.Sprintf("Not copied to X display %s (%s): %s", display, from, why)}
	}
	return nativeRoute{ok: true, alsoTerminal: true, display: display, authority: authority, forwarded: true,
		dest: "the X display your SSH session forwards (" + display + ")"}
}

// sessionDisplay returns the tmux session's DISPLAY, which tmux refreshes
// on every attach, and the X authority file that goes with it: the
// session's XAUTHORITY, ~/.Xauthority when the client attaching had none
// (what its X clients read), or "" (keetui's own) when tmux does not track
// it. known is false when tmux does not track DISPLAY, or did not answer.
func (l *linuxClipboard) sessionDisplay(e tmuxEnv) (display, authority string, known bool) {
	if display, known = e.lookup("DISPLAY"); !known {
		return "", "", false
	}
	if a, tracked := e.lookup("XAUTHORITY"); tracked {
		authority = a
		if a == "" && l.getenv("HOME") != "" {
			authority = filepath.Join(l.getenv("HOME"), ".Xauthority")
		}
	}
	return display, authority, true
}

// localRoute picks this machine's clipboard. Inside tmux, the X display is
// the session's (see route): a pane keeps the DISPLAY of the attach it
// started in, which may be an SSH session's forwarded display, long gone.
// In a herdr pane, the clipboard is the one on the display of the herdr
// client the user typed on (see herdrDesktop), when it differs from
// keetui's: without a display of its own, keetui would leave the copy to
// herdr, whose client writes it unmarked, and an old one may be gone.
func (l *linuxClipboard) localRoute(s remoteSession) nativeRoute {
	c := s.desktop
	// keetui's own environment, with the tmux session's display.
	own, from := l.getenv, "DISPLAY"
	tmuxDisplay, tmuxAuthority, fromTmux := l.sessionDisplay(s.tmux)
	if fromTmux {
		own, from = func(name string) string {
			if name == "DISPLAY" {
				return tmuxDisplay
			}
			return l.getenv(name)
		}, "the tmux session's DISPLAY"
	}
	clientEnv := func(name string) string {
		if v := envValue(c.env, name); v != "" || name != "XDG_RUNTIME_DIR" {
			return v
		}
		return l.getenv(name) // the same user's
	}
	tool, getenv, client := linuxTool(own, l.has), own, 0
	if t := linuxTool(clientEnv, l.has); t != "" && (t != tool || clientEnv(displayVar[t]) != own(displayVar[t])) {
		tool, getenv, client = t, clientEnv, c.pid
		from = fmt.Sprintf("herdr client pid %d's DISPLAY", c.pid)
	}
	switch tool {
	case "wl-copy":
		wayland := getenv("WAYLAND_DISPLAY")
		r := nativeRoute{ok: true, client: client, dest: "the Wayland clipboard (" + wayland + ")"}
		if client != 0 {
			r.env = []string{"WAYLAND_DISPLAY=" + wayland}
			if dir := getenv("XDG_RUNTIME_DIR"); dir != "" {
				r.env = append(r.env, "XDG_RUNTIME_DIR="+dir)
			}
		}
		return r
	case "x11":
		display := getenv("DISPLAY")
		if why := l.displayRefusal(display); why != "" {
			return nativeRoute{dest: "X display " + display, why: fmt.Sprintf("Not copied to X display %s (%s): %s", display, from, why)}
		}
		r := nativeRoute{ok: true, client: client, display: display, dest: "X display " + display}
		switch {
		case client != 0:
			// The client's X clients read ~/.Xauthority without XAUTHORITY.
			r.authority = getenv("XAUTHORITY")
			if r.authority == "" && l.getenv("HOME") != "" {
				r.authority = filepath.Join(l.getenv("HOME"), ".Xauthority")
			}
		case fromTmux:
			r.authority = tmuxAuthority
			if display != l.getenv("DISPLAY") {
				r.dest += " (the tmux session's DISPLAY; this pane's is " + l.getenv("DISPLAY") + ")"
			}
		default:
			return r
		}
		// xclip and xsel would reach keetui's own display.
		r.env = []string{"DISPLAY=" + display}
		if r.authority != "" {
			r.env = append(r.env, "XAUTHORITY="+r.authority)
		}
		return r
	}
	if fromTmux && tmuxDisplay == "" && l.getenv("DISPLAY") != "" {
		return nativeRoute{why: fmt.Sprintf("No local clipboard: WAYLAND_DISPLAY is not set, and the tmux session has no DISPLAY "+
			"(the client that attached last had none); this pane's, %s, is left from an earlier attach.", l.getenv("DISPLAY"))}
	}
	return nativeRoute{why: "No local clipboard: neither WAYLAND_DISPLAY nor DISPLAY is set."}
}

// displayRefusal says why keetui must not copy to an X display that it
// reaches over TCP on this machine's loopback; "" when it may, or when the
// display is elsewhere. That is the display sshd forwards (localhost:10.0),
// whose port sshd frees when the SSH session ends: a DISPLAY left from that
// session (in a tmux pane, or in the tmux session's environment until a
// client attaches again) then names a port that any user of this machine
// may listen on, who would receive keetui's copies. The listener must be
// keetui's user's, as sshd's forwarding is, or root's.
func (l *linuxClipboard) displayRefusal(display string) string {
	port, ok := loopbackDisplay(display)
	if !ok {
		return ""
	}
	ended := ""
	if forwardedDisplay(display) {
		ended = " The SSH session that forwarded it has probably ended: in tmux, attach again from your current session " +
			"(tmux then updates DISPLAY), or restart keetui."
	}
	uids, err := l.listeners(port)
	switch {
	case err != nil:
		return fmt.Sprintf("keetui could not tell whether its TCP port %d is yours (%v): another user could be listening there.", port, err)
	case len(uids) == 0:
		return fmt.Sprintf("nothing listens on its TCP port %d, which any user of this machine could take.%s", port, ended)
	}
	for _, uid := range uids {
		if uid != l.uid && uid != 0 {
			return fmt.Sprintf("its TCP port %d belongs to another user (uid %d), who would receive the copy.%s", port, uid, ended)
		}
	}
	return ""
}

// x11TCPBase is the TCP port of X display 0.
const x11TCPBase = 6000

// loopbackDisplay reports an X display that is reached over TCP on this
// machine's loopback, as x11clip and xclip parse it, and its port.
func loopbackDisplay(display string) (port int, ok bool) {
	i := strings.LastIndexByte(display, ':')
	if i <= 0 || display[0] == '/' {
		return 0, false // a local socket, or a launchd one
	}
	host := display[:i]
	if j := strings.LastIndexByte(host, '/'); j >= 0 {
		host = host[j+1:] // protocol/host
	}
	number, _, _ := strings.Cut(display[i+1:], ".")
	n, err := strconv.Atoi(number)
	if err != nil || n < 0 || !loopbackHost(host) {
		return 0, false
	}
	return x11TCPBase + n, true
}

// loopbackHost reports a display's host part that names this machine's
// loopback: localhost, as sshd's forwarded displays do, or a loopback
// address.
func loopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	a, err := netip.ParseAddr(host)
	return err == nil && a.Unmap().IsLoopback()
}

// displayVar names the variable that names the display of each of
// linuxTool's choices.
var displayVar = map[string]string{"wl-copy": "WAYLAND_DISPLAY", "x11": "DISPLAY"}

// forwardedDisplay reports a display as sshd forwards it: a host other than
// unix and a display number of at least 10 (X11DisplayOffset), as in
// localhost:10.0 or, with X11UseLocalhost no, hostname:10.0.
func forwardedDisplay(display string) bool {
	i := strings.LastIndexByte(display, ':')
	if i <= 0 || display[:i] == "unix" {
		return false
	}
	number, _, _ := strings.Cut(display[i+1:], ".")
	n, err := strconv.Atoi(number)
	return err == nil && n >= 10
}

func (l *linuxClipboard) wlSensitive() bool {
	l.sensitiveOnce.Do(func() {
		out, err := l.output(nil, "wl-copy", "--help")
		l.sensitive = err == nil && bytes.Contains(out, []byte("--sensitive"))
	})
	return l.sensitive
}

func (l *linuxClipboard) write(value []byte, _ bool, r nativeRoute) (bool, error) {
	tool := ""
	switch {
	case r.ok && r.display != "":
		tool = "x11"
	case r.ok:
		tool = "wl-copy"
	}
	var err error
	marked := false
	switch tool {
	case "wl-copy":
		// An explicit type also keeps wl-copy from running xdg-mime on a
		// temporary file holding the secret.
		args := []string{"--type", "text/plain;charset=utf-8"}
		if l.wlSensitive() {
			args, marked = append(args, "--sensitive"), true
		}
		err = l.run(wlEnv(r.env, l.getenv), value, "wl-copy", args...)
	case "x11":
		tool, marked, err = l.writeX11(value, r)
	default:
		err = errNoClipboard
	}
	if err != nil {
		return false, err
	}
	l.tool, l.env, l.byContent = tool, r.env, false
	return marked, nil
}

// writeX11 copies through the in-process owner. When the owner fails (no
// connection, unconfirmed ownership) or the value would need an INCR
// transfer, xclip or xsel makes an unmarked copy instead, but not after a
// timeout, as they would wait for the same server, and not on a forwarded
// display, where what they fork keeps the SSH session's X11 channel, and so
// ssh, open after keetui exits.
func (l *linuxClipboard) writeX11(value []byte, r nativeRoute) (tool string, marked bool, err error) {
	slot := l.slot(r)
	if l.x11 != nil && l.x11 != slot {
		// The session's display changed: wipe the copy on the old one, but
		// keep its clipboard (see x11Slot).
		l.x11.owner.Blank()
	}
	l.x11 = slot
	if err = slot.owner.Copy(value, true); err == nil {
		return "x11", true, nil
	}
	if r.forwarded || errors.Is(err, x11clip.ErrTimeout) {
		return "", false, err
	}
	// Only local routes get here, whose display is keetui's $DISPLAY, or the
	// tmux session's or a herdr client's, which r.env names.
	tool = x11Fallback(l.has)
	switch tool {
	case "xclip":
		if ferr := l.runX(toolEnv(r.env), value, "xclip", "-selection", "clipboard"); ferr != nil {
			return "", false, fmt.Errorf("%w; %w", err, ferr)
		}
	case "xsel":
		if ferr := l.runX(toolEnv(r.env), value, "xsel", "--clipboard", "--input"); ferr != nil {
			return "", false, fmt.Errorf("%w; %w", err, ferr)
		}
	default:
		return "", false, err
	}
	return tool, false, nil
}

// slot returns keetui's owner for a route's display, making one if needed.
func (l *linuxClipboard) slot(r nativeRoute) *x11Slot {
	for _, s := range l.owners {
		if s.display == r.display && s.authority == r.authority {
			s.forwarded = r.forwarded
			return s
		}
	}
	s := &x11Slot{owner: l.newX11(r.display, r.authority), display: r.display, authority: r.authority, forwarded: r.forwarded}
	l.owners = append(l.owners, s)
	return s
}

// holds asks the X server whether keetui's copy is still on the clipboard,
// and reads tool copies back; an unreadable clipboard (empty, or an image)
// is not ours.
func (l *linuxClipboard) holds(matches func([]byte) bool, alsoTerminal bool) bool {
	l.byContent, l.gone = false, false
	var out []byte
	var err error
	switch l.tool {
	case "x11":
		x := l.x11.owner
		if x.Owns() {
			return true
		}
		if l.x11.forwarded && x.Lost() {
			// The X server at the user's end went away (XQuartz quit, the
			// SSH session ended), and keetui's copy with it. Nothing is
			// left to clear there, and asking again would make the ssh
			// client print over the screen, but the terminal copy made
			// next to it is still to be cleared.
			l.gone = true
			return true
		}
		if !alsoTerminal {
			return false // another client copied since
		}
		// The terminal applies its OSC 52 copy asynchronously, possibly
		// after keetui's, so the clipboard may hold the same value.
		out, err = x.Read()
		l.byContent = err == nil && matches(out)
		clear(out)
		return l.byContent
	case "wl-copy":
		out, err = l.output(toolEnv(l.env), "wl-paste", "--no-newline", "--type", "text")
	case "xclip":
		out, err = l.output(toolEnv(l.env), "xclip", "-selection", "clipboard", "-o")
	case "xsel":
		out, err = l.output(toolEnv(l.env), "xsel", "--clipboard", "--output")
	default:
		return false
	}
	// A tool that got no answer from the server leaves the copy to the
	// clear, as Owns does for keetui's own copy: replacing a copy made
	// before the server stopped answering beats leaving the secret, and the
	// clear reports the timeout.
	held := err == nil && matches(out) || errors.Is(err, errToolTimeout)
	clear(out)
	return held
}

func (l *linuxClipboard) clear() error {
	switch l.tool {
	case "wl-copy":
		return l.run(toolEnv(l.env), nil, "wl-copy", "--clear")
	case "x11":
		switch {
		case l.gone:
			return nil
		case l.byContent:
			// Another client (the terminal) holds keetui's value: replace
			// it with an empty copy, which keetui then keeps.
			return l.x11.owner.Copy(nil, false)
		}
		// Keep the clipboard, empty. Giving it up would let managers that
		// ignore the marker (ClipIt, Parcellite, Diodon, xfce4-clipman,
		// qlipper, GPaste before 51.1) restore the secret from history.
		l.x11.owner.Blank()
		return nil
	case "xclip":
		return l.runX(toolEnv(l.env), nil, "xclip", "-selection", "clipboard")
	case "xsel":
		return l.clearXsel()
	}
	return errNoClipboard
}

// clearXsel replaces xsel's copy with an empty one. xsel cannot own an
// empty clipboard, and its --clear gives the clipboard up, so managers that
// ignore the marker may restore the copy; it is the last resort.
func (l *linuxClipboard) clearXsel() error {
	if l.x11 != nil && l.x11.owner.Copy(nil, false) == nil {
		return nil
	}
	if l.has("xclip") {
		return l.runX(toolEnv(l.env), nil, "xclip", "-selection", "clipboard")
	}
	return l.runX(toolEnv(l.env), nil, "xsel", "--clipboard", "--clear")
}

// close runs at exit. The X server drops keetui's clipboard with its
// connection, and managers that ignore the marker then restore their newest
// entry, possibly the secret. An empty copy owned by xclip, which outlives
// keetui (see pipeDetached), prevents that; xsel cannot own an empty
// clipboard. On a display the SSH session forwards, xclip would hold the
// session's X11 channel, and with it ssh, open after keetui exits, so there
// is no hand-off: managers on the user's desktop that ignore the marker may
// then restore their newest entry.
func (l *linuxClipboard) close() {
	for _, s := range l.owners {
		if !s.forwarded && s.owner.Owns() && l.has("xclip") {
			l.handOff(s)
		}
		_ = s.owner.Close()
	}
}

// handOff gives the clipboard to an empty xclip copy and waits until xclip,
// which takes the clipboard after forking, owns it, so that it never goes
// ownerless in between. Only displays keetui reached locally get here,
// whose authority file is keetui's own, the tmux session's or a herdr
// client's.
func (l *linuxClipboard) handOff(s *x11Slot) {
	done := make(chan error, 1)
	env := envWith("DISPLAY=" + s.display)
	if s.authority != "" {
		env = envWith("DISPLAY="+s.display, "XAUTHORITY="+s.authority)
	}
	go func() { done <- l.runX(env, nil, "xclip", "-selection", "clipboard") }()
	deadline := time.After(handOffTimeout)
	select {
	case err := <-done:
		if err != nil {
			return // xclip failed, so it will not take the clipboard
		}
	case <-deadline:
		return
	}
	for s.owner.Owns() {
		select {
		case <-deadline:
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// pipeDetached runs an X11 clipboard tool like pipeTo, in a session of its
// own. xclip forks the server that keeps its copy without leaving keetui's
// session, and when keetui is its terminal's session leader (xterm -e
// keetui, tmux new-window keetui, ssh -t host keetui), the kernel hangs up
// the terminal's foreground processes as keetui exits: xclip would die with
// it, leaving the clipboard without an owner for managers to refill. xsel
// detaches itself, and wl-copy ignores SIGHUP.
//
// The copy runs in the UI's update, so pipeDetached waits at most
// x11ToolTimeout: a server that does not answer keeps xclip and xsel waiting
// without a limit. A tool still copying a value then is killed, with what it
// forked, so that a server answering later cannot receive a copy keetui
// reported as not made, and would never clear. A tool making an empty copy
// (a clear, or the hand-off at exit) is left to finish, as its copy can only
// replace what keetui copied.
func pipeDetached(env []string, input []byte, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(input)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.WaitDelay = x11ToolTimeout // in case what it forked holds the input pipe open
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	var waitErr error
	exited := make(chan struct{})
	go func() {
		waitErr = cmd.Wait()
		close(exited)
	}()
	t := time.NewTimer(x11ToolTimeout)
	defer t.Stop()
	select {
	case <-exited:
	case <-t.C:
		select {
		case <-exited: // it finished just now
		default:
			if len(input) > 0 {
				// The tool leads its own process group (Setsid), which
				// therefore holds what it forked and never keetui.
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				<-exited
			}
			return fmt.Errorf("%s: %w", name, errToolTimeout)
		}
	}
	if waitErr != nil {
		return fmt.Errorf("%s: %w", name, waitErr)
	}
	return nil
}

// outputBounded runs a tool that reads the clipboard, with env (nil:
// keetui's), for at most x11ToolTimeout: the read runs in the UI's update
// (timer, lock, quit), and xclip and xsel wait without a limit for a server
// that does not answer.
func outputBounded(env []string, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), x11ToolTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	cmd.WaitDelay = x11ToolTimeout
	out, err := cmd.Output()
	if ctx.Err() != nil {
		clear(out)
		return nil, fmt.Errorf("%s: %w", name, errToolTimeout)
	}
	return out, err
}

// toolEnv is the environment of a clipboard tool reaching a route's display:
// keetui's with the route's env set, or nil for keetui's own.
func toolEnv(set []string) []string {
	if len(set) == 0 {
		return nil
	}
	return envWith(set...)
}

// wlEnv is wl-copy's environment for a copy. It points wl-copy's temporary
// copy of stdin at the per-user runtime directory (a tmpfs) instead of
// /tmp, which may be disk-backed.
func wlEnv(set []string, getenv func(string) string) []string {
	dir := envValue(set, "XDG_RUNTIME_DIR")
	if dir == "" {
		dir = getenv("XDG_RUNTIME_DIR")
	}
	if dir != "" {
		set = append(slices.Clip(set), "TMPDIR="+dir)
	}
	return toolEnv(set)
}
