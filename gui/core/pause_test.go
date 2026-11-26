// SPDX-License-Identifier: GPL-3.0-or-later

package core

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeLFuS77/maxpack/gui/internal/fakecore"
)

// A pause holds Wait while it is on, until it is off or the context ends; none (nil) never holds.
func TestPauseWaits(t *testing.T) {
	var none *Pause
	if none.On() || none.Wait(context.Background()) != nil {
		t.Fatal("no pause holds")
	}
	p := NewPause()
	if err := p.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !p.Set(true) || p.Set(true) || !p.On() {
		t.Fatal("Set says whether it changed")
	}
	done := make(chan error, 1)
	go func() { done <- p.Wait(context.Background()) }()
	select {
	case <-done:
		t.Fatal("Wait did not wait while paused")
	case <-time.After(100 * time.Millisecond):
	}
	p.Set(false)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Wait did not end with the pause")
	}
	p.Set(true)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { done <- p.Wait(ctx) }()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled while paused: %v", err)
	}
}

// Listeners hear each change, and nothing once they stop listening.
func TestPauseListeners(t *testing.T) {
	p := NewPause()
	var heard []bool
	stop := p.Listen(func(on bool) { heard = append(heard, on) })
	p.Set(true)
	p.Set(true)
	p.Set(false)
	stop()
	p.Set(true)
	if len(heard) != 2 || !heard[0] || heard[1] {
		t.Fatalf("heard %v", heard)
	}
}

// pausing runs the fake engine that pauses as it is told (see fakecore), with what it read kept in a file.
func pausing(t *testing.T) (lines string) {
	t.Setenv(fakecore.Env, "pauses")
	lines = filepath.Join(t.TempDir(), "lines")
	t.Setenv(fakecore.LinesEnv, lines)
	return lines
}

func read(t *testing.T, path string) string {
	data, _ := os.ReadFile(path)
	return string(data)
}

// The engine at work is told "pause" and "resume" on its input as the pause changes; it goes on and ends well.
func TestRunPausesTheEngine(t *testing.T) {
	lines := pausing(t)
	p := NewPause()
	var kinds []string
	result, code, err := RunWith(context.Background(), os.Args[0], []string{"pack", "juego", "-o", filepath.Join(t.TempDir(), "x.p5z"), "--p5z"},
		RunOptions{Pause: p}, func(e Event) {
			kinds = append(kinds, e.Kind)
			switch e.Kind {
			case "progress":
				p.Set(true)
			case "paused":
				p.Set(false)
			}
		})
	if err != nil || code != 0 || result.Raw["ok"] != true {
		t.Fatalf("code %d, %+v, %v", code, result.Raw, err)
	}
	if got := strings.Join(kinds, " "); got != "progress paused resumed" {
		t.Fatalf("events %q", got)
	}
	if got := read(t, lines); got != "pause\nresume\n" {
		t.Fatalf("the engine read %q", got)
	}
}

// An engine started while the queue is paused is told so at once.
func TestRunStartsPaused(t *testing.T) {
	lines := pausing(t)
	p := NewPause()
	p.Set(true)
	result, code, err := RunWith(context.Background(), os.Args[0], []string{"unpack", "x.p5z"}, RunOptions{Pause: p}, func(e Event) {
		if e.Kind == "paused" {
			p.Set(false)
		}
	})
	if err != nil || code != 0 || result.Raw["ok"] != true {
		t.Fatalf("code %d, %+v, %v", code, result.Raw, err)
	}
	if got := read(t, lines); got != "pause\nresume\n" {
		t.Fatalf("the engine read %q", got)
	}
}

// A cancel while paused cancels the engine, as without the pause.
func TestRunCancelledWhilePaused(t *testing.T) {
	lines := pausing(t)
	p := NewPause()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result, code, err := RunWith(ctx, os.Args[0], []string{"pack", "juego", "-o", filepath.Join(t.TempDir(), "x.p5z"), "--p5z"},
		RunOptions{Pause: p}, func(e Event) {
			switch e.Kind {
			case "progress":
				p.Set(true)
			case "paused":
				cancel()
			}
		})
	if err != nil || code != 130 || result.Raw["error"] != "cancelled" {
		t.Fatalf("code %d, %+v, %v", code, result.Raw, err)
	}
	if got := read(t, lines); got != "pause\ncancel\n" {
		t.Fatalf("the engine read %q", got)
	}
	if p.Set(false); p.On() {
		t.Fatal("still paused")
	}
}

// Suspend stops every thread of a process (maxpack-par2 cannot be told to pause), and Resume lets it go on.
func TestSuspendAndResume(t *testing.T) {
	command := exec.Command(os.Args[0])
	command.Env = append(os.Environ(), fakecore.Env+"=ticks")
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer command.Wait()
	defer command.Process.Kill()
	var ticks atomic.Int64
	go func() {
		lines := bufio.NewScanner(stdout)
		for lines.Scan() {
			ticks.Add(1)
		}
	}()
	waitFor := func(what string, cond func() bool) {
		t.Helper()
		for end := time.Now().Add(10 * time.Second); !cond(); time.Sleep(5 * time.Millisecond) {
			if time.Now().After(end) {
				t.Fatal(what)
			}
		}
	}
	waitFor("it never ticked", func() bool { return ticks.Load() >= 5 })
	if err := Suspend(command.Process); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond) // what was in the pipe arrives
	at := ticks.Load()
	time.Sleep(400 * time.Millisecond)
	if now := ticks.Load(); now != at {
		t.Fatalf("it went on ticking while suspended: %d -> %d", at, now)
	}
	if err := Resume(command.Process); err != nil {
		t.Fatal(err)
	}
	waitFor("it did not go on once resumed", func() bool { return ticks.Load() > at+3 })
}

// The real engine, paused while it packs: the .p5z stops growing until it is resumed, and is whole after.
func TestRunPauseReal(t *testing.T) {
	core, _ := needCore(t)
	dump := filepath.Join(t.TempDir(), "PPSA00000")
	if err := os.MkdirAll(filepath.Join(dump, "sce_sys"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dump, "sce_sys", "param.json"), []byte(`{"titleId":"PPSA00000"}`), 0o644)
	data := make([]byte, 6<<20)
	for i := range data {
		data[i] = byte(uint32(i)*2654435761>>24) ^ byte(i>>13)
	}
	for _, name := range []string{"a.bin", "b.bin"} {
		if err := os.WriteFile(filepath.Join(dump, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(t.TempDir(), "x.p5z")
	p := NewPause()
	paused, resumed := make(chan struct{}), make(chan struct{})
	type ending struct {
		result Event
		code   int
		err    error
	}
	ended := make(chan ending, 1)
	go func() {
		var asked, once, twice bool
		result, code, err := RunWith(context.Background(), core, []string{"pack", dump, "-o", out, "--p5z", "--level", "19", "--threads", "1"},
			RunOptions{Pause: p}, func(e Event) {
				switch {
				case e.Kind == "progress" && e.Phase == "packing" && !asked:
					asked = true
					p.Set(true)
				case e.Kind == "paused" && !once:
					once = true
					close(paused)
				case e.Kind == "resumed" && !twice:
					twice = true
					close(resumed)
				}
			})
		ended <- ending{result, code, err}
	}()
	size := func() int64 {
		info, err := os.Stat(out)
		if err != nil {
			return -1
		}
		return info.Size()
	}
	select {
	case <-paused:
	case e := <-ended:
		t.Fatalf("it ended before it paused: %d %+v %v", e.code, e.result.Raw, e.err)
	case <-time.After(30 * time.Second):
		t.Fatal("it never said it paused")
	}
	time.Sleep(time.Second) // what was at work when it paused ends
	at := size()
	time.Sleep(time.Second)
	if now := size(); now != at {
		t.Fatalf("the .p5z grew while paused: %d -> %d", at, now)
	}
	p.Set(false)
	e := <-ended
	if e.err != nil || e.code != 0 || e.result.Raw["ok"] != true {
		t.Fatalf("code %d, %+v, %v", e.code, e.result.Raw, e.err)
	}
	select {
	case <-resumed:
	default:
		t.Fatal("it never said it resumed")
	}
	if result, code, err := Run(context.Background(), core, []string{"verify", out}, nil); err != nil || code != 0 || result.Raw["ok"] != true {
		t.Fatalf("the .p5z made after a pause does not verify: code %d, %+v, %v", code, result.Raw, err)
	}
}
