// SPDX-License-Identifier: GPL-3.0-or-later

//go:build windows

package core

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// NtSuspendProcess and NtResumeProcess stop and go on with every thread of a process at once (Windows has
// no documented call that does it for a whole process; the Resource Monitor uses these).
var (
	ntdll            = windows.NewLazySystemDLL("ntdll.dll")
	ntSuspendProcess = ntdll.NewProc("NtSuspendProcess")
	ntResumeProcess  = ntdll.NewProc("NtResumeProcess")
)

// Suspend stops every thread of a process until Resume: for maxpack-par2, which cannot be told to pause.
func Suspend(p *os.Process) error { return processCall(ntSuspendProcess, p) }

// Resume lets a process that Suspend stopped go on.
func Resume(p *os.Process) error { return processCall(ntResumeProcess, p) }

func processCall(call *windows.LazyProc, p *os.Process) error {
	if err := call.Find(); err != nil {
		return err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SUSPEND_RESUME, false, uint32(p.Pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)
	if status, _, _ := call.Call(uintptr(process)); status != 0 {
		return fmt.Errorf("%s: NTSTATUS 0x%08X", call.Name, uint32(status))
	}
	return nil
}
