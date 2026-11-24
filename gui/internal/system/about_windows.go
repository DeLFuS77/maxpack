// SPDX-License-Identifier: GPL-3.0-or-later

//go:build windows

package system

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// OSVersion is the system and its version, for a report: "Windows 11 (10.0.26200)". Windows is
// asked with RtlGetVersion, which tells the truth whatever the manifest of the program says.
func OSVersion() string {
	v := windows.RtlGetVersion()
	return windowsName(v.MajorVersion, v.MinorVersion, v.BuildNumber)
}

// memoryStatusEx is MEMORYSTATUSEX.
type memoryStatusEx struct {
	length                                  uint32
	memoryLoad                              uint32
	totalPhys, availPhys                    uint64
	totalPageFile, availPageFile            uint64
	totalVirtual, availVirtual, availExtend uint64
}

// TotalMemory is the memory of the PC in bytes, 0 when Windows does not tell.
func TotalMemory() uint64 {
	status := memoryStatusEx{length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	if ok, _, _ := kernel32.NewProc("GlobalMemoryStatusEx").Call(uintptr(unsafe.Pointer(&status))); ok == 0 {
		return 0
	}
	return status.totalPhys
}
