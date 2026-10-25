//go:build darwin

package ui

import (
	"errors"
	"os/exec"

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

const darwinDest = "this Mac's pasteboard"

// route uses the pasteboard unless the user is elsewhere: over SSH, or
// attached over SSH to the tmux or herdr session keetui runs in.
func (*darwinClipboard) route(s remoteSession) nativeRoute {
	if s.remote {
		return nativeRoute{dest: darwinDest, why: "Not copied to " + darwinDest + ": you are not at this Mac."}
	}
	return nativeRoute{ok: true, dest: darwinDest}
}

func (d *darwinClipboard) write(value []byte, transient bool, _ nativeRoute) (bool, error) {
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
