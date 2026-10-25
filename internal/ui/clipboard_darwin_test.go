//go:build darwin

package ui

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/rmarchei/keetui/internal/pasteboard"
)

func TestSSHSessionUsesTmuxSessionEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name         string
		env          map[string]string
		remote, know bool
		want         bool
	}{
		{"local", nil, false, false, false},
		{"ssh", map[string]string{"SSH_CONNECTION": "10.0.0.1 1 10.0.0.2 22"}, false, false, true},
		{"ssh tty", map[string]string{"SSH_TTY": "/dev/ttys001"}, false, false, true},
		{"tmux reattached over ssh", map[string]string{"TMUX": "/tmp/tmux"}, true, true, true},
		{"tmux reattached locally", map[string]string{"TMUX": "/tmp/tmux", "SSH_CONNECTION": "stale"}, false, true, false},
		{"tmux without the variable", map[string]string{"TMUX": "/tmp/tmux", "SSH_TTY": "/dev/ttys001"}, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getenv := func(k string) string { return tc.env[k] }
			tmux := func() (bool, bool) { return tc.remote, tc.know }
			if got := sshSession(getenv, tmux); got != tc.want {
				t.Fatalf("sshSession = %v, want %v", got, tc.want)
			}
		})
	}
}

// The backend runs against a private named pasteboard; the user's clipboard
// is never read or written.
func privatePasteboard(t *testing.T) string {
	t.Helper()
	name := fmt.Sprintf("keetui-ui-test-%d-%s", os.Getpid(), t.Name())
	t.Cleanup(pasteboard.UsePrivate(name))
	if _, err := pasteboard.ChangeCount(); err != nil {
		t.Skipf("pasteboard unavailable: %v", err)
	}
	return name
}

// writeAsOtherApp replaces the private pasteboard from another process.
func writeAsOtherApp(t *testing.T, name, text string) {
	t.Helper()
	js := `ObjC.import("AppKit"); function run(a) { const p = $.NSPasteboard.pasteboardWithName(a[0]); p.clearContents; p.setStringForType($(a[1]), "public.utf8-plain-text") }`
	if out, err := exec.Command("/usr/bin/osascript", "-l", "JavaScript", "-e", js, name, text).CombinedOutput(); err != nil {
		t.Skipf("osascript: %v %s", err, out)
	}
}

func TestDarwinClipboardTracksOwnership(t *testing.T) {
	name := privatePasteboard(t)
	h := newHeldCopy([]byte("synthetic-secret"), copyResult{native: true}, nil)
	d := &darwinClipboard{}
	if marked, err := d.write([]byte("synthetic-secret"), true); err != nil || !marked {
		t.Fatalf("write = %v, %v", marked, err)
	}
	if !d.holds(h.matches, false) {
		t.Fatal("own copy not recognized")
	}

	// The terminal's OSC 52 copy of the same value replaces ours.
	writeAsOtherApp(t, name, "synthetic-secret")
	if d.holds(h.matches, false) {
		t.Fatal("a changed pasteboard counted as ours without an OSC 52 copy")
	}
	if !d.holds(h.matches, true) {
		t.Fatal("the OSC 52 copy of our value was not recognized")
	}

	writeAsOtherApp(t, name, "copied by the user")
	if d.holds(h.matches, true) {
		t.Fatal("another app's copy counted as ours")
	}

	if _, err := d.write([]byte("synthetic-secret"), false); err != nil {
		t.Fatal(err)
	}
	if err := d.clear(); err != nil {
		t.Fatal(err)
	}
	if _, err := pasteboard.Text(); err == nil {
		t.Fatal("pasteboard still holds text after clear")
	}
}
