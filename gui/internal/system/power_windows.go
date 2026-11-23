// SPDX-License-Identifier: GPL-3.0-or-later

//go:build windows

package system

import "runtime"

const (
	esContinuous     = 0x80000000
	esSystemRequired = 0x00000001
)

// preventSleep keeps the computer from going to sleep until the function it gives is called.
func preventSleep() func() {
	state := kernel32.NewProc("SetThreadExecutionState")
	done := make(chan struct{})
	released := make(chan struct{})
	// the state belongs to a thread: one goroutine, locked to its thread, holds it
	go func() {
		runtime.LockOSThread()
		state.Call(esContinuous | esSystemRequired)
		<-done
		state.Call(esContinuous)
		close(released)
	}()
	return func() {
		close(done)
		<-released
	}
}
