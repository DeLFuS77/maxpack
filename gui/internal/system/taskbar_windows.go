// SPDX-License-Identifier: GPL-3.0-or-later

//go:build windows

package system

import (
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	ole "github.com/go-ole/go-ole"
	"golang.org/x/sys/windows"
)

// the states of the progress on the button of the taskbar (ITaskbarList3::SetProgressState)
const (
	taskbarNormal = 0x2
	taskbarError  = 0x4
	taskbarPaused = 0x8
)

var (
	clsidTaskbarList = ole.NewGUID("{56FDF344-FD6D-11d0-958A-006097C9A090}")
	iidTaskbarList3  = ole.NewGUID("{EA1AFB91-9E28-4B86-90E9-9E9F8A5EEFAF}")
	taskbar          = newTaskbarSlot()
	taskbarOnce      sync.Once
)

type taskbarState struct {
	flag        uintptr
	done, total uint64
}

// taskbarSlot keeps the last state asked for until the thread of the taskbar takes it: a state
// asked for meanwhile takes the place of the one not shown yet, so the last one is always shown.
type taskbarSlot struct {
	mutex sync.Mutex
	state taskbarState
	wake  chan struct{}
}

func newTaskbarSlot() *taskbarSlot {
	return &taskbarSlot{wake: make(chan struct{}, 1)}
}

func (s *taskbarSlot) put(state taskbarState) {
	s.mutex.Lock()
	s.state = state
	s.mutex.Unlock()
	// one wake waiting is enough: it takes the last state
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *taskbarSlot) take() taskbarState {
	<-s.wake
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.state
}

// SetTaskbarProgress shows the progress of the work on the button of MaxPack in the taskbar:
// state is "none", "normal", "paused" or "error", done of total. The states go to one thread
// of their own, where COM lives.
func SetTaskbarProgress(state string, done uint64, total uint64) {
	taskbarOnce.Do(func() { go taskbarThread() })
	flag := map[string]uintptr{"normal": taskbarNormal, "paused": taskbarPaused, "error": taskbarError}[state]
	taskbar.put(taskbarState{flag: flag, done: done, total: total})
}

// showTaskbarState shows a state on the button of the window.
func showTaskbarState(list *ole.IUnknown, window windows.HWND, state taskbarState) {
	methods := *(*[11]uintptr)(unsafe.Pointer(list.RawVTable))
	// SetProgressState is the method 10 of the table, SetProgressValue the 9
	syscall.SyscallN(methods[10], uintptr(unsafe.Pointer(list)), uintptr(window), state.flag)
	if state.flag != 0 {
		syscall.SyscallN(methods[9], uintptr(unsafe.Pointer(list)), uintptr(window), uintptr(state.done), uintptr(state.total))
	}
}

func taskbarThread() {
	runtime.LockOSThread()
	// an error here is S_FALSE, COM already initialized on this thread: it works the same
	ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED)
	var list *ole.IUnknown
	var window windows.HWND
	for {
		state := taskbar.take()
		if window == 0 || !windows.IsWindowVisible(window) {
			window = FindOwnWindow(false)
		}
		if window == 0 {
			continue
		}
		if list == nil {
			created, err := ole.CreateInstance(clsidTaskbarList, iidTaskbarList3)
			if err != nil {
				continue
			}
			methods := *(*[4]uintptr)(unsafe.Pointer(created.RawVTable))
			// HrInit, the method 3
			syscall.SyscallN(methods[3], uintptr(unsafe.Pointer(created)))
			list = created
		}
		showTaskbarState(list, window, state)
	}
}
