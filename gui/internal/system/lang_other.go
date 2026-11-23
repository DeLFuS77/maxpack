// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !windows

package system

import "os"

// Language is the language of the user's system, as "es_ES.UTF-8".
func Language() string {
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}
