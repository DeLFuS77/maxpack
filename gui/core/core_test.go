// SPDX-License-Identifier: GPL-3.0-or-later

package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeLFuS77/maxpack/gui/internal/fakecore"
)

// needCore gives the engine and the fixtures of the environment, or skips the test.
func needCore(t *testing.T) (core, fixtures string) {
	t.Helper()
	core, fixtures = os.Getenv("MAXPACK_CORE"), os.Getenv("MP_FIXTURES")
	if core == "" || fixtures == "" {
		t.Skip("MAXPACK_CORE and MP_FIXTURES are missing: the engine and the test images are needed")
	}
	return core, fixtures
}

func TestRunProgressAndResult(t *testing.T) {
	core, fixtures := needCore(t)
	out := filepath.Join(t.TempDir(), "x.p5z")
	progress := 0
	result, code, err := Run(context.Background(), core,
		[]string{"pack", filepath.Join(fixtures, "basic.exfat"), "-o", out, "--p5z"},
		func(e Event) {
			if e.Kind == "progress" {
				progress++
			}
		})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 || result.Kind != "result" || result.Raw["ok"] != true {
		t.Fatalf("code %d, result %+v", code, result)
	}
	if progress == 0 {
		t.Fatal("no progress event arrived")
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatal(err)
	}
}

func TestRunCancel(t *testing.T) {
	core, fixtures := needCore(t)
	out := filepath.Join(t.TempDir(), "x.p5z")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result, code, err := Run(ctx, core,
		[]string{"pack", filepath.Join(fixtures, "basic.exfat"), "-o", out, "--p5z"},
		func(e Event) {
			if e.Kind == "progress" {
				cancel()
			}
		})
	if err != nil {
		t.Fatal(err)
	}
	if code != 130 || result.Raw["error"] != "cancelled" {
		t.Fatalf("code %d, result %+v", code, result)
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("the half-written .p5z was left")
	}
}

func TestLocateMissing(t *testing.T) {
	t.Setenv("MAXPACK_CORE", filepath.Join(t.TempDir(), "no-existe.exe"))
	_, err := Locate()
	if err == nil || !strings.Contains(err.Error(), "maxpack-core") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVersionInfoIcon(t *testing.T) {
	core, fixtures := needCore(t)
	version, err := Version(core)
	if err != nil || !strings.HasPrefix(version, "maxpack ") {
		t.Fatalf("version %q, %v", version, err)
	}
	pack := filepath.Join(t.TempDir(), "x.p5z")
	if _, code, err := Run(context.Background(), core, []string{"pack", filepath.Join(fixtures, "basic.exfat"), "-o", pack, "--p5z"}, nil); err != nil || code != 0 {
		t.Fatalf("pack: %d, %v", code, err)
	}
	if info, err := Info(core, pack); err != nil || len(info) == 0 {
		t.Fatalf("info: %v, %v", info, err)
	}
	// the test dump has no icon: Icon says so with an error
	if err := Icon(core, pack, filepath.Join(t.TempDir(), "i.png")); err == nil {
		t.Log("the dump has an icon")
	}
}

// TestMain lets the test binary act as an engine (see fakecore).
func TestMain(m *testing.M) {
	fakecore.Main()
	os.Exit(m.Run())
}

func TestRunKillsStubbornCore(t *testing.T) {
	t.Setenv(fakecore.Env, "stubborn")
	old := CancelGrace
	CancelGrace = 300 * time.Millisecond
	defer func() { CancelGrace = old }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	begin := time.Now()
	result, code, err := Run(ctx, os.Args[0], []string{"pack"}, func(e Event) { cancel() })
	if err != nil {
		t.Fatal(err)
	}
	if code != 130 || result.Raw["error"] != "cancelled" || result.Raw["ok"] != false {
		t.Fatalf("code %d, result %+v", code, result)
	}
	if time.Since(begin) > 5*time.Second {
		t.Fatalf("took %v", time.Since(begin))
	}
}

// CancelGrace is only for an engine that hangs: one that cancels a batch of 32 MiB zstd 19 frames
// on each thread, or takes away a big output from a slow USB, takes well over 5 s, and killing it
// then leaves its output half written.
func TestCancelGraceIsForAHungEngine(t *testing.T) {
	if CancelGrace < 2*time.Minute {
		t.Fatalf("CancelGrace = %v: an engine still cleaning up would be killed", CancelGrace)
	}
}

// A queue stopped because the window closes asks the engine to cancel and closes its input, but
// does not kill it, however long it takes: it takes away what it wrote on its own, even once the
// window is gone.
func TestRunClosingLeavesTheEngineToCleanUp(t *testing.T) {
	t.Setenv(fakecore.Env, "writes-slow-clean")
	t.Setenv("MP_FAKE_CLEAN_MS", "800")
	old := CancelGrace
	CancelGrace = 100 * time.Millisecond
	defer func() { CancelGrace = old }()
	out := filepath.Join(t.TempDir(), "x.p5z")
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	result, code, err := Run(ctx, os.Args[0], []string{"pack", "juego", "-o", out, "--p5z"}, func(e Event) { cancel(ErrClosing) })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(out + ".cleaned"); err != nil || code != 130 || result.Raw["error"] != "cancelled" {
		t.Fatalf("the engine did not end on its own: code %d, %+v, %v", code, result.Raw, err)
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the output is still there: %v", err)
	}
}

func TestCapabilities(t *testing.T) {
	core, _ := needCore(t)
	missing, err := Missing(core)
	if err != nil || len(missing) != 0 {
		t.Fatalf("this version's engine: missing %v, %v", missing, err)
	}
}

// An engine of another version: one of before --capabilities fails it, and one that lacks
// something says so.
func TestCapabilitiesOfAnotherVersion(t *testing.T) {
	t.Setenv(fakecore.Env, "old")
	if _, err := Missing(os.Args[0]); err == nil {
		t.Fatal("an engine without --capabilities gives no error")
	}
	t.Setenv(fakecore.Env, "partial")
	missing, err := Missing(os.Args[0])
	if err != nil || len(missing) == 0 {
		t.Fatalf("missing %v, %v", missing, err)
	}
	for _, m := range missing {
		if m == "progress_json" || m == "control_stdin" {
			t.Fatalf("%s does have it: %v", m, missing)
		}
	}
}

// With low priority the engine runs below the other programs, so the PC stays usable: below
// normal on Windows, nice 10 elsewhere. Without it, at the priority of the window.
func TestRunLowPriority(t *testing.T) {
	t.Setenv(fakecore.Env, "priority")
	for _, low := range []bool{false, true} {
		result, code, err := RunWith(context.Background(), os.Args[0], []string{"verify", "x"}, RunOptions{Low: low}, nil)
		if err != nil || code != 0 || result.Raw["ok"] != true {
			t.Fatalf("low=%v: code %d, %+v, %v", low, code, result.Raw, err)
		}
		got, _ := result.Raw["priority"].(string)
		if want := fakecore.NormalPriority(); !low && got != want {
			t.Errorf("without low priority the engine runs at %q, not %q", got, want)
		}
		if want := fakecore.LowPriority(); low && got != want {
			t.Errorf("with low priority the engine runs at %q, not %q", got, want)
		}
	}
}
