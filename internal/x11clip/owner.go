// Package x11clip owns the X11 CLIPBOARD selection in-process. Unlike xclip
// and xsel, it can offer x-kde-passwordManagerHint=secret, which tells
// clipboard managers to skip a copy, and it can blank a copy while keeping
// the clipboard, so managers that refill an ownerless clipboard from their
// history have nothing to restore.
//
// It speaks the X protocol through github.com/jezek/xgb, without cgo. Values
// must fit in one ChangeProperty request: xgb has no BIG-REQUESTS support and
// Copy does not serve INCR transfers, so it refuses larger values with
// ErrTooLarge. Read accepts INCR transfers (ICCCM 2.7.2), as other clients
// send large text, up to the same size; it drains longer ones to their end
// and discards them.
package x11clip

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// xgb logs to stderr by default, which would corrupt a terminal UI.
func init() { xgb.Logger = log.New(io.Discard, "", 0) }

var (
	// ErrTimeout reports that the X server, or for Read the clipboard's
	// owner, did not answer in time. The connection is kept: the server
	// carries out the requests it was sent once it answers again (see
	// send), while closing it would leave the clipboard without an owner.
	// Until the server answers, calls return ErrTimeout at once; that
	// includes a connect that timed out, which goes on in the background.
	ErrTimeout = errors.New("x11clip: X server did not answer in time")
	// ErrNotOwner reports that the server did not confirm the ownership.
	ErrNotOwner = errors.New("x11clip: clipboard ownership not confirmed")
	// ErrClosed reports a connection that closed during the call.
	ErrClosed = errors.New("x11clip: connection closed")
	// ErrTooLarge reports a value that does not fit in one X request: Copy
	// cannot serve it, and Read discards it, as no copy can match it.
	ErrTooLarge = errors.New("x11clip: value too large for one X request")
	// ErrNoText reports a clipboard without an owner or without text.
	ErrNoText = errors.New("x11clip: clipboard has no text")
)

// DefaultTimeout bounds connecting and each round trip.
const DefaultTimeout = 2 * time.Second

// Owner holds at most one copy on CLIPBOARD. Its methods are safe for
// concurrent use. The zero value connects to $DISPLAY on first use.
type Owner struct {
	Display   string        // X display; "" uses $DISPLAY
	Authority string        // X authority file; "" uses $XAUTHORITY, else ~/.Xauthority
	Name      string        // owner window WM_NAME, _NET_WM_NAME and WM_CLASS
	Timeout   time.Duration // bounds connecting and each round trip; 0 means DefaultTimeout

	opMu sync.Mutex // serializes the exported methods

	mu         sync.Mutex // guards the fields below; never held across X requests
	s          *session
	connecting bool             // a connect that timed out goes on; see session
	epoch      uint64           // counts Close calls, so a late connect after one is closed
	value      []byte           // the text served; empty after Blank
	marked     bool             // offer the password manager hint
	ts         xproto.Timestamp // acquisition time of the copy
	owned      bool             // no SelectionClear since the acquisition
	broken     bool             // the connection broke while it held the copy
}

// session is one display connection.
type session struct {
	c     *xgb.Conn
	win   xproto.Window
	a     atoms
	limit int           // largest value one ChangeProperty can carry
	stamp chan stamp    // PropertyNotify times, for serverTime
	done  chan struct{} // closed when the event loop ends

	asked   uint64                        // times serverTime asked for; guarded by Owner.opMu
	told    uint64                        // times the server reported; used only by the event loop
	wait    atomic.Pointer[pending]       // the Read waiting for its conversion, if any
	xfer    *pending                      // the Read whose INCR transfer is under way; used only by the event loop
	overdue atomic.Pointer[chan struct{}] // closed once the server answers again; see stalled
}

// pending is a Read waiting for the conversion it requested at ts. The
// event loop reads the answer from the window and hands the Read exactly one
// result, only after taking it out of session.wait; a Read that gives up
// takes itself out first. So no result is lost or left unwiped, whichever
// comes first.
type pending struct {
	ts     xproto.Timestamp
	result chan readResult // buffered, so the event loop never blocks on it

	mu   sync.Mutex
	buf  []byte // the INCR chunks so far: the event loop appends, giveUp wipes
	gone bool   // Read gave up: chunks read from now on are wiped, not kept
}

type readResult struct {
	value []byte
	err   error
}

// stamp is the server time reported for the n-th zero-length append.
type stamp struct {
	n  uint64
	ts xproto.Timestamp
}

// stalled reports whether the server has yet to answer since a wait for it
// timed out. The server answers a client's requests in order, so a new
// request cannot be answered sooner: waits fail at once instead of adding
// to the delay, which keeps a stalled server from stretching keetui's exit.
func (s *session) stalled() bool {
	ch := s.overdue.Load()
	if ch == nil {
		return false
	}
	select {
	case <-*ch:
		return false
	default:
		return true
	}
}

// timedOut records that a wait for the server timed out. A round trip sent
// after the request that timed out tells when the server answers again.
func (s *session) timedOut() {
	old := s.overdue.Load()
	if s.stalled() {
		return
	}
	ch := make(chan struct{})
	if !s.overdue.CompareAndSwap(old, &ch) {
		return
	}
	go func() {
		defer close(ch)
		_, _ = xproto.GetInputFocus(s.c).Reply() // io.EOF once the connection closes
	}()
}

// roundTrip runs f, a request and the wait for its reply, within the
// timeout, failing at once while the server is stalled.
func roundTrip[T any](o *Owner, s *session, f func() (T, error)) (T, error) {
	if s.stalled() {
		var zero T
		return zero, ErrTimeout
	}
	v, err := within(o.timeout(), f)
	if errors.Is(err, ErrTimeout) {
		s.timedOut()
	}
	return v, err
}

// send runs f, which sends requests that have no reply. xgb returns only
// once a request is written, and a server that stops reading lets the
// socket buffer fill up (for example while keetui serves a large value), so
// send stops waiting after the timeout, or at once while the server is
// stalled. f then goes on in a goroutine of its own, and its requests reach
// the server when it reads again, but in no guaranteed order with those of
// other calls. Callers send only requests that tolerate this:
// SetSelectionOwner with the acquisition time, which has the same effect in
// any order; DeleteProperty of the read property, which at worst deletes an
// answer a later Read then misses, leaving no text behind either way; and
// the zero-length append and ConvertSelection, whose answers are matched by
// count and by timestamp.
func (o *Owner) send(s *session, f func()) error {
	if s.stalled() {
		go f()
		return ErrTimeout
	}
	_, err := within(o.timeout(), func() (struct{}, error) {
		f()
		return struct{}{}, nil
	})
	if errors.Is(err, ErrTimeout) {
		s.timedOut()
	}
	return err
}

type atoms struct {
	clipboard, targets, timestamp, multiple, atomPair,
	utf8, text, textPlain, hint, incr, netWMName, stamp, read xproto.Atom
}

func (a *atoms) table() []struct {
	dst  *xproto.Atom
	name string
} {
	return []struct {
		dst  *xproto.Atom
		name string
	}{
		{&a.clipboard, "CLIPBOARD"},
		{&a.targets, "TARGETS"},
		{&a.timestamp, "TIMESTAMP"},
		{&a.multiple, "MULTIPLE"},
		{&a.atomPair, "ATOM_PAIR"},
		{&a.utf8, "UTF8_STRING"},
		{&a.text, "TEXT"},
		{&a.textPlain, "text/plain;charset=utf-8"},
		{&a.hint, "x-kde-passwordManagerHint"},
		{&a.incr, "INCR"},
		{&a.netWMName, "_NET_WM_NAME"},
		{&a.stamp, "_X11CLIP_TIMESTAMP"},
		{&a.read, "_X11CLIP_SELECTION"},
	}
}

func (o *Owner) timeout() time.Duration {
	if o.Timeout > 0 {
		return o.Timeout
	}
	return DefaultTimeout
}

// within runs f and gives up after d; f keeps running after a timeout.
func within[T any](d time.Duration, f func() (T, error)) (T, error) {
	type result struct {
		v   T
		err error
	}
	ch := make(chan result, 1)
	go func() {
		v, err := f()
		ch <- result{v, err}
	}()
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case r := <-ch:
		return r.v, r.err
	case <-t.C:
		var zero T
		return zero, ErrTimeout
	}
}

// session returns the connection, opening one if there is none. A connect
// that times out goes on in the background: until it ends, calls fail at
// once rather than each wait again for a server that has yet to answer the
// first, and a connection it opens serves the next call.
func (o *Owner) session() (*session, error) {
	o.mu.Lock()
	s, connecting, epoch := o.s, o.connecting, o.epoch
	o.mu.Unlock()
	switch {
	case s != nil:
		return s, nil
	case connecting:
		return nil, ErrTimeout
	}
	type result struct {
		s   *session
		err error
	}
	ch := make(chan result, 1)
	go func() {
		s, err := open(o.Display, o.Authority, o.Name)
		ch <- result{s, err}
	}()
	t := time.NewTimer(o.timeout())
	defer t.Stop()
	select {
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}
		o.mu.Lock()
		o.s = r.s
		o.mu.Unlock()
		go o.loop(r.s)
		return r.s, nil
	case <-t.C:
	}
	o.mu.Lock()
	o.connecting = true
	o.mu.Unlock()
	go func() {
		r := <-ch
		o.mu.Lock()
		current := o.epoch == epoch
		adopt := r.err == nil && current && o.s == nil
		if adopt {
			o.s = r.s
		}
		if current {
			o.connecting = false
		}
		o.mu.Unlock()
		switch {
		case adopt:
			o.loop(r.s)
		case r.err == nil:
			r.s.c.Close() // Close ran meanwhile
		}
	}()
	return nil, ErrTimeout
}

func open(display, authority, name string) (*session, error) {
	c, err := connect(display, authority)
	if err != nil {
		return nil, fmt.Errorf("x11clip: %w", err)
	}
	s, err := setUp(c, name)
	if err != nil {
		c.Close()
		return nil, err
	}
	return s, nil
}

// setUp interns the atoms and creates the owner window: unmapped,
// InputOnly and override-redirect, so it never reaches a window manager.
func setUp(c *xgb.Conn, name string) (*session, error) {
	setup := xproto.Setup(c)
	if len(setup.Roots) == 0 {
		return nil, errors.New("x11clip: display has no screens")
	}
	root := setup.Roots[0].Root
	if n := c.DefaultScreen; n > 0 && n < len(setup.Roots) {
		root = setup.Roots[n].Root
	}

	s := &session{
		c: c,
		// ChangeProperty has a 24-byte header, and xgb encodes only the
		// 16-bit request length.
		limit: max(int(setup.MaximumRequestLength)*4-24, 0),
		stamp: make(chan stamp, 1),
		done:  make(chan struct{}),
	}
	table := s.a.table()
	cookies := make([]xproto.InternAtomCookie, len(table))
	for i, t := range table {
		cookies[i] = xproto.InternAtom(c, false, uint16(len(t.name)), t.name)
	}
	for i, ck := range cookies {
		r, err := ck.Reply()
		if err != nil {
			return nil, fmt.Errorf("x11clip: intern %s: %w", table[i].name, err)
		}
		if r == nil {
			return nil, ErrClosed
		}
		*table[i].dst = r.Atom
	}

	win, err := xproto.NewWindowId(c)
	if err != nil {
		return nil, fmt.Errorf("x11clip: window id: %w", err)
	}
	if err := xproto.CreateWindowChecked(c, 0, win, root, -10, -10, 1, 1, 0,
		xproto.WindowClassInputOnly, 0,
		xproto.CwOverrideRedirect|xproto.CwEventMask,
		[]uint32{1, xproto.EventMaskPropertyChange}).Check(); err != nil {
		return nil, fmt.Errorf("x11clip: create window: %w", err)
	}
	s.win = win
	if name != "" {
		// Clipboard managers that filter by window name or class
		// (clipmenu, greenclip) can recognize the owner.
		class := name + "\x00" + name + "\x00"
		xproto.ChangeProperty(c, xproto.PropModeReplace, win, xproto.AtomWmName, xproto.AtomString, 8, uint32(len(name)), []byte(name))
		xproto.ChangeProperty(c, xproto.PropModeReplace, win, s.a.netWMName, s.a.utf8, 8, uint32(len(name)), []byte(name))
		xproto.ChangeProperty(c, xproto.PropModeReplace, win, xproto.AtomWmClass, xproto.AtomString, 8, uint32(len(class)), []byte(class))
	}
	return s, nil
}

// forgetLocked wipes the copy. o.mu must be held.
func (o *Owner) forgetLocked() {
	clear(o.value)
	o.value, o.marked = nil, false
}

// drop abandons a connection whose state is unknown; the next call
// reconnects. Closing waits for the server, so it runs in the background.
func (o *Owner) drop(s *session) {
	o.mu.Lock()
	if o.s == s {
		o.broken = o.broken || o.owned
		o.s, o.owned = nil, false
		o.forgetLocked()
	}
	o.mu.Unlock()
	go s.c.Close()
}

// fail drops the connection if it broke. A server that is only slow keeps
// it, because closing it would leave the clipboard without an owner, and
// managers that ignore the marker fill an ownerless clipboard from their
// history, possibly with the secret.
func (o *Owner) fail(s *session, err error) error {
	var xerr xgb.Error
	if !errors.Is(err, ErrTimeout) && !errors.Is(err, ErrNotOwner) && !errors.As(err, &xerr) {
		o.drop(s)
	}
	return err
}

// serverTime returns the server's current time, which selection requests
// must carry instead of CurrentTime (ICCCM 2.1): a zero-length append to a
// property on the owner window reports it in a PropertyNotify.
func (o *Owner) serverTime(s *session) (xproto.Timestamp, error) {
	if s.stalled() {
		return 0, ErrTimeout
	}
	s.asked++
	n := s.asked
	if err := o.send(s, func() {
		xproto.ChangeProperty(s.c, xproto.PropModeAppend, s.win, s.a.stamp, xproto.AtomInteger, 8, 0, nil)
	}); err != nil {
		return 0, err
	}
	ts, err := o.awaitStamp(s, n)
	if errors.Is(err, ErrTimeout) {
		s.timedOut()
	}
	return ts, err
}

// awaitStamp waits for the time of the n-th append. The server reports the
// appends in order, so the late report of an earlier one, whose wait timed
// out, is skipped.
func (o *Owner) awaitStamp(s *session, n uint64) (xproto.Timestamp, error) {
	t := time.NewTimer(o.timeout())
	defer t.Stop()
	for {
		select {
		case st := <-s.stamp:
			if st.n >= n {
				return st.ts, nil
			}
		case <-s.done:
			return 0, ErrClosed
		case <-t.C:
			return 0, ErrTimeout
		}
	}
}

// selectionOwner asks the server who owns CLIPBOARD, after any requests
// sent before it.
func (o *Owner) selectionOwner(s *session) (xproto.Window, error) {
	r, err := roundTrip(o, s, func() (*xproto.GetSelectionOwnerReply, error) {
		return xproto.GetSelectionOwner(s.c, s.a.clipboard).Reply()
	})
	switch {
	case err != nil:
		return 0, err
	case r == nil:
		return 0, ErrClosed
	}
	return r.Owner, nil
}

// Copy takes CLIPBOARD with value, replacing any earlier copy. marked offers
// x-kde-passwordManagerHint=secret. Copy returns nil only once the server
// has confirmed the ownership. When Copy fails, the earlier copy is blanked
// (see Blank), so it cannot outlast a value the caller then copies elsewhere.
func (o *Owner) Copy(value []byte, marked bool) error {
	o.opMu.Lock()
	defer o.opMu.Unlock()
	o.mu.Lock()
	o.broken = false
	o.mu.Unlock()
	s, err := o.session()
	if err != nil {
		return err // without a connection there is no earlier copy either
	}
	if len(value) > s.limit {
		o.blank(s)
		return ErrTooLarge
	}
	ts, err := o.serverTime(s)
	if err != nil {
		o.blank(s)
		return o.fail(s, err)
	}
	o.mu.Lock()
	o.forgetLocked()
	o.value, o.marked, o.ts, o.owned = bytes.Clone(value), marked, ts, true
	o.mu.Unlock()
	var owner xproto.Window
	err = o.send(s, func() { xproto.SetSelectionOwner(s.c, s.win, s.a.clipboard, ts) })
	if err == nil {
		owner, err = o.selectionOwner(s)
	}
	if err == nil && owner == s.win {
		return nil
	}
	if err == nil {
		err = ErrNotOwner // another client holds the clipboard
	}
	o.mu.Lock()
	if o.s == s && o.ts == ts {
		o.forgetLocked()
		// Without an answer the server may still make keetui the owner:
		// the copy stays, blank, for Owns, Blank and Close to settle.
		o.owned = errors.Is(err, ErrTimeout)
	}
	o.mu.Unlock()
	return o.fail(s, err)
}

// Owns reports whether CLIPBOARD still holds the last copy, live or blanked.
// The server is asked, so a SelectionClear still in flight cannot mislead
// it. If the server does not answer in time, Owns reports true: keetui may
// still hold the copy, and a caller that then blanks it is right either way.
func (o *Owner) Owns() bool {
	o.opMu.Lock()
	defer o.opMu.Unlock()
	o.mu.Lock()
	s, owned := o.s, o.owned
	o.mu.Unlock()
	if s == nil || !owned {
		return false
	}
	owner, err := o.selectionOwner(s)
	if err != nil {
		_ = o.fail(s, err)
		return true
	}
	return owner == s.win
}

// Lost reports that the connection holding the last copy broke while the
// copy was on CLIPBOARD, live or blanked: the X server went away, or dropped
// keetui's connection, and the copy with it. A server that comes back has no
// copy of keetui's; Owns then reports false without reconnecting.
func (o *Owner) Lost() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.broken
}

// Blank keeps CLIPBOARD but replaces the copy with empty, unmarked text.
// Giving the clipboard up instead would let managers that ignore the marker
// put their newest entry, possibly the secret, straight back. Blank does
// nothing once another client has taken the clipboard. It waits for no
// answer: the copy is wiped at once, and a server that is slow carries out
// the rest when it answers again.
func (o *Owner) Blank() {
	o.opMu.Lock()
	defer o.opMu.Unlock()
	o.mu.Lock()
	s := o.s
	o.mu.Unlock()
	if s != nil {
		o.blank(s)
	}
}

// blank wipes the copy on s and, if keetui may still hold CLIPBOARD,
// reasserts the ownership, which makes clipboard managers (XFixes clients)
// read the empty text. The acquisition time, never a newer one, leaves a
// copy another client made since in place.
func (o *Owner) blank(s *session) {
	o.mu.Lock()
	ts, owned := o.ts, o.s == s && o.owned
	if owned {
		o.forgetLocked()
	}
	o.mu.Unlock()
	if owned {
		_ = o.send(s, func() { xproto.SetSelectionOwner(s.c, s.win, s.a.clipboard, ts) })
	}
}

// Read returns the text on CLIPBOARD, as UTF8_STRING, from whichever client
// owns it. It checks whether another client's copy has the same value, so
// text longer than any copy (one X request) is drained and discarded, and
// Read reports ErrTooLarge. The event loop reads the answer (see converted),
// and deletes whatever arrives once Read has given up, so the clipboard's
// text is never left on keetui's window, where any client could read it.
func (o *Owner) Read() ([]byte, error) {
	o.opMu.Lock()
	defer o.opMu.Unlock()
	s, err := o.session()
	if err != nil {
		return nil, err
	}
	ts, err := o.serverTime(s)
	if err != nil {
		return nil, o.fail(s, err)
	}
	p := &pending{ts: ts, result: make(chan readResult, 1)}
	s.wait.Store(p)
	err = o.send(s, func() {
		xproto.ConvertSelection(s.c, s.win, s.a.clipboard, s.a.utf8, s.a.read, ts)
	})
	if err == nil {
		t := time.NewTimer(o.timeout())
		defer t.Stop()
		select {
		case r := <-p.result:
			return r.value, r.err
		case <-s.done:
			if r, claimed := s.giveUp(p); claimed {
				return r.value, r.err
			}
			return nil, o.fail(s, ErrClosed)
		case <-t.C:
			// The owner, not necessarily the server, failed to answer.
			err = ErrTimeout
		}
	}
	if r, claimed := s.giveUp(p); claimed {
		return r.value, r.err
	}
	// The owner may still answer. The event loop deletes what it writes
	// from now on; this deletes what it wrote already, which the loop left
	// for Read.
	o.deleteRead(s)
	return nil, err
}

// giveUp ends p's wait and wipes the chunks it collected. If the event loop
// has claimed p's result meanwhile, giveUp returns that result instead.
func (s *session) giveUp(p *pending) (readResult, bool) {
	if !s.wait.CompareAndSwap(p, nil) {
		return <-p.result, true // the event loop sends it right after claiming it
	}
	p.mu.Lock()
	clear(p.buf)
	p.buf, p.gone = nil, true
	p.mu.Unlock()
	return readResult{}, false
}

// deliver hands r to p's Read if it still waits, and wipes r otherwise.
func (s *session) deliver(p *pending, r readResult) {
	if s.wait.CompareAndSwap(p, nil) {
		p.result <- r
		return
	}
	clear(r.value)
}

// converted handles the SelectionNotify that answers a Read's
// ConvertSelection; other clients can send this event too. A late answer,
// for a Read that gave up, needs nothing here: its text, written before the
// event was sent, was deleted by written or by Read.
func (o *Owner) converted(s *session, e xproto.SelectionNotifyEvent) {
	p := s.wait.Load()
	if s.xfer != nil && s.xfer != p {
		s.xfer = nil // its Read gave up and wiped the chunks
	}
	if p == nil || s.xfer == p || e.Selection != s.a.clipboard || e.Target != s.a.utf8 ||
		e.Time != p.ts && e.Time != xproto.TimeCurrentTime {
		return // a late or duplicate answer, or not an answer
	}
	if e.Property != s.a.read {
		// A refusal (None), or a property Read did not name, which is left
		// alone: any client can send this event, naming any property.
		s.deliver(p, readResult{err: ErrNoText})
		o.deleteRead(s) // any text that arrived meanwhile is nobody's
		return
	}
	r, err := o.getRead(s)
	switch {
	case err != nil:
		o.deleteRead(s)
		s.deliver(p, readResult{err: err})
	case r.Type == xproto.AtomNone:
		// Nothing was written (a forged or duplicate event): keep waiting.
	case r.Type == s.a.incr:
		// Reading deleted the marker, which asks the owner for the first
		// chunk. The marker holds a lower bound on the size.
		if r.Format == 32 && len(r.Value) >= 4 && int64(xgb.Get32(r.Value)) > int64(s.limit) {
			s.deliver(p, readResult{err: ErrTooLarge}) // written drains the chunks
			return
		}
		s.xfer = p
	case r.BytesAfter != 0:
		clear(r.Value)
		o.deleteRead(s) // GetProperty deletes only a value it read whole
		s.deliver(p, readResult{err: ErrTooLarge})
	case r.Format != 8:
		clear(r.Value)
		s.deliver(p, readResult{err: ErrNoText})
	default:
		s.deliver(p, readResult{value: r.Value})
	}
}

// written handles a new value on the read property: a chunk of the
// transfer under way, the answer a Read is about to read, or text nobody
// waits for, which it deletes. Deleting also asks an INCR owner for the next
// chunk (ICCCM 2.7.2), so a transfer nobody reads, such as one that began
// after its Read gave up, still runs to its empty last chunk: the owner
// finishes, and nothing is left on the window.
func (o *Owner) written(s *session) {
	if p := s.xfer; p != nil {
		if s.wait.Load() == p {
			o.chunk(s, p)
			return
		}
		s.xfer = nil // its Read gave up and wiped the chunks
	}
	if s.wait.Load() != nil {
		return // the answer to the Read waiting; converted reads it
	}
	o.deleteRead(s)
}

// chunk reads, and deletes, the next chunk of p's transfer.
func (o *Owner) chunk(s *session, p *pending) {
	r, err := o.getRead(s)
	switch {
	case err != nil:
		o.deleteRead(s)
		s.xfer = nil
		s.giveUpChunks(p, readResult{err: err})
		return
	case r.Type == xproto.AtomNone:
		return // nothing to read (a forged event): wait for the next chunk
	case r.BytesAfter != 0:
		o.deleteRead(s)
	}
	res, done := p.add(r, s.limit)
	if done {
		s.xfer = nil // on failure written drains the rest
		s.deliver(p, res)
	}
}

// giveUpChunks ends p's transfer with r, wiping the chunks collected.
func (s *session) giveUpChunks(p *pending, r readResult) {
	p.mu.Lock()
	clear(p.buf)
	p.buf = nil
	p.mu.Unlock()
	s.deliver(p, r)
}

// add appends a chunk, read whole or not (BytesAfter), to p's transfer and
// wipes it. At the empty last chunk it returns the text. It ends the
// transfer early, wiping the chunks, once Read has given up or at a chunk
// that cannot belong to a copy: not 8-bit text, or more than limit bytes in
// all.
func (p *pending) add(r *xproto.GetPropertyReply, limit int) (readResult, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	defer clear(r.Value)
	var err error
	switch {
	case p.gone:
		return readResult{err: ErrTimeout}, true // nobody to hand the text to
	case len(r.Value) == 0 && r.BytesAfter == 0:
		v := p.buf
		p.buf = nil
		return readResult{value: v}, true
	case r.BytesAfter != 0 || len(p.buf)+len(r.Value) > limit:
		err = ErrTooLarge
	case r.Format != 8:
		err = ErrNoText
	case p.buf == nil:
		// The full capacity up front: append never leaves a copy behind.
		p.buf = make([]byte, 0, limit)
		fallthrough
	default:
		p.buf = append(p.buf, r.Value...)
		return readResult{}, false
	}
	clear(p.buf)
	p.buf = nil
	return readResult{err: err}, true
}

// getRead reads and deletes the read property. It reads at most one
// request's worth (s.limit); GetProperty leaves a longer value in place and
// reports the rest in BytesAfter.
func (o *Owner) getRead(s *session) (*xproto.GetPropertyReply, error) {
	r, err := roundTrip(o, s, func() (*xproto.GetPropertyReply, error) {
		return xproto.GetProperty(s.c, true, s.win, s.a.read, xproto.GetPropertyTypeAny, 0, uint32(s.limit/4+1)).Reply()
	})
	if err == nil && r == nil {
		err = ErrClosed
	}
	return r, err
}

// deleteRead deletes the read property, wiping any text on it.
func (o *Owner) deleteRead(s *session) {
	_ = o.send(s, func() { xproto.DeleteProperty(s.c, s.win, s.a.read) })
}

// Close gives up CLIPBOARD if it still holds the last copy, and closes the
// connection. A later call reconnects. On a stalled server Close returns
// ErrTimeout at once, and the connection closes when the server answers.
func (o *Owner) Close() error {
	o.opMu.Lock()
	defer o.opMu.Unlock()
	o.mu.Lock()
	s, ts, owned := o.s, o.ts, o.owned
	o.s, o.owned, o.connecting, o.broken = nil, false, false, false
	o.epoch++
	o.forgetLocked()
	o.mu.Unlock()
	if s == nil {
		return nil
	}
	shut := func() {
		if owned {
			// The acquisition time leaves a newer owner in place.
			xproto.SetSelectionOwner(s.c, xproto.WindowNone, s.a.clipboard, ts)
		}
		// Close only queues the end of the connection: xgb then waits for
		// the server to answer what was sent before, and closes it, which
		// ends the event loop.
		s.c.Close()
		<-s.done
	}
	if s.stalled() {
		go shut() // finishes when the server answers, if the process lives on
		return ErrTimeout
	}
	_, err := within(o.timeout(), func() (struct{}, error) {
		shut()
		return struct{}{}, nil
	})
	return err
}

// before compares server timestamps, which wrap around after 49.7 days.
func before(a, b xproto.Timestamp) bool { return int32(a-b) < 0 }

// loop handles the session's events until the connection closes.
func (o *Owner) loop(s *session) {
	defer close(s.done)
	for {
		ev, xerr := s.c.WaitForEvent()
		if ev == nil && xerr == nil {
			o.disconnected(s)
			return
		}
		if xerr != nil {
			continue // for example BadWindow from a requestor that went away
		}
		switch e := ev.(type) {
		case xproto.PropertyNotifyEvent:
			if e.Window != s.win || e.State != xproto.PropertyNewValue {
				break
			}
			switch e.Atom {
			case s.a.stamp:
				s.told++
				offer(s.stamp, stamp{s.told, e.Time})
			case s.a.read:
				o.written(s)
			}
		case xproto.SelectionRequestEvent:
			o.serve(s, e)
		case xproto.SelectionClearEvent:
			o.lost(s, e)
		case xproto.SelectionNotifyEvent:
			if e.Requestor == s.win {
				o.converted(s, e)
			}
		}
	}
}

// offer hands v to a reader, replacing a value nobody read. The event loop
// is the only sender, so the loop ends.
func offer[T any](ch chan T, v T) {
	for {
		select {
		case ch <- v:
			return
		default:
		}
		select {
		case <-ch:
		default:
		}
	}
}

// disconnected forgets a connection that closed, by Close or on its own.
func (o *Owner) disconnected(s *session) {
	o.mu.Lock()
	if o.s == s {
		o.broken = o.broken || o.owned
		o.s, o.owned = nil, false
		o.forgetLocked()
	}
	o.mu.Unlock()
	s.c.Close() // releases xgb's goroutines; safe on a closed connection
}

// lost handles SelectionClear: another client took CLIPBOARD.
func (o *Owner) lost(s *session, e xproto.SelectionClearEvent) {
	if e.Selection != s.a.clipboard || e.Owner != s.win {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	// The clear for an earlier acquisition can arrive after a new Copy; it
	// must not drop the newer copy.
	if o.s == s && o.owned && !before(e.Time, o.ts) {
		o.owned = false
		o.forgetLocked()
	}
}
