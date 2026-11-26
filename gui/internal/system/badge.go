// SPDX-License-Identifier: GPL-3.0-or-later

package system

import "fmt"

// badgeText is what the icon of the Dock shows of the work on macOS (taskbar_darwin.go): its percentage while it
// works, and while it is paused too (it is not over), "" once it ended or failed.
func badgeText(state string, done, total uint64) string {
	if (state != "normal" && state != "paused") || total == 0 {
		return ""
	}
	return fmt.Sprintf("%d%%", min(done, total)*100/total)
}
