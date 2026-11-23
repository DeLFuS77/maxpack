// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/DeLFuS77/maxpack/gui/internal/portable"
	"github.com/DeLFuS77/maxpack/gui/jobs"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// noticeText is the notice of the system when a queue ends: the title and a body that says the
// games done, the ones that failed and what was saved, in the language of the settings. The
// texts of the notice are all here.
func noticeText(lang string, s jobs.Summary) (title, body string) {
	title = "MaxPack"
	saved := fmt.Sprintf("%.1f", float64(s.Saved)/(1<<30))
	var parts []string
	if lang == "es" {
		saved = strings.Replace(saved, ".", ",", 1)
		parts = append(parts, plural(s.Done, "juego listo", "juegos listos"))
		if s.Failed > 0 {
			parts = append(parts, fmt.Sprintf("%d con error", s.Failed))
		}
		if s.Cancelled > 0 {
			parts = append(parts, plural(s.Cancelled, "cancelado", "cancelados"))
		}
		if s.Saved > 0 {
			parts = append(parts, saved+" GB ahorrados")
		}
		return title, strings.Join(parts, ", ")
	}
	parts = append(parts, plural(s.Done, "game done", "games done"))
	if s.Failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", s.Failed))
	}
	if s.Cancelled > 0 {
		parts = append(parts, fmt.Sprintf("%d cancelled", s.Cancelled))
	}
	if s.Saved > 0 {
		parts = append(parts, saved+" GB saved")
	}
	return title, strings.Join(parts, ", ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
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
