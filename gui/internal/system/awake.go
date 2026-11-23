// SPDX-License-Identifier: GPL-3.0-or-later

package system

import "sync"

// the computer is kept awake while files are at work
var awake struct {
	mutex   sync.Mutex
	holders int
	release func()
}

// BlockSleep keeps the computer from going to sleep until the function it gives is called.
var BlockSleep = preventSleep

// KeepAwake keeps the computer from going to sleep while files are at work, until the function
// it gives is called.
func KeepAwake() func() {
	awake.mutex.Lock()
	defer awake.mutex.Unlock()
	awake.holders++
	holdAwake()
	var once sync.Once
	return func() {
		once.Do(func() {
			awake.mutex.Lock()
			defer awake.mutex.Unlock()
			awake.holders--
			holdAwake()
		})
	}
}

// holdAwake keeps the computer awake while work wants it so, and lets it sleep once not; the
// mutex is held.
func holdAwake() {
	switch {
	case awake.holders > 0 && awake.release == nil:
		awake.release = BlockSleep()
	case awake.holders == 0 && awake.release != nil:
		awake.release()
		awake.release = nil
	}
}
