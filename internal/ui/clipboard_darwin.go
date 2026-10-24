//go:build darwin

package ui

import (
	"errors"
	"os"
	"os/exec"
	"strings"

	"github.com/rmarchei/keetui/internal/pasteboard"
)

// darwinClipboard writes NSPasteboard in-process: copies are concealed from
// clipboard history and kept off other devices, and ownership is tracked by
// changeCount rather than by reading the pasteboard back. pbcopy remains as
// an unmarked fallback.
type darwinClipboard struct {
	count  int  // changeCount right after the last pasteboard write
	pbcopy bool // the last write fell back to pbcopy, tracked by content
}

func newNativeClipboard() nativeClipboard { return &darwinClipboard{} }

func (*darwinClipboard) available() bool {
	return !sshSession(os.Getenv, tmuxSSHConnection)
}

func (d *darwinClipboard) write(value []byte, transient bool) (bool, error) {
	count, err := pasteboard.Copy(value, transient)
	if err == nil {
		d.count, d.pbcopy = count, false
		return true, nil
	}
	if perr := pipeTo(utf8Env(), value, "/usr/bin/pbcopy"); perr != nil {
		return false, errors.Join(err, perr)
	}
	d.pbcopy = true
	return false, nil
}

func (d *darwinClipboard) holds(matches func([]byte) bool, alsoTerminal bool) bool {
	if d.pbcopy {
		cmd := exec.Command("/usr/bin/pbpaste")
		cmd.Env = utf8Env()
		out, err := cmd.Output()
		return err == nil && matches(out)
	}
	count, err := pasteboard.ChangeCount()
	if err != nil {
		return false
	}
	if count == d.count {
		return true
	}
	if !alsoTerminal {
		return false
	}
	// The terminal applies its OSC 52 copy asynchronously, possibly after
	// ours, so a changed count may still hold the same value.
	text, err := pasteboard.Text()
	return err == nil && matches(text)
}

func (d *darwinClipboard) clear() error {
	if d.pbcopy {
		return pipeTo(utf8Env(), nil, "/usr/bin/pbcopy")
	}
	return pasteboard.Clear()
}

// pbcopy and pbpaste pick the text encoding from the locale.
func utf8Env() []string { return envWith("LANG=en_US.UTF-8", "LC_ALL=en_US.UTF-8") }

// sshSession reports whether keetui runs over SSH, where the local
// pasteboard belongs to the remote Mac rather than to the user. Inside tmux
// the pane environment goes stale when a session is re-attached from
// elsewhere, so the session environment, which tmux refreshes on attach,
// takes precedence.
func sshSession(getenv func(string) string, tmuxSSH func() (remote, known bool)) bool {
	if getenv("TMUX") != "" {
		if remote, known := tmuxSSH(); known {
			return remote
		}
	}
	return getenv("SSH_CONNECTION") != "" || getenv("SSH_TTY") != ""
}

func tmuxSSHConnection() (remote, known bool) {
	out, err := exec.Command("tmux", "show-environment", "SSH_CONNECTION").Output()
	if err != nil {
		return false, false // never set in this session
	}
	line := strings.TrimSpace(string(out))
	switch {
	case strings.HasPrefix(line, "SSH_CONNECTION="):
		return true, true
	case line == "-SSH_CONNECTION": // removed on attach from a local client
		return false, true
	}
	return false, false
}
