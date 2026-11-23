// SPDX-License-Identifier: GPL-3.0-or-later

// Package fakecore lets the binary of a test stand in for maxpack-core: a test runs itself as the
// engine (os.Args[0]) with MP_FAKE_CORE saying how that engine behaves. Only the tests use it.
package fakecore

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Env says that the binary is to act as the engine, and how:
//   - "stubborn": tells some progress and then ignores cancel, for a minute;
//   - "writes-stubborn": writes part of its output (-o) and then ignores cancel, for a minute;
//   - "writes-slow-clean": writes part of its output and, once cancelled (the line "cancel", or
//     its input closed), takes MP_FAKE_CLEAN_MS (1000 by default) to take it away, as a big
//     batch of zstd on a slow disk does; then it leaves <output>.cleaned and ends as cancelled.
const Env = "MP_FAKE_CORE"

// Main acts as the engine and exits when Env says so; else it returns at once.
func Main() {
	mode := os.Getenv(Env)
	if mode == "" {
		return
	}
	os.Exit(run(mode, os.Args[1:]))
}

func run(mode string, args []string) int {
	if len(args) > 0 && args[0] == "--version" {
		fmt.Println("maxpack 0.0.1 (zstd 1.5.6, zlib 1.3.1)")
		return 0
	}
	switch mode {
	case "stubborn":
		progress()
		time.Sleep(time.Minute)
		return 0
	case "writes-stubborn", "writes-slow-clean":
		out := output(args)
		if out == "" {
			fmt.Fprintln(os.Stderr, "error: falta -o")
			return 2
		}
		if err := writePart(out, hasArg(args, "--p5z")); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		progress()
		if mode == "writes-stubborn" {
			time.Sleep(time.Minute)
			return 0
		}
		waitCancel()
		wait := 1000
		if ms, err := strconv.Atoi(os.Getenv("MP_FAKE_CLEAN_MS")); err == nil {
			wait = ms
		}
		time.Sleep(time.Duration(wait) * time.Millisecond)
		os.RemoveAll(out)
		os.WriteFile(out+".cleaned", []byte("limpio"), 0o644)
		fmt.Println(`{"event":"result","ok":false,"error":"cancelled"}`)
		return 130
	}
	fmt.Fprintln(os.Stderr, "MP_FAKE_CORE desconocido:", mode)
	return 2
}

func progress() {
	fmt.Println(`{"event":"progress","phase":"packing","done":1,"total":2}`)
}

func output(args []string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-o" {
			return args[i+1]
		}
	}
	return ""
}

func hasArg(args []string, a string) bool {
	for _, x := range args {
		if x == a {
			return true
		}
	}
	return false
}

// writePart leaves part of an output: a .p5z begun, or a folder with a file.
func writePart(out string, p5z bool) error {
	if p5z {
		return os.WriteFile(out, []byte("MAXPKP5Z a medias"), 0o644)
	}
	if err := os.MkdirAll(filepath.Join(out, "sce_sys"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, "eboot.bin"), []byte("a medias"), 0o644)
}

// waitCancel waits for the line "cancel" or the end of the input.
func waitCancel() {
	lines := bufio.NewScanner(os.Stdin)
	for lines.Scan() {
		if lines.Text() == "cancel" {
			return
		}
	}
}
