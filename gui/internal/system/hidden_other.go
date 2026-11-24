// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !windows

package system

// SetHidden does nothing here: only Windows hides a file by an attribute, and the name of what
// MaxPack writes is fixed (<name>.maxpack-tmp).
func SetHidden(path string, hidden bool) error { return nil }

// IsHidden is false here: nothing is hidden by an attribute.
func IsHidden(path string) bool { return false }
