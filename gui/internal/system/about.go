// SPDX-License-Identifier: GPL-3.0-or-later

package system

import "fmt"

// windowsName is how a report names a version of Windows: Windows 11 still says 10.0, and its
// builds from 22000 on are 11.
func windowsName(major, minor, build uint32) string {
	switch {
	case major == 10 && minor == 0 && build >= 22000:
		return fmt.Sprintf("Windows 11 (10.0.%d)", build)
	case major == 10 && minor == 0:
		return fmt.Sprintf("Windows 10 (10.0.%d)", build)
	}
	return fmt.Sprintf("Windows %d.%d.%d", major, minor, build)
}
