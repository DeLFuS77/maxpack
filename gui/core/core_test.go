// SPDX-License-Identifier: GPL-3.0-or-later

package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// needCore gives the engine and the fixtures of the environment, or skips the test.
func needCore(t *testing.T) (core, fixtures string) {
	t.Helper()
	core, fixtures = os.Getenv("MAXPACK_CORE"), os.Getenv("MP_FIXTURES")
	if core == "" || fixtures == "" {
		t.Skip("faltan MAXPACK_CORE y MP_FIXTURES: se necesita el motor y las imágenes de prueba")
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
		t.Fatalf("código %d, resultado %+v", code, result)
	}
	if progress == 0 {
		t.Fatal("no llegó ningún evento de progreso")
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
		t.Fatalf("código %d, resultado %+v", code, result)
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("quedó el .p5z a medias")
	}
}

func TestLocateMissing(t *testing.T) {
	t.Setenv("MAXPACK_CORE", filepath.Join(t.TempDir(), "no-existe.exe"))
	_, err := Locate()
	if err == nil || !strings.Contains(err.Error(), "maxpack-core") {
		t.Fatalf("error inesperado: %v", err)
	}
}

func TestVersionInfoIcon(t *testing.T) {
	core, fixtures := needCore(t)
	version, err := Version(core)
	if err != nil || !strings.HasPrefix(version, "maxpack ") {
		t.Fatalf("versión %q, %v", version, err)
	}
	pack := filepath.Join(t.TempDir(), "x.p5z")
	if _, code, err := Run(context.Background(), core, []string{"pack", filepath.Join(fixtures, "basic.exfat"), "-o", pack, "--p5z"}, nil); err != nil || code != 0 {
		t.Fatalf("pack: %d, %v", code, err)
	}
	if info, err := Info(core, pack); err != nil || len(info) == 0 {
		t.Fatalf("info: %v, %v", info, err)
	}
	// el dump de prueba no trae icono: Icon lo dice con un error
	if err := Icon(core, pack, filepath.Join(t.TempDir(), "i.png")); err == nil {
		t.Log("el dump trae icono")
	}
}

// TestMain lets the test binary act as an engine that ignores cancel.
func TestMain(m *testing.M) {
	if os.Getenv("MP_FAKE_CORE") == "1" {
		fmt.Println(`{"event":"progress","phase":"packing","done":1,"total":2}`)
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestRunKillsStubbornCore(t *testing.T) {
	t.Setenv("MP_FAKE_CORE", "1")
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
		t.Fatalf("código %d, resultado %+v", code, result)
	}
	if time.Since(begin) > 5*time.Second {
		t.Fatalf("tardó %v", time.Since(begin))
	}
}
