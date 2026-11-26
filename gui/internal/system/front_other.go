// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !windows

package system

// WindowInFront cannot tell on macOS and Linux (known is false): the page tells it instead, as it gains
// and loses the focus.
func WindowInFront() (front, known bool) { return false, false }
