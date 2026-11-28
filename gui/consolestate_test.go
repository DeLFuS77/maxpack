// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeLFuS77/maxpack/gui/settings"
)

// a 1x1 PNG
const tinyPNG = "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89"

func stateJSON(pairingOn string) string {
	return `{"games":[
 {"title_id":"PPSA01234","title_name":"Sample Game","version":"01.000.000","path":"/mnt/usb0/homebrew/a.p5z","kind":"p5z","packing":"x","drive":"/mnt/usb0","size":1000,"status":"","error":"","verified":{"kind":"quick","at":1700000000,"ok":true,"files_ok":3,"files_bad":0}},
 {"title_id":"PPSA01234","title_name":"Sample Game","version":"01.001.000","kind":"pkg","drive":"/mnt/usb0","size":2000,"status":"duplicate","verified":null},
 {"title_id":"PPSA01235","title_name":"Folder Game","version":"01.000.000","kind":"folder","drive":"/data","size":0,"status":"","verified":null}],
 "images":[{"path":"/data/homebrew/x.exfat","kind":"exfat","title_id":"PPSA01236","other":1}],
 "drives":[{"mount":"/mnt/usb0","label":"USB 1","kind":"usb","total":1000000,"free":400000,"ejectable":true},{"mount":"/data","label":"Internal","kind":"internal","total":2000000,"free":5}],
 "receive":null,
 "receive_left":[{"path":"/data/homebrew/r.p5z.maxpack-recv","name":"r.p5z","size":77}],
 "pairing":{"on":` + pairingOn + `,"code":"123456","here":true},
 "extra":{"ignored":true}}`
}

func consoleServer(t *testing.T, pairingOn string, hits *atomic.Int32) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte(stateJSON(pairingOn)))
	})
	mux.HandleFunc("/icon/", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/icon/PPSA01234" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(tinyPNG))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

func TestConsoleStateMaps(t *testing.T) {
	var hits atomic.Int32
	addr := consoleServer(t, "true", &hits)
	withConsole(t, addr)
	v := (&App{}).ConsoleState()
	if v.State != "connected" || v.Address != addr || v.Receiving {
		t.Fatalf("%+v", v)
	}
	if len(v.Games) != 3 {
		t.Fatalf("games: %+v", v.Games)
	}
	g := v.Games[0]
	if g.TitleID != "PPSA01234" || g.Name != "Sample Game" || g.Version != "01.000.000" || g.Kind != "p5z" || g.Drive != "/mnt/usb0" ||
		g.Size != 1000 || g.Verified == nil || g.Verified.At != 1700000000 || !g.Verified.OK {
		t.Errorf("game 0: %+v", g)
	}
	if v.Games[1].Status != "duplicate" || v.Games[1].Verified != nil || v.Games[1].Kind != "pkg" {
		t.Errorf("game 1: %+v", v.Games[1])
	}
	if v.Games[2].Kind != "folder" || v.Games[2].Size != 0 || v.Games[2].Drive != "/data" {
		t.Errorf("game 2: %+v", v.Games[2])
	}
	if len(v.Images) != 1 || v.Images[0] != (ConsoleImage{Path: "/data/homebrew/x.exfat", Kind: "exfat", TitleID: "PPSA01236"}) {
		t.Errorf("images: %+v", v.Images)
	}
	if len(v.Disks) != 2 || v.Disks[0] != (ConsoleDisk{Label: "USB 1", Kind: "usb", Total: 1000000, Free: 400000}) || v.Disks[1] != (ConsoleDisk{Label: "Internal", Kind: "internal", Total: 2000000, Free: 5}) {
		t.Errorf("disks: %+v", v.Disks)
	}
	if len(v.Left) != 1 || v.Left[0] != (ConsoleLeft{Path: "/data/homebrew/r.p5z.maxpack-recv", Name: "r.p5z", Size: 77}) {
		t.Errorf("left: %+v", v.Left)
	}
}

func TestConsoleStateReceiveOff(t *testing.T) {
	var hits atomic.Int32
	withConsole(t, consoleServer(t, "false", &hits))
	if v := (&App{}).ConsoleState(); v.State != "receive_off" {
		t.Errorf("%+v", v)
	}
}

func TestConsoleStateNoConsole(t *testing.T) {
	useSettings(t, t.TempDir())
	saveSettings(t, func(s *settings.Settings) {})
	if v := (&App{}).ConsoleState(); v.State != "no_console" {
		t.Errorf("%+v", v)
	}
}

func TestConsoleStateUnreachableBacksOff(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		select {
		case <-time.After(5 * time.Second):
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	withConsole(t, strings.TrimPrefix(srv.URL, "http://"))
	a := &App{}
	t0 := time.Now()
	if v := a.ConsoleState(); v.State != "unreachable" || time.Since(t0) > 4500*time.Millisecond {
		t.Fatalf("first: %+v after %v", v, time.Since(t0))
	}
	t0 = time.Now()
	if v := a.ConsoleState(); v.State != "unreachable" || time.Since(t0) > time.Second {
		t.Fatalf("second: %+v after %v", v, time.Since(t0))
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("requests: %d", n)
	}
}

func TestConsoleURLKeepsPort(t *testing.T) {
	if got := consoleURL("10.0.0.5:9000", "/api/state"); got != "http://10.0.0.5:9000/api/state" {
		t.Error(got)
	}
	if got := consoleURL("10.0.0.5", "/x"); got != "http://10.0.0.5:8095/x" {
		t.Error(got)
	}
}

func TestConsoleIconValidatesAndCaches(t *testing.T) {
	var hits atomic.Int32
	withConsole(t, consoleServer(t, "true", &hits))
	a := &App{}
	if got := a.ConsoleIcon("PPSA01234"); !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Errorf("icon: %q", got)
	}
	if got := a.ConsoleIcon("PPSA09999"); got != "" {
		t.Errorf("404: %q", got)
	}
	n := hits.Load()
	if n != 2 {
		t.Fatalf("requests: %d", n)
	}
	a.ConsoleIcon("PPSA01234")
	a.ConsoleIcon("PPSA09999")
	for _, bad := range []string{"../x", "", "ppsa01234"} {
		if got := a.ConsoleIcon(bad); got != "" {
			t.Errorf("%q: %q", bad, got)
		}
	}
	if hits.Load() != n {
		t.Errorf("extra requests: %d", hits.Load()-n)
	}
}

// a console that hangs: every request waits (or until the client gives up)
func hangingConsole(t *testing.T, hits *atomic.Int32) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		select {
		case <-time.After(5 * time.Second):
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

func TestConsoleIconHonoursBackoff(t *testing.T) {
	var hits atomic.Int32
	withConsole(t, hangingConsole(t, &hits))
	a := &App{}
	if v := a.ConsoleState(); v.State != "unreachable" {
		t.Fatalf("%+v", v)
	}
	t0 := time.Now()
	if got := a.ConsoleIcon("PPSA01234"); got != "" || time.Since(t0) > time.Second {
		t.Errorf("icon: %q after %v", got, time.Since(t0))
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("requests: %d", n)
	}

	// an icon request that fails sets the back-off too: state and other icons then make no request
	var hits2 atomic.Int32
	withConsole(t, hangingConsole(t, &hits2))
	b := &App{}
	if got := b.ConsoleIcon("PPSA01234"); got != "" {
		t.Errorf("icon: %q", got)
	}
	t0 = time.Now()
	b.ConsoleIcon("PPSA01235")
	if v := b.ConsoleState(); v.State != "unreachable" || time.Since(t0) > time.Second {
		t.Errorf("%+v after %v", v, time.Since(t0))
	}
	if n := hits2.Load(); n != 1 {
		t.Errorf("requests: %d", n)
	}
}

func TestConsoleStateSingleFlight(t *testing.T) {
	var hits atomic.Int32
	withConsole(t, hangingConsole(t, &hits))
	a := &App{}
	t0 := time.Now()
	var wg sync.WaitGroup
	states := make([]string, 5)
	for i := range states {
		wg.Add(1)
		go func() { defer wg.Done(); states[i] = a.ConsoleState().State }()
	}
	wg.Wait()
	if n := hits.Load(); n != 1 || time.Since(t0) > 4500*time.Millisecond {
		t.Errorf("requests: %d after %v", n, time.Since(t0))
	}
	for _, s := range states {
		if s != "unreachable" {
			t.Errorf("states: %v", states)
		}
	}
}

func TestConsoleIconSingleFlight(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(200 * time.Millisecond)
		w.Write([]byte(tinyPNG))
	}))
	t.Cleanup(srv.Close)
	withConsole(t, strings.TrimPrefix(srv.URL, "http://"))
	a := &App{}
	var wg sync.WaitGroup
	icons := make([]string, 5)
	for i := range icons {
		wg.Add(1)
		go func() { defer wg.Done(); icons[i] = a.ConsoleIcon("PPSA01234") }()
	}
	wg.Wait()
	if n := hits.Load(); n != 1 {
		t.Errorf("requests: %d", n)
	}
	for _, s := range icons {
		if !strings.HasPrefix(s, "data:image/png;base64,") {
			t.Errorf("icons: %v", icons)
		}
	}
}

// What the window asks of the console goes to the console only: a redirect is not followed (the host it points
// to gets no request), and no proxy of the system is used, by the poll, the icons or the search of the network.
func TestConsoleClientsStayOnTheConsole(t *testing.T) {
	var elsewhere atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhere.Add(1)
		if r.URL.Path == "/api/state" {
			w.Write([]byte(stateJSON("true")))
			return
		}
		w.Write([]byte(tinyPNG))
	}))
	t.Cleanup(other.Close)
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusFound)
	}))
	t.Cleanup(redirect.Close)
	address := strings.TrimPrefix(redirect.URL, "http://")
	withConsole(t, address)
	if v := (&App{}).ConsoleState(); v.State != "unreachable" {
		t.Errorf("state through a redirect: %+v", v)
	}
	if got := (&App{}).ConsoleIcon("PPSA01234"); got != "" {
		t.Errorf("icon through a redirect: %.40q", got)
	}
	if found := probeConsoles(context.Background(), []string{address}, 8095, time.Second, 1); len(found) != 0 {
		t.Errorf("found through a redirect: %v", found)
	}
	if n := elsewhere.Load(); n != 0 {
		t.Errorf("the redirect was followed: %d requests to the other host", n)
	}
	for name, client := range map[string]*http.Client{"state and icons": consoleClient, "search": consoleHTTPClient(probePerHost)} {
		transport, ok := client.Transport.(*http.Transport)
		if !ok || transport.Proxy != nil {
			t.Errorf("%s: the client may go through a proxy: %T", name, client.Transport)
		}
		if client.CheckRedirect == nil || client.Timeout == 0 || client.Timeout > consoleTimeout {
			t.Errorf("%s: redirect check %v, timeout %v", name, client.CheckRedirect != nil, client.Timeout)
		}
	}
}
