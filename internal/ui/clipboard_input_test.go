//go:build linux || darwin

package ui

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// TestLastInputReadsTheTerminalOfARealProcess types into a pseudo terminal
// that a stand-in herdr client reads, and checks the time keetui reports
// for that client's terminal (fd 0 under /proc on Linux, the controlling
// terminal on macOS).
func TestLastInputReadsTheTerminalOfARealProcess(t *testing.T) {
	table := newProcessTable()
	master, slave, name := openPTY(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe)
	cmd.Env = []string{fakeHerdrEnv + "=1", "HOME=/nonexistent"}
	cmd.Stdin = slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	_ = slave.Close()

	// Backdate the terminal, so that only input can bring its time forward.
	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(name, past, past); err != nil {
		t.Fatal(err)
	}
	waitFor := func(what string, ok func(time.Time) bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			at, found := table.lastInput(cmd.Process.Pid)
			if found && ok(at) {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: lastInput = %v, %v", what, at, found)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	waitFor("before any input", func(at time.Time) bool { return at.Equal(past) })

	if _, err := master.Write([]byte("synthetic keypress\n")); err != nil {
		t.Fatal(err)
	}
	waitFor("after input", func(at time.Time) bool { return time.Since(at) <= localInputWindow })
}
