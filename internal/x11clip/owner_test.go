package x11clip

import (
	"bytes"
	"errors"
	"net"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// These tests need no X server. Tests against a real one are in
// xvfb_test.go and run only with KEETUI_X11_TEST_DISPLAY.

func TestBeforeWrapsAround(t *testing.T) {
	for _, tc := range []struct {
		a, b xproto.Timestamp
		want bool
	}{
		{1, 2, true},
		{2, 1, false},
		{5, 5, false},
		{0xfffffff0, 0x10, true}, // b is after a wrap-around
		{0x10, 0xfffffff0, false},
	} {
		if got := before(tc.a, tc.b); got != tc.want {
			t.Errorf("before(%#x, %#x) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestLatin1(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []byte
		ok   bool
	}{
		{"", []byte{}, true},
		{"plain", []byte("plain"), true},
		{"pässwörd", []byte("p\xe4ssw\xf6rd"), true},
		{"€uro", nil, false},
		{"\xff\xfe", nil, false}, // not UTF-8
		{"�", nil, false},
	} {
		got, ok := latin1([]byte(tc.in))
		if ok != tc.ok || !bytes.Equal(got, tc.want) || isLatin1([]byte(tc.in)) != tc.ok {
			t.Errorf("latin1(%q) = %q, %v", tc.in, got, ok)
		}
	}
}

func testAtoms() atoms {
	var a atoms
	for i, t := range a.table() {
		*t.dst = xproto.Atom(100 + i)
	}
	return a
}

func decodeAtoms(b []byte) []xproto.Atom {
	list := make([]xproto.Atom, len(b)/4)
	for i := range list {
		list[i] = xproto.Atom(xgb.Get32(b[4*i:]))
	}
	return list
}

func TestConvertTargets(t *testing.T) {
	a := testAtoms()
	base := []xproto.Atom{a.targets, a.timestamp, a.multiple, a.utf8, a.text, a.textPlain}
	for _, tc := range []struct {
		name   string
		value  string
		marked bool
		want   []xproto.Atom
	}{
		{"marked", "synthetic-secret", true, append(slices.Clone(base), xproto.AtomString, a.hint)},
		{"unmarked", "synthetic-secret", false, append(slices.Clone(base), xproto.AtomString)},
		{"not latin-1", "synthetic-€", true, append(slices.Clone(base), a.hint)},
		{"blank", "", false, append(slices.Clone(base), xproto.AtomString)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, ok := a.convert(a.targets, content{value: []byte(tc.value), marked: tc.marked})
			if !ok || p.typ != xproto.AtomAtom || p.format != 32 {
				t.Fatalf("TARGETS = %+v, %v", p, ok)
			}
			if got := decodeAtoms(p.data); !slices.Equal(got, tc.want) {
				t.Fatalf("TARGETS = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestConvertValues(t *testing.T) {
	a := testAtoms()
	marked := content{value: []byte("pässwörd"), marked: true, ts: 0x01020304}
	for _, tc := range []struct {
		name   string
		target xproto.Atom
		c      content
		typ    xproto.Atom
		format byte
		data   []byte // nil: refused
	}{
		{"UTF8_STRING", a.utf8, marked, a.utf8, 8, marked.value},
		{"TEXT answers as UTF8_STRING", a.text, marked, a.utf8, 8, marked.value},
		{"text/plain", a.textPlain, marked, a.textPlain, 8, marked.value},
		{"STRING is Latin-1", xproto.AtomString, marked, xproto.AtomString, 8, []byte("p\xe4ssw\xf6rd")},
		{"STRING refused beyond Latin-1", xproto.AtomString, content{value: []byte("€")}, 0, 0, nil},
		{"hint", a.hint, marked, a.hint, 8, []byte("secret")},
		{"hint refused when unmarked", a.hint, content{value: []byte("x")}, 0, 0, nil},
		{"TIMESTAMP", a.timestamp, marked, xproto.AtomInteger, 32, []byte{4, 3, 2, 1}},
		{"unknown target", 9999, marked, 0, 0, nil},
		{"MULTIPLE is not a plain conversion", a.multiple, marked, 0, 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			orig := bytes.Clone(tc.c.value)
			p, ok := a.convert(tc.target, tc.c)
			if tc.data == nil {
				if ok {
					t.Fatalf("convert = %+v, want a refusal", p)
				}
				return
			}
			if !ok || p.typ != tc.typ || p.format != tc.format || !bytes.Equal(p.data, tc.data) {
				t.Fatalf("convert = %+v, %v", p, ok)
			}
			// The writer wipes the data; the copy itself must survive.
			clear(p.data)
			if !bytes.Equal(tc.c.value, orig) {
				t.Fatal("wiping a conversion wiped the copy")
			}
		})
	}
}

func pairBytes(atoms ...xproto.Atom) []byte {
	b := make([]byte, 4*len(atoms))
	for i, a := range atoms {
		xgb.Put32(b[4*i:], uint32(a))
	}
	return b
}

func TestConvertPairs(t *testing.T) {
	const multiple, utf8, bogus, p1, p2, p3, p4 = 1, 2, 3, 11, 12, 13, 14
	var converted []xproto.Atom
	conv := func(target, prop xproto.Atom) bool {
		converted = append(converted, target)
		return target == utf8
	}
	in := pairBytes(utf8, p1, bogus, p2, multiple, p3, utf8, xproto.AtomNone, utf8, p4)
	out, ok := convertPairs(in, multiple, conv)
	want := pairBytes(utf8, p1, bogus, xproto.AtomNone, multiple, xproto.AtomNone, utf8, xproto.AtomNone, utf8, p4)
	if !ok || !bytes.Equal(out, want) {
		t.Fatalf("pairs = %v, want %v", decodeAtoms(out), decodeAtoms(want))
	}
	if !slices.Equal(converted, []xproto.Atom{utf8, bogus, utf8}) {
		t.Fatalf("converted %v; nested MULTIPLE and None properties must be skipped", converted)
	}
	for _, bad := range [][]byte{nil, make([]byte, 4), make([]byte, 12)} {
		if _, ok := convertPairs(bad, multiple, conv); ok {
			t.Fatalf("accepted %d bytes of pairs", len(bad))
		}
	}
}

func ownedSession(o *Owner, ts xproto.Timestamp) *session {
	s := &session{win: 7, a: testAtoms()}
	o.s, o.ts, o.owned, o.value, o.marked = s, ts, true, []byte("synthetic-secret"), true
	return s
}

func TestLostIgnoresStaleClear(t *testing.T) {
	o := &Owner{}
	s := ownedSession(o, 1000)
	clearAt := func(ts xproto.Timestamp) xproto.SelectionClearEvent {
		return xproto.SelectionClearEvent{Time: ts, Owner: s.win, Selection: s.a.clipboard}
	}

	o.lost(s, clearAt(999)) // for an earlier acquisition
	o.lost(s, xproto.SelectionClearEvent{Time: 2000, Owner: s.win, Selection: xproto.AtomPrimary})
	o.lost(s, xproto.SelectionClearEvent{Time: 2000, Owner: 8, Selection: s.a.clipboard})
	o.lost(&session{win: 7, a: s.a}, clearAt(2000)) // an abandoned connection
	if !o.owned || string(o.value) != "synthetic-secret" {
		t.Fatal("a SelectionClear that does not apply dropped the copy")
	}

	value := o.value
	o.lost(s, clearAt(1000))
	if o.owned || o.value != nil || value[0] != 0 {
		t.Fatal("SelectionClear did not drop and wipe the copy")
	}

	s = ownedSession(o, 0xfffffff0)
	o.lost(s, clearAt(0x10)) // after the server clock wrapped
	if o.owned {
		t.Fatal("SelectionClear after a wrap-around was taken as stale")
	}
}

func TestSnapshotRefusesStaleRequests(t *testing.T) {
	o := &Owner{}
	s := ownedSession(o, 1000)
	request := func(ts xproto.Timestamp) xproto.SelectionRequestEvent {
		return xproto.SelectionRequestEvent{Time: ts, Owner: s.win, Selection: s.a.clipboard, Target: s.a.utf8}
	}
	for _, tc := range []struct {
		name string
		e    xproto.SelectionRequestEvent
		want bool
	}{
		{"current", request(1500), true},
		{"CurrentTime", request(xproto.TimeCurrentTime), true},
		{"at the acquisition", request(1000), true},
		{"before the acquisition", request(999), false},
		{"other selection", xproto.SelectionRequestEvent{Time: 1500, Owner: s.win, Selection: xproto.AtomPrimary}, false},
		{"other window", xproto.SelectionRequestEvent{Time: 1500, Owner: 8, Selection: s.a.clipboard}, false},
	} {
		c, ok := o.snapshot(s, tc.e)
		if ok != tc.want || ok && string(c.value) != "synthetic-secret" {
			t.Errorf("%s: snapshot = %q, %v", tc.name, c.value, ok)
		}
	}
	c, _ := o.snapshot(s, request(1500))
	clear(c.value)
	if string(o.value) != "synthetic-secret" {
		t.Fatal("snapshot shares the copy's memory")
	}
	o.owned = false
	if _, ok := o.snapshot(s, request(1500)); ok {
		t.Fatal("served after losing the clipboard")
	}
}

func TestWithin(t *testing.T) {
	release := make(chan struct{})
	finished := make(chan struct{})
	_, err := within(10*time.Millisecond, func() (int, error) {
		defer close(finished)
		<-release
		return 42, nil
	})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("within = %v, want ErrTimeout", err)
	}
	close(release)
	select {
	case <-finished: // f went on after the timeout
	case <-time.After(2 * time.Second):
		t.Fatal("f did not finish")
	}
	if v, err := within(time.Second, func() (int, error) { return 7, nil }); v != 7 || err != nil {
		t.Fatalf("within = %d, %v", v, err)
	}
}

// While the server is stalled, send still queues the requests, for the
// server to carry out later, but does not wait for them to be written.
func TestSendOnAStalledServer(t *testing.T) {
	o, s := &Owner{Timeout: time.Second}, &session{}
	sent := make(chan struct{})
	if err := o.send(s, func() { close(sent) }); err != nil {
		t.Fatalf("send = %v", err)
	}
	<-sent

	ch := make(chan struct{})
	s.overdue.Store(&ch)
	release, queued := make(chan struct{}), make(chan struct{})
	start := time.Now()
	err := o.send(s, func() {
		<-release // a request stuck behind a full socket buffer
		close(queued)
	})
	if !errors.Is(err, ErrTimeout) || time.Since(start) > 100*time.Millisecond {
		t.Fatalf("send on a stalled server = %v after %v", err, time.Since(start))
	}
	close(release)
	select {
	case <-queued:
	case <-time.After(2 * time.Second):
		t.Fatal("the requests were dropped")
	}
}

// After a timed-out wait, the server's late report of that append must not
// pass for the time of the next one.
func TestAwaitStampSkipsLateReports(t *testing.T) {
	o := &Owner{Timeout: 100 * time.Millisecond}
	s := &session{stamp: make(chan stamp, 1), done: make(chan struct{})}
	offer(s.stamp, stamp{1, 1000}) // the report of append 1, which timed out
	go func() {
		time.Sleep(20 * time.Millisecond)
		offer(s.stamp, stamp{2, 2000})
	}()
	if ts, err := o.awaitStamp(s, 2); ts != 2000 || err != nil {
		t.Fatalf("awaitStamp = %d, %v, want the report of append 2", ts, err)
	}
	offer(s.stamp, stamp{2, 2000})
	if _, err := o.awaitStamp(s, 3); !errors.Is(err, ErrTimeout) {
		t.Fatalf("awaitStamp with only earlier reports = %v, want ErrTimeout", err)
	}
	close(s.done)
	if _, err := o.awaitStamp(s, 4); !errors.Is(err, ErrClosed) {
		t.Fatalf("awaitStamp on a closed connection = %v", err)
	}
}

// While the server owes an answer, round trips fail at once: it answers in
// order, so waiting again would only add to the delay.
func TestStalledUntilAnswered(t *testing.T) {
	o, s := &Owner{Timeout: time.Second}, &session{}
	ch := make(chan struct{})
	s.overdue.Store(&ch)
	called := false
	f := func() (int, error) { called = true; return 1, nil }
	start := time.Now()
	if _, err := roundTrip(o, s, f); !errors.Is(err, ErrTimeout) || called || time.Since(start) > 100*time.Millisecond {
		t.Fatalf("roundTrip on a stalled server = %v (called %v) after %v", err, called, time.Since(start))
	}
	close(ch) // the server answered again
	if v, err := roundTrip(o, s, f); v != 1 || err != nil || !called {
		t.Fatalf("roundTrip after the stall = %d, %v", v, err)
	}
}

// The event loop hands a Read its result only after taking it out of
// session.wait, and a Read that gives up takes itself out first: whichever
// comes first, the result reaches the Read or is wiped, never both or
// neither.
func TestReadHandOff(t *testing.T) {
	s := &session{}
	newPending := func() *pending {
		p := &pending{ts: 500, result: make(chan readResult, 1)}
		s.wait.Store(p)
		return p
	}

	// The loop claims the result just as the Read gives up.
	p := newPending()
	s.deliver(p, readResult{value: []byte("synthetic-terminal-copy")})
	if r, claimed := s.giveUp(p); !claimed || string(r.value) != "synthetic-terminal-copy" {
		t.Fatalf("giveUp after deliver = %q, %v, want the delivered result", r.value, claimed)
	}

	// The Read gives up first: a late result is wiped.
	p = newPending()
	if _, claimed := s.giveUp(p); claimed {
		t.Fatal("giveUp claimed a result nobody delivered")
	}
	late := []byte("synthetic-late-copy")
	s.deliver(p, readResult{value: late})
	if !bytes.Equal(late, make([]byte, len(late))) {
		t.Fatalf("a result nobody waits for was not wiped: %q", late)
	}
	select {
	case r := <-p.result:
		t.Fatalf("a result was handed to a Read that gave up: %q", r.value)
	default:
	}

	// Giving up wipes the chunks collected, and those still to come.
	p = newPending()
	if _, done := p.add(chunkReply("synthetic-"), 64); done {
		t.Fatal("the first chunk ended the transfer")
	}
	collected := p.buf[:cap(p.buf)]
	s.giveUp(p)
	if !bytes.Equal(collected, make([]byte, len(collected))) {
		t.Fatalf("collected chunks not wiped: %q", collected)
	}
	next := chunkReply("secret")
	if r, done := p.add(next, 64); !done || r.err == nil || r.value != nil || p.buf != nil {
		t.Fatalf("a chunk after giving up = %+v, %v (buf %q)", r, done, p.buf)
	}
	if !bytes.Equal(next.Value, make([]byte, len(next.Value))) {
		t.Fatalf("a chunk after giving up was not wiped: %q", next.Value)
	}
}

func chunkReply(text string) *xproto.GetPropertyReply {
	return &xproto.GetPropertyReply{Format: 8, Type: 1, Value: []byte(text), ValueLen: uint32(len(text))}
}

// An INCR transfer (ICCCM 2.7.2) arrives in chunks and ends with an empty
// one. Each chunk is wiped once copied, and the transfer ends early, wiping
// what it collected, when the text cannot be a copy.
func TestTransferChunks(t *testing.T) {
	const limit = 16
	p := &pending{}
	var chunks []*xproto.GetPropertyReply
	for _, text := range []string{"synth", "etic-", "secret"} {
		r := chunkReply(text)
		chunks = append(chunks, r)
		if res, done := p.add(r, limit); done {
			t.Fatalf("chunk %q ended the transfer: %+v", text, res)
		}
	}
	if cap(p.buf) != limit {
		t.Fatalf("buffer capacity %d, want %d up front, so append never copies the text", cap(p.buf), limit)
	}
	for _, r := range chunks {
		if !bytes.Equal(r.Value, make([]byte, len(r.Value))) {
			t.Fatalf("chunk not wiped: %q", r.Value)
		}
	}
	res, done := p.add(chunkReply(""), limit)
	if !done || res.err != nil || string(res.value) != "synthetic-secret" {
		t.Fatalf("last chunk = %q, %v, %v", res.value, res.err, done)
	}

	for _, tc := range []struct {
		name string
		last *xproto.GetPropertyReply
		want error
	}{
		{"over the limit", chunkReply("-and-more"), ErrTooLarge},
		{"not read whole", &xproto.GetPropertyReply{Format: 8, Value: []byte("x"), BytesAfter: 4}, ErrTooLarge},
		{"not 8-bit text", &xproto.GetPropertyReply{Format: 32, Value: make([]byte, 4)}, ErrNoText},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &pending{}
			p.add(chunkReply("synthetic-"), limit)
			collected := p.buf[:cap(p.buf)]
			res, done := p.add(tc.last, limit)
			if !done || !errors.Is(res.err, tc.want) || res.value != nil || p.buf != nil {
				t.Fatalf("add = %+v, %v (buf %q), want %v", res, done, p.buf, tc.want)
			}
			if !bytes.Equal(collected, make([]byte, len(collected))) || !bytes.Equal(tc.last.Value, make([]byte, len(tc.last.Value))) {
				t.Fatalf("not wiped: collected %q, chunk %q", collected, tc.last.Value)
			}
		})
	}
}

func TestOfferKeepsTheNewestValue(t *testing.T) {
	ch := make(chan int, 1)
	offer(ch, 1) // never read, like the answer to an abandoned request
	offer(ch, 2)
	if v := <-ch; v != 2 {
		t.Fatalf("read %d, want the newest value", v)
	}
	offer(ch, 3)
	if v := <-ch; v != 3 {
		t.Fatalf("read %d", v)
	}
}

// fakeDisplay listens where Owner{Display: path} connects (xgb dials
// "/dir/x:0" as a Unix socket path) and hands each connection to serve.
func fakeDisplay(t *testing.T, serve func(net.Conn)) string {
	t.Helper()
	// No real authority file is read, so no cookie leaves the test.
	t.Setenv("XAUTHORITY", filepath.Join(t.TempDir(), "none"))
	path := filepath.Join(t.TempDir(), "x:0")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	t.Cleanup(func() {
		_ = l.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			_ = c.Close()
		}
	})
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
			serve(c)
		}
	}()
	return path
}

func TestUnresponsiveDisplayTimesOut(t *testing.T) {
	display := fakeDisplay(t, func(net.Conn) {}) // accepts, never answers
	const timeout = 300 * time.Millisecond
	o := &Owner{Display: display, Timeout: timeout}
	start := time.Now()
	if err := o.Copy([]byte("synthetic-secret"), true); !errors.Is(err, ErrTimeout) {
		t.Fatalf("Copy = %v, want ErrTimeout", err)
	}
	if elapsed := time.Since(start); elapsed < timeout || elapsed > 3*timeout {
		t.Fatalf("Copy took %v", elapsed)
	}
	// The first connect is still waiting for the server, so the next calls
	// do not wait for it again (each copy would freeze the UI).
	start = time.Now()
	if err := o.Copy([]byte("synthetic-secret"), true); !errors.Is(err, ErrTimeout) {
		t.Fatalf("second Copy = %v, want ErrTimeout", err)
	}
	if _, err := o.Read(); !errors.Is(err, ErrTimeout) {
		t.Fatalf("Read = %v, want ErrTimeout", err)
	}
	if elapsed := time.Since(start); elapsed > timeout/2 {
		t.Fatalf("calls during a pending connect took %v", elapsed)
	}
	if o.Owns() {
		t.Fatal("Owns after a failed connection")
	}
	o.Blank() // without a connection: nothing to do
	if err := o.Close(); err != nil {
		t.Fatalf("Close without a connection = %v", err)
	}
	// After Close, a call connects again.
	start = time.Now()
	if err := o.Copy([]byte("synthetic-secret"), true); !errors.Is(err, ErrTimeout) || time.Since(start) < timeout {
		t.Fatalf("Copy after Close = %v after %v, want a new connect", err, time.Since(start))
	}
}

// Once a pending connect fails, the next call tries again.
func TestPendingConnectEnds(t *testing.T) {
	release := make(chan struct{})
	display := fakeDisplay(t, func(c net.Conn) {
		go func() {
			<-release
			_ = c.Close()
		}()
	})
	o := &Owner{Display: display, Timeout: 100 * time.Millisecond}
	if err := o.Copy([]byte("synthetic-secret"), true); !errors.Is(err, ErrTimeout) {
		t.Fatalf("Copy = %v, want ErrTimeout", err)
	}
	close(release) // the server hangs up
	deadline := time.Now().Add(3 * time.Second)
	for {
		err := o.Copy([]byte("synthetic-secret"), true)
		if err != nil && !errors.Is(err, ErrTimeout) {
			break // a new connect, refused at once
		}
		if err == nil || time.Now().After(deadline) {
			t.Fatalf("Copy = %v, want the error of a new connect", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRefusedConnectionFailsFast(t *testing.T) {
	display := fakeDisplay(t, func(c net.Conn) { _ = c.Close() })
	o := &Owner{Display: display}
	start := time.Now()
	err := o.Copy([]byte("synthetic-secret"), true)
	if err == nil || errors.Is(err, ErrTimeout) || time.Since(start) > time.Second {
		t.Fatalf("Copy = %v after %v", err, time.Since(start))
	}
}
