// SPDX-License-Identifier: GPL-3.0-or-later

package main

// Copy report: a plain text for an issue on GitHub, with what MaxPack, its engine and the PC are,
// the settings that matter and, for the games it is about, how each one is and how it ended. It
// never has the name of a game, its title ID or a path: the owner of the project wants no game
// names in reports, and a path says the name of the user. It ends with the row of each game for
// the compatibility list of MaxPack for the console, the numbers the PC knows filled in.

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"runtime"
	"strings"

	"github.com/DeLFuS77/maxpack/gui/bundled"
	"github.com/DeLFuS77/maxpack/gui/core"
	"github.com/DeLFuS77/maxpack/gui/internal/system"
	"github.com/DeLFuS77/maxpack/gui/settings"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// links are the pages the window opens in the browser of the system, by name: the forms of a bug
// and of a game for the compatibility list. The page asks for them by name, never by address.
var links = map[string]string{
	"bug":  "https://github.com/DeLFuS77/maxpack/issues/new?template=bug_report.yml",
	"game": "https://github.com/DeLFuS77/maxpack-ps5/issues/new?template=game_report.yml",
}

// ReportGame is a game as the page tells it for a report: what it is, its sizes, and how it ended
// in a queue (State and Detail, "" when it was not in one). Out is what the queue made, read for
// its volumes; it is never written in the report.
type ReportGame struct {
	Kind                      string
	Packed                    bool
	OriginalSize, InstallSize uint64
	P5zSize, OutSize, Volumes uint64
	Files                     int
	HasZstdRuntime            bool
	HasRuntime                bool
	State, Detail, Out        string
}

// ReportRequest is what a report is about: the section and mode of the games, and the games; none
// for a report of MaxPack and the PC only.
type ReportRequest struct {
	Section, Mode string
	Games         []ReportGame
}

// gib is a size in GiB with one decimal, as the compatibility list writes it.
func gib(bytes uint64) string {
	return fmt.Sprintf("%.1f GiB", float64(bytes)/(1<<30))
}

// sized is a size and its bytes.
func sized(bytes uint64) string { return fmt.Sprintf("%s (%d bytes)", gib(bytes), bytes) }

// detailCode keeps of a part of a detail its code ("files_bad:2", "tmp_not_deleted"): what comes
// after it (a path, a message of the system or of the engine) can have the name of a game.
var detailCode = regexp.MustCompile(`^[a-z][a-z_]*(?::\d+)?`)

// codesOf are the codes of a detail, "a, b"; "other" for a part that is not one.
func codesOf(detail string) string {
	var codes []string
	for _, part := range strings.Split(detail, "; ") {
		if part == "" {
			continue
		}
		code := detailCode.FindString(part)
		if code == "" || len(code) < len(part) && part[len(code)] != ':' {
			code = "other"
		}
		codes = append(codes, code)
		// a verification that failed says why after it
		if rest, ok := strings.CutPrefix(part, "verify_failed: "); ok && code == "verify_failed" {
			codes = append(codes, codesOf(rest))
		}
	}
	return strings.Join(codes, ", ")
}

// kindWords is what a game is, in the words of the report.
func kindWords(g ReportGame) string {
	switch {
	case g.Kind == "p5z":
		return ".p5z"
	case g.Kind == "exfat":
		return "exFAT image"
	case g.Packed:
		return "compressed folder"
	}
	return "dump folder"
}

var modeWords = map[string]string{
	"p5z": "MaxPack installer (.p5z)", "folder": "Play from the USB (folder)",
	"original": "original dump", "console": "folder for the PS5", "verify": "verify",
}

// row is the row of a game for the compatibility list, "" when the PC does not know its sizes as
// packed: each "?" is for the user to fill in, as the console page leaves it.
func row(g ReportGame, mode string) string {
	before, after, format, volumes := g.OriginalSize, uint64(0), "", "—"
	switch {
	case g.Kind == "p5z" && g.P5zSize > 0:
		after, format = g.P5zSize, "p5z"
	case g.State == "ok" && mode == "p5z" && g.OutSize > 0:
		after, format = g.OutSize, "p5z"
	case g.State == "ok" && mode == "folder" && g.OutSize > 0:
		after, format = g.OutSize, "zstd folder"
	}
	if format == "" || before == 0 || after == 0 {
		return ""
	}
	if format == "p5z" {
		volumes = "?"
		if g.Volumes > 0 {
			volumes = fmt.Sprint(g.Volumes)
		}
	}
	smaller := int(math.Round((1 - float64(after)/float64(before)) * 100))
	sizes := fmt.Sprintf("%.1f → %.1f GiB (−%d %%)", float64(before)/(1<<30), float64(after)/(1<<30), smaller)
	return fmt.Sprintf("| %s | %s | %s | ? | ? | ? | ? | ? | ? |  |", sizes, format, volumes)
}

// reportText is the text of a report.
func (a *App) reportText(r ReportRequest) string {
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	saved, _ := settings.Load()
	engine := "not found"
	if path, err := a.engine(); err == nil {
		if v, err := core.Version(path); err == nil {
			engine = v
		}
	}
	line("MaxPack report (no game names, title IDs or paths in it)")
	line("MaxPack: %s", version)
	line("Engine: %s", engine)
	line("System: %s, %s", system.OSVersion(), runtime.GOARCH)
	line("CPU threads: %d", runtime.NumCPU())
	if memory := system.TotalMemory(); memory > 0 {
		line("RAM: %s", gib(memory))
	} else {
		line("RAM: unknown")
	}
	runtimeWords := "none"
	switch {
	case saved.Runtime != "":
		runtimeWords = "custom libSceAmpr.sprx"
	case a.builtInRuntime() != "":
		runtimeWords = "built in (ampr-emu-ng " + bundled.Version() + ")"
	}
	originals := "originals kept"
	switch {
	case saved.DeleteOriginals && saved.DeletePermanently:
		originals = "originals deleted for good once checked"
	case saved.DeleteOriginals:
		originals = "originals to the recycle bin once checked"
	}
	priority := "off"
	if saved.LowPriority {
		priority = "on"
	}
	mode := r.Mode
	if mode == "" {
		mode = string(saved.Mode)
	}
	modeText := modeWords[mode]
	if modeText == "" {
		modeText = mode
	}
	threads := "all"
	if saved.Threads > 0 {
		threads = fmt.Sprint(saved.Threads)
	}
	repair := "off"
	if saved.RepairData {
		repair = fmt.Sprintf("%d %%", saved.RepairPercent)
	}
	line("Settings: %s, level %d, threads %s, runtime %s, low priority %s, %s, names %s, repair data %s", modeText, saved.Level, threads, runtimeWords, priority, originals, saved.NameTemplate, repair)
	if len(r.Games) > 0 && r.Section != "" {
		line("Section: %s", r.Section)
	}
	var rows []string
	for n, g := range r.Games {
		// what a queue made says its volumes
		if g.Volumes == 0 && g.Out != "" && g.State == "ok" && mode == "p5z" {
			if path, err := a.engine(); err == nil {
				if info, err := core.Info(path, g.Out); err == nil {
					if volumes, ok := info["volumes"].(float64); ok {
						g.Volumes = uint64(volumes)
					}
				}
			}
		}
		line("")
		line("Game %d", n+1)
		line("Kind: %s", kindWords(g))
		line("Size: %s", sized(g.OriginalSize))
		if g.InstallSize > 0 {
			line("Installed: %s", sized(g.InstallSize))
		}
		if g.P5zSize > 0 {
			line(".p5z: %s", sized(g.P5zSize))
		}
		if g.OutSize > 0 {
			line("Made: %s", sized(g.OutSize))
		}
		if g.Volumes > 0 {
			line("Volumes: %d", g.Volumes)
		}
		if g.Files > 0 {
			line("Files: %d", g.Files)
		}
		if g.Kind != "" {
			carries := "none"
			if g.HasZstdRuntime {
				carries = "one that reads zstd"
			} else if g.HasRuntime {
				carries = "one that does not read zstd"
			}
			line("Carries ampr-emu-ng: %s", carries)
		}
		if g.State != "" {
			result := g.State
			if codes := codesOf(g.Detail); codes != "" {
				result += " (" + codes + ")"
			}
			line("Result: %s", result)
		}
		if r := row(g, mode); r != "" {
			rows = append(rows, r)
		}
	}
	if len(rows) > 0 {
		line("")
		line("Rows for the compatibility list (fill in each ?):")
		for _, r := range rows {
			line("%s", r)
		}
	}
	line("")
	line("Bug report: %s", links["bug"])
	line("Game report: %s", links["game"])
	return b.String()
}

// CopyReport puts the report on the clipboard, and gives it.
func (a *App) CopyReport(r ReportRequest) (string, error) {
	text := a.reportText(r)
	switch {
	case a.clipboard != nil:
		return text, a.clipboard(text)
	case a.ctx == nil:
		return text, errNoWindow
	}
	return text, wailsruntime.ClipboardSetText(a.ctx, text)
}

// OpenLink opens a page of the window (links) in the browser of the system.
func (a *App) OpenLink(name string) error {
	url, ok := links[name]
	if !ok {
		return errors.New("no such link: " + name)
	}
	switch {
	case a.openURL != nil:
		a.openURL(url)
	case a.ctx == nil:
		return errNoWindow
	default:
		wailsruntime.BrowserOpenURL(a.ctx, url)
	}
	return nil
}
