package ui

import (
	"errors"
	"io"
	"os"
	"testing"
)

// TestMain keeps every test away from the user's clipboard, terminal and
// tmux: a model without an injected backend gets a process clipboard that
// refuses to copy.
func TestMain(m *testing.M) {
	if os.Getenv(fakeHerdrEnv) == "1" {
		// Re-executed as a stand-in herdr client (see
		// TestHerdrScanFindsRealProcesses): wait until the test is done.
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}
	processClipboard.Store(&systemClipboard{terminal: terminalRoutes{tty: refusingTerminal{}}})
	os.Exit(m.Run())
}

// fakeHerdrEnv makes the test binary act as a herdr client process.
const fakeHerdrEnv = "KEETUI_TEST_FAKE_HERDR"

// refusingTerminal stands in for the terminal and tmux: tests never write
// to the real terminal or run tmux.
type refusingTerminal struct{}

var errInjectClipboard = errors.New("tests must inject a clipboard")

func (refusingTerminal) copy([]byte) (terminalResult, error) {
	return terminalResult{}, errInjectClipboard
}

func (refusingTerminal) clear(terminalResult) (clearOutcome, error) {
	return clearNothing, errInjectClipboard
}
