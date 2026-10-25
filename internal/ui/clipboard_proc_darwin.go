//go:build darwin

package ui

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"

	"golang.org/x/sys/unix"
)

// sysctlProcs reads processes with sysctl: kern.proc lists them with their
// names, and kern.procargs2 gives a process's executable path, argv and
// environment, which macOS shows only to the same user.
type sysctlProcs struct{ uid int }

func newProcessTable() processTable { return sysctlProcs{uid: os.Geteuid()} }

// maxComm is the length macOS keeps of a process name (MAXCOMLEN).
const maxComm = 16

func (p sysctlProcs) named(names []string) ([]procInfo, error) {
	kps, err := unix.SysctlKinfoProcSlice("kern.proc.uid", p.uid)
	if err != nil {
		return nil, fmt.Errorf("kern.proc.uid: %w", err)
	}
	comms := make([]string, len(names))
	for i, name := range names {
		comms[i] = name[:min(len(name), maxComm)]
	}
	deadline := time.Now().Add(procScanBudget)
	var procs []procInfo
	for i := range kps {
		if !slices.Contains(comms, unix.ByteSliceToString(kps[i].Proc.P_comm[:])) {
			continue
		}
		if time.Now().After(deadline) {
			return nil, errors.New("process scan took too long")
		}
		pid := int(kps[i].Proc.P_pid)
		buf, err := unix.SysctlRaw("kern.procargs2", pid)
		if err != nil {
			continue // exited, or not ours
		}
		info, ok := parseProcargs(buf)
		if !ok {
			continue
		}
		info.pid = pid
		procs = append(procs, info)
	}
	return procs, nil
}

func (sysctlProcs) parent(pid int) (int, string, error) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return 0, "", fmt.Errorf("kern.proc.pid: %w", err)
	}
	if int(kp.Proc.P_pid) != pid {
		return 0, "", fmt.Errorf("kern.proc.pid: no process %d", pid)
	}
	return int(kp.Eproc.Ppid), unix.ByteSliceToString(kp.Proc.P_comm[:]), nil
}

// parseProcargs reads kern.procargs2: argc, the executable path, NUL
// padding, argc arguments, then the environment up to an empty string.
func parseProcargs(buf []byte) (info procInfo, ok bool) {
	if len(buf) < 4 {
		return info, false
	}
	argc := int(binary.NativeEndian.Uint32(buf))
	rest := buf[4:]
	exe, rest, found := bytes.Cut(rest, []byte{0})
	if !found {
		return info, false
	}
	info.exe = string(exe)
	rest = bytes.TrimLeft(rest, "\x00")
	next := func() (string, bool) {
		s, after, found := bytes.Cut(rest, []byte{0})
		if !found {
			return "", false
		}
		rest = after
		return string(s), true
	}
	for range argc {
		arg, found := next()
		if !found {
			return info, false
		}
		info.args = append(info.args, arg)
	}
	info.env = []string{}
	for {
		kv, found := next()
		if !found || kv == "" {
			break
		}
		info.env = append(info.env, kv)
	}
	return info, true
}
