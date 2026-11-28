// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeLFuS77/maxpack/gui/internal/fakecore"
	"github.com/DeLFuS77/maxpack/gui/settings"
)

func TestSaveConsoleValidates(t *testing.T) {
	useSettings(t, t.TempDir())
	saveSettings(t, func(s *settings.Settings) { s.ConsoleIP, s.ConsoleCode = "192.168.1.20", consoleCode })
	a := &App{}
	if v := a.SaveConsole("192.168.1", "123456"); v.Error != "bad_ip" {
		t.Errorf("bad ip: %+v", v)
	}
	if v := a.SaveConsole("10.0.0.5", "12a456"); v.Error != "bad_code" {
		t.Errorf("bad code: %+v", v)
	}
	if s := a.GetSettings(); s.ConsoleIP != "192.168.1.20" || s.ConsoleCode != consoleCode {
		t.Errorf("settings changed: %q %q", s.ConsoleIP, s.ConsoleCode)
	}
}

// The code is checked once per save: five wrong ones lock the PC out of the console for a minute.
func TestSaveConsoleChecksOnce(t *testing.T) {
	t.Setenv(fakecore.Env, "sends")
	t.Setenv(fakecore.SendFailEnv, "bad_code")
	calls := filepath.Join(t.TempDir(), "calls.txt")
	t.Setenv(fakecore.CallsEnv, calls)
	useSettings(t, t.TempDir())
	a := &App{core: os.Args[0]}
	v := a.SaveConsole("10.0.0.5", "123456")
	if v.Error != "bad_code" {
		t.Fatalf("%+v", v)
	}
	if s := a.GetSettings(); s.ConsoleIP != "10.0.0.5" || s.ConsoleCode != "123456" {
		t.Errorf("settings: %q %q", s.ConsoleIP, s.ConsoleCode)
	}
	data, _ := os.ReadFile(calls)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "send\t--drives") && !strings.Contains(lines[0], "--drives") {
		t.Errorf("calls: %q", data)
	}
}

func TestProbeConsoles(t *testing.T) {
	maxpack := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/state" {
			w.Write([]byte(`{"games":[],"drives":[],"other":1}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer maxpack.Close()
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))
	defer other.Close()
	hosts := []string{strings.TrimPrefix(maxpack.URL, "http://"), strings.TrimPrefix(other.URL, "http://"), "127.0.0.1:1"}
	perHost := 300 * time.Millisecond
	start := time.Now()
	got := probeConsoles(context.Background(), hosts, 8095, perHost, 8)
	if len(got) != 1 || got[0] != hosts[0] {
		t.Errorf("found %v, want %v", got, hosts[:1])
	}
	if d := time.Since(start); d > perHost+time.Second {
		t.Errorf("took %v", d)
	}
}

func TestLocalPrefixesAreSlash24(t *testing.T) {
	for _, p := range localPrefixes() {
		if p.Bits() != 24 || p.Addr().IsLoopback() {
			t.Errorf("prefix %v", p)
		}
	}
}
