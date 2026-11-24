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
//   - "old": an engine of before --capabilities: it knows --version and nothing else;
//   - "partial": an engine that says it can do less than the window needs;
//   - "writes-stubborn": writes part of its output (-o) and then ignores cancel, for a minute;
//   - "writes-slow-clean": writes part of its output and, once cancelled (the line "cancel", or
//     its input closed), takes MP_FAKE_CLEAN_MS (1000 by default) to take it away, as a big
//     batch of zstd on a slow disk does; then it leaves <output>.cleaned and ends as cancelled.
//   - "exists": something that is not the engine's is at its output (-o) when it looks: it writes
//     part of it and refuses with error "exists" (exit 1), as the real one does.
//   - "no-zstd-runtime": pack writes its output and ends well, saying that what it made carries no
//     runtime that reads zstd (runtime_without_zstd), as the real one does for a dump without one
//     and no --runtime; verify ends well.
//   - "priority": ends well at once, saying the priority it runs at ("priority" in its result: see
//     NormalPriority and LowPriority).
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
		fmt.Println("maxpack 1.0.0 (zstd 1.5.6, zlib 1.3.1)")
		return 0
	}
	switch mode {
	case "old":
		fmt.Fprintln(os.Stderr, "error: unknown command:", args)
		return 2
	case "partial":
		if len(args) > 0 && args[0] == "--capabilities" {
			fmt.Println(`{"progress_json":true,"control_stdin":true}`)
			return 0
		}
		return 2
	case "priority":
		fmt.Printf("{\"event\":\"result\",\"ok\":true,\"files_bad\":0,\"priority\":%q}\n", priority())
		return 0
	case "stubborn":
		progress()
		time.Sleep(time.Minute)
		return 0
	case "exists":
		out := output(args)
		if out == "" {
			fmt.Fprintln(os.Stderr, "error: missing -o")
			return 2
		}
		if err := writePart(out, hasArg(args, "--p5z")); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		fmt.Println(`{"event":"result","ok":false,"error":"exists"}`)
		return 1
	case "no-zstd-runtime":
		if len(args) > 0 && args[0] == "verify" {
			fmt.Println(`{"event":"result","ok":true,"files_bad":0}`)
			return 0
		}
		out := output(args)
		if out == "" {
			fmt.Fprintln(os.Stderr, "error: missing -o")
			return 2
		}
		if err := writePart(out, hasArg(args, "--p5z")); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		fmt.Println(`{"event":"result","ok":true,"original_size":10,"output_size":8,"runtime_without_zstd":true}`)
		return 0
	case "writes-stubborn", "writes-slow-clean":
		out := output(args)
		if out == "" {
			fmt.Fprintln(os.Stderr, "error: missing -o")
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
	fmt.Fprintln(os.Stderr, "MP_FAKE_CORE unknown:", mode)
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
