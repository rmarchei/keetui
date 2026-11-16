//go:build linux

package ui

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// procfs reads processes from /proc.
type procfs struct {
	root string
	uid  uint32
}

func newProcessTable() processTable { return procfs{root: "/proc", uid: uint32(os.Geteuid())} }

var errScanBudget = errors.New("process scan took too long")

func (p procfs) named(names []string) ([]procInfo, error) {
	dir, err := os.Open(p.root)
	if err != nil {
		return nil, err
	}
	ents, err := dir.Readdirnames(-1)
	_ = dir.Close()
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(procScanBudget)
	var procs []procInfo
	for i, ent := range ents {
		pid, err := strconv.Atoi(ent)
		if err != nil || pid <= 0 {
			continue
		}
		if i%64 == 0 && time.Now().After(deadline) {
			return nil, errScanBudget
		}
		base := filepath.Join(p.root, ent)
		var st syscall.Stat_t
		if syscall.Stat(base, &st) != nil || st.Uid != p.uid {
			continue
		}
		exe, err := os.Readlink(filepath.Join(base, "exe"))
		if err != nil {
			continue // exited, or not readable
		}
		// After an update replaced the binary, the kernel marks it.
		exe = strings.TrimSuffix(exe, " (deleted)")
		if !slices.Contains(names, filepath.Base(exe)) {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join(base, "cmdline"))
		if err != nil {
			continue
		}
		info := procInfo{pid: pid, exe: exe, args: splitNUL(cmdline)}
		if environ, err := os.ReadFile(filepath.Join(base, "environ")); err == nil {
			info.env = splitNUL(environ)
			if info.env == nil {
				info.env = []string{}
			}
		}
		procs = append(procs, info)
	}
	return procs, nil
}

func (p procfs) parent(pid int) (int, string, error) {
	stat, err := os.ReadFile(filepath.Join(p.root, strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, "", err
	}
	return parseProcStat(stat)
}

// lastInput reads the access time of the terminal on pid's standard input
// (a process of keetui's user, so /proc lets keetui read its descriptors).
func (p procfs) lastInput(pid int) (time.Time, bool) {
	tty, err := os.Readlink(filepath.Join(p.root, strconv.Itoa(pid), "fd", "0"))
	if err != nil || !strings.HasPrefix(tty, "/dev/pts/") && !strings.HasPrefix(tty, "/dev/tty") {
		return time.Time{}, false
	}
	var st syscall.Stat_t
	if err := syscall.Stat(tty, &st); err != nil {
		return time.Time{}, false
	}
	return time.Unix(st.Atim.Sec, st.Atim.Nsec), true
}

// parseProcStat reads the name and parent from /proc/<pid>/stat. The name
// is in parentheses and may hold any byte, parentheses included, so the
// fields after it start after the last ')'.
func parseProcStat(stat []byte) (ppid int, name string, err error) {
	open, end := bytes.IndexByte(stat, '('), bytes.LastIndexByte(stat, ')')
	if open < 0 || end < open {
		return 0, "", fmt.Errorf("unexpected /proc stat %q", stat)
	}
	fields := strings.Fields(string(stat[end+1:]))
	if len(fields) < 2 {
		return 0, "", fmt.Errorf("unexpected /proc stat %q", stat)
	}
	ppid, err = strconv.Atoi(fields[1]) // after the state
	if err != nil {
		return 0, "", fmt.Errorf("unexpected /proc stat %q", stat)
	}
	return ppid, string(stat[open+1 : end]), nil
}

// splitNUL splits a NUL-terminated list, such as /proc/<pid>/cmdline.
func splitNUL(b []byte) []string {
	b = bytes.TrimRight(b, "\x00")
	if len(b) == 0 {
		return nil
	}
	return strings.Split(string(b), "\x00")
}

// tcpListen is the state of a listening socket in /proc/net/tcp.
const tcpListen = "0A"

// listenerUIDs returns the owners of the TCP sockets of keetui's network
// namespace that listen on port at a loopback or unspecified address: those
// that a connection to a loopback address (an X display localhost:10.0) may
// reach. /proc/net/tcp6 is absent only without IPv6; any other read error
// fails the lookup.
func listenerUIDs(port int) ([]uint32, error) {
	var uids []uint32
	for _, name := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		table, err := os.ReadFile(name)
		switch {
		case err == nil:
		case name == "/proc/net/tcp6" && errors.Is(err, fs.ErrNotExist):
			continue
		default:
			return nil, err
		}
		found, err := parseListeners(table, port)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		uids = append(uids, found...)
	}
	return uids, nil
}

// parseListeners reads a /proc/net/tcp or /proc/net/tcp6 table for the
// owners of the sockets listening on port at a loopback or unspecified
// address. A line it cannot read fails it: that may be the listener.
func parseListeners(table []byte, port int) ([]uint32, error) {
	header, rows, _ := strings.Cut(string(table), "\n")
	if !strings.Contains(header, "local_address") {
		return nil, errors.New("unexpected table header")
	}
	var uids []uint32
	for row := range strings.SplitSeq(rows, "\n") {
		f := strings.Fields(row)
		if len(f) == 0 {
			continue
		}
		if len(f) < 8 {
			return nil, fmt.Errorf("unexpected line %q", row)
		}
		if f[3] != tcpListen {
			continue
		}
		host, hexPort, _ := strings.Cut(f[1], ":")
		p, err := strconv.ParseUint(hexPort, 16, 16)
		if err != nil {
			return nil, fmt.Errorf("unexpected line %q", row)
		}
		if int(p) != port {
			continue
		}
		addr, ok := procAddr(host)
		if !ok {
			return nil, fmt.Errorf("unexpected line %q", row)
		}
		if addr = addr.Unmap(); !addr.IsLoopback() && !addr.IsUnspecified() {
			continue
		}
		uid, err := strconv.ParseUint(f[7], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("unexpected line %q", row)
		}
		uids = append(uids, uint32(uid))
	}
	return uids, nil
}

// procAddr decodes an address of /proc/net/tcp (8 hex digits) or tcp6 (32),
// which the kernel prints as 32-bit words in the host's byte order.
func procAddr(h string) (netip.Addr, bool) {
	b, err := hex.DecodeString(h)
	if err != nil || len(b) != 4 && len(b) != 16 {
		return netip.Addr{}, false
	}
	var ip [16]byte
	for i := 0; i < len(b); i += 4 {
		binary.NativeEndian.PutUint32(ip[i:], binary.BigEndian.Uint32(b[i:]))
	}
	if len(b) == 4 {
		return netip.AddrFrom4([4]byte(ip[:4])), true
	}
	return netip.AddrFrom16(ip), true
}
