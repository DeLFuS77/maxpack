// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/internal/fakecore"
)

const testCode = "482913"

func sendOptions() Options {
	return Options{Mode: ModeSend, Console: "192.168.1.20", ConsoleCode: testCode, Drive: "usb0"}
}

// A .p5z goes to the console with `maxpack send`: to the drive chosen, the code never on the command line
// (the engine gets it in its environment); nothing is written on the PC. Anything but a .p5z cannot go, nor
// anything at all without the console's address and code.
func TestPlanSend(t *testing.T) {
	dir := t.TempDir()
	p5z := filepath.Join(dir, "Game [PPSA10001].p5z")
	os.WriteFile(p5z, []byte("a .p5z"), 0o644)
	items := []Item{{Path: p5z, Kind: "p5z", Name: "Game", OriginalSize: 10 * gib}, {Path: dir, Kind: "folder"}}
	plans := PlanSend(items, sendOptions())
	p := plans[0]
	if p.Block != "" || p.Out != "" || p.Need != 0 || len(p.Replace) != 0 {
		t.Errorf("the .p5z: %+v", p)
	}
	if strings.Join(p.Args, " ") != "send "+p5z+" 192.168.1.20 --drive usb0" {
		t.Errorf("its args: %q", p.Args)
	}
	if slices.ContainsFunc(p.Args, func(a string) bool { return strings.Contains(a, testCode) }) {
		t.Errorf("the code is on the command line: %q", p.Args)
	}
	if plans[1].Block != "not_p5z" {
		t.Errorf("a folder: %+v", plans[1])
	}
	for _, o := range []Options{{Mode: ModeSend, ConsoleCode: testCode}, {Mode: ModeSend, Console: "192.168.1.20"}} {
		if p := PlanSend(items[:1], o)[0]; p.Block != "no_console" {
			t.Errorf("without the console's address or code (%+v): %+v", o, p)
		}
	}
	// no drive chosen: the engine's own (internal when the console has it)
	o := sendOptions()
	o.Drive = ""
	if p := PlanSend(items[:1], o)[0]; slices.Contains(p.Args, "--drive") {
		t.Errorf("no drive: %q", p.Args)
	}
}

// The Runner sends each .p5z, which the console then verifies: the states the page shows are sending,
// verifying (the console's) and ok, "sent"; the engine had the code in its environment.
func TestSendRun(t *testing.T) {
	t.Setenv(fakecore.Env, "sends")
	dir := t.TempDir()
	items := existing(t, dir, 2)
	o := sendOptions()
	rec := newRecorder()
	sum := (&Runner{Core: os.Args[0], OnItem: rec.item}).Run(context.Background(), PlanSend(items, o), o)
	for i := range items {
		if state, detail := rec.last(i); state != StateOK || detail != "sent" || strings.Join(rec.states[i], ",") != "waiting,sending,verifying,ok" {
			t.Errorf("game %d: %v %q", i, rec.states[i], detail)
		}
		said, _ := os.ReadFile(items[i].Path + ".sent")
		if string(said) != "to 192.168.1.20 drive usb0 code "+testCode {
			t.Errorf("what the engine was given: %q", said)
		}
	}
	if sum.Done != 2 || sum.Failed != 0 {
		t.Errorf("%+v", sum)
	}
}

// What the console refuses, or a .p5z that arrived damaged, is said with a code of its own (send_...),
// so that the page does not take them for Compress's; a .p5z another MaxPack is changing is "busy".
func TestSendFailures(t *testing.T) {
	t.Setenv(fakecore.Env, "sends")
	dir := t.TempDir()
	items := existing(t, dir, 1)
	o := sendOptions()
	for code, want := range map[string]string{"bad_code": "send_bad_code", "receive_off": "send_receive_off", "no_space": "send_no_space",
		"exists": "send_exists", "busy": "send_busy", "unreachable": "send_unreachable", "console_cancelled": "send_console_cancelled",
		"verify_failed": "send_verify_failed: ampr_zstd-001.pak", "p5z_busy": "busy", "invalid": "send_invalid", "fat32": "send_fat32",
		"console_stopped": "send_console_stopped", "stalled": "send_stalled", "write_error": "send_write_error",
		// a drive with no room where part of it is already (contract v3): kept there, to be deleted; a send this PC
		// slept through says so, not that nothing moved on the console
		"no_space+kept": "send_no_space_kept", "stalled+slept": "send_slept"} {
		t.Setenv(fakecore.SendFailEnv, code)
		rec := newRecorder()
		sum := (&Runner{Core: os.Args[0], OnItem: rec.item}).Run(context.Background(), PlanSend(items, o), o)
		if state, detail := rec.last(0); state != StateError || detail != want || sum.Failed != 1 {
			t.Errorf("%s: %s %q", code, state, detail)
		}
	}
}

// Cancelled while it sends: the engine stops and the console keeps what it got (the next send goes on from
// there): the game is cancelled, "send_kept". The window never asks the engine to delete it on a cancel.
func TestSendCancelled(t *testing.T) {
	t.Setenv(fakecore.Env, "sends-waits")
	dir := t.TempDir()
	items := existing(t, dir, 1)
	o := sendOptions()
	ctx, cancel := context.WithCancel(context.Background())
	rec := newRecorder()
	onItem := func(i int, state, detail string) {
		rec.item(i, state, detail)
		if state == StateSending {
			time.AfterFunc(200*time.Millisecond, cancel)
		}
	}
	sum := (&Runner{Core: os.Args[0], OnItem: onItem}).Run(ctx, PlanSend(items, o), o)
	if state, detail := rec.last(0); state != StateCancelled || detail != "send_kept" || sum.Cancelled != 1 {
		t.Errorf("%v %q %+v", rec.states[0], detail, sum)
	}
	if args, _ := os.ReadFile(items[0].Path + ".args"); strings.Contains(string(args), "--delete-on-cancel") {
		t.Errorf("the window asked the engine to delete on a cancel: %s", args)
	}
}

// An engine that does not stop when the send is cancelled is killed after core.CancelGrace: what arrived stays on
// the console as with any stop here (the window never asks it to delete), so the row says so and can delete it.
func TestSendKilledKeeps(t *testing.T) {
	t.Setenv(fakecore.Env, "stubborn")
	grace := core.CancelGrace
	core.CancelGrace = 300 * time.Millisecond
	defer func() { core.CancelGrace = grace }()
	dir := t.TempDir()
	items := existing(t, dir, 1)
	o := sendOptions()
	ctx, cancel := context.WithCancel(context.Background())
	rec := newRecorder()
	onItem := func(i int, state, detail string) {
		rec.item(i, state, detail)
		if state == StateSending {
			time.AfterFunc(200*time.Millisecond, cancel)
		}
	}
	sum := (&Runner{Core: os.Args[0], OnItem: onItem}).Run(ctx, PlanSend(items, o), o)
	if state, detail := rec.last(0); state != StateCancelled || detail != "send_kept" || sum.Cancelled != 1 {
		t.Errorf("%v %q %+v", rec.states[0], detail, sum)
	}
}
