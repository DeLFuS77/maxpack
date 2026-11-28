// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DeLFuS77/maxpack/gui/settings"
)

// ConnectView is what SaveConsole tells the page about the check of the console.
type ConnectView struct {
	Error   string `json:"error"`   // "" when the console took the code; "bad_ip", "bad_code", "save_failed" (Settings could not be written; Message says why), or ConsoleDrives' code
	Message string `json:"message"` // the engine's words, if any
}

// SaveConsole keeps the console's address and code in Settings (even when the check fails, as Settings
// does) and asks the console once with them: five wrong codes lock this PC out for a minute.
func (a *App) SaveConsole(ip, code string) ConnectView {
	ip, code = strings.TrimSpace(ip), strings.TrimSpace(code)
	if !settings.ValidConsoleIP(ip) {
		return ConnectView{Error: "bad_ip"}
	}
	if !settings.ValidConsoleCode(code) {
		return ConnectView{Error: "bad_code"}
	}
	s := a.GetSettings()
	s.ConsoleIP, s.ConsoleCode = ip, code
	if err := a.SetSettings(s); err != nil {
		return ConnectView{Error: "save_failed", Message: err.Error()}
	}
	view := a.ConsoleDrives()
	if view.Error == "" {
		// the console answered just now: what it has is asked at once, not after the back-off of an earlier failure
		a.consoleMutex.Lock()
		if a.consoleBackoff.address == ip {
			a.consoleBackoff = consoleBackoffState{}
		}
		a.consoleMutex.Unlock()
	}
	return ConnectView{Error: view.Error, Message: view.Message}
}

const (
	probePort     = 8095
	probePerHost  = 600 * time.Millisecond
	probeParallel = 64
)

// FindConsoles lists the addresses on this PC's local /24 networks that answer as MaxPack for the console.
func (a *App) FindConsoles() []string {
	var hosts []string
	for _, p := range localPrefixes() {
		own := map[netip.Addr]bool{}
		for _, addr := range ownAddrs() {
			own[addr] = true
		}
		base := p.Addr().As4()
		for i := 1; i < 255; i++ {
			base[3] = byte(i)
			if addr := netip.AddrFrom4(base); !own[addr] {
				hosts = append(hosts, addr.String())
			}
		}
	}
	return probeConsoles(context.Background(), hosts, probePort, probePerHost, probeParallel)
}

// probeConsoles asks each host (an address, or address:port to override port) and returns, sorted, the
// ones that answer as MaxPack: GET /api/state is 200 with JSON holding a "games" and a "drives" array.
func probeConsoles(ctx context.Context, hosts []string, port int, perHost time.Duration, parallel int) []string {
	if parallel < 1 {
		parallel = 1
	}
	// its own connections, closed once the search ends
	client := consoleHTTPClient(perHost)
	defer client.CloseIdleConnections()
	var (
		mu    sync.Mutex
		found = []string{}
		wg    sync.WaitGroup
		slots = make(chan struct{}, parallel)
	)
	for _, host := range hosts {
		slots <- struct{}{}
		if ctx.Err() != nil {
			<-slots
			break
		}
		wg.Add(1)
		go func(host string) {
			defer func() { <-slots; wg.Done() }()
			target := host
			if !strings.Contains(host, ":") {
				target = net.JoinHostPort(host, strconv.Itoa(port))
			}
			if isMaxPack(ctx, client, target) {
				mu.Lock()
				found = append(found, host)
				mu.Unlock()
			}
		}(host)
	}
	wg.Wait()
	sort.Strings(found)
	return found
}

func isMaxPack(ctx context.Context, client *http.Client, target string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, consoleURL(target, "/api/state"), nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var state struct {
		Games  *[]json.RawMessage `json:"games"`
		Drives *[]json.RawMessage `json:"drives"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, consoleMaxBody)).Decode(&state) != nil {
		return false
	}
	return state.Games != nil && state.Drives != nil
}

// localPrefixes are the IPv4 /24 networks of the interfaces that are up, loopback left out.
func localPrefixes() []netip.Prefix {
	var out []netip.Prefix
	seen := map[netip.Prefix]bool{}
	for _, addr := range ownAddrs() {
		p := netip.PrefixFrom(addr, 24).Masked()
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// ownAddrs are the IPv4 addresses of the interfaces that are up and not loopback.
func ownAddrs() []netip.Addr {
	var out []netip.Addr
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			addr, ok := netip.AddrFromSlice(ipn.IP.To4())
			if ok && !addr.IsLoopback() && !addr.IsLinkLocalUnicast() {
				out = append(out, addr)
			}
		}
	}
	return out
}
