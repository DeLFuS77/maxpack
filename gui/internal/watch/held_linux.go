// SPDX-License-Identifier: GPL-3.0-or-later

//go:build linux

package watch

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// heldOpen tells whether a process of this user holds a file at root, or under it, open for writing, as /proc
// shows it (each process's fd/ and fdinfo/). A best effort: the processes of other users are not seen, nor a
// writer on another machine (a network share).
func heldOpen(root string) bool {
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	procs, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	uid := uint32(os.Getuid())
	for _, proc := range procs {
		if _, err := strconv.Atoi(proc.Name()); err != nil {
			continue
		}
		if info, err := proc.Info(); err != nil {
			continue
		} else if st, ok := info.Sys().(*syscall.Stat_t); !ok || st.Uid != uid {
			continue
		}
		dir := "/proc/" + proc.Name()
		fds, err := os.ReadDir(dir + "/fd")
		if err != nil {
			continue
		}
		for _, fd := range fds {
			target, err := os.Readlink(dir + "/fd/" + fd.Name())
			if err != nil || (target != root && !strings.HasPrefix(target, root+"/")) {
				continue
			}
			if writing(dir + "/fdinfo/" + fd.Name()) {
				return true
			}
		}
	}
	return false
}

// writing tells whether an fdinfo says its file is open for writing (its flags, in octal: O_WRONLY or O_RDWR).
func writing(fdinfo string) bool {
	data, err := os.ReadFile(fdinfo)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(line, "flags:"); ok {
			flags, err := strconv.ParseUint(strings.TrimSpace(rest), 8, 64)
			return err == nil && flags&(syscall.O_WRONLY|syscall.O_RDWR) != 0
		}
	}
	return false
}
