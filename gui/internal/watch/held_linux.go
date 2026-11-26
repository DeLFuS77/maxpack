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

// heldOpen tells which of roots a process of this user holds a file of open for writing (the root itself, or a
// file under it), as /proc shows it: one look through each process's fd/ and fdinfo/ for all of them. A best
// effort: the processes of other users are not seen, nor a writer on another machine (a network share), nor one
// that opened the file through another path (a bind mount, another mount namespace).
func heldOpen(roots []string) map[string]bool {
	if len(roots) == 0 {
		return nil
	}
	// each root as /proc names what is open: absolute, its links followed
	real := make(map[string]string, len(roots))
	for _, root := range roots {
		r := root
		if abs, err := filepath.Abs(r); err == nil {
			r = abs
		}
		if resolved, err := filepath.EvalSymlinks(r); err == nil {
			r = resolved
		}
		real[r] = root
	}
	under := func(target string) (string, bool) {
		for r, root := range real {
			if target == r || strings.HasPrefix(target, r+"/") {
				return root, true
			}
		}
		return "", false
	}
	procs, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	found := map[string]bool{}
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
			if err != nil {
				continue
			}
			root, ok := under(target)
			if !ok || found[root] {
				continue
			}
			if writing(dir + "/fdinfo/" + fd.Name()) {
				found[root] = true
				if len(found) == len(real) {
					return found
				}
			}
		}
	}
	return found
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
