//go:build linux

package ui

import (
	"bytes"
	"os"
	"os/exec"
	"sync"
)

// linuxClipboard uses wl-copy on Wayland, where wl-clipboard 2.3.0 and later
// can mark copies as sensitive (x-kde-passwordManagerHint=secret), and
// xclip or xsel on X11, which cannot. Ownership is checked by content.
type linuxClipboard struct {
	getenv func(string) string
	has    func(string) bool
	tool   string // tool used for the last write

	sensitiveOnce sync.Once
	sensitive     bool // wl-copy understands --sensitive
}

func newNativeClipboard() nativeClipboard {
	return &linuxClipboard{getenv: os.Getenv, has: func(name string) bool {
		_, err := exec.LookPath(name)
		return err == nil
	}}
}

// linuxTool picks the clipboard tool for the session. X11 tools reach
// Wayland apps only on some compositors (not wlroots or Hyprland without an
// X11 window in focus), so wl-copy comes first.
func linuxTool(getenv func(string) string, has func(string) bool) string {
	if getenv("WAYLAND_DISPLAY") != "" && has("wl-copy") && has("wl-paste") {
		return "wl-copy"
	}
	if getenv("DISPLAY") != "" {
		for _, tool := range []string{"xclip", "xsel"} {
			if has(tool) {
				return tool
			}
		}
	}
	return ""
}

func (l *linuxClipboard) available() bool { return linuxTool(l.getenv, l.has) != "" }

func (l *linuxClipboard) wlSensitive() bool {
	l.sensitiveOnce.Do(func() {
		out, err := exec.Command("wl-copy", "--help").Output()
		l.sensitive = err == nil && bytes.Contains(out, []byte("--sensitive"))
	})
	return l.sensitive
}

func (l *linuxClipboard) write(value []byte, _ bool) (bool, error) {
	tool := linuxTool(l.getenv, l.has)
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
		err = pipeTo(wlEnv(l.getenv), value, "wl-copy", args...)
	case "xclip":
		err = pipeTo(nil, value, "xclip", "-selection", "clipboard")
	case "xsel":
		err = pipeTo(nil, value, "xsel", "--clipboard", "--input")
	default:
		err = errNoClipboard
	}
	if err != nil {
		return false, err
	}
	l.tool = tool
	return marked, nil
}

// holds treats an unreadable clipboard (empty, or an image) as not ours.
func (l *linuxClipboard) holds(matches func([]byte) bool, _ bool) bool {
	var cmd *exec.Cmd
	switch l.tool {
	case "wl-copy":
		cmd = exec.Command("wl-paste", "--no-newline", "--type", "text")
	case "xclip":
		cmd = exec.Command("xclip", "-selection", "clipboard", "-o")
	case "xsel":
		cmd = exec.Command("xsel", "--clipboard", "--output")
	default:
		return false
	}
	out, err := cmd.Output()
	return err == nil && matches(out)
}

func (l *linuxClipboard) clear() error {
	switch l.tool {
	case "wl-copy":
		return exec.Command("wl-copy", "--clear").Run()
	case "xclip":
		return pipeTo(nil, nil, "xclip", "-selection", "clipboard")
	case "xsel":
		return exec.Command("xsel", "--clipboard", "--clear").Run()
	}
	return errNoClipboard
}

// wlEnv points wl-copy's temporary copy of stdin at the per-user runtime
// directory (a tmpfs) instead of /tmp, which may be disk-backed.
func wlEnv(getenv func(string) string) []string {
	if dir := getenv("XDG_RUNTIME_DIR"); dir != "" {
		return envWith("TMPDIR=" + dir)
	}
	return nil
}
