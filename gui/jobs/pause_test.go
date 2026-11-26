// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/internal/fakecore"
)

// told keeps what the Runner told of each game, in order.
type told struct {
	mutex sync.Mutex
	lines []string
}

func (t *told) item(i int, state, detail string) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.lines = append(t.lines, string(rune('0'+i))+":"+state)
}

func (t *told) all() string {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	return strings.Join(t.lines, " ")
}

// runPaused runs the queue in the background; the summary comes on the channel.
func runPaused(r *Runner, ctx context.Context, plans []Plan, o Options) chan Summary {
	done := make(chan Summary, 1)
	go func() { done <- r.Run(ctx, plans, o) }()
	return done
}

// still says that the queue has not ended for a while, and gives what was told meanwhile.
func still(t *testing.T, done chan Summary, said *told) string {
	t.Helper()
	select {
	case sum := <-done:
		t.Fatalf("the queue went on while paused: %+v, %s", sum, said.all())
	case <-time.After(300 * time.Millisecond):
	}
	return said.all()
}

// A queue paused before it starts starts nothing (no engine runs) until it is resumed; then every game is done.
func TestPausedQueueStartsNothing(t *testing.T) {
	t.Setenv(fakecore.Env, "writes")
	o := Options{Mode: ModeP5z, Dest: t.TempDir()}
	plans := PlanCompress([]Item{item("Starfall Odyssey", "PPSA10001", 10), item("Iron Harbor", "PPSA10002", 10)}, o, newFS())
	pause := core.NewPause()
	pause.Set(true)
	said := &told{}
	done := runPaused(&Runner{Core: os.Args[0], OnItem: said.item, Pause: pause}, context.Background(), plans, o)
	if got := still(t, done, said); got != "0:waiting 1:waiting" {
		t.Fatalf("told while paused: %s", got)
	}
	pause.Set(false)
	if sum := <-done; sum.Done != 2 {
		t.Fatalf("%+v, %s", sum, said.all())
	}
}

// Paused once a game is done, the next one does not start; paused between the pack and its verify, the
// verify does not start. Resumed, both go on.
func TestPauseHoldsTheNextStep(t *testing.T) {
	t.Setenv(fakecore.Env, "writes")
	for _, at := range []string{"0:" + StateOK, "0:" + StateVerifying} {
		o := Options{Mode: ModeP5z, Dest: t.TempDir()}
		plans := PlanCompress([]Item{item("Starfall Odyssey", "PPSA10001", 10), item("Iron Harbor", "PPSA10002", 10)}, o, newFS())
		pause := core.NewPause()
		said := &told{}
		r := &Runner{Core: os.Args[0], Pause: pause, OnItem: func(i int, state, detail string) {
			said.item(i, state, detail)
			if string(rune('0'+i))+":"+state == at {
				pause.Set(true)
			}
		}}
		done := runPaused(r, context.Background(), plans, o)
		got := still(t, done, said)
		if !strings.HasSuffix(got, at) {
			t.Fatalf("paused at %s, then told %s", at, got)
		}
		pause.Set(false)
		if sum := <-done; sum.Done != 2 {
			t.Fatalf("%s: %+v, %s", at, sum, said.all())
		}
	}
}

// The engine at work is paused and resumed (see core.RunWith), and the game ends well.
func TestPauseTheGameAtWork(t *testing.T) {
	t.Setenv(fakecore.Env, "pauses")
	lines := filepath.Join(t.TempDir(), "lines")
	t.Setenv(fakecore.LinesEnv, lines)
	o := Options{Mode: ModeP5z, Dest: t.TempDir()}
	plans := PlanCompress([]Item{item("Starfall Odyssey", "PPSA10001", 10)}, o, newFS())
	pause := core.NewPause()
	r := &Runner{Core: os.Args[0], Pause: pause, OnEvent: func(i int, e core.Event) {
		switch e.Kind {
		case "progress":
			pause.Set(true)
		case "paused":
			pause.Set(false)
		}
	}}
	if sum := r.Run(context.Background(), plans, o); sum.Done != 1 {
		t.Fatalf("%+v", sum)
	}
	if data, _ := os.ReadFile(lines); string(data) != "pause\nresume\n" {
		t.Fatalf("the engine read %q", data)
	}
}

// A queue cancelled while paused cancels what it had, and starts nothing.
func TestCancelWhilePaused(t *testing.T) {
	t.Setenv(fakecore.Env, "writes")
	o := Options{Mode: ModeP5z, Dest: t.TempDir()}
	plans := PlanCompress([]Item{item("Starfall Odyssey", "PPSA10001", 10), item("Iron Harbor", "PPSA10002", 10)}, o, newFS())
	pause := core.NewPause()
	pause.Set(true)
	said := &told{}
	ctx, cancel := context.WithCancel(context.Background())
	done := runPaused(&Runner{Core: os.Args[0], OnItem: said.item, Pause: pause}, ctx, plans, o)
	still(t, done, said)
	cancel()
	select {
	case sum := <-done:
		if sum.Cancelled != 2 || sum.Done != 0 {
			t.Fatalf("%+v, %s", sum, said.all())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a cancel while paused did not end the queue")
	}
	for _, p := range plans {
		if _, err := os.Stat(p.Out); err == nil {
			t.Errorf("something was made of %s", p.Out)
		}
	}
}
