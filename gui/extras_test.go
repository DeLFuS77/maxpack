// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/jobs"
)

type realFS struct{}

func (realFS) Free(string) (uint64, error) { return 1 << 40, nil }
func (realFS) Type(string) (string, error) { return "NTFS", nil }
func (realFS) Root(string) string          { return "" }

func needCore(t *testing.T) (engine, fixtures string) {
	t.Helper()
	engine, fixtures = os.Getenv("MAXPACK_CORE"), os.Getenv("MP_FIXTURES")
	if engine == "" || fixtures == "" {
		t.Skip("faltan MAXPACK_CORE y MP_FIXTURES")
	}
	return engine, fixtures
}

func TestRunExtras(t *testing.T) {
	engine, fixtures := needCore(t)
	item, err := jobs.Describe(engine, filepath.Join(fixtures, "basic.exfat"))
	if err != nil {
		t.Fatal(err)
	}
	o := jobs.Options{Mode: jobs.ModeP5z, Dest: t.TempDir(), Level: 3}
	plans := jobs.PlanCompress([]jobs.Item{item}, o, realFS{})

	var mu sync.Mutex
	var log []string
	var progress []uint64
	var total uint64
	var title, body string
	notices := 0
	a := &App{core: engine, lang: func() string { return "es" }, extras: extras{
		Awake: func() func() {
			mu.Lock()
			defer mu.Unlock()
			log = append(log, "awake")
			return func() { mu.Lock(); defer mu.Unlock(); log = append(log, "release") }
		},
		Taskbar: func(state string, done, tot uint64) {
			mu.Lock()
			defer mu.Unlock()
			log = append(log, "taskbar:"+state)
			if state == "normal" {
				progress = append(progress, done)
				total = tot
			}
		},
		Notify: func(ti, bo string) { mu.Lock(); defer mu.Unlock(); notices++; title, body = ti, bo },
	}}
	sum := a.run(context.Background(), plans, o)
	if sum.Done != 1 {
		t.Fatalf("%+v", sum)
	}
	if log[0] != "awake" || log[1] != "taskbar:normal" {
		t.Fatalf("al empezar: %v", log)
	}
	if !strings.Contains(strings.Join(log, " "), "release") {
		t.Fatalf("no se suelta: %v", log)
	}
	if len(progress) < 2 || total == 0 {
		t.Fatalf("progreso: %v de %d", progress, total)
	}
	for i := 1; i < len(progress); i++ {
		if progress[i] < progress[i-1] {
			t.Fatalf("el progreso baja: %v", progress)
		}
	}
	if progress[len(progress)-1] != total {
		t.Fatalf("no llega al final: %v de %d", progress, total)
	}
	if !strings.Contains(strings.Join(log, " "), "taskbar:none") {
		t.Fatalf("la barra no se limpia: %v", log)
	}
	if notices != 1 || title != "MaxPack" || !strings.Contains(body, "1") || !strings.Contains(body, "GB") {
		t.Fatalf("aviso: %d %q %q", notices, title, body)
	}
}

func TestRunExtrasFailureShowsError(t *testing.T) {
	var states []string
	notices := 0
	a := &App{core: filepath.Join(t.TempDir(), "no-existe.exe"), lang: func() string { return "en" }, extras: extras{
		Awake:   func() func() { return func() {} },
		Taskbar: func(state string, done, total uint64) { states = append(states, state) },
		Notify:  func(string, string) { notices++ },
	}}
	plans := []jobs.Plan{{Item: jobs.Item{Path: t.TempDir(), Name: "x", Kind: "folder"}, Out: filepath.Join(t.TempDir(), "x.p5z"), Args: []string{"pack"}}}
	sum := a.run(context.Background(), plans, jobs.Options{Mode: jobs.ModeP5z})
	if sum.Failed != 1 || states[len(states)-1] != "error" || notices != 1 {
		t.Fatalf("%+v %v %d", sum, states, notices)
	}
}

func TestNoticeText(t *testing.T) {
	s := jobs.Summary{Done: 2, Failed: 1, Saved: 3_758_096_384}
	title, es := noticeText("es", s)
	_, en := noticeText("en", s)
	if title != "MaxPack" || !strings.Contains(es, "2") || !strings.Contains(es, "error") || !strings.Contains(es, "3,5 GB") {
		t.Errorf("es: %q", es)
	}
	if !strings.Contains(en, "2 games") || !strings.Contains(en, "failed") || !strings.Contains(en, "3.5 GB") {
		t.Errorf("en: %q", en)
	}
}
