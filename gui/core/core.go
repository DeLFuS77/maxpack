// SPDX-License-Identifier: GPL-3.0-or-later

// Package core runs maxpack-core, the engine of MaxPack, and reads what it says.
package core

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

// CancelGrace is how long Run waits for the engine to stop after asking it to cancel, before it
// kills it. It is only for an engine that hangs: one that cancels finishes the batch at work
// first (a 32 MiB zstd 19 frame on each thread) and then takes away what it wrote, which on a slow
// USB takes a while; killed before, it would leave that half written.
var CancelGrace = 120 * time.Second

// ErrClosing is the cause of a cancel because the window closes (context.WithCancelCause): the
// engine is asked to cancel and its input is closed, but it is never killed, so it takes away
// what it wrote on its own even once the window is gone.
var ErrClosing = errors.New("the window is closing")

// Required is what the window needs of the engine, as `maxpack --capabilities` says it: an engine
// that does not say one of them is of another version. "images": it reads the images ShadowMountPlus
// mounts (.ffpkg, .ffpfs, .ffpfsc), which the window offers besides folders and .exfat; "fpkg" and
// "passcode": it reads PS5 debug packages (.pkg), with the passcode of Settings; "completeness":
// info --json says whether a game has every file of the original package, and pack takes
// --leave-out-leftovers; "dlc": pack takes --dlc, and info --json says what a DLC is and the DLC a
// game carries.
var Required = []string{"progress_json", "control_stdin", "max_file_size", "info_json", "icon", "packs", "content_id", "levels", "measure", "images", "fpkg", "passcode", "completeness", "dlc"}

// passcode is the one of Settings › FPKG passcode ("" is the default, which the engine knows).
var passcode atomic.Value

// SetPasscode gives the engine, from now on, the passcode of a PS5 package that is not the default
// ("" for the default). Every run of the engine that reads a game (info, icon, pack, analyze) takes it.
func SetPasscode(p string) { passcode.Store(p) }

// PasscodeArgs are the arguments of the passcode for a run of the engine that reads a game: none for
// the default.
func PasscodeArgs() []string {
	if p, _ := passcode.Load().(string); p != "" {
		return []string{"--passcode", p}
	}
	return nil
}

// ValidPasscode tells whether p is a passcode a package can have: 32 printable ASCII characters,
// no spaces (as the engine wants it).
func ValidPasscode(p string) bool {
	if len(p) != 32 {
		return false
	}
	for i := 0; i < len(p); i++ {
		if p[i] < 0x21 || p[i] > 0x7E {
			return false
		}
	}
	return true
}

// Event is a line of the engine: "progress" or the final "result".
type Event struct {
	Kind, Phase string
	Done, Total uint64
	Raw         map[string]any
}

// Name is the file of the engine next to the window.
func Name() string {
	if runtime.GOOS == "windows" {
		return "maxpack-core.exe"
	}
	return "maxpack-core"
}

// Locate finds the engine: MAXPACK_CORE, else next to the program.
func Locate() (string, error) {
	if path := os.Getenv("MAXPACK_CORE"); path != "" {
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			return "", fmt.Errorf("maxpack-core not found at %s (MAXPACK_CORE)", path)
		}
		return path, nil
	}
	program, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("maxpack-core not found: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(program); err == nil {
		program = resolved
	}
	path := filepath.Join(filepath.Dir(program), Name())
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return "", fmt.Errorf("maxpack-core not found: it should be next to the program, at %s", path)
	}
	return path, nil
}

// Version is what the engine says of itself ("maxpack 1.0 (zstd ..., zlib ...)").
func Version(core string) (string, error) {
	out, err := hide(exec.Command(core, "--version")).Output()
	if err != nil {
		return "", fmt.Errorf("maxpack-core does not respond: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Missing gives what of Required the engine does not do, from `--capabilities`; an error when it
// does not answer it (an engine of before it).
func Missing(core string) ([]string, error) {
	var stderr bytes.Buffer
	command := hide(exec.Command(core, "--capabilities"))
	command.Stderr = &stderr
	out, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("maxpack-core does not say what it can do: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var can map[string]bool
	if err := json.Unmarshal(out, &can); err != nil {
		return nil, fmt.Errorf("maxpack-core does not say properly what it can do: %w", err)
	}
	missing := []string{}
	for _, need := range Required {
		if !can[need] {
			missing = append(missing, need)
		}
	}
	return missing, nil
}

func parse(line []byte) (Event, bool) {
	var raw map[string]any
	if json.Unmarshal(line, &raw) != nil {
		return Event{}, false
	}
	var numbers struct {
		Event, Phase string
		Done, Total  uint64
	}
	json.Unmarshal(line, &numbers)
	return Event{Kind: numbers.Event, Phase: numbers.Phase, Done: numbers.Done, Total: numbers.Total, Raw: raw}, true
}

// Run runs the engine with args plus --progress json --control stdin, and gives each progress to
// onEvent (which may be nil). When ctx ends it asks the engine to cancel, closes its input and
// waits for it, up to CancelGrace; an engine that does not stop by then is killed, and Run gives a
// result of its own ({"ok": false, "error": "cancelled"}) with code 130, as an engine that
// cancelled would. When ctx ends with the cause ErrClosing the engine is never killed. It gives
// the result, and the exit code of the engine.
func Run(ctx context.Context, core string, args []string, onEvent func(Event)) (Event, int, error) {
	return RunWith(ctx, core, args, RunOptions{}, onEvent)
}

// RunOptions are how the engine is run.
type RunOptions struct {
	// Low runs it below the other programs, so the PC stays usable while it works: below normal on
	// Windows (its threads go with it), nice 10 on macOS and Linux
	Low bool
}

// RunWith is Run with options.
func RunWith(ctx context.Context, core string, args []string, options RunOptions, onEvent func(Event)) (Event, int, error) {
	command, started := engineCommand(core, append(append([]string{}, args...), "--progress", "json", "--control", "stdin"), options.Low)
	stdin, err := command.StdinPipe()
	if err != nil {
		return Event{}, -1, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return Event{}, -1, err
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return Event{}, -1, err
	}
	if started != nil {
		started(command.Process)
	}
	ended := make(chan struct{})
	stopped := make(chan struct{})
	var killed atomic.Bool
	go func() {
		defer close(stopped)
		select {
		case <-ctx.Done():
			stdin.Write([]byte("cancel\n"))
			stdin.Close()
			if errors.Is(context.Cause(ctx), ErrClosing) {
				<-ended
				return
			}
			select {
			case <-ended:
			case <-time.After(CancelGrace):
				killed.Store(true)
				command.Process.Kill()
			}
		case <-ended:
			stdin.Close()
		}
	}()

	var result Event
	reader := bufio.NewReaderSize(stdout, 1<<16)
	for {
		line, err := reader.ReadBytes('\n')
		if event, ok := parse(bytes.TrimSpace(line)); ok {
			if event.Kind == "result" {
				result = event
			} else if onEvent != nil {
				onEvent(event)
			}
		}
		if err != nil {
			break
		}
	}
	waitErr := command.Wait()
	close(ended)
	<-stopped

	if killed.Load() {
		return Event{Kind: "result", Raw: map[string]any{"event": "result", "ok": false, "error": "cancelled"}}, 130, nil
	}
	code := command.ProcessState.ExitCode()
	var exit *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &exit) {
		return result, code, waitErr
	}
	if result.Kind != "result" {
		message := strings.TrimSpace(stderr.String())
		return result, code, fmt.Errorf("maxpack-core ended without a result (code %d): %s", code, message)
	}
	return result, code, nil
}

// Info is `info --json` of a folder, an image or a .p5z.
func Info(core, path string) (map[string]any, error) {
	var stderr bytes.Buffer
	command := hide(exec.Command(core, append([]string{"info", "--json", path}, PasscodeArgs()...)...))
	command.Stderr = &stderr
	out, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("info of %s: %w: %s", path, err, strings.TrimSpace(stderr.String()))
	}
	var info map[string]any
	if err := json.Unmarshal(out, &info); err != nil {
		return nil, fmt.Errorf("info of %s: invalid response: %w", path, err)
	}
	return info, nil
}

// Icon writes the icon of the game to out, as a PNG; an error when it has none.
func Icon(core, path, out string) error {
	var stderr bytes.Buffer
	command := hide(exec.Command(core, append([]string{"icon", path, "-o", out}, PasscodeArgs()...)...))
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("icon of %s: %w: %s", path, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
