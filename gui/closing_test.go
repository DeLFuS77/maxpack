// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DeLFuS77/maxpack/gui/jobs"
)

// queueContext is the context of the queue running, as Start made it.
func queueContext(t *testing.T, a *App) context.Context {
	t.Helper()
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if a.queue == nil {
		t.Fatal("no queue running")
	}
	return a.queue.ctx
}

// Closing while a queue runs is asked by the page in its own dialog once it told its words, and
// the window stays open until the answer is to cancel and close; with nothing running it closes
// at once.
func TestCloseAskedWhileWorking(t *testing.T) {
	var asked int
	events := newPageEvents()
	a := pageApp(filepath.Join(t.TempDir(), "no-existe.exe"), events)
	a.emit = func(event string, data any) {
		if event == "close:ask" {
			asked++
			return
		}
		events.emit(event, data)
	}
	if a.holdClose(context.Background()) || asked != 0 {
		t.Fatal("with nothing running, the window does not close at once")
	}
	awake := make(chan struct{})
	a.extras.Awake = func() func() { <-awake; return func() {} }
	item := jobs.Item{Path: t.TempDir(), TitleID: "PPSA00001", Name: "x", Kind: "folder", OriginalSize: 1, HasZstdRuntime: true}
	if err := a.Start("compress", []jobs.Item{item}, Choices{Options: jobs.Options{Mode: jobs.ModeP5z, Dest: t.TempDir()}}); err != nil {
		t.Fatal(err)
	}
	if !a.pageAsksClose() {
		a.CloseTexts("¿Cancelar y cerrar?", "Cancelar y cerrar", "Seguir trabajando")
	}
	if !a.holdClose(context.Background()) || asked != 1 {
		t.Fatalf("closing with a queue running is not asked: %d", asked)
	}
	// answered «Cancelar y cerrar»: the window closes (and closed stops the queue)
	a.ConfirmClose()
	if a.holdClose(context.Background()) || asked != 1 {
		t.Fatal("the close confirmed is asked again")
	}
	// the window closes: the queue is cancelled before its game starts, and waited for
	ctx := queueContext(t, a)
	closedDone := make(chan struct{})
	go func() { a.closed(context.Background()); close(closedDone) }()
	<-ctx.Done()
	close(awake)
	<-closedDone
	if sum := events.wait(t); sum.Cancelled != 1 || a.working() {
		t.Fatalf("%+v, working %v", sum, a.working())
	}
}

// The words of the dialog of the system, for a page that cannot ask: the ones the page told,
// else the ones of the language of the settings.
func TestCloseQuestionInTheLanguage(t *testing.T) {
	a := &App{lang: func() string { return "es" }}
	if question, yes, no := a.closeQuestion(); question == "" || yes != "Cancelar y cerrar" || no != "Seguir trabajando" {
		t.Fatalf("%q %q %q", question, yes, no)
	}
	a = &App{lang: func() string { return "en" }}
	if _, yes, no := a.closeQuestion(); yes != "Cancel and close" || no != "Keep working" {
		t.Fatalf("%q %q", yes, no)
	}
	a.CloseTexts("Q", "Y", "N")
	if question, yes, no := a.closeQuestion(); question != "Q" || yes != "Y" || no != "N" {
		t.Fatalf("%q %q %q", question, yes, no)
	}
	a.CloseTexts("", "", "")
	if question, _, _ := a.closeQuestion(); question != "Q" {
		t.Fatalf("a page without words lost the ones it told: %q", question)
	}
}

// A close confirmed during the verification of a game cancels the queue and waits for the Runner
// to return: what was made but not verified is taken away, not left at the destination looking
// done ("Cada juego se comprueba al terminar").
func TestClosingDiscardsUnverified(t *testing.T) {
	engine, fixtures := needCore(t)
	image, err := (&App{core: engine}).Describe(filepath.Join(fixtures, "basic.exfat"))
	if err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	events := newPageEvents()
	a := pageApp(engine, events)
	atVerify := make(chan struct{})
	a.onItem = func(i int, state, detail string) {
		if state != jobs.StateVerifying {
			return
		}
		ctx := queueContext(t, a)
		close(atVerify)
		// the close lands here, during the verification; the Runner takes a while to go on, so a
		// close that does not wait for it ends before the output is taken away
		<-ctx.Done()
		time.Sleep(300 * time.Millisecond)
	}
	if err := a.Start("compress", []jobs.Item{image.Item}, Choices{Options: jobs.Options{Mode: jobs.ModeP5z, Dest: dest, Level: 1}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-atVerify:
	case <-time.After(time.Minute):
		t.Fatal("the game never got to its verification")
	}
	a.ConfirmClose()
	a.closed(context.Background())
	if a.working() {
		t.Fatal("closed ended with the queue still running")
	}
	if left, _ := os.ReadDir(dest); len(left) != 0 {
		t.Fatalf("left at the destination after a close during verification: %v", left)
	}
	if sum := events.wait(t); sum.Cancelled != 1 || sum.Done != 0 {
		t.Fatalf("%+v", sum)
	}
	if last := events.last(0); last.State != jobs.StateCancelled {
		t.Fatalf("%+v", last)
	}
}

// A queue that does not stop keeps MaxPack from ending for closeWait at most; without a queue
// closing does not wait at all.
func TestClosingWaitsAWhileAtMost(t *testing.T) {
	waited := closeWait
	closeWait = 100 * time.Millisecond
	defer func() { closeWait = waited }()
	events := newPageEvents()
	a := pageApp(filepath.Join(t.TempDir(), "no-existe.exe"), events)
	stuck, atWork := make(chan struct{}), make(chan struct{})
	defer close(stuck)
	a.onItem = func(i int, state, detail string) {
		if state == jobs.StatePacking {
			close(atWork)
			<-stuck
		}
	}
	item := jobs.Item{Path: t.TempDir(), TitleID: "PPSA00001", Name: "x", Kind: "folder", OriginalSize: 1, HasZstdRuntime: true}
	if err := a.Start("compress", []jobs.Item{item}, Choices{Options: jobs.Options{Mode: jobs.ModeP5z, Dest: t.TempDir()}}); err != nil {
		t.Fatal(err)
	}
	<-atWork
	start := time.Now()
	a.closed(context.Background())
	if took := time.Since(start); took < closeWait || took > 2*time.Second {
		t.Fatalf("closing waited %v for a queue that does not stop (%v at most)", took, closeWait)
	}
	start = time.Now()
	(&App{}).closed(context.Background())
	if time.Since(start) > 50*time.Millisecond {
		t.Fatal("closing waited with nothing running")
	}
}
