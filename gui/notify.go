// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"math"
	"runtime"
	"strconv"
	"strings"

	"github.com/DeLFuS77/maxpack/gui/internal/portable"
	"github.com/DeLFuS77/maxpack/gui/jobs"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// sizeText is a size as the page writes it (size() in app.js, as NXPress does): in steps of
// 1024, B with no decimals and one from KB on, with the decimal comma in the languages that
// write it so (Spanish, Russian, German, French, Portuguese, Italian) and no thousands separator.
func sizeText(lang string, bytes uint64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	value, unit := float64(bytes), 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 {
		return isolate(lang, strconv.FormatUint(bytes, 10)+" B")
	}
	// half away from zero, as Intl.NumberFormat rounds on the page
	text := strconv.FormatFloat(math.Round(value*10)/10, 'f', 1, 64)
	if decimalComma[lang] {
		text = strings.Replace(text, ".", ",", 1)
	}
	return isolate(lang, text+" "+units[unit])
}

// noticeText is the notice of the system when a queue ends: the title and a body that says the
// games done, the ones that failed or were cancelled and what was saved, in the language of the
// window and in the words of the page (see langs.go).
func noticeText(lang string, s jobs.Summary) (title, body string) {
	w := wordsFor(lang)
	parts := []string{w.done(s.Done)}
	if s.Failed > 0 {
		parts = append(parts, w.failed(s.Failed))
	}
	if s.Cancelled > 0 {
		parts = append(parts, w.cancelled(s.Cancelled))
	}
	if s.Saved > 0 {
		parts = append(parts, w.saved(sizeText(lang, s.Saved)))
	}
	return "MaxPack", strings.Join(parts, w.sep)
}

// notify shows a notice of the system.
func (a *App) notify(title, body string) {
	if a.ctx == nil {
		return
	}
	a.notices.Do(func() { a.noticesReady = a.readyNotices() })
	if a.noticesReady {
		wailsruntime.SendNotification(a.ctx, wailsruntime.NotificationOptions{ID: "maxpack-done", Title: title, Body: body})
	}
}

// readyNotices readies the notices of the system as the first one is sent, not at every start,
// and tells whether they can be sent. On Windows that writes MaxPack in the registry, which a
// portable MaxPack does not do: it sends none. macOS asks the user first.
func (a *App) readyNotices() bool {
	if runtime.GOOS == "windows" && portable.Folder() != "" {
		return false
	}
	wailsruntime.InitializeNotifications(a.ctx)
	if !wailsruntime.IsNotificationAvailable(a.ctx) {
		return false
	}
	if runtime.GOOS == "darwin" {
		if allowed, err := wailsruntime.CheckNotificationAuthorization(a.ctx); err != nil || !allowed {
			allowed, err = wailsruntime.RequestNotificationAuthorization(a.ctx)
			return err == nil && allowed
		}
	}
	return true
}
