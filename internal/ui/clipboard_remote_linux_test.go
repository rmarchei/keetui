//go:build linux

package ui

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/x11clip"
)

func TestForwardedDisplay(t *testing.T) {
	for display, want := range map[string]bool{
		"localhost:10.0":    true,
		"localhost:10":      true,
		"localhost:13.0":    true,
		"remotehost:10.0":   true, // X11UseLocalhost no
		"127.0.0.1:11.0":    true,
		":0":                false,
		":0.0":              false,
		":1":                false, // VNC
		":10":               false, // no host part: a local server
		"unix:0":            false,
		"unix:10":           false,
		"localhost:0":       false,
		"localhost:9.0":     false,
		"":                  false,
		"localhost":         false,
		"localhost:x":       false,
		"/tmp/launch/org.x": false,
	} {
		if got := forwardedDisplay(display); got != want {
			t.Errorf("forwardedDisplay(%q) = %v, want %v", display, got, want)
		}
	}
}

var (
	sshSession   = remoteSession{remote: true, why: "SSH_CONNECTION is set"}
	localSession = remoteSession{why: "no SSH session found"}
)

func tmuxSession(lines string) remoteSession {
	return remoteSession{remote: true, why: "the tmux session was last attached over SSH", tmux: parseTmuxEnv([]byte(lines))}
}

func TestLinuxRouteUnderSSH(t *testing.T) {
	wl := []string{"wl-copy", "wl-paste", "xclip"}
	for _, tc := range []struct {
		name      string
		env       map[string]string
		session   remoteSession
		ok        bool
		tool      string
		display   string
		forwarded bool
		why       string
	}{
		// Local sessions behave as before.
		{"local x11", map[string]string{"DISPLAY": ":0"}, localSession, true, "x11", ":0", false, ""},
		{"local wayland", map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"}, localSession, true, "wl-copy", "", false, ""},
		{"local, nothing", nil, localSession, false, "", "", false, "neither WAYLAND_DISPLAY nor DISPLAY"},
		{"local herdr pane", map[string]string{"DISPLAY": ":0"}, remoteSession{herdr: true, why: "every herdr client runs locally (1 found)"},
			true, "x11", ":0", false, ""},
		// Over SSH.
		{"ssh, no display", nil, sshSession, false, "", "", false, "no X display is forwarded"},
		{"ssh -X", map[string]string{"DISPLAY": "localhost:10.0"}, sshSession, true, "x11", "localhost:10.0", true, ""},
		{"ssh, desktop display", map[string]string{"DISPLAY": ":0"}, sshSession, false, "", "", false, "sshd did not forward it"},
		{"ssh, unix display", map[string]string{"DISPLAY": "unix:0"}, sshSession, false, "", "", false, "X display unix:0"},
		{"ssh, wayland", map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, sshSession, false, "", "", false, "this host's compositor"},
		{"ssh -X with wayland", map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": "localhost:10.0"}, sshSession,
			true, "x11", "localhost:10.0", true, ""},
		{"herdr client over ssh, server from the desktop", map[string]string{"DISPLAY": ":0", "HERDR_ENV": "1"},
			remoteSession{remote: true, herdr: true, why: "a herdr client runs over SSH (pid 7)"}, false, "", "", false, "sshd did not forward it"},
		// tmux: the session's DISPLAY over the pane's.
		{"tmux attach over ssh, stale :0", map[string]string{"DISPLAY": ":0", "TMUX": "x"}, tmuxSession("-DISPLAY\nSSH_CONNECTION=a b c d\n"),
			false, "", "", false, "no X display is forwarded"},
		{"tmux attach over ssh -X", map[string]string{"DISPLAY": ":0", "TMUX": "x"}, tmuxSession("DISPLAY=localhost:11.0\n"),
			true, "x11", "localhost:11.0", true, ""},
		{"tmux, stale forwarded pane display", map[string]string{"DISPLAY": "localhost:10.0", "TMUX": "x"}, tmuxSession("-DISPLAY\n"),
			false, "", "", false, "no X display"},
		{"tmux session display from the desktop", map[string]string{"DISPLAY": "localhost:10.0", "TMUX": "x"}, tmuxSession("DISPLAY=:0\n"),
			false, "", "", false, "the tmux session's DISPLAY"},
		{"tmux without DISPLAY in the session", map[string]string{"DISPLAY": "localhost:10.0", "TMUX": "x"}, tmuxSession("SSH_CONNECTION=a\n"),
			true, "x11", "localhost:10.0", true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := newTestLinux(tc.env, &fakeX11{}, &fakeTools{installed: wl})
			r := l.route(tc.session)
			tool := map[bool]string{true: "x11", false: "wl-copy"}[r.display != ""]
			if !r.ok {
				tool = ""
			}
			if r.ok != tc.ok || tool != tc.tool || r.forwarded != tc.forwarded || r.alsoTerminal != tc.forwarded ||
				r.display != tc.display || !strings.Contains(r.why, tc.why) {
				t.Fatalf("route = %+v", r)
			}
			if tc.ok && r.dest == "" {
				t.Fatalf("route without a destination: %+v", r)
			}
		})
	}
}

// A forwarded display gets the marked copy and, with auto, the terminal
// copy; the X server's copy goes to the display the route named, never
// through xclip or xsel, and keetui hands nothing to xclip at exit.
func TestForwardedDisplayCopy(t *testing.T) {
	var displays []string
	x, tools := &fakeX11{}, &fakeTools{installed: []string{"xclip", "xsel"}}
	l := newTestLinux(map[string]string{"DISPLAY": ":0", "TMUX": "x"}, x, tools)
	l.newX11 = func(display, _ string) x11Owner { displays = append(displays, display); return x }
	c, term := newFakeClipboard(nil)
	c.native = l
	c.detect = func() remoteSession { return tmuxSession("DISPLAY=localhost:12.0\n") }
	res, err := c.copy("synthetic-secret", copyOptions{transient: true, osc52: config.OSC52Auto})
	if err != nil || !res.native || !res.marked || !res.terminal || len(term.sent) != 1 {
		t.Fatalf("copy = %+v, %v (terminal %q)", res, err, term.sent)
	}
	if !slices.Equal(displays, []string{"localhost:12.0"}) {
		t.Fatalf("owner displays = %q", displays)
	}
	help := clipboardHelp(res, nil)
	if !strings.Contains(help, "localhost:12.0") || !strings.Contains(help, "Remote session:") {
		t.Fatalf("help = %q", help)
	}
	r, err := c.release()
	if err != nil || r.outcome != clearDone || r.leg != clearRequested || len(term.sent) != 2 {
		t.Fatalf("release = %+v, %v (terminal %q)", r, err, term.sent)
	}
	x.calls = nil
	l.close()
	if len(tools.runs) != 0 || !slices.Equal(x.calls, []string{"Close"}) {
		t.Fatalf("close ran %q, calls %q", tools.runs, x.calls)
	}

	// The owner fails: no tools, the terminal leg still goes out.
	x.copyErr = errors.New("x11clip: EOF")
	res, err = c.copy("synthetic-secret", copyOptions{transient: true, osc52: config.OSC52Auto})
	if err != nil || res.native || !res.terminal || res.nativeErr == nil || len(tools.runs) != 0 {
		t.Fatalf("copy with a dead forwarded server = %+v, %v (runs %q)", res, err, tools.runs)
	}
	x.copyErr = x11clip.ErrTooLarge
	if _, err := l.write([]byte("large"), true, l.route(tmuxSession("DISPLAY=localhost:12.0\n"))); err == nil || len(tools.runs) != 0 {
		t.Fatalf("large forwarded copy = %v (runs %q)", err, tools.runs)
	}
}

// A copy to another display than the last one wipes the copy left there,
// but keeps that clipboard, empty, until exit: an ownerless clipboard would
// let managers that ignore the marker restore the secret from their
// history. At exit a display keetui reached locally goes to xclip.
func TestDisplayChangeKeepsTheOldClipboard(t *testing.T) {
	owners := map[string]*fakeX11{}
	tools := &fakeTools{installed: []string{"xclip"}}
	tools.onRun = func(name string, _ []byte) {
		for display, x := range owners {
			if name == "xclip" && !forwardedDisplay(display) {
				x.stolen("") // xclip took the clipboard
			}
		}
	}
	l := newTestLinux(map[string]string{"DISPLAY": ":0", "TMUX": "x", "HOME": "/home/u"}, nil, tools)
	l.newX11 = func(display, _ string) x11Owner {
		owners[display] = &fakeX11{}
		return owners[display]
	}
	// At the desk, then attached over ssh -X: the desktop's :0, then the
	// forwarded display, then another forwarded one.
	if _, err := l.writeLocal([]byte("one"), true); err != nil {
		t.Fatal(err)
	}
	if _, err := l.write([]byte("two"), true, l.route(tmuxSession("DISPLAY=localhost:10.0\n"))); err != nil {
		t.Fatal(err)
	}
	desk := owners[":0"]
	if !slices.Equal(desk.calls, []string{`Copy("one", true)`, "Blank"}) || !desk.owned || desk.value != nil {
		t.Fatalf("desktop owner calls %q (owned %v, value %q)", desk.calls, desk.owned, desk.value)
	}
	if _, err := l.write([]byte("three"), true, l.route(tmuxSession("DISPLAY=localhost:11.0\n"))); err != nil {
		t.Fatal(err)
	}
	if old := owners["localhost:10.0"]; !slices.Equal(old.calls, []string{`Copy("two", true)`, "Blank"}) {
		t.Fatalf("old forwarded owner calls %q", old.calls)
	}
	// The same display keeps its owner, and coming back reuses one.
	for _, display := range []string{"localhost:11.0", "localhost:10.0"} {
		if _, err := l.write([]byte("four"), true, l.route(tmuxSession("DISPLAY="+display+"\n"))); err != nil || len(owners) != 3 {
			t.Fatalf("write = %v, owners %d", err, len(owners))
		}
	}
	if string(owners["localhost:10.0"].value) != "four" || owners["localhost:11.0"].value != nil {
		t.Fatalf("values %q, %q", owners["localhost:10.0"].value, owners["localhost:11.0"].value)
	}
	// Exit: the desktop's clipboard goes to xclip on :0, forwarded ones close
	// without a hand-off.
	for _, x := range owners {
		x.calls = nil
	}
	l.close()
	if !slices.Equal(tools.runs, []string{`xclip -selection clipboard <""`}) || !slices.Contains(tools.envs[0], "DISPLAY=:0") {
		t.Fatalf("close ran %q with %q", tools.runs, tools.envs)
	}
	for display, x := range owners {
		if len(x.calls) == 0 || x.calls[len(x.calls)-1] != "Close" || forwardedDisplay(display) && slices.Contains(x.calls, "Owns") {
			t.Errorf("%s: calls %q", display, x.calls)
		}
	}
}

// Over ssh -X into a tmux started on a desktop, the pane's XAUTHORITY is
// the display manager's: the session's, which tmux refreshes on attach,
// goes with the session's DISPLAY.
func TestSessionDisplayTakesTheSessionsAuthority(t *testing.T) {
	env := map[string]string{"DISPLAY": ":0", "XAUTHORITY": "/run/user/1000/gdm/Xauthority", "HOME": "/home/u", "TMUX": "x"}
	for _, tc := range []struct {
		session string
		want    string
	}{
		{"DISPLAY=localhost:10.0\n-XAUTHORITY\n", "/home/u/.Xauthority"},
		{"DISPLAY=localhost:10.0\nXAUTHORITY=/tmp/ssh.xauth\n", "/tmp/ssh.xauth"},
		{"DISPLAY=localhost:10.0\n", ""}, // tmux does not track it: keetui's own
	} {
		var authorities []string
		l := newTestLinux(env, nil, &fakeTools{})
		l.newX11 = func(_, authority string) x11Owner { authorities = append(authorities, authority); return &fakeX11{} }
		r := l.route(tmuxSession(tc.session))
		if !r.ok || r.authority != tc.want {
			t.Fatalf("%q: route = %+v", tc.session, r)
		}
		if _, err := l.write([]byte("synthetic"), true, r); err != nil || !slices.Equal(authorities, []string{tc.want}) {
			t.Fatalf("%q: owner authorities %q, %v", tc.session, authorities, err)
		}
	}
	// Without tmux, keetui's own environment counts.
	l := newTestLinux(map[string]string{"DISPLAY": "localhost:10.0", "XAUTHORITY": "/tmp/x"}, nil, &fakeTools{})
	if r := l.route(sshSession); !r.ok || r.authority != "" {
		t.Fatalf("route = %+v", r)
	}

	// F1 names the file of a copy that failed.
	c, _ := newFakeClipboard(nil)
	c.native = newTestLinux(env, &fakeX11{copyErr: errors.New("x11clip: EOF")}, &fakeTools{})
	c.detect = func() remoteSession { return tmuxSession("DISPLAY=localhost:10.0\n-XAUTHORITY\n") }
	res, err := c.copy("synthetic-secret", copyOptions{transient: true, osc52: config.OSC52Auto})
	if help := clipboardHelp(res, err); res.native || !strings.Contains(help, "X authority file /home/u/.Xauthority") {
		t.Fatalf("copy = %+v, %v; help %q", res, err, help)
	}
}

// A local session in tmux copies to the session's DISPLAY, which tmux
// refreshes on attach, not to the pane's: a pane started during ssh -X
// keeps that session's forwarded display after it ended, a port that
// another user may listen on by then. The session's XAUTHORITY goes with
// its DISPLAY, and xclip, the fallback, reaches the same display.
func TestLocalTmuxSessionDisplayOverThePanes(t *testing.T) {
	pane := map[string]string{"DISPLAY": "localhost:10.0", "TMUX": "/tmp/tmux-1000/default,1,0", "HOME": "/home/u"}
	d := sessionDetector{
		getenv: func(k string) string { return pane[k] },
		tmuxEnv: func() (tmuxEnv, error) {
			return parseTmuxEnv([]byte("-SSH_CONNECTION\nDISPLAY=:0\nXAUTHORITY=/run/user/1000/gdm/Xauthority\n")), nil
		},
	}
	s := d.detect()
	if s.remote {
		t.Fatalf("session = %+v", s)
	}
	var dialed []string
	x, tools := &fakeX11{copyErr: x11clip.ErrNotOwner}, &fakeTools{installed: []string{"xclip"}, clip: []byte("synthetic-secret")}
	l := newTestLinux(pane, x, tools)
	l.newX11 = func(display, authority string) x11Owner { dialed = append(dialed, display+" "+authority); return x }
	l.listeners = func(port int) ([]uint32, error) {
		t.Errorf("looked up port %d for a local socket", port)
		return nil, nil
	}
	c, term := newFakeClipboard(nil)
	c.native = l
	c.detect = func() remoteSession { return s }
	res, err := c.copy("synthetic-secret", copyOptions{transient: true, osc52: config.OSC52Auto})
	if err != nil || !res.native || res.terminal || len(term.sent) != 0 {
		t.Fatalf("copy = %+v, %v", res, err)
	}
	if !slices.Equal(dialed, []string{":0 /run/user/1000/gdm/Xauthority"}) {
		t.Fatalf("owner dialed %q", dialed)
	}
	if help := clipboardHelp(res, nil); !strings.Contains(help, "Copied to X display :0 (the tmux session's DISPLAY; this pane's is localhost:10.0).") {
		t.Fatalf("help = %q", help)
	}
	if r, err := c.release(); err != nil || r.outcome != clearDone {
		t.Fatalf("release = %+v, %v", r, err)
	}
	want := []string{"DISPLAY=:0", "XAUTHORITY=/run/user/1000/gdm/Xauthority"}
	if len(tools.envs) != 3 {
		t.Fatalf("xclip ran %q", tools.runs)
	}
	for i, env := range tools.envs {
		if !containsAll(env, want) {
			t.Errorf("%s ran with %q, want %q", tools.runs[i], env, want)
		}
	}

	wl := []string{"wl-copy", "wl-paste"}
	for _, tc := range []struct {
		name      string
		pane      map[string]string
		session   string // the tmux session environment
		tools     []string
		ok        bool
		display   string // "" for none, or Wayland with ok
		authority string
		env       []string // what the tools get set; nil for keetui's own
		dest, why string
	}{
		{"the session's display", map[string]string{"DISPLAY": "localhost:10.0"}, "DISPLAY=:0\n", nil,
			true, ":0", "", []string{"DISPLAY=:0"}, "X display :0 (the tmux session's DISPLAY; this pane's is localhost:10.0)", ""},
		{"the attaching client's authority file", map[string]string{"DISPLAY": "localhost:10.0", "HOME": "/home/u"}, "DISPLAY=:0\n-XAUTHORITY\n", nil,
			true, ":0", "/home/u/.Xauthority", []string{"DISPLAY=:0", "XAUTHORITY=/home/u/.Xauthority"}, "X display :0 (the tmux session's DISPLAY", ""},
		{"the same display", map[string]string{"DISPLAY": ":0"}, "DISPLAY=:0\n", nil,
			true, ":0", "", []string{"DISPLAY=:0"}, "X display :0", ""},
		{"no display in the session", map[string]string{"DISPLAY": "localhost:10.0"}, "-DISPLAY\n", nil,
			false, "", "", nil, "", "the tmux session has no DISPLAY (the client that attached last had none); this pane's, localhost:10.0, is left"},
		{"no display in the session, Wayland", map[string]string{"DISPLAY": "localhost:10.0", "WAYLAND_DISPLAY": "wayland-0"}, "-DISPLAY\n", wl,
			true, "", "", nil, "the Wayland clipboard (wayland-0)", ""},
		{"the session's display under Wayland", map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, "DISPLAY=:0\n", wl,
			true, "", "", nil, "the Wayland clipboard (wayland-0)", ""},
		{"display not tracked", map[string]string{"DISPLAY": ":1"}, "SSH_AUTH_SOCK=/tmp/a\n", nil,
			true, ":1", "", nil, "X display :1", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pane := maps.Clone(tc.pane)
			pane["TMUX"] = "x"
			l := newTestLinux(pane, &fakeX11{}, &fakeTools{installed: tc.tools})
			r := l.route(remoteSession{why: "the tmux session was last attached locally", tmux: parseTmuxEnv([]byte(tc.session))})
			if r.ok != tc.ok || r.display != tc.display || r.authority != tc.authority || r.forwarded || r.alsoTerminal ||
				!slices.Equal(r.env, tc.env) || !strings.HasPrefix(r.dest, tc.dest) || !strings.Contains(r.why, tc.why) {
				t.Fatalf("route = %+v", r)
			}
		})
	}
}

// A display on this machine's loopback, as sshd forwards it, is used only
// while keetui's user, or root, listens on its port: once the SSH session
// ends, any user may take the port and would receive the copies. Then
// nothing goes to that X server, F1 says why, and auto sends the copy to
// the terminal instead. This holds for every way keetui comes by the
// display: its own DISPLAY, a tmux session's, or a herdr client's.
func TestLoopbackDisplayNeedsYourListener(t *testing.T) {
	sessions := []struct {
		name    string
		env     map[string]string
		session remoteSession
	}{
		{"ssh -X", map[string]string{"DISPLAY": "localhost:10.0"}, sshSession},
		{"tmux attached over ssh -X", map[string]string{"DISPLAY": ":0", "TMUX": "x"}, tmuxSession("DISPLAY=localhost:10.0\nSSH_CONNECTION=a b c d\n")},
		{"a local tmux session", map[string]string{"DISPLAY": ":0", "TMUX": "x"},
			remoteSession{why: "the tmux session was last attached locally", tmux: parseTmuxEnv([]byte("-SSH_CONNECTION\nDISPLAY=localhost:10.0\n"))}},
		{"a pane left from ssh -X", map[string]string{"DISPLAY": "localhost:10.0", "TMUX": "x"},
			remoteSession{why: "the tmux session was last attached locally", tmux: parseTmuxEnv([]byte("-SSH_CONNECTION\n"))}},
		{"a herdr client", map[string]string{"HOME": "/home/u"},
			remoteSession{herdr: true, why: "every herdr client runs locally (1 found)", desktop: herdrDesktop{pid: 201, env: []string{"DISPLAY=localhost:10.0"}}}},
	}
	for _, tc := range []struct {
		name string
		uids []uint32
		err  error
		why  string // "" when the copy goes to the display
	}{
		{"yours", []uint32{testUID}, nil, ""},
		{"root's", []uint32{0}, nil, ""},
		{"yours and root's", []uint32{testUID, 0}, nil, ""},
		{"another user's", []uint32{1001}, nil, "its TCP port 6010 belongs to another user (uid 1001), who would receive the copy."},
		{"yours on one address, another user's on another", []uint32{testUID, 1001}, nil, "another user (uid 1001)"},
		{"nobody's", nil, nil, "nothing listens on its TCP port 6010, which any user of this machine could take."},
		{"unknown", nil, fs.ErrPermission, "could not tell whether its TCP port 6010 is yours (permission denied)"},
	} {
		for _, s := range sessions {
			t.Run(tc.name+"/"+s.name, func(t *testing.T) {
				x, tools := &fakeX11{}, &fakeTools{installed: []string{"xclip", "xsel"}}
				l := newTestLinux(s.env, x, tools)
				var ports []int
				l.listeners = func(port int) ([]uint32, error) { ports = append(ports, port); return tc.uids, tc.err }
				c, term := newFakeClipboard(nil)
				c.native = l
				c.detect = func() remoteSession { return s.session }
				res, err := c.copy("synthetic-secret", copyOptions{transient: true, osc52: config.OSC52Auto})
				if !slices.Equal(ports, []int{6010}) {
					t.Fatalf("looked up ports %v", ports)
				}
				if tc.why == "" {
					if err != nil || !res.native || string(x.value) != "synthetic-secret" {
						t.Fatalf("copy = %+v, %v", res, err)
					}
					return
				}
				if err != nil || res.native || !res.terminal || len(term.sent) != 1 || len(x.calls)+len(tools.runs) != 0 {
					t.Fatalf("copy = %+v, %v (x %q, runs %q)", res, err, x.calls, tools.runs)
				}
				help := clipboardHelp(res, nil)
				if !strings.Contains(help, "Not copied to X display localhost:10.0 (") || !strings.Contains(help, tc.why) ||
					tc.err == nil && !strings.Contains(help, "has probably ended") {
					t.Fatalf("help = %q", help)
				}
				// never: no clipboard at all.
				if _, err := c.copy("synthetic-secret", copyOptions{transient: true, osc52: config.OSC52Never}); !errors.Is(err, errNoClipboard) || len(x.calls) != 0 {
					t.Fatalf("never copy = %v (x %q)", err, x.calls)
				}
			})
		}
	}
	// Displays elsewhere are not looked up.
	for _, display := range []string{":0", "unix:0", "/tmp/launch-x/org.xquartz:0"} {
		l := newTestLinux(map[string]string{"DISPLAY": display}, &fakeX11{}, &fakeTools{})
		l.listeners = func(port int) ([]uint32, error) { t.Errorf("%s: looked up port %d", display, port); return nil, nil }
		if r := l.route(localSession); !r.ok {
			t.Errorf("%s: route = %+v", display, r)
		}
	}
	l := newTestLinux(map[string]string{"DISPLAY": "remotehost:10.0"}, &fakeX11{}, &fakeTools{})
	l.listeners = func(port int) ([]uint32, error) { t.Errorf("remotehost: looked up port %d", port); return nil, nil }
	if r := l.route(sshSession); !r.ok {
		t.Errorf("remotehost:10.0: route = %+v", r)
	}
}

func TestLoopbackDisplay(t *testing.T) {
	for display, want := range map[string]int{
		"localhost:10.0":     6010,
		"localhost:10":       6010,
		"LOCALHOST:11.0":     6011,
		"127.0.0.1:12.0":     6012,
		"127.0.1.1:0":        6000,
		"[::1]:13":           6013,
		"::1:13.0":           6013,
		"::ffff:127.0.0.1:1": 6001,
		"tcp/localhost:10.0": 6010,
		":0":                 0,
		":10.0":              0,
		"unix:10":            0,
		"remotehost:10.0":    0,
		"192.168.1.5:10.0":   0,
		"/tmp/launch-x:0":    0,
		"localhost:x":        0,
		"localhost":          0,
		"":                   0,
	} {
		port, ok := loopbackDisplay(display)
		if ok != (want != 0) || port != want {
			t.Errorf("loopbackDisplay(%q) = %d, %v; want %d", display, port, ok, want)
		}
	}
}

// The tables are in the format of the kernel's /proc/net/tcp and tcp6, as
// a little-endian host prints them.
const (
	procTCP = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:177A 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 31337 1 0000000000000000 100 0 0 10 0
   1: 00000000:177B 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1001        0 31338 1 0000000000000000 100 0 0 10 0
   2: 0501A8C0:177A 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1002        0 31339 1 0000000000000000 100 0 0 10 0
   3: 0100007F:177A 0100007F:D431 01 00000000:00000000 00:00000000 00000000  1003        0 31340 1 0000000000000000 20 4 30 10 -1
   4: 0100007F:0016 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 31341 1 0000000000000000 100 0 0 10 0
`
	procTCP6 = `  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000000000000000000001000000:177A 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1004        0 31342 1 0000000000000000 100 0 0 10 0
   1: 0000000000000000FFFF00000100007F:177A 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1005        0 31343 1 0000000000000000 100 0 0 10 0
   2: 00000000000000000000000000000000:177B 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1006        0 31344 1 0000000000000000 100 0 0 10 0
   3: B80D0120000000000000000001000000:177A 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1007        0 31345 1 0000000000000000 100 0 0 10 0
`
)

// Listeners count on 127.0.0.1, ::1, ::ffff:127.0.0.1 and the unspecified
// addresses; not on other addresses, and not connections.
func TestParseListeners(t *testing.T) {
	if binary.NativeEndian.Uint16([]byte{1, 0}) != 1 {
		t.Skip("the tables are a little-endian host's")
	}
	for _, tc := range []struct {
		table string
		port  int
		want  []uint32
	}{
		{procTCP, 6010, []uint32{1000}},
		{procTCP, 6011, []uint32{1001}},
		{procTCP, 6012, nil},
		{procTCP, 22, []uint32{0}},
		{procTCP6, 6010, []uint32{1004, 1005}},
		{procTCP6, 6011, []uint32{1006}},
	} {
		got, err := parseListeners([]byte(tc.table), tc.port)
		if err != nil || !slices.Equal(got, tc.want) {
			t.Errorf("port %d: %v, %v; want %v", tc.port, got, err, tc.want)
		}
	}
	for _, bad := range []string{
		"",
		"sl local_address\n   0: 0100007F:177A 00000000:0000 0A\n",
		"sl local_address\n   0: 0100007F:ZZZZ 00000000:0000 0A 0:0 0:0 0 1000\n",
		"sl local_address\n   0: 0100007:177A 00000000:0000 0A 0:0 0:0 0 1000\n",
		"sl local_address\n   0: 0100007F:177A 00000000:0000 0A 0:0 0:0 0 x\n",
	} {
		if got, err := parseListeners([]byte(bad), 6010); err == nil {
			t.Errorf("parseListeners(%q) = %v, want an error", bad, got)
		}
	}
}

// listenerUIDs reads the kernel's own tables: a loopback listener of the
// test's is keetui's uid's.
func TestListenerUIDsFindsYourListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no loopback:", err)
	}
	defer func() { _ = ln.Close() }()
	uids, err := listenerUIDs(ln.Addr().(*net.TCPAddr).Port)
	if err != nil || !slices.Contains(uids, uint32(os.Geteuid())) {
		t.Fatalf("listenerUIDs = %v, %v; want uid %d", uids, err, os.Geteuid())
	}
}

// A forwarded X server that went away took keetui's copy with it: the clear
// does not ask it again, reports the copy cleared, and clears the terminal
// copy made next to it. On a local display a lost connection still counts
// as a change.
func TestLostForwardedServerStillClearsTheTerminal(t *testing.T) {
	x := &fakeX11{}
	l := newTestLinux(map[string]string{"DISPLAY": "localhost:10.0"}, x, &fakeTools{})
	c, term := newFakeClipboard(nil)
	c.native = l
	c.detect = func() remoteSession { return sshSession }
	if _, err := c.copy("synthetic-secret", copyOptions{transient: true, osc52: config.OSC52Auto}); err != nil {
		t.Fatal(err)
	}
	x.gone()
	x.calls = nil
	r, err := c.release()
	if err != nil || r.outcome != clearDone || r.leg != clearRequested || len(term.sent) != 2 || !r.repaint {
		t.Fatalf("release = %+v, %v (terminal %q)", r, err, term.sent)
	}
	if slices.Contains(x.calls, "Read") || slices.Contains(x.calls, "Blank") || slices.ContainsFunc(x.calls, func(c string) bool { return strings.HasPrefix(c, "Copy") }) {
		t.Fatalf("calls %q", x.calls)
	}

	x = &fakeX11{}
	c, _ = newFakeClipboard(nil)
	c.native = newTestLinux(x11Env, x, &fakeTools{})
	if _, err := c.copy("synthetic-secret", copyOptions{transient: true, osc52: config.OSC52Auto}); err != nil {
		t.Fatal(err)
	}
	x.gone()
	if r, err := c.release(); err != nil || r.outcome != clearChanged {
		t.Fatalf("local release = %+v, %v", r, err)
	}
}

// Over SSH, a desktop display is not the user's: auto goes to the terminal,
// never sends the secret to that X server, and says why in F1.
func TestDesktopDisplayUnderSSHGoesToTheTerminal(t *testing.T) {
	x, tools := &fakeX11{}, &fakeTools{installed: []string{"xclip"}}
	c, term := newFakeClipboard(nil)
	c.native = newTestLinux(map[string]string{"DISPLAY": ":0"}, x, tools)
	c.detect = func() remoteSession {
		return remoteSession{remote: true, herdr: true, why: "a herdr client runs over SSH (pid 7)"}
	}
	res, err := c.copy("synthetic-secret", copyOptions{transient: true, osc52: config.OSC52Auto})
	if err != nil || res.native || !res.terminal || len(x.calls)+len(tools.runs) != 0 || len(term.sent) != 1 {
		t.Fatalf("copy = %+v, %v (x %q, runs %q)", res, err, x.calls, tools.runs)
	}
	help := clipboardHelp(res, nil)
	if !strings.Contains(help, "X display :0") || !strings.Contains(help, "herdr client runs over SSH") {
		t.Fatalf("help = %q", help)
	}
	// never: no clipboard at all, and F1 says why.
	_, err = c.copy("synthetic-secret", copyOptions{transient: true, osc52: config.OSC52Never})
	if !errors.Is(err, errNoClipboard) || len(x.calls) != 0 {
		t.Fatalf("never copy = %v (x %q)", err, x.calls)
	}
}

// F1 names the local destination of a native copy.
func TestNativeCopyHelpNamesTheDestination(t *testing.T) {
	for env, want := range map[string]string{
		":0":        "Copied to X display :0.",
		"wayland-1": "Copied to the Wayland clipboard (wayland-1).",
	} {
		vars := map[string]string{"DISPLAY": env}
		if strings.HasPrefix(env, "wayland") {
			vars = map[string]string{"WAYLAND_DISPLAY": env}
		}
		c, _ := newFakeClipboard(nil)
		c.native = newTestLinux(vars, &fakeX11{}, &fakeTools{installed: []string{"wl-copy", "wl-paste"}})
		c.detect = func() remoteSession { return localSession }
		res, err := c.copy("synthetic-secret", copyOptions{osc52: config.OSC52Auto})
		if err != nil || !res.native {
			t.Fatalf("copy = %+v, %v", res, err)
		}
		if help := clipboardHelp(res, nil); !strings.Contains(help, want) || !strings.Contains(help, "Local session:") {
			t.Errorf("help = %q, want %q", help, want)
		}
	}
}

// A herdr pane keeps the environment its server started with, which may
// name no display (a server started over SSH) or an old one: a local copy
// goes to the display of the herdr client the user typed on, marked, and
// never to herdr, whose client would write it unmarked. The tools that
// check and clear the copy reach the same display.
func TestHerdrPaneCopiesToTheClientsDisplay(t *testing.T) {
	herdrLocal := func(env ...string) remoteSession {
		return remoteSession{herdr: true, why: "every herdr client runs locally (1 found)",
			desktop: herdrDesktop{pid: 201, env: env}}
	}
	for _, tc := range []struct {
		name     string
		pane     map[string]string // keetui's environment
		session  remoteSession
		wantEnv  []string // set in every wl-copy and wl-paste run; nil: keetui's own
		wantDest string
		client   int // whose display F1 names; 0: keetui's own
	}{
		{"no display in the pane", map[string]string{"HOME": "/home/u"},
			herdrLocal("WAYLAND_DISPLAY=wayland-1", "XDG_RUNTIME_DIR=/run/user/1000"),
			[]string{"WAYLAND_DISPLAY=wayland-1", "XDG_RUNTIME_DIR=/run/user/1000"},
			"the Wayland clipboard (wayland-1)", 201},
		{"the pane's runtime directory", map[string]string{"XDG_RUNTIME_DIR": "/run/user/1000"},
			herdrLocal("WAYLAND_DISPLAY=wayland-1"),
			[]string{"WAYLAND_DISPLAY=wayland-1", "XDG_RUNTIME_DIR=/run/user/1000"},
			"the Wayland clipboard (wayland-1)", 201},
		{"an old display in the pane", map[string]string{"WAYLAND_DISPLAY": "wayland-0", "XDG_RUNTIME_DIR": "/run/user/1000"},
			herdrLocal("WAYLAND_DISPLAY=wayland-1", "XDG_RUNTIME_DIR=/run/user/1000"),
			[]string{"WAYLAND_DISPLAY=wayland-1", "XDG_RUNTIME_DIR=/run/user/1000"},
			"the Wayland clipboard (wayland-1)", 201},
		{"the pane's display", map[string]string{"WAYLAND_DISPLAY": "wayland-1", "DISPLAY": ":1"},
			herdrLocal("WAYLAND_DISPLAY=wayland-1", "XDG_RUNTIME_DIR=/run/user/1000"),
			nil, "the Wayland clipboard (wayland-1)", 0},
		{"a client without a display", map[string]string{"WAYLAND_DISPLAY": "wayland-0"},
			herdrLocal("XDG_RUNTIME_DIR=/run/user/1000"),
			nil, "the Wayland clipboard (wayland-0)", 0},
		{"not in herdr", map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, localSession,
			nil, "the Wayland clipboard (wayland-0)", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tools := &fakeTools{installed: []string{"wl-copy", "wl-paste"}, clip: []byte("synthetic-secret")}
			l := newTestLinux(tc.pane, nil, tools)
			c, term := newFakeClipboard(nil)
			c.native = l
			c.detect = func() remoteSession { return tc.session }
			res, err := c.copy("synthetic-secret", copyOptions{transient: true, osc52: config.OSC52Auto})
			if err != nil || !res.native || !res.marked || res.terminal || len(term.sent) != 0 {
				t.Fatalf("copy = %+v, %v (terminal got %d)", res, err, len(term.sent))
			}
			help := clipboardHelp(res, nil)
			if !strings.Contains(help, "Copied to "+tc.wantDest+".") {
				t.Errorf("help = %q, want %q", help, tc.wantDest)
			}
			if note := fmt.Sprintf("display of herdr client pid %d (%s)", tc.client, strings.Join(tc.wantEnv, " ")); tc.client != 0 &&
				!strings.Contains(help, note) || tc.client == 0 && strings.Contains(help, "herdr client pid") {
				t.Errorf("help = %q, want it to name client %d", help, tc.client)
			}
			if !l.holds(matching("synthetic-secret"), false) {
				t.Fatal("wl-paste content not recognized")
			}
			if err := l.clear(); err != nil {
				t.Fatal(err)
			}
			want := []string{
				"wl-copy --help",
				`wl-copy --type text/plain;charset=utf-8 --sensitive <"synthetic-secret"`,
				"wl-paste --no-newline --type text",
				`wl-copy --clear <""`,
			}
			if !slices.Equal(tools.runs, want) {
				t.Fatalf("runs = %q", tools.runs)
			}
			for i, env := range tools.envs[1:] {
				switch {
				case tc.wantEnv == nil && i != 0 && env != nil:
					t.Errorf("%s ran with %q, want keetui's environment", tools.runs[i+1], env)
				case tc.wantEnv != nil && !containsAll(env, tc.wantEnv):
					t.Errorf("%s ran with %q, want %q set", tools.runs[i+1], env, tc.wantEnv)
				}
			}
			if dir := envValue(tc.wantEnv, "XDG_RUNTIME_DIR"); dir != "" && !slices.Contains(tools.envs[1], "TMPDIR="+dir) {
				t.Errorf("wl-copy ran with %q, want its temporary file in %s", tools.envs[1], dir)
			}
		})
	}
}

// On an X11 desktop the client's display goes with the authority file its
// X clients read, ~/.Xauthority without XAUTHORITY: the in-process owner,
// the xclip fallback and the hand-off at exit all reach it.
func TestHerdrPaneCopiesToTheClientsX11Display(t *testing.T) {
	session := remoteSession{herdr: true, why: "every herdr client runs locally (1 found)",
		desktop: herdrDesktop{pid: 201, env: []string{"DISPLAY=:1"}}}
	x, tools := &fakeX11{}, &fakeTools{installed: []string{"xclip"}}
	tools.onRun = func(name string, _ []byte) {
		if name == "xclip" {
			x.stolen("") // xclip took the clipboard
		}
	}
	l := newTestLinux(map[string]string{"HOME": "/home/u"}, x, tools)
	var dialed []string
	l.newX11 = func(display, authority string) x11Owner {
		dialed = append(dialed, display+" "+authority)
		return x
	}
	r := l.route(session)
	if !r.ok || r.display != ":1" || r.dest != "X display :1" || r.client != 201 {
		t.Fatalf("route = %+v", r)
	}
	if marked, err := l.write([]byte("synthetic-secret"), true, r); err != nil || !marked {
		t.Fatalf("write = %v, %v", marked, err)
	}
	if !slices.Equal(dialed, []string{":1 /home/u/.Xauthority"}) {
		t.Fatalf("owner dialed %q", dialed)
	}
	l.close()
	want := []string{"DISPLAY=:1", "XAUTHORITY=/home/u/.Xauthority"}
	if !slices.Equal(tools.runs, []string{`xclip -selection clipboard <""`}) || !containsAll(tools.envs[0], want) {
		t.Fatalf("close ran %q with %q", tools.runs, tools.envs)
	}

	// The owner failing, xclip copies to the same display.
	x, tools = &fakeX11{copyErr: x11clip.ErrNotOwner}, &fakeTools{installed: []string{"xclip"}}
	l = newTestLinux(map[string]string{"HOME": "/home/u"}, x, tools)
	if _, err := l.write([]byte("synthetic-secret"), true, l.route(session)); err != nil || l.tool != "xclip" {
		t.Fatalf("write = %v (tool %q)", err, l.tool)
	}
	if err := l.clear(); err != nil {
		t.Fatal(err)
	}
	if len(tools.envs) != 2 || !containsAll(tools.envs[0], want) || !containsAll(tools.envs[1], want) {
		t.Fatalf("xclip ran %q with %q", tools.runs, tools.envs)
	}
}

func containsAll(env, want []string) bool {
	for _, kv := range want {
		if !slices.Contains(env, kv) {
			return false
		}
	}
	return true
}
