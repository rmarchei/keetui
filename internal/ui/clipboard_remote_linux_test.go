//go:build linux

package ui

import (
	"errors"
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
	l.newX11 = func(display string) x11Owner { displays = append(displays, display); return x }
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
	l.newX11 = func(display string) x11Owner {
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
