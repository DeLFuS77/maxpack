// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/DeLFuS77/maxpack/gui/internal/minisign/minisigntest"
	"github.com/DeLFuS77/maxpack/gui/internal/update"
	"github.com/DeLFuS77/maxpack/gui/settings"
)

// testUpdateSeed makes the key of these tests only, which signs the made-up releases below; the key of the
// real releases is never in the tests.
var testUpdateSeed = []byte("maxpack window test key, not real")[:32]

// fakeGitHub is a made-up GitHub with one release, v1.1.0, signed by the test key (or not, with bad), and
// every request it was asked.
type fakeGitHub struct {
	mutex sync.Mutex
	asked []string
	files map[string][]byte
}

func newFakeGitHub(bad bool) *fakeGitHub {
	private := ed25519.NewKeyFromSeed(testUpdateSeed)
	g := &fakeGitHub{files: map[string][]byte{"maxpack-windows-x64-setup.exe": []byte("installer 1.1.0")}}
	sum := sha256.Sum256(g.files["maxpack-windows-x64-setup.exe"])
	g.files["SHA256SUMS"] = []byte(hex.EncodeToString(sum[:]) + "  maxpack-windows-x64-setup.exe\n")
	g.files["SHA256SUMS.minisig"] = minisigntest.Sign(private, [8]byte{'w', 'i', 'n', 'd', 'o', 'w', 's', '1'}, g.files["SHA256SUMS"], true, "file:SHA256SUMS")
	if bad {
		g.files["SHA256SUMS"] = append(g.files["SHA256SUMS"], '\n')
	}
	return g
}

func (g *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mutex.Lock()
	g.asked = append(g.asked, r.URL.Path)
	g.mutex.Unlock()
	if r.URL.Path == "/repos/DeLFuS77/maxpack/releases/latest" {
		assets := []map[string]any{}
		for name, data := range g.files {
			assets = append(assets, map[string]any{"name": name, "size": len(data)})
		}
		json.NewEncoder(w).Encode(map[string]any{"tag_name": "v1.1.0", "assets": assets})
		return
	}
	if data, ok := g.files[strings.TrimPrefix(r.URL.Path, "/DeLFuS77/maxpack/releases/download/v1.1.0/")]; ok {
		w.Write(data)
		return
	}
	http.NotFound(w, r)
}

func (g *fakeGitHub) requests() []string {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	return append([]string{}, g.asked...)
}

// updateApp is a window that looks for versions on server, as MaxPack 1.0.7 on Windows would (download says
// whether the file of the system is downloaded), with stand-ins for running the installer, quitting and the
// browser.
type updateApp struct {
	*App
	ran, opened []string
	quits       int
}

func newUpdateApp(t *testing.T, server *httptest.Server, download bool) *updateApp {
	private := ed25519.NewKeyFromSeed(testUpdateSeed)
	c := update.Config{Current: "1.0.7", PublicKey: minisigntest.PublicKey(private.Public().(ed25519.PublicKey), [8]byte{'w', 'i', 'n', 'd', 'o', 'w', 's', '1'}),
		Asset: "maxpack-windows-x64-setup.exe", Download: download, Folder: t.TempDir(), API: server.URL, Downloads: server.URL,
		Hosts: []string{strings.TrimPrefix(server.URL, "https://")}, Client: server.Client()}
	u := &updateApp{}
	u.App = &App{updates: &c, emit: func(string, any) {}}
	u.runInstaller = func(path string) error { u.ran = append(u.ran, path); return nil }
	u.quit = func() { u.quits++ }
	u.openURL = func(url string) { u.opened = append(u.opened, url) }
	return u
}

// Off (the default), nothing is asked of the internet; without a key in the build, neither, and it says so.
func TestUpdateOffAsksNothing(t *testing.T) {
	useSettings(t, t.TempDir())
	g := newFakeGitHub(false)
	server := httptest.NewTLSServer(g)
	defer server.Close()
	u := newUpdateApp(t, server, true)
	if view := u.CheckUpdate(); view.State != "off" || len(g.requests()) != 0 {
		t.Fatalf("off: %+v, asked %v", view, g.requests())
	}
	saveSettings(t, func(s *settings.Settings) { s.UpdateCheck = true })
	u.updates.PublicKey = ""
	if view := u.CheckUpdate(); view.State != "unavailable" || len(g.requests()) != 0 {
		t.Fatalf("no key: %+v, asked %v", view, g.requests())
	}
	if u.updateAvailable() {
		t.Fatal("a build without a key says it can look for versions")
	}
}

// On, it looks once per start (a second call gets the first answer), finds 1.1.0 signed, with its installer
// checked; Update checks the installer again, runs it and closes MaxPack, but not while a queue runs.
func TestUpdateOncePerStartAndInstall(t *testing.T) {
	useSettings(t, t.TempDir())
	saveSettings(t, func(s *settings.Settings) { s.UpdateCheck = true })
	g := newFakeGitHub(false)
	server := httptest.NewTLSServer(g)
	defer server.Close()
	u := newUpdateApp(t, server, true)
	if err := u.Update(); err == nil {
		t.Fatal("updated before anything was found")
	}
	view := u.CheckUpdate()
	if view.State != "found" || view.Version != "1.1.0" || !view.Install || view.Page != "https://github.com/DeLFuS77/maxpack/releases/tag/v1.1.0" {
		t.Fatalf("%+v", view)
	}
	if again := u.CheckUpdate(); again != view {
		t.Fatalf("the second look: %+v", again)
	}
	latest := 0
	for _, path := range g.requests() {
		if strings.HasSuffix(path, "/latest") {
			latest++
		}
	}
	if latest != 1 {
		t.Fatalf("GitHub was asked %d times: %v", latest, g.requests())
	}
	u.queue = &queue{}
	if err := u.Update(); err == nil || len(u.ran) != 0 {
		t.Fatalf("updated while a queue runs: %v %v", err, u.ran)
	}
	u.queue = nil
	if err := u.Update(); err != nil || len(u.ran) != 1 || u.quits != 1 {
		t.Fatalf("Update: %v, ran %v, quit %d", err, u.ran, u.quits)
	}
	if data, _ := os.ReadFile(u.ran[0]); string(data) != "installer 1.1.0" {
		t.Fatalf("it ran %q", data)
	}
	// changed on disk since it was checked: not run
	os.WriteFile(u.ran[0], []byte("not the installer"), 0o644)
	if err := u.Update(); err == nil || len(u.ran) != 1 {
		t.Fatalf("a changed installer was run: %v %v", err, u.ran)
	}
}

// Where nothing is downloaded (macOS, Linux, the portable version), Update opens the release's page.
func TestUpdateOpensThePage(t *testing.T) {
	useSettings(t, t.TempDir())
	saveSettings(t, func(s *settings.Settings) { s.UpdateCheck = true })
	server := httptest.NewTLSServer(newFakeGitHub(false))
	defer server.Close()
	u := newUpdateApp(t, server, false)
	if view := u.CheckUpdate(); view.State != "found" || view.Install {
		t.Fatalf("%+v", view)
	}
	if err := u.Update(); err != nil || len(u.ran) != 0 || len(u.opened) != 1 || u.opened[0] != "https://github.com/DeLFuS77/maxpack/releases/tag/v1.1.0" {
		t.Fatalf("%v, ran %v, opened %v", err, u.ran, u.opened)
	}
}

// A release whose signature is not right is not offered: the page is told why, and nothing else was downloaded.
func TestUpdateBadSignature(t *testing.T) {
	useSettings(t, t.TempDir())
	saveSettings(t, func(s *settings.Settings) { s.UpdateCheck = true })
	g := newFakeGitHub(true)
	server := httptest.NewTLSServer(g)
	defer server.Close()
	u := newUpdateApp(t, server, true)
	if view := u.CheckUpdate(); view.State != "error" || view.Error != "bad_signature" || view.Version != "" {
		t.Fatalf("%+v", view)
	}
	for _, path := range g.requests() {
		if strings.HasSuffix(path, ".exe") {
			t.Fatalf("the installer was downloaded: %v", g.requests())
		}
	}
	if err := u.Update(); err == nil || len(u.ran)+len(u.opened) != 0 {
		t.Fatalf("updated to a release badly signed: %v", err)
	}
}

// The real configuration: GitHub's hosts, the file of this system, and the key of the build.
func TestUpdateConfigOfTheBuild(t *testing.T) {
	useSettings(t, t.TempDir())
	c := (&App{}).updateConfig()
	if c.API != "https://api.github.com" || c.Downloads != "https://github.com" || c.PublicKey != updateKey || c.Current != version {
		t.Fatalf("%+v", c)
	}
	if folder, _ := settings.Dir(); !strings.HasPrefix(c.Folder, folder) || !strings.HasSuffix(c.Folder, "updates") {
		t.Fatalf("downloads go to %s", c.Folder)
	}
}
