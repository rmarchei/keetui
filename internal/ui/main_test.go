package ui

import (
	"errors"
	"os"
	"testing"
)

// TestMain keeps every test away from the user's clipboard, terminal and
// tmux: a model without an injected backend gets a process clipboard that
// refuses to copy.
func TestMain(m *testing.M) {
	processClipboard.Store(&systemClipboard{terminal: terminalRoutes{tty: refusingTerminal{}}})
	os.Exit(m.Run())
}

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
