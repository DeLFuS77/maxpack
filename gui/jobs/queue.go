// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

import "sync"

// Queue is the list of plans a Runner goes through, one at a time: games added while it runs go
// at the end and are done with the same options, and a game taken out before its turn is not
// done. Once the Runner found nothing more to do, the queue is closed and takes nothing.
type Queue struct {
	mutex   sync.Mutex
	plans   []Plan
	removed map[int]bool
	next    int
	closed  bool
}

// NewQueue is a queue of the plans.
func NewQueue(plans []Plan) *Queue {
	return &Queue{plans: append([]Plan{}, plans...), removed: map[int]bool{}}
}

// Add puts plans at the end of the queue, and gives the index of the first of them; ok is false
// when the queue is closed (the Runner ended).
func (q *Queue) Add(plans ...Plan) (first int, ok bool) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	if q.closed {
		return 0, false
	}
	first = len(q.plans)
	q.plans = append(q.plans, plans...)
	return first, true
}

// Remove takes game i out of the queue before its turn; false when it started already, or there
// is no such game.
func (q *Queue) Remove(i int) bool {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	if q.closed || i < q.next || i >= len(q.plans) {
		return false
	}
	q.removed[i] = true
	return true
}

// Plan gives game i of the queue.
func (q *Queue) Plan(i int) (Plan, bool) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	if i < 0 || i >= len(q.plans) {
		return Plan{}, false
	}
	return q.plans[i], true
}

// Waiting are the games that have not started yet (and were not taken out).
func (q *Queue) Waiting() []Plan {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	waiting := []Plan{}
	for i := q.next; i < len(q.plans); i++ {
		if !q.removed[i] {
			waiting = append(waiting, q.plans[i])
		}
	}
	return waiting
}

// All are every game the queue has had, by their index.
func (q *Queue) All() []Plan {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	return append([]Plan{}, q.plans...)
}

// Removed tells whether game i was taken out before its turn.
func (q *Queue) Removed(i int) bool {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	return q.removed[i]
}

// Len is how many games the queue has had.
func (q *Queue) Len() int {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	return len(q.plans)
}

// more tells whether a game is left to do (not taken out), without taking it.
func (q *Queue) more() bool {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	for i := q.next; i < len(q.plans); i++ {
		if !q.removed[i] {
			return true
		}
	}
	return false
}

// take gives the next game to do, past the ones taken out; when there is none the queue closes.
func (q *Queue) take() (int, Plan, bool) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	for q.next < len(q.plans) && q.removed[q.next] {
		q.next++
	}
	if q.next >= len(q.plans) {
		q.closed = true
		return 0, Plan{}, false
	}
	i := q.next
	q.next++
	return i, q.plans[i], true
}
