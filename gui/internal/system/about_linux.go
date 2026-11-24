// SPDX-License-Identifier: GPL-3.0-or-later

package system

import (
	"bufio"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// OSVersion is the system and its version, for a report: the name of the distribution as
// /etc/os-release gives it, and the version of Linux, "Ubuntu 24.04.1 LTS (Linux 6.8.0-49)".
func OSVersion() string {
	name := "Linux"
	if file, err := os.Open("/etc/os-release"); err == nil {
		lines := bufio.NewScanner(file)
		for lines.Scan() {
			if value, ok := strings.CutPrefix(lines.Text(), "PRETTY_NAME="); ok {
				if value = strings.Trim(value, `"'`); value != "" {
					name = value
				}
			}
		}
		file.Close()
	}
	var uname unix.Utsname
	if unix.Uname(&uname) == nil {
		return name + " (Linux " + unix.ByteSliceToString(uname.Release[:]) + ")"
	}
	return name
}

// TotalMemory is the memory of the PC in bytes, 0 when Linux does not tell.
func TotalMemory() uint64 {
	var info unix.Sysinfo_t
	if unix.Sysinfo(&info) != nil {
		return 0
	}
	unit := uint64(info.Unit)
	if unit == 0 {
		unit = 1
	}
	return uint64(info.Totalram) * unit
}
