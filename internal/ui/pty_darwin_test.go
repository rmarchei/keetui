package ui

import (
	"fmt"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// openPTY opens a pseudo terminal: its controlling side, its terminal side
// and the terminal's path. The terminal is /dev/ttysNNN, numbered by the
// controlling side's minor device number; a line written through the
// controlling side confirms the pair.
func openPTY(t *testing.T) (master, slave *os.File, name string) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo terminals: %v", err)
	}
	t.Cleanup(func() { _ = master.Close() })
	fd := int(master.Fd())
	for _, req := range []uint{unix.TIOCPTYGRANT, unix.TIOCPTYUNLK} {
		if err := unix.IoctlSetInt(fd, req, 0); err != nil {
			t.Fatal(err)
		}
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		t.Fatal(err)
	}
	name = fmt.Sprintf("/dev/ttys%03d", st.Rdev&0xffffff)
	slave, err = os.OpenFile(name, os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := master.Write([]byte("pair\n")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 16)
	if n, err := slave.Read(buf); err != nil || string(buf[:n]) != "pair\n" {
		t.Fatalf("%s is not this pseudo terminal's: read %q, %v", name, buf[:n], err)
	}
	return master, slave, name
}
