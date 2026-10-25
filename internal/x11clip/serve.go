package x11clip

import (
	"bytes"
	"unicode/utf8"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// hintValue is the x-kde-passwordManagerHint value that Klipper, CopyQ and
// GPaste skip.
const hintValue = "secret"

// maxPairs bounds a MULTIPLE request; clients send a handful of targets.
const maxPairs = 64

// content is a snapshot of the copy, so that no X request runs under mu.
type content struct {
	value  []byte
	marked bool
	ts     xproto.Timestamp
}

// property is what a conversion writes to the requestor's window. data is
// always a fresh slice, which the writer wipes.
type property struct {
	typ    xproto.Atom
	format byte
	data   []byte
}

// snapshot copies what answering e needs, or reports false to refuse it.
func (o *Owner) snapshot(s *session, e xproto.SelectionRequestEvent) (content, bool) {
	if e.Selection != s.a.clipboard || e.Owner != s.win {
		return content{}, false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.s != s || !o.owned {
		return content{}, false
	}
	// ICCCM 2.2: refuse requests timed before the acquisition.
	if e.Time != xproto.TimeCurrentTime && before(e.Time, o.ts) {
		return content{}, false
	}
	return content{value: bytes.Clone(o.value), marked: o.marked, ts: o.ts}, true
}

// serve answers a SelectionRequest (ICCCM 2.2). A refused request gets a
// SelectionNotify with property None.
func (o *Owner) serve(s *session, e xproto.SelectionRequestEvent) {
	prop := e.Property
	if prop == xproto.AtomNone {
		prop = e.Target // obsolete requestors (ICCCM 2.2)
	}
	served := false
	if c, ok := o.snapshot(s, e); ok {
		if e.Target == s.a.multiple {
			// MULTIPLE names its (target, property) pairs in the property.
			served = e.Property != xproto.AtomNone && o.serveMultiple(s, e.Requestor, prop, c)
		} else {
			served = put(s, e.Requestor, prop, e.Target, c)
		}
		clear(c.value)
	}
	reply := xproto.SelectionNotifyEvent{
		Time: e.Time, Requestor: e.Requestor, Selection: e.Selection, Target: e.Target,
		Property: xproto.AtomNone,
	}
	if served {
		reply.Property = prop
	}
	xproto.SendEvent(s.c, false, e.Requestor, xproto.EventMaskNoEvent, string(reply.Bytes()))
}

// put writes target's conversion to prop on the requestor's window.
func put(s *session, win xproto.Window, prop, target xproto.Atom, c content) bool {
	p, ok := s.a.convert(target, c)
	if !ok {
		return false
	}
	defer clear(p.data)
	n := len(p.data) / int(p.format/8)
	xproto.ChangeProperty(s.c, xproto.PropModeReplace, win, prop, p.typ, p.format, uint32(n), p.data)
	return true
}

// convert returns the property for target, or false to refuse the target.
func (a *atoms) convert(target xproto.Atom, c content) (property, bool) {
	switch target {
	case a.targets:
		list := []xproto.Atom{a.targets, a.timestamp, a.multiple, a.utf8, a.text, a.textPlain}
		if isLatin1(c.value) {
			list = append(list, xproto.AtomString)
		}
		if c.marked {
			list = append(list, a.hint)
		}
		vals := make([]uint32, len(list))
		for i, atom := range list {
			vals[i] = uint32(atom)
		}
		return property{xproto.AtomAtom, 32, card32s(vals...)}, true
	case a.timestamp:
		return property{xproto.AtomInteger, 32, card32s(uint32(c.ts))}, true
	case a.utf8, a.text:
		// TEXT leaves the encoding to the owner.
		return property{a.utf8, 8, bytes.Clone(c.value)}, true
	case a.textPlain:
		return property{a.textPlain, 8, bytes.Clone(c.value)}, true
	case xproto.AtomString:
		if l, ok := latin1(c.value); ok {
			return property{xproto.AtomString, 8, l}, true
		}
	case a.hint:
		if c.marked {
			return property{a.hint, 8, []byte(hintValue)}, true
		}
	}
	return property{}, false
}

// serveMultiple answers MULTIPLE (ICCCM 2.6.2): it converts each pair in
// prop, then writes the list back with the property of each refused pair
// replaced by None.
func (o *Owner) serveMultiple(s *session, win xproto.Window, prop xproto.Atom, c content) bool {
	r, err := roundTrip(o, s, func() (*xproto.GetPropertyReply, error) {
		return xproto.GetProperty(s.c, false, win, prop, s.a.atomPair, 0, 2*maxPairs).Reply()
	})
	if err != nil || r == nil || r.Type != s.a.atomPair || r.Format != 32 || r.BytesAfter != 0 {
		return false
	}
	pairs, ok := convertPairs(r.Value, s.a.multiple, func(target, p xproto.Atom) bool {
		return put(s, win, p, target, c)
	})
	if !ok {
		return false
	}
	xproto.ChangeProperty(s.c, xproto.PropModeReplace, win, prop, s.a.atomPair, 32, uint32(len(pairs)/4), pairs)
	return true
}

// convertPairs runs conv for each (target, property) pair and returns the
// list with refused pairs' properties set to None. Nested MULTIPLE is
// refused.
func convertPairs(pairs []byte, multiple xproto.Atom, conv func(target, prop xproto.Atom) bool) ([]byte, bool) {
	if len(pairs) == 0 || len(pairs)%8 != 0 {
		return nil, false
	}
	out := bytes.Clone(pairs)
	for i := 0; i < len(out); i += 8 {
		target := xproto.Atom(xgb.Get32(out[i:]))
		prop := xproto.Atom(xgb.Get32(out[i+4:]))
		if target == multiple || prop == xproto.AtomNone || !conv(target, prop) {
			xgb.Put32(out[i+4:], uint32(xproto.AtomNone))
		}
	}
	return out, true
}

// card32s encodes format-32 property data in the connection's byte order
// (xgb connects little-endian).
func card32s(vals ...uint32) []byte {
	b := make([]byte, 4*len(vals))
	for i, v := range vals {
		xgb.Put32(b[4*i:], v)
	}
	return b
}

// isLatin1 reports whether value is UTF-8 text that STRING, which is
// ISO 8859-1, can represent.
func isLatin1(value []byte) bool {
	for len(value) > 0 {
		r, n := utf8.DecodeRune(value)
		if r > 0xff || r == utf8.RuneError && n == 1 {
			return false
		}
		value = value[n:]
	}
	return true
}

// latin1 converts UTF-8 text to ISO 8859-1, or reports false.
func latin1(value []byte) ([]byte, bool) {
	if !isLatin1(value) {
		return nil, false
	}
	out := make([]byte, 0, len(value))
	for len(value) > 0 { // never via string(value), which cannot be wiped
		r, n := utf8.DecodeRune(value)
		out = append(out, byte(r))
		value = value[n:]
	}
	return out, true
}
