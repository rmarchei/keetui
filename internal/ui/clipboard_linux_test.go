//go:build linux

package ui

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/x11clip"
)

func TestLinuxToolSelection(t *testing.T) {
	for _, tc := range []struct {
		name  string
		env   map[string]string
		tools []string
		want  string
	}{
		{"wayland", map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"}, []string{"wl-copy", "wl-paste", "xclip"}, "wl-copy"},
		{"wayland without wl-clipboard", map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"}, []string{"xclip"}, "x11"},
		{"x11", map[string]string{"DISPLAY": ":0"}, []string{"xclip", "xsel", "wl-copy", "wl-paste"}, "x11"},
		{"x11 without tools", map[string]string{"DISPLAY": ":0"}, nil, "x11"},
		{"no display", nil, []string{"xclip", "wl-copy", "wl-paste"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getenv := func(k string) string { return tc.env[k] }
			has := func(name string) bool { return slices.Contains(tc.tools, name) }
			if got := linuxTool(getenv, has); got != tc.want {
				t.Fatalf("linuxTool = %q, want %q", got, tc.want)
			}
		})
	}
}

// fakeX11 stands in for x11clip.Owner; tests never reach an X server.
type fakeX11 struct {
	copyErr error
	owned   bool   // keetui owns CLIPBOARD
	value   []byte // what keetui serves
	marked  bool
	other   []byte // another client's copy, returned by Read
	readErr error
	lost    bool // the connection broke while it held the copy
	calls   []string
}

func (f *fakeX11) Copy(v []byte, marked bool) error {
	f.calls = append(f.calls, fmt.Sprintf("Copy(%q, %v)", v, marked))
	if f.copyErr != nil {
		f.value, f.marked = nil, false // a failed Copy blanks the earlier copy
		return f.copyErr
	}
	f.owned, f.value, f.marked = true, bytes.Clone(v), marked
	return nil
}

func (f *fakeX11) Owns() bool {
	f.calls = append(f.calls, "Owns")
	return f.owned
}

func (f *fakeX11) Lost() bool {
	f.calls = append(f.calls, "Lost")
	return f.lost
}

// gone simulates the X server going away with keetui's copy.
func (f *fakeX11) gone() { f.owned, f.value, f.lost = false, nil, true }

func (f *fakeX11) Blank() {
	f.calls = append(f.calls, "Blank")
	if f.owned {
		f.value, f.marked = nil, false
	}
}

func (f *fakeX11) Read() ([]byte, error) {
	f.calls = append(f.calls, "Read")
	return bytes.Clone(f.other), f.readErr
}

func (f *fakeX11) Close() error {
	f.calls = append(f.calls, "Close")
	f.owned, f.value = false, nil
	return nil
}

// stolen simulates another client taking the clipboard.
func (f *fakeX11) stolen(other string) { f.owned, f.value, f.other = false, nil, []byte(other) }

// fakeTools records clipboard tool runs instead of executing them.
type fakeTools struct {
	installed []string
	runs      []string
	envs      [][]string // the environment of each run
	runErr    error
	clip      []byte // what xclip -o / xsel -o / wl-paste return
	outErr    error  // what they fail with
	onRun     func(name string, input []byte)
}

func (f *fakeTools) run(env []string, input []byte, name string, args ...string) error {
	f.runs = append(f.runs, fmt.Sprintf("%s %s <%q", name, strings.Join(args, " "), input))
	f.envs = append(f.envs, env)
	if f.runErr != nil {
		return fmt.Errorf("%s: %w", name, f.runErr)
	}
	if f.onRun != nil {
		f.onRun(name, input)
	}
	return nil
}

func (f *fakeTools) output(name string, args ...string) ([]byte, error) {
	f.runs = append(f.runs, name+" "+strings.Join(args, " "))
	if name == "wl-copy" {
		return []byte("Options: --sensitive"), nil
	}
	if f.outErr != nil {
		return nil, fmt.Errorf("%s: %w", name, f.outErr)
	}
	return bytes.Clone(f.clip), nil
}

func newTestLinux(env map[string]string, x *fakeX11, tools *fakeTools) *linuxClipboard {
	return &linuxClipboard{
		getenv: func(k string) string { return env[k] },
		has:    func(name string) bool { return slices.Contains(tools.installed, name) },
		run:    tools.run,
		runX:   tools.run,
		output: tools.output,
		newX11: func(string) x11Owner { return x },
	}
}

var x11Env = map[string]string{"DISPLAY": ":0"}

// writeLocal writes as a copy in a local session does.
func (l *linuxClipboard) writeLocal(value []byte, transient bool) (bool, error) {
	return l.write(value, transient, l.route(remoteSession{why: "no SSH session found"}))
}

func matching(value string) func([]byte) bool {
	return func(b []byte) bool { return string(b) == value }
}

func TestX11CopiesThroughOwner(t *testing.T) {
	x, tools := &fakeX11{}, &fakeTools{installed: []string{"xclip", "xsel"}}
	l := newTestLinux(x11Env, x, tools)
	if !l.route(remoteSession{}).ok {
		t.Fatal("X11 without tools must still be available")
	}
	marked, err := l.writeLocal([]byte("synthetic-secret"), true)
	if err != nil || !marked || !x.marked || l.tool != "x11" {
		t.Fatalf("write = %v, %v (tool %q)", marked, err, l.tool)
	}
	if !l.holds(func([]byte) bool { return false }, false) {
		t.Fatal("holds checked content instead of ownership")
	}
	if err := l.clear(); err != nil || !x.owned || x.value != nil {
		t.Fatalf("clear = %v; owned %v, value %q: must keep the clipboard, empty", err, x.owned, x.value)
	}
	if len(tools.runs) != 0 {
		t.Fatalf("owner copy ran tools: %q", tools.runs)
	}
}

func TestX11FallsBackToTools(t *testing.T) {
	failures := []error{x11clip.ErrNotOwner, x11clip.ErrTooLarge, errors.New("x11clip: cannot connect to :0")}
	for _, failure := range failures {
		for _, tc := range []struct {
			installed []string
			wantTool  string
			wantRun   string
		}{
			{[]string{"xclip", "xsel"}, "xclip", `xclip -selection clipboard <"synthetic-secret"`},
			{[]string{"xsel"}, "xsel", `xsel --clipboard --input <"synthetic-secret"`},
		} {
			t.Run(fmt.Sprintf("%v/%s", failure, tc.wantTool), func(t *testing.T) {
				x, tools := &fakeX11{copyErr: failure}, &fakeTools{installed: tc.installed}
				l := newTestLinux(x11Env, x, tools)
				marked, err := l.writeLocal([]byte("synthetic-secret"), true)
				if err != nil || marked || l.tool != tc.wantTool {
					t.Fatalf("write = %v, %v (tool %q)", marked, err, l.tool)
				}
				if !slices.Equal(tools.runs, []string{tc.wantRun}) {
					t.Fatalf("runs = %q", tools.runs)
				}
			})
		}
	}

	x, tools := &fakeX11{copyErr: x11clip.ErrNotOwner}, &fakeTools{}
	if _, err := newTestLinux(x11Env, x, tools).writeLocal([]byte("synthetic-secret"), true); !errors.Is(err, x11clip.ErrNotOwner) {
		t.Fatalf("write without tools = %v", err)
	}
	tools = &fakeTools{installed: []string{"xclip"}, runErr: errors.New("exit status 1")}
	_, err := newTestLinux(x11Env, x, tools).writeLocal([]byte("synthetic-secret"), true)
	if !errors.Is(err, x11clip.ErrNotOwner) || !strings.Contains(err.Error(), "xclip: exit status 1") || strings.Contains(err.Error(), "\n") {
		t.Fatalf("write with a failing fallback = %q, want both causes on one line", err)
	}
}

// A server that did not answer keetui's connection would keep xclip and
// xsel waiting too: the copy fails at once and goes to the terminal.
func TestX11TimeoutSkipsTools(t *testing.T) {
	x, tools := &fakeX11{copyErr: x11clip.ErrTimeout}, &fakeTools{installed: []string{"xclip", "xsel"}}
	if _, err := newTestLinux(x11Env, x, tools).writeLocal([]byte("synthetic-secret"), true); !errors.Is(err, x11clip.ErrTimeout) || len(tools.runs) != 0 {
		t.Fatalf("write = %v, runs %q", err, tools.runs)
	}
	c, term := newFakeClipboard(nil)
	c.native = newTestLinux(x11Env, x, tools)
	res, err := c.copy("synthetic-secret", copyOptions{transient: true, osc52: config.OSC52Auto})
	if err != nil || res.native || !res.terminal || !errors.Is(res.nativeErr, x11clip.ErrTimeout) || len(term.sent) != 1 || len(tools.runs) != 0 {
		t.Fatalf("copy = %+v, %v (terminal %q, runs %q)", res, err, term.sent, tools.runs)
	}
}

func TestX11HoldsFollowsOwnership(t *testing.T) {
	x, tools := &fakeX11{}, &fakeTools{installed: []string{"xclip"}}
	l := newTestLinux(x11Env, x, tools)
	_, _ = l.writeLocal([]byte("synthetic-secret"), true)

	x.stolen("synthetic-secret") // even the same value, copied by another app
	x.calls = nil
	if l.holds(matching("synthetic-secret"), false) || slices.Contains(x.calls, "Read") {
		t.Fatalf("holds after losing the clipboard (calls %q)", x.calls)
	}

	// With OSC 52 "always", the terminal may have replaced keetui's copy
	// with its own copy of the same value.
	if !l.holds(matching("synthetic-secret"), true) {
		t.Fatal("terminal's copy of the value not recognized")
	}
	x.calls = nil
	if err := l.clear(); err != nil || !slices.Equal(x.calls, []string{`Copy("", false)`}) {
		t.Fatalf("clear of the terminal's copy = %v, calls %q", err, x.calls)
	}

	x.stolen("copied-later")
	if l.holds(matching("synthetic-secret"), true) {
		t.Fatal("a different value counted as keetui's")
	}
	x.other, x.readErr = nil, x11clip.ErrNoText
	if l.holds(matching(""), true) {
		t.Fatal("an unreadable clipboard counted as keetui's")
	}
	if len(tools.runs) != 0 {
		t.Fatalf("X11 checks ran tools: %q", tools.runs)
	}
}

func TestXselClearKeepsClipboard(t *testing.T) {
	setup := func(installed ...string) (*linuxClipboard, *fakeX11, *fakeTools) {
		x, tools := &fakeX11{copyErr: x11clip.ErrTooLarge}, &fakeTools{installed: installed}
		l := newTestLinux(x11Env, x, tools)
		if _, err := l.writeLocal([]byte("large-synthetic-note"), true); err != nil || l.tool != "xsel" {
			t.Fatalf("write = %v (tool %q)", err, l.tool)
		}
		tools.clip, tools.runs = []byte("large-synthetic-note"), nil
		if !l.holds(matching("large-synthetic-note"), false) {
			t.Fatal("xsel copy not recognized by content")
		}
		return l, x, tools
	}

	// The owner can take an empty copy, which it keeps.
	l, x, tools := setup("xsel")
	x.copyErr = nil
	if err := l.clear(); err != nil || !x.owned || slices.ContainsFunc(tools.runs, isXselClear) {
		t.Fatalf("clear = %v; runs %q, owned %v", err, tools.runs, x.owned)
	}

	// Without the owner, an empty xclip copy.
	l, _, tools = setup("xsel")
	l.has = func(string) bool { return true }
	if err := l.clear(); err != nil || !slices.Equal(tools.runs[1:], []string{`xclip -selection clipboard <""`}) {
		t.Fatalf("clear = %v; runs %q", err, tools.runs)
	}

	// xsel --clear, which gives the clipboard up, only as the last resort.
	l, _, tools = setup("xsel")
	if err := l.clear(); err != nil || !slices.ContainsFunc(tools.runs, isXselClear) {
		t.Fatalf("clear = %v; runs %q", err, tools.runs)
	}
}

func isXselClear(run string) bool { return strings.HasPrefix(run, "xsel --clipboard --clear") }

func TestXclipFallbackClearsWithEmptyCopy(t *testing.T) {
	x, tools := &fakeX11{copyErr: x11clip.ErrTooLarge}, &fakeTools{installed: []string{"xclip"}}
	l := newTestLinux(x11Env, x, tools)
	_, _ = l.writeLocal([]byte("large-synthetic-note"), true)
	tools.clip, tools.runs = []byte("large-synthetic-note"), nil
	if !l.holds(matching("large-synthetic-note"), false) {
		t.Fatal("xclip copy not recognized by content")
	}
	if err := l.clear(); err != nil || tools.runs[len(tools.runs)-1] != `xclip -selection clipboard <""` {
		t.Fatalf("clear = %v; runs %q", err, tools.runs)
	}
	tools.clip = []byte("copied-later")
	if l.holds(matching("large-synthetic-note"), false) {
		t.Fatal("a later copy counted as keetui's")
	}
}

// A fallback copy whose read-back got no answer from the server is still
// cleared; a read-back that failed otherwise (an image on the clipboard) is
// someone else's copy.
func TestToolReadBackTimeoutClears(t *testing.T) {
	for _, tc := range []struct {
		outErr error
		want   bool
	}{
		{errToolTimeout, true},
		{errors.New("exit status 1"), false},
	} {
		t.Run(tc.outErr.Error(), func(t *testing.T) {
			x, tools := &fakeX11{copyErr: x11clip.ErrTooLarge}, &fakeTools{installed: []string{"xclip"}}
			l := newTestLinux(x11Env, x, tools)
			_, _ = l.writeLocal([]byte("large-synthetic-note"), true)
			tools.outErr = tc.outErr
			if got := l.holds(matching("large-synthetic-note"), false); got != tc.want {
				t.Fatalf("holds = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestOutputBoundedIsBounded(t *testing.T) {
	prev := x11ToolTimeout
	x11ToolTimeout = 200 * time.Millisecond
	t.Cleanup(func() { x11ToolTimeout = prev })
	ids := filepath.Join(t.TempDir(), "pid")
	// The child keeps stdout open after the tool is killed.
	script := `sleep 30 & echo $! >"$1"; exec sleep 30`
	start := time.Now()
	out, err := outputBounded("/bin/sh", "-c", script, "sh", ids)
	if !errors.Is(err, errToolTimeout) || out != nil {
		t.Fatalf("outputBounded = %q, %v, want errToolTimeout", out, err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("outputBounded waited %v", elapsed)
	}
	child := readPID(t, ids)
	t.Cleanup(func() { _ = syscall.Kill(child, syscall.SIGKILL) })
	if out, err := outputBounded("/bin/sh", "-c", "printf synthetic"); err != nil || string(out) != "synthetic" {
		t.Fatalf("outputBounded = %q, %v", out, err)
	}
}

func TestX11CloseHandsOffToXclip(t *testing.T) {
	for _, tc := range []struct {
		name      string
		installed []string
		owned     bool
		wantRuns  []string
	}{
		{"owned, xclip installed", []string{"xclip", "xsel"}, true, []string{`xclip -selection clipboard <""`}},
		{"owned, only xsel", []string{"xsel"}, true, nil},
		{"another client copied", []string{"xclip"}, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x, tools := &fakeX11{}, &fakeTools{installed: tc.installed}
			tools.onRun = func(name string, _ []byte) {
				if name == "xclip" {
					x.stolen("") // xclip took the clipboard
				}
			}
			l := newTestLinux(x11Env, x, tools)
			_, _ = l.writeLocal([]byte("synthetic-secret"), true)
			if !tc.owned {
				x.stolen("copied-later")
			}
			x.calls = nil
			l.close()
			if !slices.Equal(tools.runs, tc.wantRuns) {
				t.Fatalf("runs = %q, want %q", tools.runs, tc.wantRuns)
			}
			if x.calls[len(x.calls)-1] != "Close" {
				t.Fatalf("calls = %q: Close must come last", x.calls)
			}
		})
	}

	// A failing xclip will not take the clipboard: no waiting for it.
	x, tools := &fakeX11{}, &fakeTools{installed: []string{"xclip"}, runErr: errors.New("exit status 1")}
	l := newTestLinux(x11Env, x, tools)
	_, _ = l.writeLocal([]byte("synthetic-secret"), true)
	x.calls = nil
	start := time.Now()
	l.close()
	if elapsed := time.Since(start); elapsed >= handOffTimeout/2 || !slices.Equal(x.calls, []string{"Owns", "Close"}) {
		t.Fatalf("close with a failing xclip took %v, calls %q", elapsed, x.calls)
	}

	// Without an X11 copy there is nothing to hand over.
	tools = &fakeTools{installed: []string{"wl-copy", "wl-paste", "xclip"}}
	l = newTestLinux(map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, nil, tools)
	_, _ = l.writeLocal([]byte("synthetic-secret"), true)
	tools.runs = nil
	l.close()
	if len(tools.runs) != 0 {
		t.Fatalf("close on Wayland ran %q", tools.runs)
	}
}

func TestWaylandUnchanged(t *testing.T) {
	tools := &fakeTools{installed: []string{"wl-copy", "wl-paste"}, clip: []byte("synthetic-secret")}
	l := newTestLinux(map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"}, nil, tools)
	marked, err := l.writeLocal([]byte("synthetic-secret"), true)
	if err != nil || !marked {
		t.Fatalf("write = %v, %v", marked, err)
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
}

// The routing in systemClipboard and the X11 backend together: the
// outcomes the status bar reports.
func TestX11ReleaseOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mode  string
		after func(*fakeX11)
		want  clearOutcome
		empty bool // the clipboard ends up keetui's, empty
	}{
		{"timer", config.OSC52Auto, func(*fakeX11) {}, clearDone, true},
		{"copied elsewhere", config.OSC52Auto, func(x *fakeX11) { x.stolen("copied-later") }, clearChanged, false},
		{"same value copied elsewhere", config.OSC52Auto, func(x *fakeX11) { x.stolen("synthetic-secret") }, clearChanged, false},
		{"terminal's copy", config.OSC52Always, func(x *fakeX11) { x.stolen("synthetic-secret") }, clearDone, true},
		{"terminal, then elsewhere", config.OSC52Always, func(x *fakeX11) { x.stolen("copied-later") }, clearChanged, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x, tools := &fakeX11{}, &fakeTools{installed: []string{"xclip"}}
			c, _ := newFakeClipboard(nil)
			c.native = newTestLinux(x11Env, x, tools)
			res, err := c.copy("synthetic-secret", copyOptions{transient: true, osc52: tc.mode})
			if err != nil || !res.native || !res.marked {
				t.Fatalf("copy = %+v, %v", res, err)
			}
			tc.after(x)
			if outcome, err := outcomeOf(c.release()); outcome != tc.want || err != nil {
				t.Fatalf("release = %v, %v", outcome, err)
			}
			if got := x.owned && len(x.value) == 0; got != tc.empty {
				t.Fatalf("keetui owns an empty clipboard = %v (owned %v, value %q)", got, x.owned, x.value)
			}
		})
	}
}

// A value the owner refuses goes elsewhere (here OSC 52, with no tools
// installed), and systemClipboard then forgets the earlier copy: it must not
// stay on the X11 clipboard, where no timer would clear it.
func TestX11RefusedCopyLeavesNoSecret(t *testing.T) {
	x, tools := &fakeX11{}, &fakeTools{}
	c, term := newFakeClipboard(nil)
	c.native = newTestLinux(x11Env, x, tools)
	opts := copyOptions{transient: true, osc52: config.OSC52Auto}
	if _, err := c.copy("small-synthetic-secret", opts); err != nil {
		t.Fatal(err)
	}
	x.copyErr = x11clip.ErrTooLarge
	res, err := c.copy("huge-synthetic-note", opts)
	if err != nil || res.native || !res.terminal {
		t.Fatalf("copy = %+v, %v", res, err)
	}
	if len(x.value) != 0 {
		t.Fatalf("the X11 clipboard still serves %q", x.value)
	}
	if outcome, err := outcomeOf(c.release()); outcome != clearRequested || err != nil {
		t.Fatalf("release = %v, %v", outcome, err)
	}
	if !slices.Equal(term.sent, []string{"huge-synthetic-note", ""}) {
		t.Fatalf("terminal got %q", term.sent)
	}
}

// xclip and xsel run detached (see TestPipeDetachedStartsASession), so that
// their copies outlive keetui; wl-copy runs as before.
func TestX11ToolsRunDetached(t *testing.T) {
	var plain, detached []string
	setup := func(env map[string]string, x *fakeX11, installed ...string) *linuxClipboard {
		tools := &fakeTools{installed: installed}
		tools.onRun = func(name string, _ []byte) {
			if name == "xclip" && x != nil {
				x.stolen("") // xclip took the clipboard
			}
		}
		l := newTestLinux(env, x, tools)
		l.run = func(env []string, input []byte, name string, args ...string) error {
			plain = append(plain, name)
			return tools.run(env, input, name, args...)
		}
		l.runX = func(env []string, input []byte, name string, args ...string) error {
			detached = append(detached, name)
			return tools.run(env, input, name, args...)
		}
		return l
	}
	// Fallback copies and their clears.
	for _, tool := range []string{"xclip", "xsel"} {
		l := setup(x11Env, &fakeX11{copyErr: x11clip.ErrTooLarge}, tool)
		if _, err := l.writeLocal([]byte("large-synthetic-note"), true); err != nil {
			t.Fatal(err)
		}
		if err := l.clear(); err != nil {
			t.Fatal(err)
		}
	}
	// The exit hand-off.
	l := setup(x11Env, &fakeX11{}, "xclip")
	if _, err := l.writeLocal([]byte("synthetic-secret"), true); err != nil {
		t.Fatal(err)
	}
	l.close()
	// Wayland.
	l = setup(map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, nil, "wl-copy", "wl-paste")
	if _, err := l.writeLocal([]byte("synthetic-secret"), true); err != nil {
		t.Fatal(err)
	}
	if err := l.clear(); err != nil {
		t.Fatal(err)
	}

	if want := []string{"xclip", "xclip", "xsel", "xsel", "xclip"}; !slices.Equal(detached, want) {
		t.Fatalf("detached runs = %q, want %q", detached, want)
	}
	if want := []string{"wl-copy", "wl-copy"}; !slices.Equal(plain, want) {
		t.Fatalf("plain runs = %q, want %q", plain, want)
	}
}

// When keetui leads its terminal's session, the kernel hangs up the
// terminal's foreground processes as keetui exits, which would kill the
// xclip server holding the empty copy handed over at exit. pipeDetached
// starts the tool as the leader of a new session instead. A shell stands in
// for xclip, which tests never run.
func TestPipeDetachedStartsASession(t *testing.T) {
	out := filepath.Join(t.TempDir(), "ids")
	// read is a builtin, so /proc/self is the shell: fields 1, 5 and 6 are
	// its PID, process group and session.
	script := `read -r pid _ _ _ pgrp sid _ </proc/self/stat && echo "$pid $pgrp $sid" >"$1"`
	if err := pipeDetached(nil, nil, "/bin/sh", "-c", script, "sh", out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	ids := strings.Fields(string(b))
	if len(ids) != 3 || ids[0] != ids[1] || ids[0] != ids[2] {
		t.Fatalf("PID, process group and session = %q, want a session leader", b)
	}
	if err := pipeDetached(nil, nil, "/bin/sh", "-c", "exit 3"); err == nil || !strings.HasPrefix(err.Error(), "/bin/sh: ") {
		t.Fatalf("failing tool = %v", err)
	}
}

// A display that does not answer keeps xclip and xsel waiting without a
// limit, and the copy runs in the UI's update. pipeDetached stops waiting
// after x11ToolTimeout. A shell stands in for the tool, which tests never
// run: it forks a child, as xclip forks the process that keeps its copy,
// then waits like a tool connecting to a server that never answers.
func TestPipeDetachedIsBounded(t *testing.T) {
	prev := x11ToolTimeout
	x11ToolTimeout = 200 * time.Millisecond
	t.Cleanup(func() { x11ToolTimeout = prev })
	script := `sleep 30 & echo $! >"$1.child"; echo $$ >"$1"; exec sleep 30`
	for _, tc := range []struct {
		name   string
		input  []byte
		killed bool
	}{
		// A copy keetui reports as not made must never land later.
		{"copy of a value", []byte("synthetic-secret"), true},
		// An empty copy (a clear, or the hand-off at exit) may still help.
		{"empty copy", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ids := filepath.Join(t.TempDir(), "pid")
			start := time.Now()
			err := pipeDetached(nil, tc.input, "/bin/sh", "-c", script, "sh", ids)
			if !errors.Is(err, errToolTimeout) || !strings.HasPrefix(err.Error(), "/bin/sh: ") {
				t.Fatalf("pipeDetached = %v, want errToolTimeout", err)
			}
			if elapsed := time.Since(start); elapsed > 2*time.Second {
				t.Fatalf("pipeDetached waited %v", elapsed)
			}
			tool, child := readPID(t, ids), readPID(t, ids+".child")
			t.Cleanup(func() { _ = syscall.Kill(-tool, syscall.SIGKILL) })
			if tc.killed {
				waitFor(t, "the tool and its child to die", func() bool { return !running(tool) && !running(child) })
			} else if !running(tool) || !running(child) {
				t.Fatal("an empty copy still waiting for the server was killed")
			}
		})
	}
}

// A tool that finishes in time leaves the process it forked, which holds
// the copy, running.
func TestPipeDetachedKeepsTheForkedServer(t *testing.T) {
	ids := filepath.Join(t.TempDir(), "pid")
	script := `sleep 30 & echo $! >"$1"`
	if err := pipeDetached(nil, []byte("synthetic-secret"), "/bin/sh", "-c", script, "sh", ids); err != nil {
		t.Fatal(err)
	}
	child := readPID(t, ids)
	t.Cleanup(func() { _ = syscall.Kill(child, syscall.SIGKILL) })
	if !running(child) {
		t.Fatal("the forked server died")
	}
}

func readPID(t *testing.T, path string) int {
	t.Helper()
	var b []byte
	waitFor(t, path+" to be written", func() bool {
		var err error
		b, err = os.ReadFile(path)
		return err == nil && strings.HasSuffix(string(b), "\n")
	})
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

// running reports whether pid is a live process, not a zombie waiting for
// its parent.
func running(pid int) bool {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	fields := strings.Fields(string(b[bytes.LastIndexByte(b, ')')+1:]))
	return len(fields) > 0 && fields[0] != "Z" && fields[0] != "X"
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); !cond(); {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
