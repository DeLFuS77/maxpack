// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/DeLFuS77/maxpack/gui/settings"
)

// What the Console tab shows of MaxPack for the console: read from its open HTTP API (GET only, no code
// needed), so nothing on the console changes. The page asks every 3 s while the tab is open.

type ConsoleVerified struct {
	At int64 `json:"at"`
	OK bool  `json:"ok"`
}

type ConsoleGame struct {
	TitleID  string           `json:"titleId"`
	Name     string           `json:"name"`
	Version  string           `json:"version"`
	Kind     string           `json:"kind"` // "p5z", "pkg" or "folder", as the console says
	Drive    string           `json:"drive"`
	Size     int64            `json:"size"`     // 0 for folders
	Status   string           `json:"status"`   // e.g. "duplicate"
	Verified *ConsoleVerified `json:"verified"` // nil when never verified
}

type ConsoleImage struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	TitleID string `json:"titleId"`
}

type ConsoleDisk struct {
	Label string `json:"label"`
	Kind  string `json:"kind"`
	Total int64  `json:"total"`
	Free  int64  `json:"free"`
}

type ConsoleLeft struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type ConsoleView struct {
	State     string         `json:"state"` // "no_console", "connected", "receive_off", "unreachable"
	Address   string         `json:"address"`
	Games     []ConsoleGame  `json:"games"`
	Images    []ConsoleImage `json:"images"`
	Disks     []ConsoleDisk  `json:"disks"`
	Left      []ConsoleLeft  `json:"left"`
	Receiving bool           `json:"receiving"` // a send is arriving now
}

const (
	consoleTimeout = 3 * time.Second
	consoleBackoff = 10 * time.Second
	consoleMaxBody = 8 << 20
)

var consoleClient = &http.Client{Timeout: consoleTimeout}

var titleIDPattern = regexp.MustCompile(`^[A-Z]{4}[0-9]{5}$`)

// consoleURL is the URL of path on the console at address: ":8095" is added when the address has no port.
func consoleURL(address, path string) string {
	if !strings.Contains(address, ":") {
		address += ":8095"
	}
	return "http://" + address + path
}

// consoleBackoffState is what a failed request leaves: the view to repeat until the time passes.
type consoleBackoffState struct {
	address string
	until   time.Time
	view    ConsoleView
}

// one call of the console in flight, which the callers that arrive meanwhile wait for
type consoleCall struct {
	done   chan struct{}
	result any
}

type consoleIconKey struct{ address, titleID string }

// the console's /api/state, only the fields the window uses
type consoleAPIState struct {
	Games []struct {
		TitleID   string `json:"title_id"`
		TitleName string `json:"title_name"`
		Version   string `json:"version"`
		Kind      string `json:"kind"`
		Drive     string `json:"drive"`
		Size      int64  `json:"size"`
		Status    string `json:"status"`
		Verified  *struct {
			At int64 `json:"at"`
			OK bool  `json:"ok"`
		} `json:"verified"`
	} `json:"games"`
	Images []struct {
		Path    string `json:"path"`
		Kind    string `json:"kind"`
		TitleID string `json:"title_id"`
	} `json:"images"`
	Drives []struct {
		Label string `json:"label"`
		Kind  string `json:"kind"`
		Total int64  `json:"total"`
		Free  int64  `json:"free"`
	} `json:"drives"`
	Receive     json.RawMessage `json:"receive"`
	ReceiveLeft []struct {
		Path string `json:"path"`
		Name string `json:"name"`
		Size int64  `json:"size"`
	} `json:"receive_left"`
	Pairing struct {
		On bool `json:"on"`
	} `json:"pairing"`
}

func emptyConsoleView(state, address string) ConsoleView {
	return ConsoleView{State: state, Address: address, Games: []ConsoleGame{}, Images: []ConsoleImage{},
		Disks: []ConsoleDisk{}, Left: []ConsoleLeft{}}
}

// ConsoleState reads what the console has. After a failed request it answers "unreachable" for 10 s
// without asking again, so a console that is off does not hold the window every 3 s.
func (a *App) ConsoleState() ConsoleView {
	saved, _ := settings.Load()
	address := saved.ConsoleIP
	if address == "" || !settings.ValidConsoleIP(address) {
		return emptyConsoleView("no_console", "")
	}
	a.consoleMutex.Lock()
	b := a.consoleBackoff
	a.consoleMutex.Unlock()
	if b.address == address && time.Now().Before(b.until) {
		return b.view
	}
	// callers that arrive while one asks share its answer
	r := a.consoleFlight("state|"+address, func() any {
		view, ok := fetchConsoleState(address)
		a.consoleMutex.Lock()
		if ok {
			a.consoleBackoff = consoleBackoffState{}
		} else {
			a.consoleBackoff = consoleBackoffState{address: address, until: time.Now().Add(consoleBackoff), view: view}
		}
		a.consoleMutex.Unlock()
		return view
	})
	return r.(ConsoleView)
}

func fetchConsoleState(address string) (ConsoleView, bool) {
	fail := emptyConsoleView("unreachable", address)
	resp, err := consoleClient.Get(consoleURL(address, "/api/state"))
	if err != nil {
		return fail, false
	}
	defer resp.Body.Close()
	var s consoleAPIState
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, consoleMaxBody)).Decode(&s) != nil {
		return fail, false
	}
	v := emptyConsoleView("connected", address)
	if !s.Pairing.On {
		v.State = "receive_off"
	}
	v.Receiving = len(s.Receive) > 0 && string(s.Receive) != "null"
	for _, g := range s.Games {
		game := ConsoleGame{TitleID: g.TitleID, Name: g.TitleName, Version: g.Version, Kind: g.Kind, Drive: g.Drive,
			Size: g.Size, Status: g.Status}
		if g.Verified != nil {
			game.Verified = &ConsoleVerified{At: g.Verified.At, OK: g.Verified.OK}
		}
		v.Games = append(v.Games, game)
	}
	for _, i := range s.Images {
		v.Images = append(v.Images, ConsoleImage{Path: i.Path, Kind: i.Kind, TitleID: i.TitleID})
	}
	for _, d := range s.Drives {
		v.Disks = append(v.Disks, ConsoleDisk{Label: d.Label, Kind: d.Kind, Total: d.Total, Free: d.Free})
	}
	for _, l := range s.ReceiveLeft {
		v.Left = append(v.Left, ConsoleLeft{Path: l.Path, Name: l.Name, Size: l.Size})
	}
	return v, true
}

// ConsoleIcon is the console icon of a game as a PNG data URL, "" when it has none yet or the title ID
// is not one. Answers, 404s included, are kept per address and title ID for the life of the window.
func (a *App) ConsoleIcon(titleID string) string {
	if !titleIDPattern.MatchString(titleID) {
		return ""
	}
	saved, _ := settings.Load()
	address := saved.ConsoleIP
	if address == "" || !settings.ValidConsoleIP(address) {
		return ""
	}
	key := consoleIconKey{address, titleID}
	a.consoleMutex.Lock()
	icon, known := a.consoleIcons[key]
	down := a.consoleBackoff.address == address && time.Now().Before(a.consoleBackoff.until)
	a.consoleMutex.Unlock()
	if known {
		return icon
	}
	if down { // the console did not answer lately: do not wait for it once per game
		return ""
	}
	r := a.consoleFlight("icon|"+address+"|"+titleID, func() any {
		icon, final, reached := fetchConsoleIcon(address, titleID)
		a.consoleMutex.Lock()
		defer a.consoleMutex.Unlock()
		if final {
			if a.consoleIcons == nil {
				a.consoleIcons = map[consoleIconKey]string{}
			}
			a.consoleIcons[key] = icon
		}
		if !reached {
			a.consoleBackoff = consoleBackoffState{address: address, until: time.Now().Add(consoleBackoff),
				view: emptyConsoleView("unreachable", address)}
		}
		return icon
	})
	return r.(string)
}

// consoleFlight runs fn once for key however many callers ask at the same time; all get its result.
// The mutex is not held while fn runs.
func (a *App) consoleFlight(key string, fn func() any) any {
	a.consoleMutex.Lock()
	if c, ok := a.consoleCalls[key]; ok {
		a.consoleMutex.Unlock()
		<-c.done
		return c.result
	}
	c := &consoleCall{done: make(chan struct{})}
	if a.consoleCalls == nil {
		a.consoleCalls = map[string]*consoleCall{}
	}
	a.consoleCalls[key] = c
	a.consoleMutex.Unlock()
	c.result = fn()
	a.consoleMutex.Lock()
	delete(a.consoleCalls, key)
	a.consoleMutex.Unlock()
	close(c.done)
	return c.result
}

// fetchConsoleIcon asks for one icon; final is false when the answer is not to be kept (a later call asks again); reached is false when the
// console did not answer at all.
func fetchConsoleIcon(address, titleID string) (icon string, final, reached bool) {
	resp, err := consoleClient.Get(consoleURL(address, "/icon/"+titleID))
	if err != nil {
		return "", false, false
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", true, true
	}
	if resp.StatusCode != http.StatusOK {
		return "", false, true
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, consoleMaxBody))
	if err != nil || len(data) == 0 {
		return "", false, true
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data), true, true
}
