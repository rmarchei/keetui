package main

import (
	"fmt"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func openCLIPTY(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("pseudo terminals unavailable: %v", err)
	}
	t.Cleanup(func() { _ = master.Close() })
	for _, request := range []uint{unix.TIOCPTYGRANT, unix.TIOCPTYUNLK} {
		if err := unix.IoctlSetInt(int(master.Fd()), request, 0); err != nil {
			t.Fatal(err)
		}
	}
	var st unix.Stat_t
	if err := unix.Fstat(int(master.Fd()), &st); err != nil {
		t.Fatal(err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/ttys%03d", st.Rdev&0xffffff), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = slave.Close() })
	return master, slave
}
