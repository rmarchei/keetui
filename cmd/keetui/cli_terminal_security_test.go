//go:build linux || darwin

package main

import (
	"bytes"
	"os"
	"os/exec"
	"reflect"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// Compare terminal states rather than sleep after seeing a prompt: the
// prompt is written before ReadPassword has actually disabled echo.
func startPasswordReader(t *testing.T) (*exec.Cmd, *os.File, *os.File, *term.State, *bytes.Buffer) {
	t.Helper()
	master, slave := openCLIPTY(t)
	initial, err := term.GetState(int(slave.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	cmd := newCLICommand(t, "{}")
	cmd.Env = append(cmd.Env, "KEETUI_TEST_READ_PASSWORD=1")
	var out, errOut bytes.Buffer
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, &out, &errOut
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	deadline := time.Now().Add(5 * time.Second)
	for {
		current, err := term.GetState(int(slave.Fd()))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(initial, current) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("password reader did not change the terminal state")
		}
		time.Sleep(time.Millisecond)
	}
	return cmd, master, slave, initial, &out
}

func TestReadPasswordDoesNotEchoAndRestoresTerminal(t *testing.T) {
	cmd, master, slave, initial, out := startPasswordReader(t)
	if _, err := master.Write([]byte("synthetic-password\n")); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if out.String() != "read 18 bytes\n" {
		t.Fatalf("password reader output %q", out.String())
	}
	current, err := term.GetState(int(slave.Fd()))
	if err != nil || !reflect.DeepEqual(initial, current) {
		t.Fatalf("terminal state was not restored: %v", err)
	}
	fds := []unix.PollFd{{Fd: int32(master.Fd()), Events: unix.POLLIN}}
	if _, err := unix.Poll(fds, 0); err != nil {
		t.Fatal(err)
	}
	if fds[0].Revents&unix.POLLIN != 0 {
		var echo [256]byte
		n, err := master.Read(echo[:])
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(echo[:n], []byte("synthetic-password")) {
			t.Fatal("master password was echoed to the terminal")
		}
	}
}

func TestInterruptedPasswordReadRestoresTerminal(t *testing.T) {
	for _, signal := range []os.Signal{os.Interrupt, unix.SIGTERM} {
		t.Run(signal.String(), func(t *testing.T) {
			cmd, _, slave, initial, out := startPasswordReader(t)
			if err := cmd.Process.Signal(signal); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Wait(); err == nil || cmd.ProcessState.ExitCode() != 130 {
				t.Fatalf("interrupted reader returned %v, state %v", err, cmd.ProcessState)
			}
			current, err := term.GetState(int(slave.Fd()))
			if err != nil || !reflect.DeepEqual(initial, current) {
				t.Fatalf("interrupted read did not restore terminal echo: %v", err)
			}
			if out.Len() != 0 {
				t.Fatal("interrupted reader returned a password result")
			}
		})
	}
}
