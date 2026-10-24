package ui

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/rmarchei/keetui/internal/config"
)

// Clipboard copies prefer the local clipboard, where the platform backend can
// mark them as sensitive (excluded from clipboard history). The terminal
// clipboard (OSC 52) cannot be marked, and tmux buffers and session
// recorders may keep it, so it is used according to clipboard_osc52.
//
// Backends remember their last copy (as a keyed hash, never the value), so
// clearing only ever removes keetui's own copy.

type copyOptions struct {
	transient bool   // an automatic clear is scheduled
	osc52     string // config.OSC52Auto, OSC52Always or OSC52Never
}

type copyResult struct {
	native   bool // written to the local clipboard
	marked   bool // the local copy carries sensitive markers
	terminal bool // sent to the terminal with OSC 52
}

type clearOutcome int

const (
	clearNothing   clearOutcome = iota // no keetui copy was outstanding
	clearDone                          // keetui's copy was removed
	clearChanged                       // the clipboard holds something else; left untouched
	clearRequested                     // OSC 52 only: an empty copy was sent, unverifiable
)

// Keep clipboard I/O injectable so timer regressions never access the user's
// real clipboard. A nil AppModel backend uses the process-wide one.
type clipboardAccess interface {
	copy(value string, opts copyOptions) (copyResult, error)
	// release clears the clipboard if it still holds the last copy, then
	// forgets that copy.
	release() (clearOutcome, error)
}

// nativeClipboard is the platform's local clipboard.
type nativeClipboard interface {
	// available reports whether the local clipboard is the user's clipboard
	// (for example, not a remote Mac reached over SSH).
	available() bool
	write(value []byte, transient bool) (marked bool, err error)
	// holds reports whether the clipboard still holds the last write.
	// matches checks content where ownership cannot be tracked otherwise;
	// alsoTerminal means an OSC 52 copy of the same value may have replaced
	// the write.
	holds(matches func([]byte) bool, alsoTerminal bool) bool
	clear() error
}

var errNoClipboard = errors.New("no clipboard available")

type systemClipboard struct {
	mu       sync.Mutex
	native   nativeClipboard    // nil when the platform has none
	terminal func(string) error // OSC 52 writer
	held     *heldCopy
}

type heldCopy struct {
	result   copyResult
	key, sum []byte
}

func newHeldCopy(value []byte, result copyResult) *heldCopy {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	return &heldCopy{result: result, key: key, sum: copyMAC(key, value)}
}

func copyMAC(key, value []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(value)
	return mac.Sum(nil)
}

func (h *heldCopy) matches(value []byte) bool {
	return hmac.Equal(copyMAC(h.key, value), h.sum)
}

func (c *systemClipboard) copy(value string, opts copyOptions) (copyResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	useNative := c.native != nil && c.native.available()
	useTerminal := opts.osc52 == config.OSC52Always || opts.osc52 != config.OSC52Never && !useNative
	var res copyResult
	var err error
	if useNative {
		if res.marked, err = c.native.write([]byte(value), opts.transient); err == nil {
			res.native = true
		} else if opts.osc52 != config.OSC52Never {
			useTerminal = true
		}
	}
	if useTerminal {
		if terr := c.terminal(value); terr == nil {
			res.terminal = true
		} else if err == nil {
			err = terr
		}
	}
	if !res.native && !res.terminal {
		if err == nil {
			err = errNoClipboard
		}
		return copyResult{}, err
	}
	c.held = newHeldCopy([]byte(value), res)
	return res, nil
}

func (c *systemClipboard) release() (clearOutcome, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	h := c.held
	if h == nil {
		return clearNothing, nil
	}
	c.held = nil
	if !h.result.native {
		// The terminal clipboard cannot be read back to check ownership.
		return clearRequested, c.terminal("")
	}
	if !c.native.holds(h.matches, h.result.terminal) {
		return clearChanged, nil
	}
	if err := c.native.clear(); err != nil {
		return clearNothing, fmt.Errorf("clear clipboard: %w", err)
	}
	return clearDone, nil
}

var (
	processClipboardOnce sync.Once
	processClipboard     atomic.Pointer[systemClipboard]
)

// platformClipboard returns the process-wide clipboard. It lives outside the
// value-typed AppModel so ReleaseClipboard can reach it after the program
// exits.
func platformClipboard() *systemClipboard {
	processClipboardOnce.Do(func() {
		// Tests install their own first.
		processClipboard.CompareAndSwap(nil, &systemClipboard{native: newNativeClipboard(), terminal: osc52Copy})
	})
	return processClipboard.Load()
}

// ReleaseClipboard clears keetui's copy if it is still on the clipboard.
// main calls it after the program exits, because signals and recovered
// panics end the program without the model's quit handling.
func ReleaseClipboard() {
	if c := processClipboard.Load(); c != nil {
		_, _ = c.release()
	}
}

// osc52Copy writes an OSC 52 escape sequence to set the terminal clipboard.
// This works over SSH because the escape sequence travels through the connection
// to the local terminal emulator. Writes directly to /dev/tty to avoid
// interfering with bubbletea's stdout rendering.
func osc52Copy(text string) error {
	f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("terminal clipboard: %w", err)
	}
	defer func() { _ = f.Close() }()
	b64 := base64.StdEncoding.EncodeToString([]byte(text))
	if _, err := fmt.Fprintf(f, "\033]52;c;%s\a", b64); err != nil {
		return fmt.Errorf("terminal clipboard: %w", err)
	}
	return nil
}

// pipeTo runs a clipboard tool with input on stdin, never in argv. Output
// stays unattached: xclip, xsel and wl-copy fork a server that keeps serving
// the selection, and Wait must not block on it.
func pipeTo(env []string, input []byte, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(input)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// copyStatus describes where a copy went, and warns when clipboard history
// may keep it.
func copyStatus(field string, r copyResult) string {
	switch {
	case !r.native:
		return fmt.Sprintf("Sent %s to the terminal clipboard (OSC 52)", field)
	case !r.marked:
		return fmt.Sprintf("Copied %s (clipboard managers may keep it)", field)
	case r.terminal:
		return fmt.Sprintf("Copied %s (also sent via OSC 52)", field)
	}
	return fmt.Sprintf("Copied %s!", field)
}

// envWith returns the environment with the given variables replaced.
func envWith(kv ...string) []string {
	env := os.Environ()
	for _, pair := range kv {
		name := pair[:strings.IndexByte(pair, '=')+1]
		env = slices.DeleteFunc(env, func(e string) bool { return strings.HasPrefix(e, name) })
		env = append(env, pair)
	}
	return env
}
