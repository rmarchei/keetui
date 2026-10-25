//go:build darwin

package ui

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/rmarchei/keetui/internal/pasteboard"
)

// The pasteboard is the user's only while the user is at this Mac: a herdr
// client attached over SSH counts like an SSH session.
func TestDarwinPasteboardOnlyForLocalSessions(t *testing.T) {
	d := &darwinClipboard{}
	if r := d.route(remoteSession{why: "no SSH session found"}); !r.ok || r.alsoTerminal || r.dest == "" {
		t.Fatalf("local route = %+v", r)
	}
	for _, s := range []remoteSession{
		{remote: true, why: "SSH_CONNECTION is set"},
		{remote: true, herdr: true, why: "a herdr client runs over SSH (pid 7)"},
	} {
		if r := d.route(s); r.ok || r.why == "" {
			t.Fatalf("route for %+v = %+v", s, r)
		}
	}

	// tmux started from a herdr pane and attached over SSH with only a
	// local herdr client: the tmux session decides, as before herdr.
	env := map[string]string{"HERDR_ENV": "1", "TMUX": "/tmp/tmux-501/default,1,0", "HOME": "/Users/u",
		"HERDR_SOCKET_PATH": "/Users/u/.config/herdr/herdr.sock"}
	procs := &fakeProcs{procs: []procInfo{{pid: 101, exe: "/opt/homebrew/bin/herdr", args: []string{"herdr"},
		env: []string{"HOME=/Users/u"}}}, parents: localChain()}
	tmux := func() (tmuxEnv, error) { return parseTmuxEnv([]byte("SSH_CONNECTION=192.0.2.1 5 192.0.2.2 22\n")), nil }
	if r := d.route(testDetector(env, procs, tmux).detect()); r.ok {
		t.Fatalf("tmux in herdr attached over SSH: route = %+v", r)
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
	if marked, err := d.write([]byte("synthetic-secret"), true, nativeRoute{ok: true}); err != nil || !marked {
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

	if _, err := d.write([]byte("synthetic-secret"), false, nativeRoute{ok: true}); err != nil {
		t.Fatal(err)
	}
	if err := d.clear(); err != nil {
		t.Fatal(err)
	}
	if _, err := pasteboard.Text(); err == nil {
		t.Fatal("pasteboard still holds text after clear")
	}
}
