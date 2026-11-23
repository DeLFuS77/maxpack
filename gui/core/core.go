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
)

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
			return "", fmt.Errorf("no se encuentra maxpack-core en %s (MAXPACK_CORE)", path)
		}
		return path, nil
	}
	program, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("no se encuentra maxpack-core: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(program); err == nil {
		program = resolved
	}
	path := filepath.Join(filepath.Dir(program), Name())
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return "", fmt.Errorf("no se encuentra maxpack-core: debería estar junto al programa, en %s", path)
	}
	return path, nil
}

// Version is what the engine says of itself ("maxpack 1.0 (zstd ..., zlib ...)").
func Version(core string) (string, error) {
	out, err := exec.Command(core, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("maxpack-core no responde: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
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
// onEvent (which may be nil). When ctx ends it asks the engine to cancel and waits for it. It
// gives the result, and the exit code of the engine.
func Run(ctx context.Context, core string, args []string, onEvent func(Event)) (Event, int, error) {
	command := exec.Command(core, append(append([]string{}, args...), "--progress", "json", "--control", "stdin")...)
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
	ended := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-ctx.Done():
			stdin.Write([]byte("cancel\n"))
		case <-ended:
		}
		stdin.Close()
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

	code := command.ProcessState.ExitCode()
	var exit *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &exit) {
		return result, code, waitErr
	}
	if result.Kind != "result" {
		message := strings.TrimSpace(stderr.String())
		return result, code, fmt.Errorf("maxpack-core terminó sin resultado (código %d): %s", code, message)
	}
	return result, code, nil
}

// Info is `info --json` of a folder, an image or a .p5z.
func Info(core, path string) (map[string]any, error) {
	var stderr bytes.Buffer
	command := exec.Command(core, "info", "--json", path)
	command.Stderr = &stderr
	out, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("info de %s: %w: %s", path, err, strings.TrimSpace(stderr.String()))
	}
	var info map[string]any
	if err := json.Unmarshal(out, &info); err != nil {
		return nil, fmt.Errorf("info de %s: respuesta no válida: %w", path, err)
	}
	return info, nil
}

// Icon writes the icon of the game to out, as a PNG; an error when it has none.
func Icon(core, path, out string) error {
	var stderr bytes.Buffer
	command := exec.Command(core, "icon", path, "-o", out)
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("icono de %s: %w: %s", path, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
