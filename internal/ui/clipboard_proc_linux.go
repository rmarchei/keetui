//go:build linux

package ui

import (
	"bytes"
	"errors"
	"fmt"
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
