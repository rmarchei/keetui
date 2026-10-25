package x11clip_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"

	"github.com/rmarchei/keetui/internal/x11clip"
)

// These tests run against a disposable X server named by
// KEETUI_X11_TEST_DISPLAY (for example Xvfb :99), never against $DISPLAY,
// which is the user's desktop clipboard.
func TestMain(m *testing.M) {
	if d := os.Getenv("KEETUI_X11_TEST_DISPLAY"); d != "" {
		// An X server resets when its last client disconnects, and refuses
		// connections meanwhile; one idle client keeps it up between tests.
		if c, err := xgb.NewConnDisplay(d); err == nil {
			defer c.Close()
		}
	}
	m.Run()
}

func testDisplay(t *testing.T) string {
	t.Helper()
	d := os.Getenv("KEETUI_X11_TEST_DISPLAY")
	if d == "" {
		t.Skip("set KEETUI_X11_TEST_DISPLAY to a dedicated test X server (e.g. Xvfb :99)")
	}
	return d
}

func newOwner(t *testing.T, name string) *x11clip.Owner {
	t.Helper()
	o := &x11clip.Owner{Display: testDisplay(t), Name: name}
	t.Cleanup(func() { _ = o.Close() })
	return o
}

// requestor is an independent X client that reads CLIPBOARD the way a
// paste or a clipboard manager does.
type requestor struct {
	t      *testing.T
	c      *xgb.Conn
	win    xproto.Window
	events chan xproto.SelectionNotifyEvent
	atoms  map[string]xproto.Atom
}

func newRequestor(t *testing.T) *requestor {
	t.Helper()
	c, err := xgb.NewConnDisplay(testDisplay(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(c.Close)
	r := &requestor{t: t, c: c, events: make(chan xproto.SelectionNotifyEvent, 16), atoms: map[string]xproto.Atom{}}
	screen := xproto.Setup(c).Roots[0]
	if r.win, err = xproto.NewWindowId(c); err != nil {
		t.Fatal(err)
	}
	if err := xproto.CreateWindowChecked(c, 0, r.win, screen.Root, 0, 0, 1, 1, 0,
		xproto.WindowClassInputOnly, 0, xproto.CwEventMask, []uint32{xproto.EventMaskPropertyChange}).Check(); err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			ev, xerr := c.WaitForEvent()
			if ev == nil && xerr == nil {
				return
			}
			if e, ok := ev.(xproto.SelectionNotifyEvent); ok {
				r.events <- e
			}
		}
	}()
	return r
}

func (r *requestor) atom(name string) xproto.Atom {
	if a, ok := r.atoms[name]; ok {
		return a
	}
	reply, err := xproto.InternAtom(r.c, false, uint16(len(name)), name).Reply()
	if err != nil {
		r.t.Fatalf("intern %s: %v", name, err)
	}
	r.atoms[name] = reply.Atom
	return reply.Atom
}

func (r *requestor) name(a xproto.Atom) string {
	reply, err := xproto.GetAtomName(r.c, a).Reply()
	if err != nil {
		return fmt.Sprintf("atom %d", a)
	}
	return reply.Name
}

type converted struct {
	typ    string
	format byte
	data   []byte
	ok     bool
}

// convert requests target at time ts (0 is CurrentTime).
func (r *requestor) convert(target string, ts xproto.Timestamp) converted {
	r.t.Helper()
	prop := r.atom("KEETUI_TEST_PROP")
	xproto.ConvertSelection(r.c, r.win, r.atom("CLIPBOARD"), r.atom(target), prop, ts)
	select {
	case e := <-r.events:
		if e.Property == xproto.AtomNone {
			return converted{}
		}
		p, err := xproto.GetProperty(r.c, true, r.win, e.Property, xproto.GetPropertyTypeAny, 0, 1<<20).Reply()
		if err != nil {
			r.t.Fatalf("read %s: %v", target, err)
		}
		return converted{typ: r.name(p.Type), format: p.Format, data: p.Value, ok: true}
	case <-time.After(3 * time.Second):
		r.t.Fatalf("no SelectionNotify for %s", target)
	}
	return converted{}
}

func (r *requestor) text() string {
	r.t.Helper()
	c := r.convert("UTF8_STRING", 0)
	if !c.ok {
		return "<refused>"
	}
	return string(c.data)
}

func (r *requestor) targets() []string {
	r.t.Helper()
	c := r.convert("TARGETS", 0)
	if !c.ok || c.typ != "ATOM" || c.format != 32 {
		r.t.Fatalf("TARGETS = %+v", c)
	}
	var names []string
	for i := 0; i+4 <= len(c.data); i += 4 {
		names = append(names, r.name(xproto.Atom(xgb.Get32(c.data[i:]))))
	}
	return names
}

func (r *requestor) owner() xproto.Window {
	r.t.Helper()
	reply, err := xproto.GetSelectionOwner(r.c, r.atom("CLIPBOARD")).Reply()
	if err != nil {
		r.t.Fatal(err)
	}
	return reply.Owner
}

func (r *requestor) property(win xproto.Window, name string) string {
	r.t.Helper()
	p, err := xproto.GetProperty(r.c, false, win, r.atom(name), xproto.GetPropertyTypeAny, 0, 64).Reply()
	if err != nil {
		r.t.Fatal(err)
	}
	return string(p.Value)
}

func (r *requestor) hasProperty(win xproto.Window, name string) bool {
	r.t.Helper()
	p, err := xproto.GetProperty(r.c, false, win, r.atom(name), xproto.GetPropertyTypeAny, 0, 0).Reply()
	if err != nil {
		r.t.Fatal(err)
	}
	return p.Type != xproto.AtomNone
}

// grab stops the server from serving other clients, as a server that hangs
// would, until ungrab.
func (r *requestor) grab() {
	r.t.Helper()
	xproto.GrabServer(r.c)
	if _, err := xproto.GetInputFocus(r.c).Reply(); err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(r.ungrab)
}

func (r *requestor) ungrab() {
	xproto.UngrabServer(r.c)
	_, _ = xproto.GetInputFocus(r.c).Reply()
}

// eventually polls cond, because SelectionClear reaches the owner
// asynchronously.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); !cond(); {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting until %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestCopyOffersMarkedText(t *testing.T) {
	o, r := newOwner(t, "keetui"), newRequestor(t)
	const secret = "pässwörd-synthetic"
	if err := o.Copy([]byte(secret), true); err != nil {
		t.Fatal(err)
	}
	if !o.Owns() {
		t.Fatal("Owns = false after Copy")
	}
	want := []string{"TARGETS", "TIMESTAMP", "MULTIPLE", "UTF8_STRING", "TEXT", "text/plain;charset=utf-8", "STRING", "x-kde-passwordManagerHint"}
	if got := r.targets(); !slices.Equal(got, want) {
		t.Fatalf("TARGETS = %v", got)
	}
	for target, want := range map[string]converted{
		"UTF8_STRING":               {typ: "UTF8_STRING", format: 8, data: []byte(secret), ok: true},
		"TEXT":                      {typ: "UTF8_STRING", format: 8, data: []byte(secret), ok: true},
		"text/plain;charset=utf-8":  {typ: "text/plain;charset=utf-8", format: 8, data: []byte(secret), ok: true},
		"STRING":                    {typ: "STRING", format: 8, data: []byte("p\xe4ssw\xf6rd-synthetic"), ok: true},
		"x-kde-passwordManagerHint": {typ: "x-kde-passwordManagerHint", format: 8, data: []byte("secret"), ok: true},
		"BOGUS":                     {},
	} {
		if got := r.convert(target, 0); got.ok != want.ok || got.typ != want.typ || got.format != want.format || !bytes.Equal(got.data, want.data) {
			t.Errorf("%s = %+v, want %+v", target, got, want)
		}
	}
	if ts := r.convert("TIMESTAMP", 0); !ts.ok || ts.typ != "INTEGER" || ts.format != 32 || len(ts.data) != 4 || xgb.Get32(ts.data) == 0 {
		t.Errorf("TIMESTAMP = %+v", ts)
	}
	// A request timed before the acquisition (ICCCM 2.2) is refused.
	if got := r.convert("UTF8_STRING", 1); got.ok {
		t.Errorf("stale request answered: %q", got.data)
	}

	win := r.owner()
	for _, prop := range []string{"WM_NAME", "_NET_WM_NAME"} {
		if got := r.property(win, prop); got != "keetui" {
			t.Errorf("%s = %q", prop, got)
		}
	}
	if got := r.property(win, "WM_CLASS"); got != "keetui\x00keetui\x00" {
		t.Errorf("WM_CLASS = %q", got)
	}
}

func TestStringOnlyForLatin1(t *testing.T) {
	o, r := newOwner(t, ""), newRequestor(t)
	if err := o.Copy([]byte("synthetic-€"), false); err != nil {
		t.Fatal(err)
	}
	got := r.targets()
	if slices.Contains(got, "STRING") || slices.Contains(got, "x-kde-passwordManagerHint") {
		t.Fatalf("TARGETS = %v", got)
	}
	if c := r.convert("STRING", 0); c.ok {
		t.Fatalf("STRING = %q", c.data)
	}
	if c := r.convert("x-kde-passwordManagerHint", 0); c.ok {
		t.Fatal("unmarked copy answered the hint")
	}
}

func TestMultiple(t *testing.T) {
	o, r := newOwner(t, ""), newRequestor(t)
	if err := o.Copy([]byte("synthetic-secret"), true); err != nil {
		t.Fatal(err)
	}
	p1, p2, p3 := r.atom("KEETUI_TEST_M1"), r.atom("KEETUI_TEST_M2"), r.atom("KEETUI_TEST_M3")
	pairs := make([]byte, 24)
	for i, a := range []xproto.Atom{r.atom("UTF8_STRING"), p1, r.atom("BOGUS"), p2, r.atom("x-kde-passwordManagerHint"), p3} {
		xgb.Put32(pairs[4*i:], uint32(a))
	}
	xproto.ChangeProperty(r.c, xproto.PropModeReplace, r.win, r.atom("KEETUI_TEST_PROP"), r.atom("ATOM_PAIR"), 32, 6, pairs)
	c := r.convert("MULTIPLE", 0)
	if !c.ok || c.typ != "ATOM_PAIR" || len(c.data) != 24 {
		t.Fatalf("MULTIPLE = %+v", c)
	}
	if got := xproto.Atom(xgb.Get32(c.data[12:])); got != xproto.AtomNone {
		t.Fatalf("refused pair kept property %s", r.name(got))
	}
	for prop, want := range map[string]string{"KEETUI_TEST_M1": "synthetic-secret", "KEETUI_TEST_M3": "secret"} {
		if got := r.property(r.win, prop); got != want {
			t.Errorf("%s = %q, want %q", prop, got, want)
		}
	}
}

func TestBlankKeepsClipboardEmpty(t *testing.T) {
	o, r := newOwner(t, "keetui"), newRequestor(t)
	if err := o.Copy([]byte("synthetic-secret"), true); err != nil {
		t.Fatal(err)
	}
	win := r.owner()
	o.Blank()
	if got := r.text(); got != "" {
		t.Fatalf("after Blank: %q", got)
	}
	if slices.Contains(r.targets(), "x-kde-passwordManagerHint") {
		t.Fatal("blank copy is still marked")
	}
	if r.owner() != win || !o.Owns() {
		t.Fatal("Blank gave up the clipboard")
	}
	if err := o.Close(); err != nil {
		t.Fatal(err)
	}
	if r.owner() != xproto.WindowNone {
		t.Fatal("Close kept the clipboard")
	}
}

// Blank, Close and Owns must leave a copy another client made later alone,
// even though keetui's own timestamp would still be valid.
func TestForeignCopyIsLeftAlone(t *testing.T) {
	r := newRequestor(t)
	for _, op := range []string{"Blank", "Close"} {
		t.Run(op, func(t *testing.T) {
			o, other := newOwner(t, "keetui"), newOwner(t, "other")
			if err := o.Copy([]byte("synthetic-secret"), true); err != nil {
				t.Fatal(err)
			}
			if err := other.Copy([]byte("copied-later"), false); err != nil {
				t.Fatal(err)
			}
			theirs := r.owner()
			eventually(t, "keetui lost the clipboard", func() bool { return !o.Owns() })
			if op == "Blank" {
				o.Blank()
			} else if err := o.Close(); err != nil {
				t.Fatal(err)
			}
			if r.owner() != theirs || r.text() != "copied-later" {
				t.Fatalf("%s replaced the later copy: %q", op, r.text())
			}
		})
	}
}

func TestReadOtherClient(t *testing.T) {
	o, other := newOwner(t, ""), newOwner(t, "")
	if err := other.Copy([]byte("terminal-copy"), false); err != nil {
		t.Fatal(err)
	}
	got, err := o.Read()
	if err != nil || string(got) != "terminal-copy" {
		t.Fatalf("Read = %q, %v", got, err)
	}
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}
	if got, err := o.Read(); !errors.Is(err, x11clip.ErrNoText) {
		t.Fatalf("Read of an ownerless clipboard = %q, %v", got, err)
	}
	// Reading its own copy goes through the same server round trip.
	if err := o.Copy([]byte("own-copy"), true); err != nil {
		t.Fatal(err)
	}
	if got, err := o.Read(); err != nil || string(got) != "own-copy" {
		t.Fatalf("Read of own copy = %q, %v", got, err)
	}
}

// The caller copies a refused value elsewhere (a tool or OSC 52), so the
// earlier secret must not stay on the clipboard; the clipboard stays
// keetui's, empty, until the other copy takes it.
func TestRefusedCopyBlanksPreviousCopy(t *testing.T) {
	o, r := newOwner(t, ""), newRequestor(t)
	if err := o.Copy([]byte("synthetic-secret"), true); err != nil {
		t.Fatal(err)
	}
	limit := int(xproto.Setup(r.c).MaximumRequestLength)*4 - 24
	if err := o.Copy(bytes.Repeat([]byte("x"), limit), true); err != nil {
		t.Fatalf("Copy at the limit: %v", err)
	}
	if got := r.convert("UTF8_STRING", 0); len(got.data) != limit {
		t.Fatalf("read %d bytes, want %d", len(got.data), limit)
	}
	if err := o.Copy([]byte("synthetic-secret"), true); err != nil {
		t.Fatal(err)
	}
	win := r.owner()
	if err := o.Copy(bytes.Repeat([]byte("x"), limit+1), true); !errors.Is(err, x11clip.ErrTooLarge) {
		t.Fatalf("Copy above the limit = %v", err)
	}
	if got := r.text(); got != "" {
		t.Fatalf("after a refused copy the clipboard serves %q", got)
	}
	if slices.Contains(r.targets(), "x-kde-passwordManagerHint") {
		t.Fatal("the blank copy is still marked")
	}
	if r.owner() != win || !o.Owns() {
		t.Fatal("a refused copy gave up the clipboard")
	}
}

// A server that stops answering (here another client grabs it) must not make
// keetui drop its connection: the clipboard would be left without an owner,
// and managers that ignore the marker refill it with the secret.
func TestStalledServerKeepsTheClipboard(t *testing.T) {
	r := newRequestor(t)
	o := &x11clip.Owner{Display: testDisplay(t), Name: "keetui", Timeout: 300 * time.Millisecond}
	t.Cleanup(func() { _ = o.Close() })
	if err := o.Copy([]byte("synthetic-secret"), true); err != nil {
		t.Fatal(err)
	}
	win := r.owner()

	r.grab()
	start := time.Now()
	if err := o.Copy([]byte("synthetic-during-stall"), true); !errors.Is(err, x11clip.ErrTimeout) {
		t.Fatalf("Copy during the stall = %v, want ErrTimeout", err)
	}
	if elapsed := time.Since(start); elapsed < 250*time.Millisecond {
		t.Fatalf("Copy failed after %v: the server was not stalled", elapsed)
	}
	// The server answers in order, so later waits fail at once.
	start = time.Now()
	if !o.Owns() {
		t.Fatal("Owns = false while the server did not answer")
	}
	o.Blank()
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("Owns and Blank waited %v for a server known to be stalled", elapsed)
	}
	r.ungrab()

	if got := r.owner(); got != win {
		t.Fatalf("owner after the stall = %#x, want keetui's window %#x", got, win)
	}
	if got := r.text(); got != "" {
		t.Fatalf("after the stall the clipboard serves %q", got)
	}
	if slices.Contains(r.targets(), "x-kde-passwordManagerHint") || !o.Owns() {
		t.Fatal("the blank copy is marked, or no longer keetui's")
	}
	// The same connection copies again once it sees the server answer,
	// past the late report of the stalled copy's timestamp.
	eventually(t, "keetui copies again", func() bool { return o.Copy([]byte("synthetic-after-stall"), true) == nil })
	if got := r.text(); got != "synthetic-after-stall" || r.owner() != win {
		t.Fatalf("after the stall: %q", got)
	}
}

// Close must not wait for a stalled server, so that keetui's exit stays
// bounded; the connection then closes when the server answers.
func TestCloseDoesNotWaitForAStalledServer(t *testing.T) {
	r := newRequestor(t)
	o := &x11clip.Owner{Display: testDisplay(t), Timeout: 300 * time.Millisecond}
	if err := o.Copy([]byte("synthetic-secret"), true); err != nil {
		t.Fatal(err)
	}
	r.grab()
	if !o.Owns() {
		t.Fatal("Owns = false while the server did not answer")
	}
	start := time.Now()
	if err := o.Close(); !errors.Is(err, x11clip.ErrTimeout) || time.Since(start) > 200*time.Millisecond {
		t.Fatalf("Close on a stalled server = %v after %v", err, time.Since(start))
	}
	r.ungrab()
	eventually(t, "the connection closed", func() bool { return r.owner() == xproto.WindowNone })
}

// A server that does not answer a new connection (here it is grabbed by
// another client) costs one timeout: later calls fail at once instead of
// each freezing the caller again, and the connection, once the server
// answers it, serves the next copy.
func TestPendingConnectServesLater(t *testing.T) {
	r := newRequestor(t)
	o := &x11clip.Owner{Display: testDisplay(t), Name: "keetui", Timeout: 300 * time.Millisecond}
	t.Cleanup(func() { _ = o.Close() })
	r.grab()
	start := time.Now()
	if err := o.Copy([]byte("synthetic-secret"), true); !errors.Is(err, x11clip.ErrTimeout) {
		t.Fatalf("Copy while the server is grabbed = %v, want ErrTimeout", err)
	}
	if elapsed := time.Since(start); elapsed < 250*time.Millisecond {
		t.Fatalf("Copy failed after %v: the connect was not stalled", elapsed)
	}
	start = time.Now()
	if err := o.Copy([]byte("synthetic-secret"), true); !errors.Is(err, x11clip.ErrTimeout) {
		t.Fatalf("second Copy = %v, want ErrTimeout", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("second Copy waited %v for the pending connect", elapsed)
	}
	r.ungrab()
	eventually(t, "keetui copies once the server answers", func() bool { return o.Copy([]byte("synthetic-secret"), true) == nil })
	if got := r.text(); got != "synthetic-secret" {
		t.Fatalf("after the pending connect the clipboard serves %q", got)
	}
	if !slices.Contains(r.targets(), "x-kde-passwordManagerHint") {
		t.Fatal("the copy is not marked")
	}
}

// When the server stops reading while keetui writes large answers to a
// requestor, the socket buffer fills up, and xgb waits for room before it
// returns from sending any request. Blank and Copy, which the UI runs on
// its own goroutine, must return anyway, and the blank takes effect once
// the server reads again.
func TestFullSocketDoesNotBlock(t *testing.T) {
	r := newRequestor(t)
	o := &x11clip.Owner{Display: testDisplay(t), Name: "keetui", Timeout: 300 * time.Millisecond}
	t.Cleanup(func() { _ = o.Close() })
	limit := int(xproto.Setup(r.c).MaximumRequestLength)*4 - 24
	if err := o.Copy(bytes.Repeat([]byte("x"), limit), true); err != nil {
		t.Fatal(err)
	}
	win := r.owner()
	// Four conversions of the largest value, about 1 MiB in all, then the
	// grab: the server stops reading keetui's answers.
	clip, utf8 := r.atom("CLIPBOARD"), r.atom("UTF8_STRING")
	const n = 4
	for i := range n {
		xproto.ConvertSelection(r.c, r.win, clip, utf8, r.atom(fmt.Sprintf("KEETUI_TEST_PROP%d", i)), xproto.TimeCurrentTime)
	}
	r.grab()
	time.Sleep(300 * time.Millisecond) // keetui writes until the buffer is full

	done := make(chan time.Duration, 1)
	go func() {
		start := time.Now()
		o.Blank()
		if err := o.Copy([]byte("synthetic-secret"), true); !errors.Is(err, x11clip.ErrTimeout) {
			t.Errorf("Copy on a full socket = %v, want ErrTimeout", err)
		}
		done <- time.Since(start)
	}()
	select {
	case elapsed := <-done:
		if elapsed > time.Second {
			t.Errorf("Blank and Copy took %v", elapsed)
		}
	case <-time.After(3 * time.Second):
		r.ungrab()
		<-done
		t.Fatal("Blank or Copy waited for the server to read")
	}
	r.ungrab()
	for i := range n {
		select {
		case <-r.events:
		case <-time.After(3 * time.Second):
			t.Fatalf("conversion %d was never answered", i)
		}
	}
	if got := r.text(); got != "" {
		t.Fatalf("after the stall the clipboard serves %d bytes", len(got))
	}
	if r.owner() != win {
		t.Fatal("keetui lost the clipboard")
	}
}

// slowOwner takes CLIPBOARD with text and answers each conversion only after
// delay, like a busy application. It reports each answered requestor once
// the server has the answer.
func slowOwner(t *testing.T, text string, delay time.Duration) <-chan xproto.Window {
	t.Helper()
	c, err := xgb.NewConnDisplay(testDisplay(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(c.Close)
	win, err := xproto.NewWindowId(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := xproto.CreateWindowChecked(c, 0, win, xproto.Setup(c).Roots[0].Root, 0, 0, 1, 1, 0,
		xproto.WindowClassInputOnly, 0, 0, nil).Check(); err != nil {
		t.Fatal(err)
	}
	clip, err := xproto.InternAtom(c, false, 9, "CLIPBOARD").Reply()
	if err != nil {
		t.Fatal(err)
	}
	xproto.SetSelectionOwner(c, win, clip.Atom, xproto.TimeCurrentTime)
	if r, err := xproto.GetSelectionOwner(c, clip.Atom).Reply(); err != nil || r.Owner != win {
		t.Fatalf("slow owner did not get the clipboard: %v", err)
	}
	answered := make(chan xproto.Window, 4)
	go func() {
		for {
			ev, xerr := c.WaitForEvent()
			if ev == nil && xerr == nil {
				return
			}
			e, ok := ev.(xproto.SelectionRequestEvent)
			if !ok {
				continue
			}
			time.Sleep(delay)
			xproto.ChangeProperty(c, xproto.PropModeReplace, e.Requestor, e.Property, e.Target, 8, uint32(len(text)), []byte(text))
			reply := xproto.SelectionNotifyEvent{Time: e.Time, Requestor: e.Requestor, Selection: e.Selection, Target: e.Target, Property: e.Property}
			xproto.SendEvent(c, false, e.Requestor, xproto.EventMaskNoEvent, string(reply.Bytes()))
			_, _ = xproto.GetInputFocus(c).Reply()
			answered <- e.Requestor
		}
	}()
	return answered
}

// A conversion that arrives after Read gave up must not leave the text on
// keetui's window, where any client could read it.
func TestLateConversionIsWiped(t *testing.T) {
	r := newRequestor(t)
	o := &x11clip.Owner{Display: testDisplay(t), Timeout: 200 * time.Millisecond}
	t.Cleanup(func() { _ = o.Close() })
	answered := slowOwner(t, "synthetic-terminal-copy", 600*time.Millisecond)
	if got, err := o.Read(); !errors.Is(err, x11clip.ErrTimeout) {
		t.Fatalf("Read from a slow owner = %q, %v, want ErrTimeout", got, err)
	}
	var win xproto.Window
	select {
	case win = <-answered:
	case <-time.After(3 * time.Second):
		t.Fatal("the slow owner never answered")
	}
	eventually(t, "the late conversion was deleted", func() bool { return !r.hasProperty(win, "_X11CLIP_SELECTION") })
}

// A connection the server closes is detected; the next call reconnects.
func TestReconnectsAfterLosingTheConnection(t *testing.T) {
	o, r := newOwner(t, ""), newRequestor(t)
	if err := o.Copy([]byte("synthetic-secret"), true); err != nil {
		t.Fatal(err)
	}
	if err := xproto.KillClientChecked(r.c, uint32(r.owner())).Check(); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the lost connection was noticed", func() bool { return !o.Owns() })
	if r.owner() != xproto.WindowNone {
		t.Fatal("the killed client still owns the clipboard")
	}
	if err := o.Copy([]byte("after-reconnect"), true); err != nil {
		t.Fatalf("Copy after reconnect: %v", err)
	}
	if got := r.text(); got != "after-reconnect" {
		t.Fatalf("after reconnect: %q", got)
	}
}

// Copies, blanks, ownership checks and reads race with another client that
// keeps taking the clipboard and with readers. Run with -race.
func TestConcurrentUse(t *testing.T) {
	o, other := newOwner(t, ""), newOwner(t, "")
	var copied sync.Map
	copied.Store("", true)
	copied.Store("copied-later", true)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	var reads, bad atomic.Int64
	run := func(f func(i int)) {
		wg.Go(func() {
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				f(i)
			}
		})
	}
	run(func(i int) {
		v := fmt.Sprintf("synthetic-%d", i)
		copied.Store(v, true)
		if err := o.Copy([]byte(v), i%2 == 0); err != nil && !errors.Is(err, x11clip.ErrNotOwner) {
			t.Errorf("Copy: %v", err)
		}
	})
	run(func(int) {
		o.Blank()
		time.Sleep(time.Millisecond)
	})
	run(func(int) { o.Owns() })
	run(func(int) {
		v, err := o.Read()
		if err == nil {
			reads.Add(1)
			if _, ok := copied.Load(string(v)); !ok {
				bad.Add(1)
			}
		}
	})
	run(func(int) {
		_ = other.Copy([]byte("copied-later"), false)
		time.Sleep(20 * time.Millisecond)
	})
	time.Sleep(2 * time.Second)
	close(stop)
	wg.Wait()
	t.Logf("reads=%d bad=%d", reads.Load(), bad.Load())
	if bad.Load() != 0 {
		t.Fatal("a read returned text that was never copied")
	}
}

// incrOwner takes CLIPBOARD and answers UTF8_STRING conversions with an INCR
// transfer (ICCCM 2.7.2), as GTK, Qt and Xt send large text: the INCR marker
// announcing size bytes at least, then each chunk once the requestor has
// deleted the one before, and an empty chunk at the end. It waits delay
// before answering a request and pause before each chunk after the first.
type incrOwner struct {
	text         []byte
	chunk        int
	size         uint32
	delay, pause time.Duration
}

// start returns a channel that receives the requestor's window once the
// requestor has deleted the empty last chunk: the transfer is complete.
func (io incrOwner) start(t *testing.T) <-chan xproto.Window {
	t.Helper()
	c, err := xgb.NewConnDisplay(testDisplay(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(c.Close)
	win, err := xproto.NewWindowId(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := xproto.CreateWindowChecked(c, 0, win, xproto.Setup(c).Roots[0].Root, 0, 0, 1, 1, 0,
		xproto.WindowClassInputOnly, 0, 0, nil).Check(); err != nil {
		t.Fatal(err)
	}
	atom := func(name string) xproto.Atom {
		r, err := xproto.InternAtom(c, false, uint16(len(name)), name).Reply()
		if err != nil {
			t.Fatal(err)
		}
		return r.Atom
	}
	clip, utf8, incr := atom("CLIPBOARD"), atom("UTF8_STRING"), atom("INCR")
	xproto.SetSelectionOwner(c, win, clip, xproto.TimeCurrentTime)
	if r, err := xproto.GetSelectionOwner(c, clip).Reply(); err != nil || r.Owner != win {
		t.Fatalf("INCR owner did not get the clipboard: %v", err)
	}
	ended := make(chan xproto.Window, 4)
	go func() {
		type key struct {
			win  xproto.Window
			prop xproto.Atom
		}
		type transfer struct {
			off  int
			last bool // the empty chunk is written
		}
		active := map[key]*transfer{}
		for {
			ev, xerr := c.WaitForEvent()
			if ev == nil && xerr == nil {
				return
			}
			switch e := ev.(type) {
			case xproto.SelectionRequestEvent:
				time.Sleep(io.delay)
				reply := xproto.SelectionNotifyEvent{Time: e.Time, Requestor: e.Requestor, Selection: e.Selection, Target: e.Target}
				if e.Target == utf8 {
					// Deletions of the property on the requestor's window
					// drive the transfer.
					xproto.ChangeWindowAttributes(c, e.Requestor, xproto.CwEventMask, []uint32{xproto.EventMaskPropertyChange})
					size := make([]byte, 4)
					xgb.Put32(size, io.size)
					xproto.ChangeProperty(c, xproto.PropModeReplace, e.Requestor, e.Property, incr, 32, 1, size)
					active[key{e.Requestor, e.Property}] = &transfer{}
					reply.Property = e.Property
				}
				xproto.SendEvent(c, false, e.Requestor, xproto.EventMaskNoEvent, string(reply.Bytes()))
			case xproto.PropertyNotifyEvent:
				k := key{e.Window, e.Atom}
				x := active[k]
				if x == nil || e.State != xproto.PropertyDelete {
					break
				}
				if x.last {
					delete(active, k)
					ended <- e.Window
					break
				}
				if x.off > 0 {
					time.Sleep(io.pause)
				}
				n := min(io.chunk, len(io.text)-x.off)
				xproto.ChangeProperty(c, xproto.PropModeReplace, e.Window, e.Atom, utf8, 8, uint32(n), io.text[x.off:x.off+n])
				x.off += n
				x.last = n == 0
			}
		}
	}()
	return ended
}

func incrText(n int) []byte {
	return bytes.Repeat([]byte("synthetic-incr-"), n/15+1)[:n]
}

// awaitEnd waits until the requestor has taken the empty last chunk, then
// checks that no clipboard text is left on its window, where any client
// could read it.
func awaitEnd(t *testing.T, ended <-chan xproto.Window, r *requestor) {
	t.Helper()
	select {
	case win := <-ended:
		if got := r.property(win, "WM_NAME"); got != "keetui" {
			t.Fatalf("the transfer went to window %#x (%q), not keetui's", win, got)
		}
		if r.hasProperty(win, "_X11CLIP_SELECTION") {
			t.Fatal("clipboard text left on keetui's window")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the INCR transfer did not run to its end")
	}
}

func TestReadIncr(t *testing.T) {
	for _, tc := range []struct {
		name        string
		size, chunk int
	}{
		{"large chunks", 190000, 65536},
		{"many small chunks", 20000, 1000},
		{"empty", 0, 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, r := newOwner(t, "keetui"), newRequestor(t)
			text := incrText(tc.size)
			ended := incrOwner{text: text, chunk: tc.chunk, size: uint32(len(text))}.start(t)
			got, err := o.Read()
			if err != nil || !bytes.Equal(got, text) {
				t.Fatalf("Read = %d bytes, %v; want the %d bytes sent", len(got), err, len(text))
			}
			awaitEnd(t, ended, r)
		})
	}
}

// Text longer than any copy cannot match one: Read reports ErrTooLarge,
// keeping none of it, and still drains the transfer, so the owner finishes
// and no chunk stays on keetui's window.
func TestReadIncrTooLarge(t *testing.T) {
	r := newRequestor(t)
	limit := int(xproto.Setup(r.c).MaximumRequestLength)*4 - 24
	for _, tc := range []struct {
		name string
		size uint32 // the lower bound the owner announces
	}{
		{"announced", uint32(limit + 1)},
		{"understated", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := newOwner(t, "keetui")
			ended := incrOwner{text: incrText(limit + 1), chunk: 65536, size: tc.size}.start(t)
			if got, err := o.Read(); !errors.Is(err, x11clip.ErrTooLarge) || got != nil {
				t.Fatalf("Read = %d bytes, %v; want ErrTooLarge", len(got), err)
			}
			awaitEnd(t, ended, r)
		})
	}
}

// An INCR answer that comes after Read gave up starts a transfer when the
// event loop deletes its marker; the loop then drains it.
func TestLateIncrIsDrained(t *testing.T) {
	r := newRequestor(t)
	o := &x11clip.Owner{Display: testDisplay(t), Name: "keetui", Timeout: 200 * time.Millisecond}
	t.Cleanup(func() { _ = o.Close() })
	ended := incrOwner{text: incrText(100000), chunk: 65536, size: 100000, delay: 600 * time.Millisecond}.start(t)
	if got, err := o.Read(); !errors.Is(err, x11clip.ErrTimeout) {
		t.Fatalf("Read from a slow INCR owner = %d bytes, %v; want ErrTimeout", len(got), err)
	}
	awaitEnd(t, ended, r)
}

// A transfer slower than the timeout is abandoned by Read but drained by the
// event loop to its end, and a later Read is not disturbed by it.
func TestAbandonedIncrIsDrained(t *testing.T) {
	r := newRequestor(t)
	o := &x11clip.Owner{Display: testDisplay(t), Name: "keetui", Timeout: 300 * time.Millisecond}
	t.Cleanup(func() { _ = o.Close() })
	ended := incrOwner{text: incrText(50000), chunk: 10000, size: 50000, pause: 150 * time.Millisecond}.start(t)
	if got, err := o.Read(); !errors.Is(err, x11clip.ErrTimeout) {
		t.Fatalf("Read of a slow transfer = %d bytes, %v; want ErrTimeout", len(got), err)
	}
	awaitEnd(t, ended, r)
	other := newOwner(t, "")
	if err := other.Copy([]byte("synthetic-after-incr"), false); err != nil {
		t.Fatal(err)
	}
	if got, err := o.Read(); err != nil || string(got) != "synthetic-after-incr" {
		t.Fatalf("Read after the abandoned transfer = %q, %v", got, err)
	}
}

// Any client can send keetui's window a SelectionNotify naming any of its
// properties. keetui must not delete what it names, such as the WM_CLASS
// that clipmenu and greenclip filter on.
func TestForgedNotifyLeavesWindowAlone(t *testing.T) {
	o, r := newOwner(t, "keetui"), newRequestor(t)
	if err := o.Copy([]byte("synthetic-secret"), true); err != nil {
		t.Fatal(err)
	}
	win := r.owner()
	for _, prop := range []string{"WM_CLASS", "WM_NAME", "_X11CLIP_TIMESTAMP"} {
		forged := xproto.SelectionNotifyEvent{
			Time: xproto.TimeCurrentTime, Requestor: win, Selection: r.atom("CLIPBOARD"),
			Target: r.atom("UTF8_STRING"), Property: r.atom(prop),
		}
		xproto.SendEvent(r.c, false, win, xproto.EventMaskNoEvent, string(forged.Bytes()))
	}
	// keetui handles its events in order, so the forged ones come first.
	if got := r.text(); got != "synthetic-secret" {
		t.Fatalf("clipboard = %q", got)
	}
	if got := r.property(win, "WM_CLASS"); got != "keetui\x00keetui\x00" {
		t.Fatalf("WM_CLASS = %q after forged SelectionNotify events", got)
	}
	if got := r.property(win, "WM_NAME"); got != "keetui" {
		t.Fatalf("WM_NAME = %q after forged SelectionNotify events", got)
	}
	if !r.hasProperty(win, "_X11CLIP_TIMESTAMP") {
		t.Fatal("_X11CLIP_TIMESTAMP deleted by a forged SelectionNotify")
	}
}
