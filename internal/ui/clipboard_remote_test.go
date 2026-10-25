package ui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/rmarchei/keetui/internal/config"
)

// fakeProcs stands in for the process table; no test depends on the
// machine's real processes except TestHerdrScanFindsRealProcesses.
type fakeProcs struct {
	procs   []procInfo
	err     error
	parents map[int]fakeParent // pid -> its parent and its own name
	asked   []string           // names asked for
}

type fakeParent struct {
	ppid int
	name string
}

func (f *fakeProcs) named(names []string) ([]procInfo, error) {
	f.asked = append(f.asked, names...)
	if f.err != nil {
		return nil, f.err
	}
	var out []procInfo
	for _, p := range f.procs {
		for _, name := range names {
			if filepath.Base(p.exe) == name {
				out = append(out, p)
				break
			}
		}
	}
	return out, nil
}

func (f *fakeProcs) parent(pid int) (int, string, error) {
	p, ok := f.parents[pid]
	if !ok {
		return 0, "", errors.New("no such process")
	}
	return p.ppid, p.name, nil
}

const keetuiPID = 500

// localChain is keetui under a shell under a terminal emulator.
func localChain() map[int]fakeParent {
	return map[int]fakeParent{
		keetuiPID: {400, "keetui"},
		400:       {300, "zsh"},
		300:       {1, "kitty"},
	}
}

func testDetector(env map[string]string, procs *fakeProcs, tmux func() (tmuxEnv, error)) sessionDetector {
	return sessionDetector{
		getenv:  func(k string) string { return env[k] },
		procs:   procs,
		tmuxEnv: tmux,
		pid:     keetuiPID,
		resolve: func(p string) (string, error) {
			// /usr/local/bin/herdr is a link to the real binary.
			if p == "/usr/local/bin/herdr" {
				return "/opt/herdr/0.9.5/herdr", nil
			}
			return p, nil
		},
	}
}

func TestHerdrInvocation(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want herdrKind
	}{
		{[]string{"herdr"}, herdrClient},
		{[]string{"/usr/local/bin/herdr"}, herdrClient},
		{[]string{"herdr", "--session", "work"}, herdrClient},
		{[]string{"herdr", "--session=work"}, herdrClient},
		{[]string{"herdr", "client"}, herdrClient},
		{[]string{"herdr", "session", "attach", "work"}, herdrClient},
		{[]string{"herdr", "terminal", "attach", "t1"}, herdrClient},
		{[]string{"herdr", "terminal", "attach", "t1", "--takeover"}, herdrClient},
		{[]string{"herdr", "agent", "attach", "w4:p1"}, herdrClient},
		{[]string{"herdr", "remote-client-bridge"}, herdrBridge},
		{[]string{"herdr", "remote-client-bridge", "--idle-timeout-v1"}, herdrBridge},
		{[]string{"herdr", "--session", "agents", "remote-client-bridge", "--idle-timeout-v1"}, herdrBridge},
		// herdr drops --session wherever it stands before "--", and ignores
		// what follows client.
		{[]string{"herdr", "client", "--session", "work"}, herdrClient},
		{[]string{"herdr", "terminal", "attach", "--session=work", "t1"}, herdrClient},
		{[]string{"herdr", "client", "extra"}, herdrClient},
		// The server, and commands that do their job and exit.
		{nil, herdrOther},
		{[]string{"herdr", "server"}, herdrOther},
		{[]string{"herdr", "--session", "work", "server"}, herdrOther},
		{[]string{"herdr", "server", "stop"}, herdrOther},
		{[]string{"herdr", "server", "--handoff-import", "socket", "token"}, herdrOther},
		{[]string{"herdr", "status", "client"}, herdrOther},
		{[]string{"herdr", "pane", "list"}, herdrOther},
		{[]string{"herdr", "session", "attach"}, herdrOther},
		{[]string{"herdr", "session", "attach", "--help"}, herdrOther},
		{[]string{"herdr", "session", "list"}, herdrOther},
		{[]string{"herdr", "terminal", "attach", "--help"}, herdrOther},
		{[]string{"herdr", "client", "--help"}, herdrOther},
		{[]string{"herdr", "--session", "work", "session", "attach", "work"}, herdrOther}, // prints help
		{[]string{"herdr", "terminal", "attach", "--session", "work"}, herdrOther},        // no target
		{[]string{"herdr", "--remote-keybindings", "server"}, herdrOther},                 // needs --remote
		{[]string{"herdr", "--handoff"}, herdrOther},
		{[]string{"herdr", "update"}, herdrOther},
		{[]string{"herdr", "--version"}, herdrOther},
		{[]string{"herdr", "--help"}, herdrOther},
		{[]string{"herdr", "--session"}, herdrOther},
		// A client of another machine's herdr.
		{[]string{"herdr", "--remote", "mac"}, herdrOther},
		{[]string{"herdr", "--remote=user@mac", "--session", "work"}, herdrOther},
		{[]string{"herdr", "--machine", "mac", "pane", "list"}, herdrOther},
		{[]string{"herdr", "--session", "work", "--remote", "mac"}, herdrOther},
		{[]string{"herdr", "client", "--remote=mac"}, herdrOther},
		{[]string{"herdr", "--", "client"}, herdrOther},
	} {
		if got := herdrInvocation(tc.args); got != tc.want {
			t.Errorf("herdrInvocation(%q) = %v, want %v", tc.args, got, tc.want)
		}
	}
}

func TestDetectHerdrClients(t *testing.T) {
	herdrEnv := map[string]string{"HERDR_ENV": "1", "HERDR_BIN_PATH": "/usr/local/bin/herdr", "DISPLAY": ":0"}
	const bin = "/opt/herdr/0.9.5/herdr"
	ssh := []string{"SSH_CONNECTION=10.0.0.9 50000 10.0.0.2 22", "SSH_AUTH_SOCK=/tmp/agent"}
	local := []string{"HOME=/home/u", "DISPLAY=:0"}
	server := procInfo{pid: 100, exe: bin, args: []string{"herdr", "server"}, env: ssh} // started over SSH long ago
	for _, tc := range []struct {
		name   string
		env    map[string]string
		procs  []procInfo
		err    error
		remote bool
		herdr  bool
		why    string
	}{
		{"client over ssh", herdrEnv, []procInfo{server, {pid: 101, exe: bin, args: []string{"herdr"}, env: ssh}}, nil,
			true, true, "a herdr client runs over SSH (pid 101)"},
		{"client over ssh with SSH_TTY only", herdrEnv, []procInfo{{pid: 101, exe: bin, args: []string{"herdr", "client"},
			env: []string{"SSH_TTY=/dev/pts/3"}}}, nil, true, true, "pid 101"},
		{"local client only", herdrEnv, []procInfo{server, {pid: 102, exe: bin, args: []string{"herdr"}, env: local}}, nil,
			false, true, "every herdr client runs locally (1 found)"},
		{"local and ssh clients", herdrEnv, []procInfo{
			{pid: 102, exe: bin, args: []string{"herdr"}, env: local},
			{pid: 103, exe: bin, args: []string{"herdr", "session", "attach", "work"}, env: ssh},
		}, nil, true, true, "pid 103"},
		{"empty SSH_CONNECTION", herdrEnv, []procInfo{{pid: 102, exe: bin, args: []string{"herdr"}, env: []string{"SSH_CONNECTION="}}}, nil,
			false, true, "runs locally"},
		{"herdr --remote", herdrEnv, []procInfo{{pid: 104, exe: bin, args: []string{"herdr", "remote-client-bridge"}}}, nil,
			true, true, "remote-client-bridge, pid 104"},
		// Neither the server nor CLI commands are clients: no decision.
		{"server only", herdrEnv, []procInfo{server, {pid: 105, exe: bin, args: []string{"herdr", "pane", "list"}, env: ssh}}, nil,
			false, false, "no SSH session found"},
		{"another herdr binary", herdrEnv, []procInfo{{pid: 106, exe: "/home/u/src/herdr/target/herdr", args: []string{"herdr"}, env: ssh}}, nil,
			false, false, "no SSH session found"},
		{"unreadable environment", herdrEnv, []procInfo{{pid: 107, exe: bin, args: []string{"herdr"}}}, nil,
			false, false, "no SSH session found"},
		{"scan failed", herdrEnv, nil, errors.New("permission denied"), false, false, "no SSH session found"},
		{"TERM_PROGRAM only, herdr on PATH", map[string]string{"TERM_PROGRAM": "herdr"},
			[]procInfo{{pid: 108, exe: "/usr/bin/herdr", args: []string{"herdr"}, env: ssh}}, nil, true, true, "pid 108"},
		{"not in herdr", map[string]string{}, []procInfo{{pid: 109, exe: bin, args: []string{"herdr"}, env: ssh}}, nil,
			false, false, "no SSH session found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			procs := &fakeProcs{procs: tc.procs, err: tc.err, parents: localChain()}
			s := testDetector(tc.env, procs, nil).detect()
			if s.remote != tc.remote || s.herdr != tc.herdr || !strings.Contains(s.why, tc.why) {
				t.Fatalf("detect = %+v, want remote %v herdr %v why %q", s, tc.remote, tc.herdr, tc.why)
			}
		})
	}

	// The executable's name narrows the scan; a symlinked HERDR_BIN_PATH
	// asks for both names.
	procs := &fakeProcs{parents: localChain()}
	testDetector(map[string]string{"HERDR_ENV": "1", "HERDR_BIN_PATH": "/home/u/bin/hd"}, procs, nil).detect()
	if strings.Join(procs.asked, ",") != "hd" {
		t.Fatalf("asked for %q", procs.asked)
	}
}

// Without a herdr client to decide, tmux's session environment decides,
// then an sshd ancestor, then keetui's own environment.
func TestDetectOrder(t *testing.T) {
	sshChain := map[int]fakeParent{
		keetuiPID: {400, "keetui"},
		400:       {300, "bash"}, // su -: a scrubbed environment
		300:       {250, "su"},
		250:       {200, "bash"},
		200:       {150, "sshd-session"},
		150:       {1, "sshd"},
	}
	tmuxSays := func(lines string) func() (tmuxEnv, error) {
		return func() (tmuxEnv, error) { return parseTmuxEnv([]byte(lines)), nil }
	}
	tmuxFails := func() (tmuxEnv, error) { return nil, errors.New("no server running") }
	for _, tc := range []struct {
		name    string
		env     map[string]string
		parents map[int]fakeParent
		tmux    func() (tmuxEnv, error)
		remote  bool
		why     string
	}{
		{"local", nil, localChain(), nil, false, "no SSH session found"},
		{"ssh", map[string]string{"SSH_CONNECTION": "10.0.0.1 1 10.0.0.2 22"}, localChain(), nil, true, "SSH_CONNECTION is set"},
		{"ssh tty", map[string]string{"SSH_TTY": "/dev/pts/1"}, localChain(), nil, true, "SSH_TTY is set"},
		{"su - over ssh", nil, sshChain, nil, true, "keetui runs under sshd-session (pid 200)"},
		{"old sshd", nil, map[int]fakeParent{keetuiPID: {400, "keetui"}, 400: {300, "sh"}, 300: {1, "sshd"}}, nil, true, "sshd (pid 300)"},
		{"tmux attached over ssh", map[string]string{"TMUX": "/tmp/tmux-1/default,1,0"}, localChain(),
			tmuxSays("DISPLAY=localhost:10.0\nSSH_CONNECTION=10.0.0.1 1 10.0.0.2 22\n"), true, "last attached over SSH"},
		{"tmux attached locally, stale pane env", map[string]string{"TMUX": "/tmp/tmux-1/default,1,0", "SSH_CONNECTION": "stale"},
			localChain(), tmuxSays("-SSH_CONNECTION\nDISPLAY=:0\n"), false, "last attached locally"},
		{"tmux never saw ssh", map[string]string{"TMUX": "/tmp/tmux-1/default,1,0", "SSH_TTY": "/dev/pts/1"}, localChain(),
			tmuxSays("DISPLAY=:0\n"), true, "SSH_TTY is set"},
		{"tmux unreachable", map[string]string{"TMUX": "/tmp/tmux-1/default,1,0"}, localChain(), tmuxFails, false, "no SSH session found"},
		{"ancestry unreadable", map[string]string{"SSH_CONNECTION": "x"}, nil, nil, true, "SSH_CONNECTION is set"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testDetector(tc.env, &fakeProcs{parents: tc.parents}, tc.tmux).detect()
			if s.remote != tc.remote || s.herdr || !strings.Contains(s.why, tc.why) {
				t.Fatalf("detect = %+v, want remote %v why %q", s, tc.remote, tc.why)
			}
		})
	}

	// The tmux session environment comes along for the display check.
	s := testDetector(map[string]string{"TMUX": "x"}, &fakeProcs{parents: localChain()},
		tmuxSays("-DISPLAY\nSSH_CONNECTION=a b c d\n")).detect()
	if v, known := s.tmux.lookup("DISPLAY"); !known || v != "" {
		t.Fatalf("tmux DISPLAY = %q, %v", v, known)
	}

	// The walk up the parents is bounded.
	loop := map[int]fakeParent{keetuiPID: {600, "keetui"}}
	for pid := 600; pid < 700; pid++ {
		loop[pid] = fakeParent{pid + 1, "sh"}
	}
	loop[700] = fakeParent{1, "sshd"}
	if s := testDetector(nil, &fakeProcs{parents: loop}, nil).detect(); s.remote {
		t.Fatalf("an sshd beyond the bound counted: %+v", s)
	}
}

func TestParseTmuxEnv(t *testing.T) {
	env := parseTmuxEnv([]byte("DISPLAY=localhost:11.0\n-SSH_CONNECTION\nSSH_AUTH_SOCK=/tmp/a=b\nXAUTHORITY=\n\ngarbage\n"))
	for name, want := range map[string]tmuxVar{
		"DISPLAY":        {value: "localhost:11.0"},
		"SSH_CONNECTION": {removed: true},
		"SSH_AUTH_SOCK":  {value: "/tmp/a=b"},
		"XAUTHORITY":     {},
	} {
		if got, ok := env[name]; !ok || got != want {
			t.Errorf("%s = %+v, %v", name, got, ok)
		}
	}
	if len(env) != 4 {
		t.Errorf("env = %+v", env)
	}
	if _, known := tmuxEnv(nil).lookup("DISPLAY"); known {
		t.Error("a nil environment knows DISPLAY")
	}
}

// The real process table finds a herdr client by its executable, argv and
// environment: the test binary runs again as a stand-in client, which
// HERDR_BIN_PATH names.
func TestHerdrScanFindsRealProcesses(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("no process table on " + runtime.GOOS)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	start := func(env ...string) {
		t.Helper()
		cmd := exec.Command(exe, "client")
		cmd.Env = append([]string{fakeHerdrEnv + "=1", "HOME=/nonexistent"}, env...)
		stdin, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = stdin.Close()
			_ = cmd.Wait()
		})
	}
	env := map[string]string{"HERDR_ENV": "1", "HERDR_BIN_PATH": exe}
	d := sessionDetector{getenv: func(k string) string { return env[k] }, procs: newProcessTable(),
		pid: os.Getpid(), resolve: filepath.EvalSymlinks}

	start("DISPLAY=:0") // a local client
	began := time.Now()
	s := d.detect()
	elapsed := time.Since(began)
	if s.remote || !s.herdr || !strings.Contains(s.why, "runs locally") {
		t.Fatalf("with a local client: %+v", s)
	}
	t.Logf("herdr scan took %v", elapsed)
	if elapsed > procScanBudget {
		t.Errorf("scan took %v", elapsed)
	}

	start("SSH_CONNECTION=192.0.2.1 50000 192.0.2.2 22", "SSH_TTY=/dev/pts/9")
	if s := d.detect(); !s.remote || !s.herdr || !strings.Contains(s.why, "runs over SSH") {
		t.Fatalf("with a client over SSH: %+v", s)
	}

	// The real parent of this process.
	ppid, name, err := d.procs.parent(os.Getpid())
	if err != nil || ppid != os.Getppid() || name == "" {
		t.Fatalf("parent = %d, %q, %v; want %d", ppid, name, err, os.Getppid())
	}
}

// A copy asks where the user is each time, and a herdr clear asks again.
func TestSessionIsDetectedForEachCopy(t *testing.T) {
	sessions := []remoteSession{{why: "no SSH session found"}, {remote: true, herdr: true, why: "a herdr client runs over SSH (pid 1)"}}
	calls := 0
	native := &fakeNative{local: true, marks: true}
	c, term := newFakeClipboard(native)
	c.detect = func() remoteSession {
		s := sessions[min(calls, len(sessions)-1)]
		calls++
		return s
	}
	_, _ = c.copy("one", copyOptions{osc52: config.OSC52Auto})
	_, _ = c.copy("two", copyOptions{osc52: config.OSC52Auto})
	if calls != 2 || len(native.sessions) != 2 || native.sessions[1].why != sessions[1].why || len(term.sent) != 0 {
		t.Fatalf("calls %d, sessions %+v", calls, native.sessions)
	}
}

// Forwarded X displays: auto also sends the terminal leg, and the timed
// clear clears both.
func TestAlsoTerminalRouteSendsBothLegs(t *testing.T) {
	native := &fakeNative{local: true, marks: true, forwarded: true}
	c, term := newFakeClipboard(native)
	res, err := c.copy("both", copyOptions{transient: true, osc52: config.OSC52Auto})
	if err != nil || !res.native || !res.terminal || len(term.sent) != 1 {
		t.Fatalf("copy = %+v, %v (terminal %q)", res, err, term.sent)
	}
	if got := copyStatus("password", res); got != "Copied password (also sent via OSC 52)" {
		t.Fatalf("status = %q", got)
	}
	if help := clipboardHelp(res, nil); !strings.Contains(help, "XQuartz") || !strings.Contains(help, "localhost:10.0") {
		t.Fatalf("help = %q", help)
	}
	if r, _ := c.release(); r.outcome != clearDone || r.leg != clearRequested || len(term.sent) != 2 {
		t.Fatalf("release = %+v (terminal %q)", r, term.sent)
	}
	// never still means never.
	_, _ = c.copy("local only", copyOptions{transient: true, osc52: config.OSC52Never})
	if len(term.sent) != 2 {
		t.Fatalf("never sent %q", term.sent)
	}
}

func TestHerdrRouteClearsWithASpace(t *testing.T) {
	var writes []string
	write := func(seq string) error { writes = append(writes, seq); return nil }
	session := remoteSession{remote: true, herdr: true, why: "a herdr client runs over SSH (pid 9)"}
	h := herdrClipboard{write: write, detect: func() remoteSession { return session }}
	res, err := h.copy([]byte("synthetic-secret"))
	if err != nil || res.route != routeHerdr || !res.clearable() {
		t.Fatalf("copy = %+v, %v", res, err)
	}
	if outcome, err := h.clear(res); outcome != clearOverwritten || err != nil {
		t.Fatalf("clear with a client over SSH = %v, %v", outcome, err)
	}
	for _, s := range []remoteSession{
		{herdr: true, why: "every herdr client runs locally (1 found)"},
		{remote: true, why: "SSH_CONNECTION is set"}, // no herdr client found
		{why: "no SSH session found"},
	} {
		session = s
		if outcome, err := h.clear(res); outcome != clearNoRemoteClient || err != nil {
			t.Fatalf("clear for %+v = %v, %v", s, outcome, err)
		}
	}
	if outcome, _ := (herdrClipboard{write: write}).clear(res); outcome != clearNoRemoteClient {
		t.Fatalf("clear without detection = %v", outcome)
	}
	want := []string{"\x1b]52;c;c3ludGhldGljLXNlY3JldA==\a", "\x1b]52;c;IA==\a"}
	if strings.Join(writes, "|") != strings.Join(want, "|") {
		t.Fatalf("writes = %q, want %q", writes, want)
	}

	text, isErr := clearStatus(clearResult{outcome: clearOverwritten}, nil)
	if text != "Terminal clipboard overwritten with a space" || isErr ||
		!strings.Contains(clearDetail(clearResult{outcome: clearOverwritten}), "ignores an empty OSC 52") {
		t.Fatalf("overwrite status = %q, %v", text, isErr)
	}
	text, isErr = clearStatus(clearResult{outcome: clearNoRemoteClient}, nil)
	if text != "Terminal clipboard not cleared: no herdr client over SSH" || !isErr ||
		!strings.Contains(clearDetail(clearResult{outcome: clearNoRemoteClient}), "pbcopy") {
		t.Fatalf("no-client status = %q, %v", text, isErr)
	}
	if help := clipboardHelp(copyResult{terminal: true, via: res, session: session}, nil); !strings.Contains(help, "client that last typed") {
		t.Fatalf("copy help = %q", help)
	}
}

func TestHerdrPanePicksTheHerdrRoute(t *testing.T) {
	env := map[string]string{"HERDR_ENV": "1"}
	getenv := func(k string) string { return env[k] }
	routes := newTerminalRoutes(getenv, func() remoteSession { return remoteSession{} })
	for _, kind := range []terminalKind{{}, {class: terminalOther}, {class: terminalTmux, tmux: "3.5a"}} {
		if h, ok := routes.pick(kind, false).(herdrClipboard); !ok || h.detect == nil {
			t.Fatalf("herdr pane (%v) routed to %#v", kind, routes.pick(kind, false))
		}
	}
	delete(env, "HERDR_ENV")
	env["TERM_PROGRAM"] = "herdr"
	if _, ok := newTerminalRoutes(getenv, nil).pick(terminalKind{}, false).(herdrClipboard); !ok {
		t.Fatal("TERM_PROGRAM=herdr not routed through herdr")
	}
	// tmux inside a herdr pane is tmux.
	env["TMUX"] = "/tmp/tmux-1/default,1,0"
	if _, ok := newTerminalRoutes(getenv, nil).pick(terminalKind{}, false).(tmuxClipboard); !ok {
		t.Fatal("tmux in a herdr pane not routed through tmux")
	}
}

// After a failed local copy in a remote session (the ssh client may have
// printed X11 errors over the frame), the whole screen is repainted.
func TestFailedRemoteNativeCopyRepaints(t *testing.T) {
	for _, tc := range []struct {
		session   remoteSession
		nativeErr error
		repaint   bool
	}{
		{remoteSession{remote: true, why: "SSH_CONNECTION is set"}, errors.New("x11clip: EOF"), true},
		{remoteSession{remote: true, why: "SSH_CONNECTION is set"}, nil, false},
		{remoteSession{why: "no SSH session found"}, errors.New("x11clip: EOF"), false},
	} {
		m := createTestApp(t)
		clipboard := m.clipboard.(*testClipboard)
		clipboard.terminalOnly, clipboard.session, clipboard.nativeErr = true, tc.session, tc.nativeErr
		_, cmd := m.Update(CopyToClipboardMsg{Field: "password", Value: "synthetic"})
		if got := batchHas(cmd, tea.ClearScreen()); got != tc.repaint {
			t.Errorf("%+v, %v: repaint = %v", tc.session, tc.nativeErr, got)
		}
	}
	// A copy that failed altogether repaints as well.
	m := createTestApp(t)
	clipboard := m.clipboard.(*testClipboard)
	clipboard.writeErr = errors.New("x11clip: EOF; terminal clipboard: no tty")
	clipboard.session, clipboard.nativeErr = remoteSession{remote: true}, errors.New("x11clip: EOF")
	_, cmd := m.Update(CopyToClipboardMsg{Field: "password", Value: "synthetic"})
	if !batchHas(cmd, tea.ClearScreen()) {
		t.Error("failed copy did not repaint")
	}
}

// batchHas reports whether cmd, or a command in its batch (nested ones
// included), returns want. Timers, which do not return at once, are left
// running.
func batchHas(cmd tea.Cmd, want tea.Msg) bool {
	if cmd == nil {
		return false
	}
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-result:
	case <-time.After(100 * time.Millisecond):
		return false
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if batchHas(c, want) {
				return true
			}
		}
		return false
	}
	return msg == want
}

// A clear that spoke to the local clipboard of a remote session, an X
// display forwarded over SSH, repaints whatever its outcome: the ssh client
// prints over the frame when that display has gone away, and keetui's
// ownership check or clear may be what made it try to reconnect.
func TestRemoteNativeClearRepaints(t *testing.T) {
	remote := func() remoteSession { return remoteSession{remote: true, why: "SSH_CONNECTION is set"} }
	native := &fakeNative{local: true, marks: true, forwarded: true}
	c, _ := newFakeClipboard(native)
	c.detect = remote
	copyOnce := func() {
		t.Helper()
		if _, err := c.copy("synthetic", copyOptions{transient: true, osc52: config.OSC52Auto}); err != nil {
			t.Fatal(err)
		}
	}
	copyOnce()
	if r, err := c.release(); err != nil || r.outcome != clearDone || !r.repaint {
		t.Fatalf("clear = %+v, %v", r, err)
	}
	copyOnce()
	native.owned = false // the display went away, or another client copied
	if r, err := c.release(); err != nil || r.outcome != clearChanged || !r.repaint {
		t.Fatalf("clear of a lost copy = %+v, %v", r, err)
	}
	copyOnce()
	native.clearErr = errors.New("x11clip: EOF")
	if r, err := c.release(); err == nil || !r.repaint {
		t.Fatalf("failed clear = %+v, %v", r, err)
	}
	native.clearErr = nil

	// Locally, and for terminal-only copies, nothing asks for a repaint.
	c.detect = nil
	copyOnce()
	if r, _ := c.release(); r.repaint {
		t.Fatalf("local clear repaints: %+v", r)
	}
	c, _ = newFakeClipboard(&fakeNative{})
	c.detect = remote
	copyOnce()
	if r, _ := c.release(); r.outcome != clearRequested || r.repaint {
		t.Fatalf("terminal-only clear = %+v", r)
	}

	// The app repaints after the timed clear and after the auto-lock's.
	for _, session := range []remoteSession{remote(), {why: "no SSH session found"}} {
		m := createTestApp(t)
		m.clipboard.(*testClipboard).session = session
		m = copyForCountdown(t, m, "synthetic")
		_, cmd := m.Update(clipboardClearMsg{gen: m.clipboardGen})
		if got := batchHas(cmd, tea.ClearScreen()); got != session.remote {
			t.Errorf("%+v: timed clear repaint = %v", session, got)
		}
		m = copyForCountdown(t, m, "synthetic")
		m.lastActivity = m.lastActivity.Add(-10 * time.Minute)
		updated, cmd := m.Update(autoLockMsg{gen: m.autoLockGen})
		if updated.(AppModel).state != viewUnlock {
			t.Fatal("database did not lock")
		}
		if got := batchHas(cmd, tea.ClearScreen()); got != session.remote {
			t.Errorf("%+v: auto-lock repaint = %v", session, got)
		}
	}
}

func TestHerdrClearStatusInApp(t *testing.T) {
	for _, tc := range []struct {
		outcome clearOutcome
		text    string
		isErr   bool
	}{
		{clearOverwritten, "Terminal clipboard overwritten with a space", false},
		{clearNoRemoteClient, "Terminal clipboard not cleared: no herdr client over SSH", true},
	} {
		m := createTestApp(t)
		clipboard := m.clipboard.(*testClipboard)
		clipboard.terminalOnly = true
		clipboard.via = terminalResult{route: routeHerdr}
		clipboard.outcomes = []clearOutcome{tc.outcome}
		m = copyForCountdown(t, m, "synthetic")
		_, cmd := m.Update(clipboardClearMsg{gen: m.clipboardGen})
		status := statusOf(t, cmd)
		if status.Text != tc.text || status.IsError != tc.isErr || !strings.Contains(status.Detail, "herdr") {
			t.Fatalf("status = %+v", status)
		}
	}

	// A local copy whose terminal leg went through herdr.
	m := createTestApp(t)
	m.clipboard.(*testClipboard).leg = clearNoRemoteClient
	m = copyForCountdown(t, m, "synthetic")
	_, cmd := m.Update(clipboardClearMsg{gen: m.clipboardGen})
	if status := statusOf(t, cmd); status.Text != "Clipboard cleared; terminal copy left: no herdr client over SSH" || !status.IsError ||
		!strings.Contains(status.Detail, "once") {
		t.Fatalf("leg status = %+v", status)
	}
}
