// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

// The repair data with the real engine and the real maxpack-par2 (MAXPACK_CORE, MP_FIXTURES and
// MAXPACK_PAR2; the CI sets them once it built both): a test image is packed to a .p5z with 5 % of
// repair data, damaged and repaired back to its bytes.

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/internal/par2"
)

func needPar2(t *testing.T) (engine, fixtures, program string) {
	t.Helper()
	engine, fixtures = needCore(t)
	program = os.Getenv("MAXPACK_PAR2")
	if program == "" {
		t.Skip("MAXPACK_PAR2 is missing: maxpack-par2 is needed")
	}
	return engine, fixtures, program
}

// protectedP5z packs a test image to a .p5z with repair data, as Compress does, and gives it.
func protectedP5z(t *testing.T, engine, program, image, dest string, percent int) Item {
	t.Helper()
	os.MkdirAll(dest, 0o755)
	it := describe(t, engine, image)
	o := Options{Mode: ModeP5z, Dest: dest, Repair: percent}
	plans := PlanCompress([]Item{it}, o, newFS())
	rec := newRecorder()
	sum := (&Runner{Core: engine, Par2: program, OnItem: rec.item}).Run(context.Background(), plans, o)
	if state, detail := rec.last(0); state != StateOK || detail != "" || sum.Protected != 1 {
		t.Fatalf("compress: %s %q %+v", state, detail, sum)
	}
	if info := par2.Inspect(plans[0].Out); info.State != par2.StateMatch || info.Percent != percent {
		t.Fatalf("its set: %+v", info)
	}
	return describe(t, engine, plans[0].Out)
}

// damage flips the bytes of a file at each offset, 64 of them.
func damage(t *testing.T, path string, offsets ...int64) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, at := range offsets {
		chunk := make([]byte, 64)
		f.ReadAt(chunk, at)
		for i := range chunk {
			chunk[i] ^= 0xA5
		}
		if _, err := f.WriteAt(chunk, at); err != nil {
			t.Fatal(err)
		}
	}
}

func repairRun(t *testing.T, engine, program string, it Item) (*recorder, Summary) {
	t.Helper()
	o := Options{Mode: ModeRepair}
	plans := PlanRepair([]Item{it}, o, newFS())
	if plans[0].Block != "" {
		t.Fatalf("repair blocked: %q", plans[0].Block)
	}
	rec := newRecorder()
	sum := (&Runner{Core: engine, Par2: program, OnItem: rec.item, OnEvent: rec.event}).Run(context.Background(), plans, o)
	return rec, sum
}

func noRepairFolder(t *testing.T, it Item) {
	t.Helper()
	if _, err := os.Stat(it.Path + RepairSuffix); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the folder of the repair is left: %v", err)
	}
}

func TestRepairForReal(t *testing.T) {
	engine, fixtures, program := needPar2(t)
	it := protectedP5z(t, engine, program, filepath.Join(fixtures, "basic.exfat"), t.TempDir(), 5)
	original, _ := os.ReadFile(it.Path)
	size := int64(len(original))
	info := par2.Inspect(it.Path)

	// three damaged places, each in a block of its own: the engine finds them, par2 repairs them
	damage(t, it.Path, size/5, size/2, size-200)
	if _, code, err := core.Run(context.Background(), engine, []string{"verify", it.Path}, nil); err != nil || code == 0 {
		t.Fatalf("the damage is not found: %d %v", code, err)
	}
	rec, sum := repairRun(t, engine, program, it)
	if state, detail := rec.last(0); state != StateOK || detail != "repaired" || sum.Repaired != 1 {
		t.Fatalf("%v %q %+v", rec.states[0], detail, sum)
	}
	if got := strings.Join(rec.states[0], ","); got != "waiting,repairing,verifying,ok" {
		t.Errorf("states %s", got)
	}
	if !strings.Contains(strings.Join(rec.phases[0], ","), "repairing") {
		t.Errorf("phases %v", rec.phases[0])
	}
	if repaired, _ := os.ReadFile(it.Path); !bytes.Equal(repaired, original) {
		t.Error("the repaired .p5z is not the one made")
	}
	noRepairFolder(t, it)
	if got := par2.Inspect(it.Path); got.State != par2.StateMatch {
		t.Errorf("its set: %+v", got)
	}

	// more damaged blocks than there are recovery blocks: it cannot be repaired, and it stays as it is
	var offsets []int64
	for n := 0; n <= info.Recovery; n++ {
		offsets = append(offsets, int64(n)*size/int64(info.Recovery+2)+100)
	}
	damage(t, it.Path, offsets...)
	damaged, _ := os.ReadFile(it.Path)
	rec, _ = repairRun(t, engine, program, it)
	if state, detail := rec.last(0); state != StateError || !hasPart(detail, "repair_not_possible") {
		t.Errorf("too much damage: %s %q", state, detail)
	}
	if now, _ := os.ReadFile(it.Path); !bytes.Equal(now, damaged) {
		t.Error("a .p5z that could not be repaired changed")
	}
	noRepairFolder(t, it)
}

// A .p5z renamed with Rename, its set with it, still repairs: par2 is given it by its own name.
func TestRepairRenamedForReal(t *testing.T) {
	engine, fixtures, program := needPar2(t)
	dir := t.TempDir()
	it := protectedP5z(t, engine, program, filepath.Join(fixtures, "basic.exfat"), dir, 5)
	original, _ := os.ReadFile(it.Path)
	results := Rename(PlanRename([]Item{it}, "{title} [{id}] [v{version}]"))
	if results[0].State != StateOK {
		t.Fatalf("rename: %+v", results[0])
	}
	renamed := it
	renamed.Path = results[0].To
	if info := par2.Inspect(renamed.Path); info.State != par2.StateMatch || info.Name != filepath.Base(it.Path) {
		t.Fatalf("the renamed one: %+v", info)
	}
	// par2 verifies it by its own name, wherever its set says it was
	meter := par2.NewMeter(par2.Making)
	if code, err := par2.Run(context.Background(), program, par2.VerifyArgs(par2.IndexOf(renamed.Path), renamed.Path, 0), false, meter); err != nil || code > par2.ExitRepairable {
		t.Errorf("par2 verify of the renamed one: %d %v", code, err)
	}
	size := int64(len(original))
	damage(t, renamed.Path, size/3, size-500)
	rec, _ := repairRun(t, engine, program, renamed)
	if state, detail := rec.last(0); state != StateOK || detail != "repaired" {
		t.Fatalf("%s %q", state, detail)
	}
	if now, _ := os.ReadFile(renamed.Path); !bytes.Equal(now, original) {
		t.Error("the repaired .p5z is not the one made")
	}
	// nothing takes the old name
	if _, err := os.Stat(it.Path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the old name is back: %v", err)
	}
	noRepairFolder(t, renamed)
}

// The set of another file is never used, even named as the .p5z's own.
func TestOtherSetNeverUsedForReal(t *testing.T) {
	engine, fixtures, program := needPar2(t)
	dir := t.TempDir()
	a := protectedP5z(t, engine, program, filepath.Join(fixtures, "basic.exfat"), filepath.Join(dir, "a"), 5)
	b := protectedP5z(t, engine, program, filepath.Join(fixtures, "cid.exfat"), filepath.Join(dir, "b"), 5)
	// the set of a takes the names of the set of b
	for _, file := range par2.Files(b.Path) {
		os.Remove(file)
	}
	for _, file := range par2.Files(a.Path) {
		if err := os.Rename(file, filepath.Join(filepath.Dir(b.Path), filepath.Base(b.Path)+strings.TrimPrefix(filepath.Base(file), filepath.Base(a.Path)))); err != nil {
			t.Fatal(err)
		}
	}
	if info := par2.Inspect(b.Path); info.State != par2.StateOther {
		t.Fatalf("%+v", info)
	}
	before, _ := os.ReadFile(b.Path)
	damage(t, b.Path, int64(len(before))/2)
	damaged, _ := os.ReadFile(b.Path)
	o := Options{Mode: ModeRepair}
	plans := PlanRepair([]Item{b}, o, newFS())
	if plans[0].Block != "repair_data_other" {
		t.Errorf("plan: %q", plans[0].Block)
	}
	// even asked to, the Runner does not repair it with that set
	plans[0].Block = ""
	on, state, detail := lastDetail()
	(&Runner{Core: engine, Par2: program, OnItem: on}).Run(context.Background(), plans, o)
	if *state != StateError || *detail != "repair_data_other" {
		t.Errorf("%s %q", *state, *detail)
	}
	if now, _ := os.ReadFile(b.Path); !bytes.Equal(now, damaged) {
		t.Error("the .p5z changed")
	}
}

// Making a set cancelled halfway leaves no part of it.
func TestCancelMakingTheSetForReal(t *testing.T) {
	_, _, program := needPar2(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "big.p5z")
	data := make([]byte, 256<<20)
	for i := range data {
		data[i] = byte(i*7 + i>>9)
	}
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &Runner{Par2: program, OnEvent: func(int, core.Event) { cancel() }}
	made, note := r.protect(ctx, 0, file, Options{Repair: 10})
	if made != 0 || note != "repair_data_cancelled" {
		t.Errorf("%d %q", made, note)
	}
	if got := names(t, dir); len(got) != 1 {
		t.Errorf("left: %v", got)
	}
}
