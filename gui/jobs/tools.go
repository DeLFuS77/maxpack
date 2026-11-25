// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

// Comparing the levels of a game and measuring this PC: two runs of the engine that write nothing
// of a game (`analyze --levels` and `measure`), told as they go and cancelled as a queue is.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/DeLFuS77/maxpack/gui/core"
)

// ErrCancelled is the error of a comparison or a measure that was cancelled.
var ErrCancelled = errors.New("cancelled")

// LevelRow is a level as the engine compared it on a game: the size of the .p5z and of the folder
// for the console it would make, the time packing the whole game takes on this PC for each, and how
// fast the pack went through the sample, in bytes of the game a second.
type LevelRow struct {
	Level                     int
	P5zSize, FolderSize       uint64
	P5zSeconds, FolderSeconds uint64
	Rate                      uint64
}

// levelJSON is a level as the engine writes it.
type levelJSON struct {
	Level         int    `json:"level"`
	P5zSize       uint64 `json:"p5z_size"`
	FolderSize    uint64 `json:"folder_size"`
	P5zSeconds    uint64 `json:"p5z_seconds"`
	FolderSeconds uint64 `json:"folder_seconds"`
	Rate          uint64 `json:"rate"`
}

// Levels is what comparing the levels of a game found: a row for each level, in increasing order,
// and what it was measured on.
type Levels struct {
	Rows         []LevelRow
	OriginalSize uint64
	SampleBytes  uint64
	// SampleRate is the share of what is packed that the sample was (1: all of it)
	SampleRate float64
	Threads    int
	args       []string
}

// Trial is a count of threads tried by a measure, and how fast it packed, in bytes a second.
type Trial struct {
	Threads int
	Rate    uint64
}

// trialJSON is a count tried as the engine writes it.
type trialJSON struct {
	Threads int    `json:"threads"`
	Rate    uint64 `json:"rate"`
}

// Measured is what measuring this PC found: every count of threads tried at the level, the one
// recommended (the fewest less than 5 % slower than the fastest) and the fastest; and the disk of
// the folder given: measured (its speeds in bytes a second), or why not (DiskError, "dest_missing"
// when the folder is not there); neither when none was given.
type Measured struct {
	CPUThreads, Level    int
	Trials               []Trial
	Recommended, Fastest int
	DiskMeasured         bool
	WriteRate, ReadRate  uint64
	DiskError            string
	args                 []string
}

// ThreadsToKeep is what Settings keeps of a measure: the threads recommended, or 0 (all the CPU's,
// the default) when it recommends them all or none.
func (m Measured) ThreadsToKeep() int {
	if m.Recommended <= 0 || m.Recommended >= m.CPUThreads {
		return 0
	}
	return m.Recommended
}

// measureSeconds, when set, is how long each part of a measure lasts (the tests make it short).
var measureSeconds = ""

// tool runs the engine with args, telling its progress and its other events, and gives its result.
func tool(ctx context.Context, engine string, args []string, onEvent func(core.Event), onProgress func(done, total uint64)) (map[string]any, error) {
	result, code, err := core.Run(ctx, engine, args, func(e core.Event) {
		if e.Kind == "progress" {
			if onProgress != nil {
				onProgress(e.Done, e.Total)
			}
		} else if onEvent != nil {
			onEvent(e)
		}
	})
	if code == 130 || ctx.Err() != nil {
		return nil, ErrCancelled
	}
	if err != nil {
		return nil, err
	}
	if ok, _ := result.Raw["ok"].(bool); !ok || code != 0 {
		if why, _ := result.Raw["error"].(string); why != "" {
			return nil, errors.New(why)
		}
		return nil, fmt.Errorf("exit_%d", code)
	}
	return result.Raw, nil
}

// into reads the fields of what the engine said into v, by their JSON names.
func into(raw map[string]any, v any) error {
	data, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// levelOf reads a level: the fields of a "level" line and of each level of the result are the same.
func levelOf(raw map[string]any) LevelRow {
	var r levelJSON
	into(raw, &r)
	return LevelRow(r)
}

// CompareLevels compares the levels of a game on this PC: the engine packs a sample of it at
// levels 3, 9, 15 and 19 with threads (0: all the CPU's), and nothing is written. Each level is told
// to onRow once measured and the progress to onProgress (either may be nil). Cancelled through ctx,
// it gives ErrCancelled.
func CompareLevels(ctx context.Context, engine string, it Item, threads int, onRow func(LevelRow), onProgress func(done, total uint64)) (Levels, error) {
	args := []string{"analyze", it.Path, "--levels"}
	if threads > 0 {
		args = append(args, "--threads", strconv.Itoa(threads))
	}
	args = append(args, core.PasscodeArgs()...)
	raw, err := tool(ctx, engine, args, func(e core.Event) {
		if e.Kind == "level" && onRow != nil {
			onRow(levelOf(e.Raw))
		}
	}, onProgress)
	if err != nil {
		return Levels{}, err
	}
	var got struct {
		OriginalSize uint64           `json:"original_size"`
		SampleBytes  uint64           `json:"sample_bytes"`
		SampleRate   float64          `json:"sample_rate"`
		Threads      int              `json:"threads"`
		Levels       []map[string]any `json:"levels"`
	}
	if err := into(raw, &got); err != nil {
		return Levels{}, err
	}
	levels := Levels{OriginalSize: got.OriginalSize, SampleBytes: got.SampleBytes, SampleRate: got.SampleRate, Threads: got.Threads, args: args}
	for _, row := range got.Levels {
		levels.Rows = append(levels.Rows, levelOf(row))
	}
	return levels, nil
}

// Measure measures this PC with the engine, with no game: how fast it packs at 25, 50, 75 and 100 %
// of the CPU's threads at the level, and the disk of folder (the destination), where a temporary file
// is written, read back and deleted; "" measures no disk, and a folder that is not there is told in
// DiskError. Each count tried is told to onTrial and the progress to onProgress (either may be nil).
// Cancelled through ctx, it gives ErrCancelled.
func Measure(ctx context.Context, engine, folder string, level int, onTrial func(Trial), onProgress func(done, total uint64)) (Measured, error) {
	missing := false
	if folder != "" {
		if info, err := os.Stat(folder); err != nil || !info.IsDir() {
			folder, missing = "", true
		}
	}
	args := []string{"measure"}
	if folder != "" {
		args = append(args, folder)
	}
	if level < 1 || level > 22 {
		level = 19
	}
	args = append(args, "--level", strconv.Itoa(level))
	if measureSeconds != "" {
		args = append(args, "--seconds", measureSeconds)
	}
	raw, err := tool(ctx, engine, args, func(e core.Event) {
		if e.Kind == "trial" && onTrial != nil {
			var t trialJSON
			into(e.Raw, &t)
			onTrial(Trial(t))
		}
	}, onProgress)
	if err != nil {
		return Measured{}, err
	}
	var got struct {
		CPUThreads  int         `json:"cpu_threads"`
		Level       int         `json:"level"`
		Trials      []trialJSON `json:"trials"`
		Recommended int         `json:"recommended_threads"`
		Fastest     int         `json:"fastest_threads"`
		Disk        *struct {
			WriteRate uint64 `json:"write_rate"`
			ReadRate  uint64 `json:"read_rate"`
			Error     string `json:"error"`
		} `json:"disk"`
	}
	if err := into(raw, &got); err != nil {
		return Measured{}, err
	}
	m := Measured{CPUThreads: got.CPUThreads, Level: got.Level, Trials: []Trial{}, Recommended: got.Recommended, Fastest: got.Fastest, args: args}
	for _, t := range got.Trials {
		m.Trials = append(m.Trials, Trial(t))
	}
	switch {
	case missing:
		m.DiskError = "dest_missing"
	case got.Disk != nil && got.Disk.Error != "":
		m.DiskError = got.Disk.Error
	case got.Disk != nil:
		m.DiskMeasured, m.WriteRate, m.ReadRate = true, got.Disk.WriteRate, got.Disk.ReadRate
	}
	return m, nil
}
