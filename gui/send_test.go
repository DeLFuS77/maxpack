// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/internal/fakecore"
	"github.com/DeLFuS77/maxpack/gui/jobs"
	"github.com/DeLFuS77/maxpack/gui/settings"
)

const consoleCode = "482913"

func withConsole(t *testing.T, address string) {
	t.Helper()
	useSettings(t, t.TempDir())
	saveSettings(t, func(s *settings.Settings) { s.ConsoleIP, s.ConsoleCode = address, consoleCode })
}

// A queue that sends takes the console's address and code from Settings, never from the page: the code goes
// to the engine in its environment, and nothing of it is in the plans the page sees.
func TestPlansOfASend(t *testing.T) {
	withConsole(t, "192.168.1.20")
	p5z := touch(t, t.TempDir(), "Game [PPSA10001].p5z", 10)
	it := jobs.Item{Path: p5z, Kind: "p5z", Name: "Game", OriginalSize: 10}
	a := &App{fs: realFS{}}
	choices := Choices{Options: jobs.Options{Mode: jobs.ModeSend, Drive: "usb0", Console: "10.0.0.66", ConsoleCode: "999999"}}
	plans, o, err := a.plans("details", []jobs.Item{it}, choices)
	if err != nil || o.Console != "192.168.1.20" || o.ConsoleCode != consoleCode || o.Drive != "usb0" {
		t.Fatalf("%+v %v", o, err)
	}
	if got := strings.Join(plans[0].Args, " "); got != "send "+p5z+" 192.168.1.20 --drive usb0" {
		t.Errorf("args: %q", got)
	}
	views, err := a.Plan("details", []jobs.Item{it}, choices)
	if data, _ := json.Marshal(views); err != nil || bytes.Contains(data, []byte(consoleCode)) {
		t.Errorf("the plans the page sees: %s %v", data, err)
	}
	saveSettings(t, func(s *settings.Settings) {})
	if plans, _, _ := a.plans("details", []jobs.Item{it}, choices); plans[0].Block != "no_console" {
		t.Errorf("no console in Settings: %+v", plans[0])
	}
	if stepsOf(jobs.ModeSend, 0)[0] != jobs.StateSending || stepsOf(jobs.ModeSend, 5)[1] != jobs.StateVerifying || len(stepsOf(jobs.ModeSend, 5)) != 2 {
		t.Errorf("steps: %v", stepsOf(jobs.ModeSend, 5))
	}
}

// The drives of the console, for the send dialog: what the engine says, with their free space; a refusal
// of the console is said with its code; without the console in Settings, the engine is not asked.
func TestConsoleDrives(t *testing.T) {
	t.Setenv(fakecore.Env, "sends")
	withConsole(t, "192.168.1.20")
	a := &App{core: os.Args[0]}
	view := a.ConsoleDrives()
	if view.Error != "" || len(view.Drives) != 2 || view.Drives[1].ID != "usb0" || view.Drives[1].Free != 1500000000000 ||
		view.Drives[1].Total != 2000000000000 || view.Drives[0].FS != "ufs" {
		t.Fatalf("%+v", view)
	}
	t.Setenv(fakecore.SendFailEnv, "bad_code")
	if view := a.ConsoleDrives(); view.Error != "bad_code" || len(view.Drives) != 0 {
		t.Errorf("a wrong code: %+v", view)
	}
	saveSettings(t, func(s *settings.Settings) { s.ConsoleIP = "192.168.1.20" })
	if view := a.ConsoleDrives(); view.Error != "no_console" {
		t.Errorf("no code: %+v", view)
	}
}

// Sending from Details: sending, verifying (the console's) and ok, one .p5z after another.
func TestStartSend(t *testing.T) {
	t.Setenv(fakecore.Env, "sends")
	withConsole(t, "192.168.1.20")
	dir := t.TempDir()
	items := []jobs.Item{{Path: touch(t, dir, "A.p5z", 100), Kind: "p5z", Name: "A"}, {Path: touch(t, dir, "B.p5z", 200), Kind: "p5z", Name: "B"}}
	events := newPageEvents()
	a := pageApp(os.Args[0], events)
	if err := a.Start("details", items, Choices{Options: jobs.Options{Mode: jobs.ModeSend, Drive: "usb0"}}); err != nil {
		t.Fatal(err)
	}
	sum := events.wait(t)
	for i := range items {
		if got := strings.Join(events.states(i), " "); got != "waiting sending verifying ok" || events.last(i).Detail != "sent" {
			t.Errorf("%d: %s %+v", i, got, events.last(i))
		}
		if said, _ := os.ReadFile(items[i].Path + ".sent"); string(said) != "to 192.168.1.20 drive usb0 code "+consoleCode {
			t.Errorf("the engine was given %q", said)
		}
	}
	if sum.Done != 2 {
		t.Errorf("%+v", sum)
	}
}

// Copy report says whether the console is set, never its address or its code.
func TestReportHasNoConsole(t *testing.T) {
	withConsole(t, "192.168.1.20")
	text := (&App{}).reportText(ReportRequest{})
	if !strings.Contains(text, "console address and code given") || strings.Contains(text, consoleCode) || strings.Contains(text, "192.168.1.20") {
		t.Errorf("%s", text)
	}
}

// A fake MaxPack for the console, as much of it as a send needs: its drives, and a .p5z received whole and
// verified at once.
type fakeConsole struct {
	mu      sync.Mutex
	got     bytes.Buffer
	size    int64
	name    string
	drive   string
	state   string
	badCode int
}

func (c *fakeConsole) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	answer := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(v)
	}
	if r.Header.Get("X-MaxPack-Code") != consoleCode {
		c.badCode++
		answer(401, map[string]string{"error": "bad_code"})
		return
	}
	switch r.URL.Path {
	case "/api/receive/drives":
		answer(200, map[string]any{"drives": []map[string]any{{"id": "internal", "free": 600e9, "total": 825e9, "fs": "ufs"}}})
	case "/api/receive/start":
		var req struct {
			Name, Drive string
			Size        int64
		}
		json.NewDecoder(r.Body).Decode(&req)
		c.name, c.drive, c.size, c.state = req.Name, req.Drive, req.Size, "receiving"
		answer(200, map[string]any{"id": "t1", "have": c.got.Len()})
	case "/api/receive/data":
		data, _ := io.ReadAll(r.Body)
		c.got.Write(data)
		answer(200, map[string]any{"have": c.got.Len()})
	case "/api/receive/finish":
		c.state = "done"
		w.WriteHeader(202)
	case "/api/receive/status":
		answer(200, map[string]any{"have": c.got.Len(), "size": c.size, "state": c.state, "verify": map[string]any{"bytes": c.size, "total": c.size, "bad": []any{}}})
	default:
		answer(404, map[string]string{"error": "no_route"})
	}
}

// The real engine sends a .p5z the real engine made to a console that takes it: the bytes are its bytes,
// under its name, and the code went in every request.
func TestSendReal(t *testing.T) {
	engine, fixtures := needCore(t)
	console := &fakeConsole{}
	server := httptest.NewServer(console)
	defer server.Close()
	withConsole(t, strings.TrimPrefix(server.URL, "http://"))
	out := filepath.Join(t.TempDir(), "Game [PPSA00000].p5z")
	if _, _, err := core.RunWith(context.Background(), engine, []string{"pack", filepath.Join(fixtures, "basic.exfat"), "-o", out, "--p5z", "--threads", "2"}, core.RunOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	game, err := (&App{core: engine}).Details(out)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{core: engine}
	if view := a.ConsoleDrives(); view.Error != "" || len(view.Drives) != 1 || view.Drives[0].ID != "internal" {
		t.Fatalf("drives: %+v", view)
	}
	events := newPageEvents()
	a = pageApp(engine, events)
	if err := a.Start("details", []jobs.Item{game.Item}, Choices{Options: jobs.Options{Mode: jobs.ModeSend}}); err != nil {
		t.Fatal(err)
	}
	if sum := events.wait(t); sum.Done != 1 || strings.Join(events.states(0), " ") != "waiting sending verifying ok" {
		t.Fatalf("%+v %v", sum, events.items)
	}
	want, _ := os.ReadFile(out)
	console.mu.Lock()
	defer console.mu.Unlock()
	if !bytes.Equal(console.got.Bytes(), want) || console.name != filepath.Base(out) || console.drive != "internal" || console.badCode != 0 {
		t.Errorf("the console got %d bytes of %d, %q on %q, %d bad codes", console.got.Len(), len(want), console.name, console.drive, console.badCode)
	}
}
