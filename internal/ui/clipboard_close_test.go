package ui

import (
	"testing"
	"time"

	"github.com/rmarchei/keetui/internal/config"
)

// closingNative is a backend that hands the clipboard over at exit.
type closingNative struct {
	fakeNative
	events []string
	block  chan struct{} // close waits on it when set
}

func (c *closingNative) clear() error {
	c.events = append(c.events, "clear")
	return c.fakeNative.clear()
}

func (c *closingNative) close() {
	c.events = append(c.events, "close")
	if c.block != nil {
		<-c.block
	}
}

func installClipboard(t *testing.T, native nativeClipboard) *systemClipboard {
	t.Helper()
	c, _ := newFakeClipboard(nil)
	c.native = native
	prev := processClipboard.Swap(c)
	t.Cleanup(func() { processClipboard.Store(prev) })
	return c
}

func TestReleaseClipboardClosesBackendAfterClearing(t *testing.T) {
	native := &closingNative{fakeNative: fakeNative{local: true, marks: true}}
	c := installClipboard(t, native)
	_, _ = c.copy("left-behind", copyOptions{osc52: config.OSC52Auto})
	ReleaseClipboard()
	if len(native.events) != 2 || native.events[0] != "clear" || native.events[1] != "close" {
		t.Fatalf("events = %q, want the clear before the hand-over", native.events)
	}

	// Already cleared by the timer: the backend still hands over its
	// (blanked) clipboard.
	native.events = nil
	ReleaseClipboard()
	if len(native.events) != 1 || native.events[0] != "close" {
		t.Fatalf("events = %q", native.events)
	}
}

func TestReleaseClipboardIsBounded(t *testing.T) {
	native := &closingNative{fakeNative: fakeNative{local: true}, block: make(chan struct{})}
	installClipboard(t, native)
	defer close(native.block)
	prev := releaseTimeout
	releaseTimeout = 50 * time.Millisecond
	t.Cleanup(func() { releaseTimeout = prev })
	start := time.Now()
	ReleaseClipboard()
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("ReleaseClipboard waited %v for a stuck backend", elapsed)
	}
}
