package ui

import (
	"fmt"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// openPTY opens a pseudo terminal: its controlling side, its terminal side
// and the terminal's path.
func openPTY(t *testing.T) (master, slave *os.File, name string) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo terminals: %v", err)
	}
	t.Cleanup(func() { _ = master.Close() })
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	n, err := unix.IoctlGetUint32(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	name = fmt.Sprintf("/dev/pts/%d", n)
	slave, err = os.OpenFile(name, os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	return master, slave, name
}
