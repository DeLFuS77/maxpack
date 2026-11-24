// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeLFuS77/maxpack/gui/internal/fakecore"
	"github.com/DeLFuS77/maxpack/gui/jobs"
	"github.com/DeLFuS77/maxpack/gui/settings"
)

// toolPage is a page that keeps what the window tells of a comparison or a measure.
type toolPage struct {
	mutex  sync.Mutex
	events map[string][]any
}

func (p *toolPage) emit(event string, data any) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if p.events == nil {
		p.events = map[string][]any{}
	}
	p.events[event] = append(p.events[event], data)
}

func (p *toolPage) count(event string) int {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	return len(p.events[event])
}

// Compress gives the engine the threads of Settings (from Measure this PC); none is all of them,
// and Decompress always takes all.
func TestQueueThreadsFromSettings(t *testing.T) {
	useSettings(t, t.TempDir())
	it := jobs.Item{Path: t.TempDir(), TitleID: "PPSA10001", Name: "Starfall Odyssey", Kind: "folder", OriginalSize: 10, HasZstdRuntime: true}
	a := &App{fs: realFS{}}
	has := func(args []string) bool { return strings.Contains(strings.Join(args, " "), "--threads 24") }
	plans, _, _ := a.plans("compress", []jobs.Item{it}, Choices{Options: jobs.Options{Mode: jobs.ModeP5z, Dest: t.TempDir(), Threads: 24}})
	if strings.Contains(strings.Join(plans[0].Args, " "), "--threads") {
		t.Errorf("the page chose the threads: %q", plans[0].Args)
	}
	saveSettings(t, func(s *settings.Settings) { s.Threads = 24 })
	plans, _, _ = a.plans("compress", []jobs.Item{it}, Choices{Options: jobs.Options{Mode: jobs.ModeP5z, Dest: t.TempDir()}})
	if !has(plans[0].Args) {
		t.Errorf("compress: %q", plans[0].Args)
	}
	p5z := jobs.Item{Path: touchFile(t, "Starfall.p5z"), TitleID: "PPSA10001", Name: "Starfall Odyssey", Kind: "p5z", OriginalSize: 10}
	plans, _, _ = a.plans("decompress", []jobs.Item{p5z}, Choices{Options: jobs.Options{Dest: t.TempDir()}})
	if strings.Contains(strings.Join(plans[0].Args, " "), "--threads") {
		t.Errorf("decompress: %q", plans[0].Args)
	}
}

func touchFile(t *testing.T, name string) string { return touch(t, t.TempDir(), name, 10) }

// Compare levels: the table of the engine, each level told as it comes and the progress, with the
// threads of Settings.
func TestCompareLevelsTellsThePage(t *testing.T) {
	useSettings(t, t.TempDir())
	saveSettings(t, func(s *settings.Settings) { s.Threads = 6 })
	t.Setenv(fakecore.Env, "levels")
	page := &toolPage{}
	a := &App{core: os.Args[0], emit: page.emit}
	got, err := a.CompareLevels(jobs.Item{Path: t.TempDir(), Kind: "folder"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rows) != 2 || got.Rows[1].Level != 19 || got.Threads != 6 {
		t.Errorf("%+v", got)
	}
	if page.count("tool:level") != 2 || page.count("tool:progress") != 2 {
		t.Errorf("told: %v", page.events)
	}
}

// Measure this PC: the result, and the threads recommended kept in Settings, which Compress then
// uses; all of them recommended keeps "all" (0).
func TestMeasureKeepsTheThreads(t *testing.T) {
	useSettings(t, t.TempDir())
	t.Setenv(fakecore.Env, "measure")
	page := &toolPage{}
	a := &App{core: os.Args[0], emit: page.emit}
	saveSettings(t, func(s *settings.Settings) { s.Level = 12 })
	got, err := a.MeasurePC(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got.Recommended != 6 || got.Level != 12 || !got.DiskMeasured || got.Threads != 6 {
		t.Errorf("%+v", got)
	}
	if saved, _ := settings.Load(); saved.Threads != 6 || saved.Level != 12 {
		t.Errorf("kept: %+v", saved)
	}
	if page.count("tool:trial") != 4 || page.count("tool:progress") != 4 {
		t.Errorf("told: %v", page.events)
	}
}

// One comparison or measure at a time, never with a queue: the other is refused until it ends, and
// a cancel ends it as cancelled.
func TestToolsOneAtATime(t *testing.T) {
	useSettings(t, t.TempDir())
	t.Setenv(fakecore.Env, "waits")
	a := &App{core: os.Args[0], emit: (&toolPage{}).emit, fs: realFS{}}
	ended := make(chan error, 1)
	go func() {
		_, err := a.CompareLevels(jobs.Item{Path: t.TempDir(), Kind: "folder"})
		ended <- err
	}()
	for start := time.Now(); !a.toolRunning(); time.Sleep(10 * time.Millisecond) {
		if time.Since(start) > 10*time.Second {
			t.Fatal("the comparison did not start")
		}
	}
	if _, err := a.MeasurePC(""); err == nil || !strings.Contains(err.Error(), "busy") {
		t.Errorf("a measure meanwhile: %v", err)
	}
	it := jobs.Item{Path: t.TempDir(), TitleID: "PPSA10001", Name: "Starfall Odyssey", Kind: "folder", OriginalSize: 10, HasZstdRuntime: true}
	if err := a.Start("compress", []jobs.Item{it}, Choices{Options: jobs.Options{Dest: t.TempDir()}}); err == nil {
		t.Error("a queue started during a measure")
	}
	a.CancelTool()
	select {
	case err := <-ended:
		if !errors.Is(err, jobs.ErrCancelled) {
			t.Errorf("cancelled: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the cancel did not stop it")
	}
	if a.toolRunning() {
		t.Error("still running")
	}
	// with a queue at work, no measure
	a.queue = &queue{section: "compress"}
	if _, err := a.CompareLevels(jobs.Item{Path: t.TempDir()}); err == nil {
		t.Error("a comparison during a queue")
	}
	a.queue = nil
}
