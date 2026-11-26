// SPDX-License-Identifier: GPL-3.0-or-later

package par2

import (
	"bytes"
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeLFuS77/maxpack/gui/core"
)

// The test binary stands in for maxpack-par2 when fakeEnv says how: "slow" tells some progress and
// waits a minute (to be killed); "fails" says why and ends with code 3; "ticks" says a line every 10 ms,
// for a minute.
const fakeEnv = "MP_FAKE_PAR2"

func TestMain(m *testing.M) {
	switch os.Getenv(fakeEnv) {
	case "slow":
		fmt.Print("Opening: a.p5z\r\nProcessing: 10.0%\r")
		time.Sleep(time.Minute)
		os.Exit(0)
	case "fails":
		fmt.Print("Invalid option specified: -x\r\n")
		os.Exit(3)
	case "ticks":
		for end := time.Now().Add(time.Minute); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
			fmt.Print("tick\r\n")
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func self(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exe
}

func TestLocate(t *testing.T) {
	dir := t.TempDir()
	program := writeFile(t, filepath.Join(dir, Name()), []byte("x"))
	t.Setenv("MAXPACK_PAR2", program)
	if got, err := Locate(); err != nil || got != program {
		t.Errorf("MAXPACK_PAR2: %q, %v", got, err)
	}
	t.Setenv("MAXPACK_PAR2", filepath.Join(dir, "missing"))
	if _, err := Locate(); err == nil || !strings.Contains(err.Error(), "MAXPACK_PAR2") {
		t.Errorf("a MAXPACK_PAR2 that is not there: %v", err)
	}
	t.Setenv("MAXPACK_PAR2", dir)
	if _, err := Locate(); err == nil {
		t.Error("a folder taken for maxpack-par2")
	}
	// next to the program: the test binary has none next to it
	t.Setenv("MAXPACK_PAR2", "")
	if _, err := Locate(); err == nil || !strings.Contains(err.Error(), "next to the program") {
		t.Errorf("none next to the program: %v", err)
	}
}

func TestRunTellsAndEnds(t *testing.T) {
	t.Setenv(fakeEnv, "fails")
	var lines []string
	meter := NewMeter(Making)
	meter.OnLine = func(line string) { lines = append(lines, line) }
	code, err := Run(context.Background(), self(t), nil, false, nil, meter)
	if err != nil || code != 3 || len(lines) != 1 || lines[0] != "Invalid option specified: -x" {
		t.Errorf("code %d, %v, %q", code, err, lines)
	}
	if _, err := Run(context.Background(), filepath.Join(t.TempDir(), "none"), nil, false, nil, NewMeter(Making)); err == nil {
		t.Error("a program that is not there ran")
	}
}

func TestRunCancelKills(t *testing.T) {
	t.Setenv(fakeEnv, "slow")
	ctx, cancel := context.WithCancel(context.Background())
	meter := NewMeter(Making)
	meter.OnProgress = func(uint64) { cancel() }
	start := time.Now()
	_, err := Run(ctx, self(t), nil, true, nil, meter)
	if !errors.Is(err, ErrCancelled) {
		t.Errorf("cancelled: %v", err)
	}
	if time.Since(start) > 20*time.Second {
		t.Errorf("it took %v to stop", time.Since(start))
	}
}

// needPar2 is the real maxpack-par2 (MAXPACK_PAR2), or the test is skipped.
func needPar2(t *testing.T) string {
	t.Helper()
	program := os.Getenv("MAXPACK_PAR2")
	if program == "" {
		t.Skip("MAXPACK_PAR2 is missing: maxpack-par2 is needed")
	}
	return program
}

func TestVersion(t *testing.T) {
	program := needPar2(t)
	v, err := Version(program)
	if err != nil || !strings.HasPrefix(v, "par2cmdline-turbo version ") {
		t.Errorf("%q, %v", v, err)
	}
}

// makeReal makes the set of file with the real maxpack-par2, as the window does: in a folder of its
// own, then next to the file.
func makeReal(t *testing.T, program, file string, percent int) {
	t.Helper()
	size := uint64(len(mustRead(t, file)))
	bs, _, rec := Blocks(size, percent)
	folder := file + ".maxpack-par2"
	os.MkdirAll(folder, 0o755)
	var out bytes.Buffer
	meter := NewMeter(Making)
	meter.OnLine = func(line string) { out.WriteString(line + "\n") }
	code, err := Run(context.Background(), program, CreateArgs(Create{File: file, Index: filepath.Join(folder, filepath.Base(file)+".par2"), BlockSize: bs, Recovery: rec}), false, nil, meter)
	if err != nil || code != 0 {
		t.Fatalf("par2 create: %d, %v: %s", code, err, out.String())
	}
	entries, _ := os.ReadDir(folder)
	for _, entry := range entries {
		if err := os.Rename(filepath.Join(folder, entry.Name()), filepath.Join(filepath.Dir(file), entry.Name())); err != nil {
			t.Fatal(err)
		}
	}
	os.Remove(folder)
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestCreateRealSet: the reader reads a set made by par2 as par2 means it, the window's names are
// par2's own, and SetSize is what the set takes (never less).
func TestCreateRealSet(t *testing.T) {
	program := needPar2(t)
	dir := t.TempDir()
	data := noise(3_000_000, 20)
	file := writeFile(t, filepath.Join(dir, "Juego é [PPSA10001].p5z"), data)
	makeReal(t, program, file, 5)
	info := Inspect(file)
	if info.State != StateMatch || info.Name != filepath.Base(file) || info.Size != uint64(len(data)) {
		t.Fatalf("%+v", info)
	}
	bs, source, rec := Blocks(uint64(len(data)), 5)
	if info.BlockSize != bs || info.Source != source || uint64(info.Recovery) != rec || info.Percent != 5 || info.Index == "" || len(info.Volumes) != 1 {
		t.Errorf("%+v, expected blocks of %d, %d and %d", info, bs, source, rec)
	}
	set, err := ReadSet(Files(file)...)
	if err != nil || set.Files[0].Hash != md5.Sum(data) || len(set.Files[0].Blocks) != int(source) {
		t.Errorf("%+v, %v", set, err)
	}
	if estimate := SetSize(uint64(len(data)), 5); estimate < info.Bytes || estimate > info.Bytes+info.Bytes/100+8<<10 {
		t.Errorf("the set takes %d, SetSize says %d", info.Bytes, estimate)
	}
	if want := filepath.Base(file) + ".vol"; !strings.HasPrefix(filepath.Base(info.Volumes[0]), want) {
		t.Errorf("recovery file %q", info.Volumes[0])
	}
}

// While the queue is paused, par2 is suspended (it cannot be told to pause): it says nothing more until
// the pause ends, and a cancel while suspended still ends it.
func TestRunSuspendedWhilePaused(t *testing.T) {
	t.Setenv(fakeEnv, "ticks")
	pause := core.NewPause()
	var mutex sync.Mutex
	ticks := 0
	meter := NewMeter(Making)
	meter.OnLine = func(string) {
		mutex.Lock()
		ticks++
		mutex.Unlock()
	}
	count := func() int {
		mutex.Lock()
		defer mutex.Unlock()
		return ticks
	}
	waitFor := func(what string, cond func() bool) {
		t.Helper()
		for end := time.Now().Add(10 * time.Second); !cond(); time.Sleep(5 * time.Millisecond) {
			if time.Now().After(end) {
				t.Fatal(what)
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ended := make(chan error, 1)
	go func() {
		_, err := Run(ctx, self(t), nil, false, pause, meter)
		ended <- err
	}()
	waitFor("it never ticked", func() bool { return count() >= 5 })
	pause.Set(true)
	time.Sleep(150 * time.Millisecond)
	at := count()
	time.Sleep(400 * time.Millisecond)
	if now := count(); now != at {
		t.Fatalf("par2 went on while paused: %d -> %d", at, now)
	}
	pause.Set(false)
	waitFor("par2 did not go on once resumed", func() bool { return count() > at+3 })
	pause.Set(true)
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-ended:
		if !errors.Is(err, ErrCancelled) {
			t.Fatalf("cancelled while suspended: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a suspended par2 was not stopped by the cancel")
	}
}
