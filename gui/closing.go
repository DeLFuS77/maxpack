// SPDX-License-Identifier: GPL-3.0-or-later

package main

// Closing the window while a queue runs, as NXPress does: it is asked first, in the dialog of the
// page; and once the answer is to cancel and close, the queue is cancelled and waited for a while,
// so the game at work leaves nothing half made or unverified behind. The engine is never killed
// as the window closes: it is told to cancel and its input is closed, and it takes away what it
// wrote on its own, even once MaxPack is gone.

import (
	"context"
	"time"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/internal/windowplace"
	"github.com/DeLFuS77/maxpack/gui/settings"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// closeWait is how long MaxPack waits as it closes for the queue to stop: the game at work takes
// away what it half made or did not verify (a big one takes a while on a slow disk). An engine
// that takes longer goes on alone, and takes it away itself.
var closeWait = 10 * time.Second

// beforeClose (OnBeforeClose) keeps the window open while it asks whether to stop the queue
// running; else it keeps the place of the window, and the window closes.
func (a *App) beforeClose(ctx context.Context) bool {
	if a.holdClose(ctx) {
		return true
	}
	windowplace.KeepWindowPlace(ctx)
	return false
}

// holdClose tells whether the window stays open for now: a queue runs and closing was not
// confirmed. The page asks in its own dialog, in the theme of the window ("close:ask"), and
// closes the window again with ConfirmClose; before the page told its words, the dialog of the
// system asks.
func (a *App) holdClose(ctx context.Context) bool {
	if !a.working() || a.closeConfirmed.Load() {
		return false
	}
	if a.pageAsksClose() {
		a.send("close:ask", nil)
		return true
	}
	question, yes, no := a.closeQuestion()
	// Windows names the buttons in its own language and answers "Yes"; the others give back the
	// button chosen
	answer, err := wailsruntime.MessageDialog(ctx, wailsruntime.MessageDialogOptions{
		Type:    wailsruntime.QuestionDialog,
		Title:   "MaxPack",
		Message: question,
		Buttons: []string{yes, no}, DefaultButton: no, CancelButton: no,
	})
	return err == nil && answer != yes && answer != "Yes"
}

// pageAsksClose tells whether the page can ask: it told the words of the question.
func (a *App) pageAsksClose() bool {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	return a.closeAsk != ""
}

// ConfirmClose closes the window after the page asked and the answer was to cancel and close:
// the queue is stopped as the window closes (see closed).
func (a *App) ConfirmClose() {
	a.closeConfirmed.Store(true)
	if a.ctx != nil {
		wailsruntime.Quit(a.ctx)
	}
}

// CloseTexts are the words of the question asked as the window closes with a queue running, in
// the language of the page: it sends them each time its language is set.
func (a *App) CloseTexts(question, yes, no string) {
	if question == "" || yes == "" || no == "" {
		return
	}
	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.closeAsk, a.closeYes, a.closeNo = question, yes, no
}

// closeQuestion is the question for the dialog of the system: the words of the page, else the
// ones of the language of the settings.
func (a *App) closeQuestion() (question, yes, no string) {
	a.mutex.Lock()
	if a.closeAsk != "" {
		defer a.mutex.Unlock()
		return a.closeAsk, a.closeYes, a.closeNo
	}
	a.mutex.Unlock()
	lang := ""
	if a.lang != nil {
		lang = a.lang()
	} else if s, err := settings.Load(); err == nil {
		lang = s.Lang
	}
	if lang == "es" {
		return "Hay juegos en marcha. ¿Cancelarlos y cerrar MaxPack?", "Cancelar y cerrar", "Seguir trabajando"
	}
	return "Games are at work. Cancel them and close MaxPack?", "Cancel and close", "Keep working"
}

// closed (OnShutdown) stops the queue running as MaxPack closes, and waits for it, closeWait at
// most: the Runner takes away what the game at work made and did not verify, or the copy that
// was to replace an old one. MaxPack ends right after; the engine is not killed (core.ErrClosing),
// and once cancelled it takes away what it wrote itself.
func (a *App) closed(context.Context) {
	a.mutex.Lock()
	q := a.queue
	a.mutex.Unlock()
	a.stop(core.ErrClosing)
	if q == nil {
		return
	}
	select {
	case <-q.done:
	case <-time.After(closeWait):
	}
}
