// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !windows

package system

// Only Windows keeps MaxPack to one window: there the Explorer starts it with files.

func WaitForFirstWindow(id string) bool { return false }

func StartWindow(id string) (done func(), another bool) { return func() {}, false }

func AllowForeground() {}

func BringToFront() {}
