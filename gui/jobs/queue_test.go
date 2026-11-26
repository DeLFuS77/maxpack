// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/internal/fakecore"
)

// Games added while a queue runs are done after the others, with its options; one taken out
// before its turn is not done nor counted; once the queue ended, nothing more is added.
func TestQueueGrowsWhileRunning(t *testing.T) {
	t.Setenv(fakecore.Env, "writes")
	dest := t.TempDir()
	o := Options{Mode: ModeP5z, Dest: dest}
	first := PlanCompress([]Item{item("Starfall Odyssey", "PPSA10001", 10)}, o, newFS())
	q := NewQueue(first)
	var mutex sync.Mutex
	var order []string
	added := false
	var removedAt int
	r := &Runner{Core: os.Args[0], OnItem: func(i int, state, detail string) {
		mutex.Lock()
		defer mutex.Unlock()
		order = append(order, string(rune('0'+i))+":"+state)
		if i == 0 && state == StatePacking && !added {
			added = true
			more := PlanCompress([]Item{item("Iron Harbor", "PPSA10002", 10), item("Neon Meridian", "PPSA10003", 10), item("Lost Signal", "PPSA10004", 10)}, o, newFS())
			at, ok := q.Add(more...)
			if !ok || at != 1 {
				t.Errorf("Add: %d %v", at, ok)
			}
			removedAt = at + 1
			if !q.Remove(removedAt) {
				t.Error("a game waiting could not be taken out")
			}
		}
		if i == 0 && state == StateOK && q.Remove(0) {
			t.Error("a game done was taken out")
		}
	}}
	sum := r.RunQueue(context.Background(), q, o)
	if sum.Done != 3 || sum.Skipped+sum.Failed+sum.Cancelled != 0 {
		t.Fatalf("%+v, %v", sum, order)
	}
	for _, state := range order {
		if state[0] == byte('0'+removedAt) {
			t.Errorf("the game taken out was told: %v", order)
		}
	}
	if order[len(order)-1] != "3:ok" {
		t.Errorf("the last added is not the last done: %v", order)
	}
	if _, ok := q.Add(first...); ok {
		t.Error("added to a queue that ended")
	}
	if p, ok := q.Plan(1); !ok || p.Item.TitleID != "PPSA10002" {
		t.Errorf("Plan(1): %+v %v", p, ok)
	}
}

// A queue cancelled cancels the games added to it too.
func TestQueueCancelledCancelsTheAdded(t *testing.T) {
	t.Setenv(fakecore.Env, "writes-stubborn")
	shortGrace(t)
	o := Options{Mode: ModeP5z, Dest: t.TempDir()}
	q := NewQueue(PlanCompress([]Item{item("Starfall Odyssey", "PPSA10001", 10)}, o, newFS()))
	q.Add(PlanCompress([]Item{item("Iron Harbor", "PPSA10002", 10)}, o, newFS())...)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &Runner{Core: os.Args[0], OnItem: func(i int, state, detail string) {
		if state == StatePacking {
			cancel()
		}
	}}
	if sum := r.RunQueue(ctx, q, o); sum.Cancelled != 2 {
		t.Errorf("%+v", sum)
	}
}

// A game added with its original kept keeps it, whatever the queue says of the others.
func TestAddedKeepsItsOriginal(t *testing.T) {
	t.Setenv(fakecore.Env, "writes")
	useBin(t, &fakeBin{})
	it := source(t, "Starfall Odyssey", "PPSA10001")
	o := Options{Mode: ModeP5z, Dest: t.TempDir(), Originals: OriginalsDelete}
	plans := PlanCompress([]Item{it}, o, newFS())
	plans[0].KeepOriginal = true
	if sum := (&Runner{Core: os.Args[0]}).Run(context.Background(), plans, o); sum.Done != 1 || sum.Deleted != 0 {
		t.Errorf("%+v", sum)
	}
	if _, err := os.Stat(it.Path); err != nil {
		t.Errorf("the original went: %v", err)
	}
}

// The next game, or that none is left, is told in one look at the queue (M11): while paused, a game that is left
// is not taken (the Runner holds), and one added meanwhile is never taken without that hold; with none left the
// queue closes, paused or not.
func TestQueueNextWhilePaused(t *testing.T) {
	q := NewQueue([]Plan{{Out: "a"}})
	if _, _, more, wait := q.take(true); !more || !wait {
		t.Fatalf("paused, a game left: more %v, wait %v", more, wait)
	}
	if i, p, more, wait := q.take(false); !more || wait || i != 0 || p.Out != "a" {
		t.Fatalf("going on: %d %+v %v %v", i, p, more, wait)
	}
	if first, ok := q.Add(Plan{Out: "b"}); !ok || first != 1 {
		t.Fatalf("added: %d %v", first, ok)
	}
	if _, _, more, wait := q.take(true); !more || !wait {
		t.Errorf("one added while paused is taken without a hold")
	}
	q.take(false)
	if _, _, more, wait := q.take(true); more || wait {
		t.Errorf("none left: more %v, wait %v", more, wait)
	}
	if _, ok := q.Add(Plan{Out: "c"}); ok {
		t.Error("the queue is not closed")
	}
}
