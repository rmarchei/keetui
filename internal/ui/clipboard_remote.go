package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// The local clipboard is the user's only while the user sits at this
// machine. In a session reached over SSH, directly or through a terminal
// multiplexer attached from elsewhere, only the terminal (OSC 52) reaches
// the user's clipboard. A multiplexer session can be attached from anywhere
// at any time, so keetui checks for each copy, in this order:
//
//  1. In a herdr pane, herdr's clients: herdr hands OSC 52 to a client, and
//     a client running over SSH passes it on to its own terminal.
//  2. In tmux, the session environment, which tmux refreshes on attach while
//     panes keep the environment they started with.
//  3. An sshd ancestor, which su - and sudo -i leave in place when they
//     scrub the environment. Daemonized tmux and herdr servers have none.
//  4. SSH_CONNECTION or SSH_TTY in keetui's environment.

// remoteSession is what keetui found out about the user's whereabouts.
type remoteSession struct {
	remote bool
	why    string  // what decided, for F1 help
	herdr  bool    // herdr's clients decided
	tmux   tmuxEnv // the tmux session environment; nil unless tmux answered
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
}

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
}

func newSessionDetector(getenv func(string) string) sessionDetector {
	return sessionDetector{
		getenv:  getenv,
		procs:   newProcessTable(),
		tmuxEnv: readTmuxEnv,
		pid:     os.Getpid(),
		resolve: filepath.EvalSymlinks,
	}
}

func (d sessionDetector) detect() remoteSession {
	if inHerdr(d.getenv) {
		if s, ok := d.herdrSession(); ok {
			return s
		}
	}
	var s remoteSession
	if d.getenv("TMUX") != "" && d.tmuxEnv != nil {
		if env, err := d.tmuxEnv(); err == nil {
			s.tmux = env
			if v, known := env.lookup("SSH_CONNECTION"); known {
				s.remote = v != ""
				s.why = "the tmux session was last attached locally (no SSH_CONNECTION in its environment)"
				if s.remote {
					s.why = "the tmux session was last attached over SSH (SSH_CONNECTION in its environment)"
				}
				return s
			}
		}
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

// herdrSession decides from herdr's clients: remote when any of them runs
// over SSH. ok is false when no client was found.
func (d sessionDetector) herdrSession() (s remoteSession, ok bool) {
	if d.procs == nil {
		return s, false
	}
	bin, want := d.getenv("HERDR_BIN_PATH"), ""
	names := []string{"herdr"}
	if bin != "" {
		want = d.resolvePath(bin)
		names = []string{filepath.Base(bin)}
		if base := filepath.Base(want); base != names[0] {
			names = append(names, base)
		}
	}
	procs, err := d.procs.named(names)
	if err != nil {
		return s, false
	}
	clients := 0
	for _, p := range procs {
		if p.pid == d.pid || want != "" && !d.sameExe(p.exe, want) {
			continue
		}
		switch herdrInvocation(p.args) {
		case herdrBridge:
			// herdr --remote on the user's machine runs it over SSH.
			return remoteSession{remote: true, herdr: true,
				why: fmt.Sprintf("herdr --remote is attached (remote-client-bridge, pid %d)", p.pid)}, true
		case herdrClient:
			if p.env == nil {
				continue
			}
			clients++
			if envValue(p.env, "SSH_CONNECTION") != "" || envValue(p.env, "SSH_TTY") != "" {
				return remoteSession{remote: true, herdr: true,
					why: fmt.Sprintf("a herdr client runs over SSH (pid %d)", p.pid)}, true
			}
		}
	}
	if clients == 0 {
		return s, false
	}
	return remoteSession{herdr: true, why: fmt.Sprintf("every herdr client runs locally (%d found)", clients)}, true
}

func (d sessionDetector) resolvePath(path string) string {
	if d.resolve != nil {
		if resolved, err := d.resolve(path); err == nil {
			return resolved
		}
	}
	return filepath.Clean(path)
}

// sameExe reports exe naming the executable at want, which is resolved. A
// relative path (macOS reports the path given to exec) cannot be resolved,
// so its name decides.
func (d sessionDetector) sameExe(exe, want string) bool {
	if !filepath.IsAbs(exe) {
		return filepath.Base(exe) == filepath.Base(want)
	}
	return d.resolvePath(exe) == want
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
