//go:build !windows && !linux && !darwin

// SPDX-License-Identifier: GPL-3.0-or-later

package system

// FilesystemType is empty where the system is not asked.
func FilesystemType(path string) (string, error) { return "", nil }
