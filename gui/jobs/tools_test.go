// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeLFuS77/maxpack/gui/internal/fakecore"
)

// toolEvents gathers what a comparison or a measure tells as it goes.
type toolEvents struct {
	mutex    sync.Mutex
	rows     []LevelRow
	trials   []Trial
	progress [][2]uint64
}

func (e *toolEvents) row(r LevelRow) { e.mutex.Lock(); e.rows = append(e.rows, r); e.mutex.Unlock() }
func (e *toolEvents) trial(t Trial)  { e.mutex.Lock(); e.trials = append(e.trials, t); e.mutex.Unlock() }
func (e *toolEvents) step(done, total uint64) {
	e.mutex.Lock()
	e.progress = append(e.progress, [2]uint64{done, total})
	e.mutex.Unlock()
}

// The levels of a game as the engine compares them: each one told as it is measured, the table at the
// end, and the threads of Settings passed on (none: all of them).
func TestCompareLevelsFake(t *testing.T) {
	t.Setenv(fakecore.Env, "levels")
	it := Item{Path: filepath.Join(t.TempDir(), "Starfall"), Kind: "folder", OriginalSize: 1000}
	var ev toolEvents
	got, err := CompareLevels(context.Background(), os.Args[0], it, 6, ev.row, ev.step)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rows) != 2 || got.Rows[0].Level != 3 || got.Rows[1].Level != 19 || got.Rows[1].P5zSize != 400 ||
		got.Rows[1].FolderSize != 410 || got.Rows[1].P5zSeconds != 9 || got.Rows[1].FolderSeconds != 8 || got.Rows[1].Rate != 50 {
		t.Errorf("rows: %+v", got.Rows)
	}
	if got.OriginalSize != 1000 || got.Threads != 6 || got.SampleBytes != 700 || got.SampleRate != 1 {
		t.Errorf("%+v", got)
	}
	if len(ev.rows) != 2 || ev.rows[0].Level != 3 || len(ev.progress) == 0 || ev.progress[len(ev.progress)-1] != [2]uint64{2, 2} {
		t.Errorf("told: %+v %+v", ev.rows, ev.progress)
	}
	if want := "analyze " + it.Path + " --levels --threads 6"; !strings.Contains(strings.Join(got.args, " "), want) {
		t.Errorf("arguments: %q", got.args)
	}
	got, err = CompareLevels(context.Background(), os.Args[0], it, 0, nil, nil)
	if err != nil || strings.Contains(strings.Join(got.args, " "), "--threads") {
		t.Errorf("all threads: %q %v", got.args, err)
	}
}

// This PC measured: every count tried, the one recommended, and the disk of the folder; without a
// folder, no disk.
func TestMeasureFake(t *testing.T) {
	t.Setenv(fakecore.Env, "measure")
	dest := t.TempDir()
	var ev toolEvents
	got, err := Measure(context.Background(), os.Args[0], dest, 12, ev.trial, ev.step)
	if err != nil {
		t.Fatal(err)
	}
	if got.CPUThreads != 8 || got.Level != 12 || len(got.Trials) != 4 || got.Trials[2].Threads != 6 || got.Trials[2].Rate != 300 ||
		got.Recommended != 6 || got.Fastest != 8 || !got.DiskMeasured || got.WriteRate != 1000 || got.ReadRate != 2000 || got.DiskError != "" {
		t.Errorf("%+v", got)
	}
	if len(ev.trials) != 4 || ev.trials[3].Threads != 8 || len(ev.progress) == 0 {
		t.Errorf("told: %+v %+v", ev.trials, ev.progress)
	}
	if want := "measure " + dest + " --level 12"; !strings.Contains(strings.Join(got.args, " "), want) {
		t.Errorf("arguments: %q", got.args)
	}
	got, err = Measure(context.Background(), os.Args[0], "", 19, nil, nil)
	if err != nil || got.DiskMeasured || got.DiskError != "" || strings.Contains(strings.Join(got.args, " "), dest) {
		t.Errorf("no folder: %+v %v", got, err)
	}
	// a destination that is gone (a USB taken out): the CPU is measured, the disk is told so
	got, err = Measure(context.Background(), os.Args[0], filepath.Join(dest, "gone"), 19, nil, nil)
	if err != nil || got.DiskMeasured || got.DiskError != "dest_missing" || len(got.Trials) != 4 {
		t.Errorf("gone: %+v %v", got, err)
	}
}

// Cancelled, both stop and say so; an engine that fails says why.
func TestToolsCancelAndFail(t *testing.T) {
	t.Setenv(fakecore.Env, "waits")
	it := Item{Path: t.TempDir(), Kind: "folder"}
	for name, run := range map[string]func(ctx context.Context) error{
		"levels":  func(ctx context.Context) error { _, err := CompareLevels(ctx, os.Args[0], it, 0, nil, nil); return err },
		"measure": func(ctx context.Context) error { _, err := Measure(ctx, os.Args[0], "", 19, nil, nil); return err },
	} {
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(300*time.Millisecond, cancel)
		if err := run(ctx); !errors.Is(err, ErrCancelled) {
			t.Errorf("%s: %v", name, err)
		}
	}
	t.Setenv(fakecore.Env, "exists")
	if _, err := CompareLevels(context.Background(), os.Args[0], it, 0, nil, nil); err == nil || errors.Is(err, ErrCancelled) {
		t.Errorf("a failure: %v", err)
	}
}

// With the real engine: the levels of the test image, and a quick measure of this PC.
func TestToolsWithTheEngine(t *testing.T) {
	engine, fixtures := needCore(t)
	it := Item{Path: filepath.Join(fixtures, "basic.exfat"), Kind: "exfat"}
	var ev toolEvents
	got, err := CompareLevels(context.Background(), engine, it, 2, ev.row, ev.step)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rows) != 4 || got.Rows[3].Level != 19 || got.Threads != 2 || got.OriginalSize == 0 || len(ev.rows) != 4 {
		t.Fatalf("%+v", got)
	}
	for _, r := range got.Rows {
		if r.P5zSize == 0 || r.P5zSize >= got.OriginalSize || r.P5zSeconds < 1 || r.Rate == 0 {
			t.Errorf("level %d: %+v", r.Level, r)
		}
	}
	old := measureSeconds
	measureSeconds = "0.1"
	defer func() { measureSeconds = old }()
	dest := t.TempDir()
	m, err := Measure(context.Background(), engine, dest, 19, ev.trial, ev.step)
	if err != nil {
		t.Fatal(err)
	}
	if m.CPUThreads < 1 || len(m.Trials) == 0 || m.Recommended < 1 || !m.DiskMeasured || m.WriteRate == 0 || m.ReadRate == 0 {
		t.Errorf("%+v", m)
	}
	if left, _ := os.ReadDir(dest); len(left) != 0 {
		t.Errorf("left in the destination: %v", left)
	}
}

// What Settings keeps of a measure: the threads recommended, or 0 (all of them, the default) when it
// recommends them all.
func TestThreadsToKeep(t *testing.T) {
	if n := (Measured{CPUThreads: 32, Recommended: 24}).ThreadsToKeep(); n != 24 {
		t.Errorf("24 of 32: %d", n)
	}
	if n := (Measured{CPUThreads: 32, Recommended: 32}).ThreadsToKeep(); n != 0 {
		t.Errorf("all: %d", n)
	}
	if n := (Measured{CPUThreads: 8}).ThreadsToKeep(); n != 0 {
		t.Errorf("none recommended: %d", n)
	}
}
