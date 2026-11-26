// SPDX-License-Identifier: GPL-3.0-or-later

// Package par2 is the repair data of a .p5z: a standard PAR2 set next to it, made and used by
// maxpack-par2 (par2cmdline-turbo, a program of its own that comes with MaxPack). It reads a set
// without par2 (which file it protects and how much it can repair), tells whether it is the set of a
// .p5z, gives the arguments that make a set and repair a file, reads the progress par2 writes, and
// runs par2.
package par2

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/DeLFuS77/maxpack/gui/core"
)

// The exit codes of par2 that the window reads.
const (
	ExitOK          = 0 // done; a verify found everything right
	ExitRepairable  = 1 // a verify found damage it can repair
	ExitNotPossible = 2 // too much damage for the recovery blocks there are
)

// ErrCancelled: the run was cancelled and par2 was stopped.
var ErrCancelled = errors.New("cancelled")

// Name is the file of maxpack-par2 next to the window.
func Name() string {
	if runtime.GOOS == "windows" {
		return "maxpack-par2.exe"
	}
	return "maxpack-par2"
}

// Locate finds maxpack-par2: MAXPACK_PAR2, else next to the program (as core.Locate finds the engine).
func Locate() (string, error) {
	if path := os.Getenv("MAXPACK_PAR2"); path != "" {
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			return "", fmt.Errorf("maxpack-par2 not found at %s (MAXPACK_PAR2)", path)
		}
		return path, nil
	}
	program, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("maxpack-par2 not found: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(program); err == nil {
		program = resolved
	}
	path := filepath.Join(filepath.Dir(program), Name())
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return "", fmt.Errorf("maxpack-par2 not found: it should be next to the program, at %s", path)
	}
	return path, nil
}

// Version is what maxpack-par2 says of itself ("par2cmdline-turbo version 1.5.0").
func Version(program string) (string, error) {
	command, _ := core.Command(program, []string{"-V"}, false)
	out, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("maxpack-par2 does not respond: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Run runs maxpack-par2 with args, low below the other programs (as the engine runs), and gives
// what it writes to meter. par2 cannot be asked to stop: when ctx ends it is killed, and Run gives
// ErrCancelled once it ended (what it left half written is for the caller to take away). Nor can it be
// asked to pause: while pause (which may be nil) is on, it is suspended. It gives the exit code of par2.
func Run(ctx context.Context, program string, args []string, low bool, pause *core.Pause, meter *Meter) (int, error) {
	command, started := core.Command(program, args, low)
	command.Stdout, command.Stderr = meter, meter
	if err := command.Start(); err != nil {
		return -1, err
	}
	if started != nil {
		started(command.Process)
	}
	// suspended as the pause is, each change taking the pause as it is when its turn comes; once it
	// ended, never again
	var mutex sync.Mutex
	suspended, gone := false, false
	follow := func() {
		mutex.Lock()
		defer mutex.Unlock()
		switch on := pause.On(); {
		case gone || on == suspended:
		case on:
			suspended = core.Suspend(command.Process) == nil
		default:
			suspended = core.Resume(command.Process) != nil
		}
	}
	defer pause.Listen(func(bool) { follow() })()
	follow()
	ended := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			command.Process.Kill()
		case <-ended:
		}
	}()
	err := command.Wait()
	mutex.Lock()
	gone = true
	mutex.Unlock()
	close(ended)
	meter.Close()
	if ctx.Err() != nil {
		return -1, ErrCancelled
	}
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return -1, err
	}
	return command.ProcessState.ExitCode(), nil
}
