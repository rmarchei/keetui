package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain runs the tests with $HOME and $XDG_CONFIG_HOME in a scratch
// directory, so that no test reaches the user's configuration: lookup falls
// back to ~/.config and, on macOS, to ~/Library/Application Support, which
// a test overriding only $XDG_CONFIG_HOME would still write.
func TestMain(m *testing.M) {
	os.Exit(runIsolated(m))
}

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
