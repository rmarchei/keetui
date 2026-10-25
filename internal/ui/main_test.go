package ui

import (
	"errors"
	"io"
	"os"
	"path/filepath"
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
	os.Exit(runIsolated(m))
}

// runIsolated runs the tests with $HOME and $XDG_CONFIG_HOME in a scratch
// directory, so that no test reaches the user's configuration: config
// lookup also falls back to ~/.config and, on macOS, to ~/Library/Application
// Support, which a test overriding only $XDG_CONFIG_HOME would still write.
func runIsolated(m *testing.M) int {
	home, err := os.MkdirTemp("", "keetui-test-home-")
	if err != nil {
		panic(err)
	}
	defer func() { _ = os.RemoveAll(home) }()
	if err := os.Setenv("HOME", home); err != nil {
		panic(err)
	}
	if err := os.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config")); err != nil {
		panic(err)
	}
	return m.Run()
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
