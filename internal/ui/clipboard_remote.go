package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// The local clipboard is the user's only while the user sits at this
// machine. In a session reached over SSH, directly or through a terminal
// multiplexer attached from elsewhere, only the terminal (OSC 52) reaches
// the user's clipboard. A multiplexer session can be attached from anywhere
// at any time, so keetui checks for each copy, in this order:
//
//  1. In a herdr pane, the clients of keetui's herdr server: herdr hands
//     OSC 52 to the client that last had input, and a client running over
//     SSH passes it on to its own terminal, while a local one writes this
//     machine's clipboard unmarked. With clients at both ends, a local one
//     that had input just now (the copy's keypress) keeps the copy here.
//     A local copy goes to that client's display (herdrDesktop).
//  2. In tmux, the session environment, which tmux refreshes on attach while
//     panes keep the environment they started with. In tmux started from a
//     herdr pane, both count: the session is remote if either says so.
//  3. An sshd ancestor, which su - and sudo -i leave in place when they
//     scrub the environment. Daemonized tmux and herdr servers have none.
//  4. SSH_CONNECTION or SSH_TTY in keetui's environment.

// remoteSession is what keetui found out about the user's whereabouts.
type remoteSession struct {
	remote   bool
	why      string  // what decided, for F1 help
	herdr    bool    // clients of keetui's herdr server were found
	herdrSSH bool    // one of them runs over SSH, or herdr --remote is attached (even when a local one decided)
	tmux     tmuxEnv // the tmux session environment; nil unless tmux answered
	desktop  herdrDesktop
}

// herdrDesktop is the display of the local herdr client the user typed on
// last, when herdr clients decided that the session is local. herdr panes
// keep the environment their server started with, which names no display,
// or an old one, when the server was started over SSH (by herdr --remote,
// say) or from another desktop session; the client's is the desktop's.
type herdrDesktop struct {
	pid int
	env []string // those of desktopVars that the client has set
}

// desktopVars name a display and what it takes to reach it.
var desktopVars = []string{"WAYLAND_DISPLAY", "XDG_RUNTIME_DIR", "DISPLAY", "XAUTHORITY"}

func desktopOf(p procInfo) herdrDesktop {
	d := herdrDesktop{pid: p.pid}
	for _, name := range desktopVars {
		if v := envValue(p.env, name); v != "" {
			d.env = append(d.env, name+"="+v)
		}
	}
	return d
}

// procInfo is a process of keetui's user.
type procInfo struct {
	pid  int
	exe  string   // executable path
	args []string // argv
	env  []string // environment; nil when unreadable
}

// processTable reads processes; tests replace it.
type processTable interface {
	// named lists the processes of keetui's user whose executable is called
	// one of names, with argv and environment.
	named(names []string) ([]procInfo, error)
	// parent returns pid's parent and the name of pid's executable.
	parent(pid int) (ppid int, name string, err error)
	// lastInput returns when the terminal that pid reads from last had
	// input (its access time, as w(1) reports idle time); false when pid
	// reads no terminal or it cannot be read.
	lastInput(pid int) (time.Time, bool)
}

// localInputWindow is how recent a local herdr client's input must be to
// count as the copy's keypress. Linux updates a terminal's access time only
// when it enters a new 8-second slot, so it may lag the keypress by up to 8
// seconds; macOS keeps it exact.
const localInputWindow = 9 * time.Second

// procScanBudget bounds a process scan. One that takes longer counts as
// failed, and detection goes on to the next check.
const procScanBudget = 200 * time.Millisecond

// ancestryDepth bounds the walk up keetui's parents.
const ancestryDepth = 32

// tmuxEnvTimeout bounds tmux show-environment.
const tmuxEnvTimeout = time.Second

type sessionDetector struct {
	getenv  func(string) string
	procs   processTable            // nil skips the process checks
	tmuxEnv func() (tmuxEnv, error) // the tmux session environment; nil skips tmux
	pid     int                     // keetui's own
	resolve func(string) (string, error)
	now     func() time.Time
}

func newSessionDetector(getenv func(string) string) sessionDetector {
	return sessionDetector{
		getenv:  getenv,
		procs:   newProcessTable(),
		tmuxEnv: readTmuxEnv,
		pid:     os.Getpid(),
		resolve: filepath.EvalSymlinks,
		now:     time.Now,
	}
}

func (d sessionDetector) detect() remoteSession {
	var s remoteSession
	if inHerdr(d.getenv) {
		s = d.herdrSession()
	}
	if d.getenv("TMUX") != "" && d.tmuxEnv != nil {
		// herdr drops $TMUX from its panes, so with both, keetui runs in a
		// tmux started from a herdr pane, which may be attached over SSH
		// itself or shown by a herdr client over SSH.
		if env, err := d.tmuxEnv(); err == nil {
			s.tmux = env
			if v, known := env.lookup("SSH_CONNECTION"); known {
				switch {
				case s.remote:
				case v != "":
					s.remote, s.why = true, "the tmux session was last attached over SSH (SSH_CONNECTION in its environment)"
				case s.herdr:
					s.why += ", and the tmux session was last attached locally"
				default:
					s.why = "the tmux session was last attached locally (no SSH_CONNECTION in its environment)"
				}
				return s
			}
		}
	}
	if s.herdr {
		return s
	}
	if name, pid, ok := d.sshdAncestor(); ok {
		s.remote, s.why = true, fmt.Sprintf("keetui runs under %s (pid %d)", name, pid)
		return s
	}
	switch {
	case d.getenv("SSH_CONNECTION") != "":
		s.remote, s.why = true, "SSH_CONNECTION is set"
	case d.getenv("SSH_TTY") != "":
		s.remote, s.why = true, "SSH_TTY is set"
	default:
		s.why = "no SSH session found"
	}
	return s
}

// inHerdr reports keetui running in a herdr pane.
func inHerdr(getenv func(string) string) bool {
	return getenv("HERDR_ENV") == "1" || getenv("TERM_PROGRAM") == "herdr"
}

// herdrSession decides from the clients of keetui's herdr server: remote
// when any of them runs over SSH. It decides nothing (herdr unset) when it
// found none.
//
// Clients count by their executable's name, not its path: herdr lets a
// client of another version attach (servers outlive upgrades, and herdr
// --remote runs the client's version on this machine), so HERDR_BIN_PATH,
// the server's binary, may not be theirs. Which server a client attaches to
// follows from its command line and environment, as herdr resolves its
// socket; clients of other herdr sessions are left out.
func (d sessionDetector) herdrSession() remoteSession {
	if d.procs == nil {
		return remoteSession{}
	}
	names := []string{"herdr"}
	if bin := strings.TrimSuffix(d.getenv("HERDR_BIN_PATH"), " (deleted)"); bin != "" {
		for _, path := range []string{bin, d.resolvePath(bin)} {
			if base := filepath.Base(path); !slices.Contains(names, base) {
				names = append(names, base)
			}
		}
	}
	procs, err := d.procs.named(names)
	if err != nil {
		return remoteSession{}
	}
	want, wantKnown := herdrClientSocket(nil, func(name string) (string, bool) {
		v := d.getenv(name)
		return v, v != ""
	})
	var local []procInfo // local clients
	remote := ""         // why the first remote client counts
	for _, p := range procs {
		kind := herdrInvocation(p.args)
		if p.pid == d.pid || kind == herdrOther {
			continue
		}
		if !d.sameHerdrServer(p, want, wantKnown) {
			continue // a client of another herdr session
		}
		switch {
		case kind == herdrBridge:
			// herdr --remote on the user's machine runs it over SSH.
			if remote == "" {
				remote = fmt.Sprintf("herdr --remote is attached (remote-client-bridge, pid %d)", p.pid)
			}
		case p.env == nil:
		case envValue(p.env, "SSH_CONNECTION") != "" || envValue(p.env, "SSH_TTY") != "":
			if remote == "" {
				remote = fmt.Sprintf("a herdr client runs over SSH (pid %d)", p.pid)
			}
		default:
			local = append(local, p)
		}
	}
	if len(local) == 0 {
		if remote == "" {
			return remoteSession{}
		}
		return remoteSession{remote: true, herdr: true, herdrSSH: true, why: remote}
	}
	typed, at, read := d.lastTyped(local)
	if remote == "" {
		return remoteSession{herdr: true, desktop: desktopOf(typed),
			why: fmt.Sprintf("every herdr client runs locally (%d found)", len(local))}
	}
	// Clients at both ends: herdr gives the copy to the one that last had
	// input. A local one that had input just now typed the copy's keypress,
	// and would write this machine's clipboard unmarked: copy here instead.
	now := time.Now
	if d.now != nil {
		now = d.now
	}
	if read && now().Sub(at) <= localInputWindow {
		return remoteSession{herdr: true, herdrSSH: true, desktop: desktopOf(typed),
			why: fmt.Sprintf("a herdr client here (pid %d) had input just now, while %s", typed.pid, remote)}
	}
	return remoteSession{remote: true, herdr: true, herdrSSH: true, why: remote}
}

// lastTyped returns the client whose terminal had input last, and when;
// read is false, and the client the first, when no terminal could be read.
func (d sessionDetector) lastTyped(clients []procInfo) (typed procInfo, at time.Time, read bool) {
	typed = clients[0]
	for _, p := range clients {
		if t, ok := d.procs.lastInput(p.pid); ok && (!read || t.After(at)) {
			typed, at, read = p, t, true
		}
	}
	return typed, at, read
}

// sameHerdrServer reports whether a herdr client attaches to keetui's
// server, whose client socket is want. A client whose environment cannot
// be read is compared by the session its command line names, if any.
func (d sessionDetector) sameHerdrServer(p procInfo, want string, wantKnown bool) bool {
	if !wantKnown {
		return true
	}
	if sock, known := herdrClientSocket(p.args, envGetter(p.env)); known {
		return d.samePath(sock, want)
	}
	name, explicit := herdrSessionArg(p.args)
	return !explicit || herdrSessionName(name) == herdrSessionName(d.getenv("HERDR_SESSION"))
}

// herdrSessionName normalizes a session name: "" is the default session.
func herdrSessionName(name string) string {
	if name == "default" {
		return ""
	}
	return name
}

// herdrClientSocket returns the client socket of the herdr server that a
// herdr command line attaches to, resolved as herdr v0.9 does
// (session.rs, server/socket_paths.rs): a session named on the command line,
// else HERDR_SOCKET_PATH (which herdr sets in its panes), else
// HERDR_CLIENT_SOCKET_PATH, else HERDR_SESSION; "default" or none is the
// default session. With args nil, getenv is keetui's, in a herdr pane.
// known is false when the environment cannot tell.
func herdrClientSocket(args []string, getenv func(string) (string, bool)) (sock string, known bool) {
	session, explicit := herdrSessionArg(args)
	if !explicit {
		if path, ok := getenv("HERDR_SOCKET_PATH"); ok {
			return herdrDeriveClientSocket(path), true
		}
		if path, ok := getenv("HERDR_CLIENT_SOCKET_PATH"); ok {
			return filepath.Clean(path), true
		}
		session, _ = getenv("HERDR_SESSION")
	}
	dir, ok := herdrConfigDir(getenv)
	if !ok {
		return "", false
	}
	if session = herdrSessionName(session); session != "" {
		dir = filepath.Join(dir, "sessions", session)
	}
	return filepath.Join(dir, "herdr-client.sock"), true
}

// herdrSessionArg returns the session a herdr command line names: herdr
// session attach NAME, or the last --session NAME before "--".
func herdrSessionArg(args []string) (name string, explicit bool) {
	if len(args) == 4 && args[1] == "session" && args[2] == "attach" {
		return args[3], true
	}
	for i := 1; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--":
			return name, explicit
		case a == "--session" && i+1 < len(args):
			name, explicit = args[i+1], true
			i++
		case strings.HasPrefix(a, "--session="):
			name, explicit = strings.TrimPrefix(a, "--session="), true
		}
	}
	return name, explicit
}

// herdrDeriveClientSocket turns an API socket path into its client socket,
// as herdr does: herdr.sock becomes herdr-client.sock.
func herdrDeriveClientSocket(api string) string {
	dir, base := filepath.Split(filepath.Clean(api))
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	if stem == "" {
		stem = "herdr"
	}
	return filepath.Join(dir, stem+"-client.sock")
}

// herdrConfigDir is herdr's configuration directory for an environment.
func herdrConfigDir(getenv func(string) (string, bool)) (string, bool) {
	if dir, ok := getenv("XDG_CONFIG_HOME"); ok {
		return filepath.Join(dir, "herdr"), true
	}
	if home, ok := getenv("HOME"); ok {
		return filepath.Join(home, ".config", "herdr"), true
	}
	return "", false
}

// envGetter looks variables up in an environment list; nil knows none.
func envGetter(env []string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		for _, kv := range env {
			if v, ok := strings.CutPrefix(kv, name+"="); ok {
				return v, true
			}
		}
		return "", false
	}
}

// samePath compares two paths, resolving their directories' symlinks if
// they differ as written.
func (d sessionDetector) samePath(a, b string) bool {
	if a == b {
		return true
	}
	if filepath.Base(a) != filepath.Base(b) {
		return false
	}
	return d.resolvePath(filepath.Dir(a)) == d.resolvePath(filepath.Dir(b))
}

func (d sessionDetector) resolvePath(path string) string {
	if d.resolve != nil {
		if resolved, err := d.resolve(path); err == nil {
			return resolved
		}
	}
	return filepath.Clean(path)
}

type herdrKind int

const (
	herdrOther  herdrKind = iota // the server, or a command that exits
	herdrClient                  // shows the server's panes on its terminal
	herdrBridge                  // remote-client-bridge: a client on another machine
)

// herdrInvocation classifies a herdr command line as herdr v0.9 parses it
// (src/main.rs, session.rs, remote/attach.rs, cli.rs): first exactly
// herdr session attach NAME; otherwise --session NAME may stand anywhere
// before "--", and what is left attaches when it is empty, client, or
// terminal attach or agent attach with a target. --remote and --machine
// reach another machine's herdr, herdr server is the server, a help flag
// prints help, and other commands do their job and exit.
func herdrInvocation(args []string) herdrKind {
	if len(args) == 0 {
		return herdrOther
	}
	isHelp := func(a string) bool { return a == "help" || a == "--help" || a == "-h" }
	if len(args) > 2 && args[1] == "session" && args[2] == "attach" {
		if len(args) == 4 && !isHelp(args[3]) {
			return herdrClient
		}
		return herdrOther
	}
	var rest []string
	for i := 1; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--":
			rest = append(rest, args[i:]...)
			i = len(args)
		case a == "--session":
			if i+1 == len(args) {
				return herdrOther // a missing name
			}
			i++
		case strings.HasPrefix(a, "--session="):
		case a == "--help", a == "-h", a == "--remote", strings.HasPrefix(a, "--remote="):
			return herdrOther
		default:
			rest = append(rest, a)
		}
	}
	switch {
	case len(rest) == 0:
		return herdrClient
	case rest[0] == "remote-client-bridge":
		return herdrBridge
	case rest[0] == "client":
		return herdrClient
	case len(rest) >= 3 && (rest[0] == "terminal" || rest[0] == "agent") && rest[1] == "attach" && !isHelp(rest[2]):
		return herdrClient
	}
	return herdrOther
}

// envValue returns a variable from an environment list.
func envValue(env []string, name string) string {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, name+"="); ok {
			return v
		}
	}
	return ""
}

// sshdAncestor finds sshd (or sshd-session, OpenSSH 9.8 and later) among
// keetui's parents.
func (d sessionDetector) sshdAncestor() (name string, pid int, ok bool) {
	if d.procs == nil {
		return "", 0, false
	}
	ppid, _, err := d.procs.parent(d.pid)
	for depth := 0; err == nil && ppid > 1 && depth < ancestryDepth; depth++ {
		var next int
		next, name, err = d.procs.parent(ppid)
		if err != nil {
			break
		}
		if name == "sshd" || name == "sshd-session" {
			return name, ppid, true
		}
		ppid = next
	}
	return "", 0, false
}

// tmuxEnv is a tmux session environment, as show-environment lists it.
type tmuxEnv map[string]tmuxVar

type tmuxVar struct {
	value   string
	removed bool // listed as -NAME: removed on attach, as the client had none
}

// lookup reports a variable tmux knows about: its value, or "" when tmux
// removed it.
func (e tmuxEnv) lookup(name string) (value string, known bool) {
	v, ok := e[name]
	return v.value, ok
}

func parseTmuxEnv(out []byte) tmuxEnv {
	env := tmuxEnv{}
	for line := range strings.SplitSeq(string(out), "\n") {
		if name, ok := strings.CutPrefix(line, "-"); ok && name != "" && !strings.Contains(name, "=") {
			env[name] = tmuxVar{removed: true}
		} else if name, value, ok := strings.Cut(line, "="); ok && name != "" {
			env[name] = tmuxVar{value: value}
		}
	}
	return env
}

// readTmuxEnv asks the tmux server in $TMUX for its session environment.
func readTmuxEnv() (tmuxEnv, error) {
	path, err := exec.LookPath("tmux")
	if err != nil {
		return nil, fmt.Errorf("tmux: %w", err)
	}
	out, err := runTmuxAt(path, tmuxEnvTimeout, nil, "show-environment")
	if err != nil {
		return nil, err
	}
	return parseTmuxEnv(out), nil
}
