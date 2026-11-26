// SPDX-License-Identifier: GPL-3.0-or-later

package core

import (
	"context"
	"sync"
)

// Pause is the pause of a queue, shared by what it runs: while it is on, the engine at work is told to
// pause (the line "pause" on its input, see RunWith) and nothing new starts (Wait). A nil Pause is
// never on.
type Pause struct {
	mutex     sync.Mutex
	on        bool
	changed   chan struct{} // closed at each change, and made again
	listeners map[int]func(on bool)
	next      int
}

// NewPause is a pause that is off.
func NewPause() *Pause {
	return &Pause{changed: make(chan struct{}), listeners: map[int]func(bool){}}
}

// Set turns the pause on or off, and tells whether that changed it. The listeners hear each change.
func (p *Pause) Set(on bool) bool {
	if p == nil {
		return false
	}
	p.mutex.Lock()
	if p.on == on {
		p.mutex.Unlock()
		return false
	}
	p.on = on
	close(p.changed)
	p.changed = make(chan struct{})
	listeners := make([]func(bool), 0, len(p.listeners))
	for _, listener := range p.listeners {
		listeners = append(listeners, listener)
	}
	p.mutex.Unlock()
	for _, listener := range listeners {
		listener(on)
	}
	return true
}

// On tells whether the pause is on.
func (p *Pause) On() bool {
	if p == nil {
		return false
	}
	p.mutex.Lock()
	defer p.mutex.Unlock()
	return p.on
}

// Wait returns once the pause is off (at once when it is), or with the error of ctx once it ends.
func (p *Pause) Wait(ctx context.Context) error {
	if p == nil {
		return nil
	}
	for {
		p.mutex.Lock()
		on, changed := p.on, p.changed
		p.mutex.Unlock()
		if !on {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

// Listen calls fn at each change of the pause, until the function it gives is called.
func (p *Pause) Listen(fn func(on bool)) (stop func()) {
	if p == nil {
		return func() {}
	}
	p.mutex.Lock()
	id := p.next
	p.next++
	p.listeners[id] = fn
	p.mutex.Unlock()
	return func() {
		p.mutex.Lock()
		delete(p.listeners, id)
		p.mutex.Unlock()
	}
}
