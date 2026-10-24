package ui

import (
	"errors"
	"os"
	"testing"
)

// TestMain keeps every test away from the user's clipboard: a model without
// an injected backend gets a process clipboard that refuses to copy.
func TestMain(m *testing.M) {
	processClipboard.Store(&systemClipboard{terminal: func(string) error {
		return errors.New("tests must inject a clipboard")
	}})
	os.Exit(m.Run())
}
