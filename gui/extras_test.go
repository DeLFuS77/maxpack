// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/jobs"
	"github.com/DeLFuS77/maxpack/gui/settings"
)

type realFS struct{}

func (realFS) Free(string) (uint64, error) { return 1 << 40, nil }
func (realFS) Type(string) (string, error) { return "NTFS", nil }
func (realFS) Root(string) string          { return "" }

func needCore(t *testing.T) (engine, fixtures string) {
	t.Helper()
	engine, fixtures = os.Getenv("MAXPACK_CORE"), os.Getenv("MP_FIXTURES")
	if engine == "" || fixtures == "" {
		t.Skip("MAXPACK_CORE and MP_FIXTURES are missing")
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
		t.Fatalf("at the start: %v", log)
	}
	if !strings.Contains(strings.Join(log, " "), "release") {
		t.Fatalf("not released: %v", log)
	}
	if len(progress) < 2 || total == 0 {
		t.Fatalf("progress: %v of %d", progress, total)
	}
	for i := 1; i < len(progress); i++ {
		if progress[i] < progress[i-1] {
			t.Fatalf("progress goes down: %v", progress)
		}
	}
	if progress[len(progress)-1] != total {
		t.Fatalf("does not reach the end: %v of %d", progress, total)
	}
	if !strings.Contains(strings.Join(log, " "), "taskbar:none") {
		t.Fatalf("the bar is not cleared: %v", log)
	}
	if notices != 1 || title != "MaxPack" || !strings.Contains(body, "1 juego listo") || !strings.Contains(body, sizeText("es", sum.Saved)+" ahorrados") {
		t.Fatalf("notice: %d %q %q", notices, title, body)
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
	// the size as the page writes it: what was saved in its own unit, not always in GB
	small := jobs.Summary{Done: 1, Saved: 500 << 20}
	if _, es := noticeText("es", small); es != "1 juego listo, 500,0 MB ahorrados" {
		t.Errorf("es: %q", es)
	}
	if _, en := noticeText("en", small); en != "1 game done, 500.0 MB saved" {
		t.Errorf("en: %q", en)
	}
}

// The notice says the same in every language of the window: the numbers, the size in the
// language's own decimal mark, and no word left over from another language.
func TestNoticeTextInEveryLanguage(t *testing.T) {
	s := jobs.Summary{Done: 4, Failed: 1, Cancelled: 3, Saved: 3_758_096_384}
	for _, lang := range settings.Languages {
		title, body := noticeText(lang, s)
		mark := "3.5"
		if decimalComma[lang] {
			mark = "3,5"
		}
		if title != "MaxPack" || !strings.Contains(body, "4") || !strings.Contains(body, "1") || !strings.Contains(body, "3") || !strings.Contains(body, mark+" GB") {
			t.Errorf("%s: %q", lang, body)
		}
		// without anything saved, failed or cancelled it is only the games done
		_, plain := noticeText(lang, jobs.Summary{Done: 1})
		if plain == "" || strings.Contains(plain, ",") && lang == "en" {
			t.Errorf("%s: %q", lang, plain)
		}
		for _, other := range settings.Languages {
			if other != lang && body == func() string { _, b := noticeText(other, s); return b }() {
				t.Errorf("%s and %s say the same: %q", lang, other, body)
			}
		}
	}
	// plurals of the languages that have them
	if _, ru := noticeText("ru", jobs.Summary{Done: 2}); ru != "2 игры готовы" {
		t.Errorf("ru 2: %q", ru)
	}
	if _, ru := noticeText("ru", jobs.Summary{Done: 5}); ru != "5 игр готово" {
		t.Errorf("ru 5: %q", ru)
	}
	if _, ru := noticeText("ru", jobs.Summary{Done: 21}); ru != "21 игра готова" {
		t.Errorf("ru 21: %q", ru)
	}
	if _, fr := noticeText("fr", jobs.Summary{Done: 1}); fr != "1 jeu terminé" {
		t.Errorf("fr: %q", fr)
	}
	if _, ar := noticeText("ar", jobs.Summary{Done: 2}); ar != "اكتمل لعبتان" {
		t.Errorf("ar: %q", ar)
	}
	if _, ja := noticeText("ja", jobs.Summary{Done: 2, Saved: 500 << 20}); ja != "2 件完了、500.0 MB 節約" {
		t.Errorf("ja: %q", ja)
	}
	// a language the window does not have says it in English
	if _, xx := noticeText("xx", jobs.Summary{Done: 1}); xx != "1 game done" {
		t.Errorf("xx: %q", xx)
	}
}

// TestSizeText: sizes as size() of the page (and NXPress) writes them: steps of 1024, B with no
// decimals, one decimal from KB on, the decimal comma in Spanish, no thousands separator.
func TestSizeText(t *testing.T) {
	cases := []struct {
		bytes  uint64
		es, en string
	}{
		{0, "0 B", "0 B"},
		{1023, "1023 B", "1023 B"},
		{1024, "1,0 KB", "1.0 KB"},
		{1536, "1,5 KB", "1.5 KB"},
		{10_200, "10,0 KB", "10.0 KB"},
		{3_147_695, "3,0 MB", "3.0 MB"},
		{1023 << 20, "1023,0 MB", "1023.0 MB"},
		{3_758_096_384, "3,5 GB", "3.5 GB"},
		{9_006_000_000, "8,4 GB", "8.4 GB"},
		{1 << 40, "1,0 TB", "1.0 TB"},
		{3 << 50, "3072,0 TB", "3072.0 TB"},
	}
	for _, c := range cases {
		for _, lang := range settings.Languages {
			want := c.en
			if decimalComma[lang] {
				want = c.es
			}
			if lang == "ar" {
				want = "\u2066" + c.en + "\u2069" // the number keeps its order in right-to-left text
			}
			if got := sizeText(lang, c.bytes); got != want {
				t.Errorf("%s %d: %q, expected %q", lang, c.bytes, got, want)
			}
		}
		if got := sizeText("es", c.bytes); got != c.es {
			t.Errorf("es %d: %q, expected %q", c.bytes, got, c.es)
		}
		if got := sizeText("en", c.bytes); got != c.en {
			t.Errorf("en %d: %q, expected %q", c.bytes, got, c.en)
		}
	}
}

// The taskbar counts a game as the page does: its steps, and the phases of a step one after the
// other over the size of the game. It is still below half when packing starts after copying the
// loose files, and it gets to the end only once the game ends.
func TestTaskbarFollowsThePhases(t *testing.T) {
	near := func(got, want float64) bool { return math.Abs(got-want) < 1e-9 }
	q := newShare([]jobs.Plan{{Item: jobs.Item{OriginalSize: 1000}}}, jobs.ModeP5z, 0)
	steps := []struct {
		state, phase string
		done, total  uint64
		want         float64
	}{
		{jobs.StatePacking, "", 0, 0, 0},
		{"", "copying", 50, 100, 0.025},
		{"", "copying", 100, 100, 0.05},
		{"", "packing", 0, 900, 0.05},
		{"", "packing", 450, 900, 0.275},
		{"", "packing", 900, 900, 0.5},
		{jobs.StateVerifying, "", 0, 0, 0.5},
		{"", "verifying", 300, 600, 0.75},
		{"", "verifying", 599, 600, 0.5 + 0.5*599/600},
		{jobs.StateOK, "", 0, 0, 1},
	}
	for n, s := range steps {
		if s.state != "" {
			q.item(0, s.state)
		} else {
			q.progress(0, s.phase, s.done, s.total)
		}
		if got := q.value(); !near(got, s.want) {
			t.Fatalf("step %d (%s %s %d/%d): %v, expected %v", n, s.state, s.phase, s.done, s.total, got, s.want)
		}
	}
	// two games: each weighs its size, and what was shown never goes back
	q = newShare([]jobs.Plan{{Item: jobs.Item{OriginalSize: 1000}}, {Item: jobs.Item{OriginalSize: 3000}}}, jobs.ModeFolder, 0)
	q.item(0, jobs.StatePacking)
	q.item(0, jobs.StateOK)
	if got := q.value(); !near(got, 0.25) {
		t.Fatalf("the first of two: %v", got)
	}
	q.item(1, jobs.StatePacking)
	if got := q.value(); !near(got, 0.25) {
		t.Fatalf("the second starts: %v", got)
	}
}

// With the engine: when the loose files are copied and packing starts, the taskbar is still
// below half; it is at half when the verification starts, and at the end only at the end.
func TestRunTaskbarPhases(t *testing.T) {
	engine, fixtures := needCore(t)
	item, err := jobs.Describe(engine, filepath.Join(fixtures, "basic.exfat"))
	if err != nil {
		t.Fatal(err)
	}
	o := jobs.Options{Mode: jobs.ModeP5z, Dest: t.TempDir(), Level: 3}
	plans := jobs.PlanCompress([]jobs.Item{item}, o, realFS{})
	var mu sync.Mutex
	var last, total uint64
	var copied, atVerifying []uint64
	a := &App{core: engine, lang: func() string { return "es" }, extras: extras{
		Awake: func() func() { return func() {} },
		Taskbar: func(state string, done, tot uint64) {
			mu.Lock()
			defer mu.Unlock()
			if state == "normal" {
				last, total = done, tot
			}
		},
		Notify: func(string, string) {},
	}}
	// told after the taskbar: what it shows with that line counted
	a.onEvent = func(i int, e core.Event) {
		mu.Lock()
		defer mu.Unlock()
		if e.Phase == "copying" && e.Done == e.Total {
			copied = append(copied, last)
		}
	}
	a.onItem = func(i int, state, detail string) {
		mu.Lock()
		defer mu.Unlock()
		if state == jobs.StateVerifying {
			atVerifying = append(atVerifying, last)
		}
	}
	if sum := a.run(context.Background(), plans, o); sum.Done != 1 {
		t.Fatalf("%+v", sum)
	}
	if len(copied) != 1 || len(atVerifying) != 1 || total == 0 {
		t.Fatalf("copied %v, verify %v, of %d", copied, atVerifying, total)
	}
	if copied[0] >= total/2 {
		t.Errorf("loose files copied, on starting to pack, it is already at %d of %d", copied[0], total)
	}
	if atVerifying[0] > total/2 {
		t.Errorf("on starting to verify it is already at %d of %d", atVerifying[0], total)
	}
	if last != total {
		t.Errorf("at the end: %d of %d", last, total)
	}
}
