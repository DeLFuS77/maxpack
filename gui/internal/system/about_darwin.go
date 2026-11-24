// SPDX-License-Identifier: GPL-3.0-or-later

package system

import "golang.org/x/sys/unix"

// OSVersion is the system and its version, for a report: "macOS 15.1".
func OSVersion() string {
	if version, err := unix.Sysctl("kern.osproductversion"); err == nil && version != "" {
		return "macOS " + version
	}
	return "macOS"
}

// TotalMemory is the memory of the Mac in bytes, 0 when it does not tell.
func TotalMemory() uint64 {
	memory, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return 0
	}
	return memory
}
