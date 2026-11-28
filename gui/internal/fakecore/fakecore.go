// SPDX-License-Identifier: GPL-3.0-or-later

// Package fakecore lets the binary of a test stand in for maxpack-core: a test runs itself as the
// engine (os.Args[0]) with MP_FAKE_CORE saying how that engine behaves. Only the tests use it.
package fakecore

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
//   - "from-packed": pack writes its output and ends well, saying it was made from a game packed already
//     at level 5 (from_packed, packed_level), its packs kept when given --keep-packs (kept_packs), as
//     the real one does; and a game whose folder ends in "-dump" was not packed. verify ends well.
//   - "priority": ends well at once, saying the priority it runs at ("priority" in its result: see
//     NormalPriority and LowPriority).
//   - "writes": pack and unpack write their output (in an empty folder that is there too), tell
//     some progress and end well; verify ends well.
//   - "bad-verify": as "writes", but verify finds a damaged file.
//   - "writes-info": as "writes", and info --json as "info" (by the name of the file, its tmp name too).
//   - "info": info --json says of a game what the JSON file InfoEnv has under the name of its file
//     or folder, and fails for any other; icon writes the head of a PNG (-o) for a game whose JSON
//     there has "icon": true, and finds none for any other. info --json takes --read-only before the
//     path, and icon takes it before the path too. When CallsEnv names a file, each call of info or
//     icon adds a line to it: its arguments, separated by tabs.
//   - "levels": analyze --levels tells two levels (3 and 19) as it goes and in its result, with the
//     threads it was given (8 without --threads).
//   - "measure": measure tries 2, 4, 6 and 8 threads of 8, recommends 6 (8 is the fastest) at the
//     level given, and says the disk of the folder given (none without one).
//   - "waits": tells some progress and waits for cancel (the line "cancel", or its input closed),
//     then ends as cancelled.
//   - "grows": add and update append "+grown" to the .p5z they are given, tell some progress and end
//     well, saying what a real one says; verify ends well.
//   - "mostly": as "grows", but update refuses with error "mostly_changed" unless given --force.
//   - "grows-verify-waits": as "grows", but verify tells some progress and waits for cancel, then ends
//     as cancelled.
//   - "sends": send --drives lists two drives, and send of a .p5z tells the progress of its bytes and of
//     the console's verify and ends well (writing <p5z>.sent: see send.go), or with the refusal
//     SendFailEnv names;
//   - "sends-waits": as "sends", but it waits for cancel while it sends, then ends as cancelled.
//   - "pauses": verify ends well at once; pack and unpack write part of their output; it tells some
//     progress and reads its input,
//     each line kept in the file LinesEnv names: "pause" says {"event":"paused"}, "resume" says
//     {"event":"resumed"} and ends well (as "writes" does), "cancel" or the end of the input ends as
//     cancelled.
//   - "ticks": prints a line every 10 ms, for a minute (to be suspended).
const Env = "MP_FAKE_CORE"

// LinesEnv is the file where "pauses" keeps each line of its input.
const LinesEnv = "MP_FAKE_LINES"

// CallsEnv is the file where "info" keeps each call of info and icon, and "sends" each call of send --drives, one line of tab-separated arguments.
const CallsEnv = "MP_FAKE_CALLS"

// InfoEnv is the file of what "info" says of each game: {"<name>": {<info --json>}, ...}.
const InfoEnv = "MP_FAKE_INFO"

// Main acts as the engine and exits when Env says so, or as maxpack-par2 when Par2Env does and it
// is run as par2; else it returns at once.
func Main() {
	if mode := os.Getenv(Par2Env); mode != "" && par2Call(os.Args[1:]) {
		os.Exit(par2(mode, os.Args[1:]))
	}
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
	case "info", "writes-info":
		if mode == "writes-info" && !(len(args) >= 3 && args[0] == "info") {
			return run("writes", args)
		}
		if path := os.Getenv(CallsEnv); path != "" && len(args) > 0 && (args[0] == "info" || args[0] == "icon") {
			if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
				fmt.Fprintln(f, strings.Join(args, "	"))
				f.Close()
			}
		}
		if len(args) >= 3 && args[0] == "info" && args[1] == "--json" {
			target := args[2]
			if target == "--read-only" && len(args) >= 4 {
				target = args[3]
			}
			var infos map[string]json.RawMessage
			data, _ := os.ReadFile(os.Getenv(InfoEnv))
			json.Unmarshal(data, &infos)
			if info, ok := infos[filepath.Base(target)]; ok {
				fmt.Println(string(info))
				return 0
			}
			fmt.Fprintln(os.Stderr, "error: not a game:", target)
			return 1
		}
		if len(args) >= 4 && args[0] == "icon" {
			target, out := args[1], ""
			if target == "--read-only" {
				target = args[2]
			}
			for i := 1; i+1 < len(args); i++ {
				if args[i] == "-o" {
					out = args[i+1]
				}
			}
			var infos map[string]struct {
				Icon bool `json:"icon"`
			}
			data, _ := os.ReadFile(os.Getenv(InfoEnv))
			json.Unmarshal(data, &infos)
			if info, ok := infos[filepath.Base(target)]; ok && info.Icon && out != "" {
				// the signature of a PNG and its first chunk's head: the window keeps the bytes as they are
				png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 13, 'I', 'H', 'D', 'R'}
				if os.WriteFile(out, png, 0o644) == nil {
					return 0
				}
			}
		}
		fmt.Fprintln(os.Stderr, "error: no icon")
		return 1
	case "writes", "bad-verify":
		if len(args) > 0 && args[0] == "verify" {
			if mode == "bad-verify" {
				fmt.Println(`{"event":"result","ok":false,"files_bad":1}`)
				return 1
			}
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
		progress()
		fmt.Println(`{"event":"result","ok":true,"original_size":10,"output_size":8}`)
		return 0
	case "stubborn":
		progress()
		time.Sleep(time.Minute)
		return 0
	case "levels":
		threads := "8"
		for i := 0; i+1 < len(args); i++ {
			if args[i] == "--threads" {
				threads = args[i+1]
			}
		}
		const low = `"level":3,"packed":500,"p5z_size":520,"folder_size":530,"rate":400,"p5z_seconds":2,"folder_seconds":2`
		const high = `"level":19,"packed":380,"p5z_size":400,"folder_size":410,"rate":50,"p5z_seconds":9,"folder_seconds":8`
		fmt.Println(`{"event":"progress","phase":"working","done":1,"total":2}`)
		fmt.Println(`{"event":"level",` + low + `}`)
		fmt.Println(`{"event":"progress","phase":"working","done":2,"total":2}`)
		fmt.Println(`{"event":"level",` + high + `}`)
		fmt.Printf("{\"event\":\"result\",\"ok\":true,\"threads\":%s,\"original_size\":1000,\"sample_bytes\":700,\"sample_rate\":1,"+
			"\"levels\":[{%s},{%s}]}\n", threads, low, high)
		return 0
	case "measure":
		level, disk := "19", "null"
		for i := 1; i < len(args); i++ {
			if args[i] == "--level" && i+1 < len(args) {
				level = args[i+1]
			}
		}
		if len(args) > 1 && args[1] != "" && args[1][0] != '-' {
			disk = `{"written":4096,"write_rate":1000,"read":4096,"read_rate":2000}`
		}
		trials := ""
		for n, rate := range []int{100, 200, 300, 310} {
			trial := fmt.Sprintf(`{"threads":%d,"bytes":1000,"seconds":1,"rate":%d}`, 2*(n+1), rate)
			fmt.Printf("{\"event\":\"progress\",\"phase\":\"working\",\"done\":%d,\"total\":4}\n", n+1)
			fmt.Println(`{"event":"trial",` + trial[1:])
			if n > 0 {
				trials += ","
			}
			trials += trial
		}
		fmt.Printf("{\"event\":\"result\",\"ok\":true,\"cpu_threads\":8,\"level\":%s,\"trials\":[%s],\"recommended_threads\":6,"+
			"\"fastest_threads\":8,\"disk\":%s}\n", level, trials, disk)
		return 0
	case "grows", "mostly", "grows-verify-waits":
		if len(args) > 0 && args[0] == "verify" {
			if mode == "grows-verify-waits" {
				progress()
				waitCancel()
				fmt.Println(`{"event":"result","ok":false,"error":"cancelled"}`)
				return 130
			}
			fmt.Println(`{"event":"result","ok":true,"files_bad":0}`)
			return 0
		}
		if len(args) < 2 || (args[0] != "add" && args[0] != "update") {
			fmt.Fprintln(os.Stderr, "error: not add or update:", args)
			return 2
		}
		if mode == "mostly" && args[0] == "update" && !hasArg(args, "--force") {
			fmt.Println(`{"event":"result","ok":false,"error":"mostly_changed"}`)
			return 1
		}
		f, err := os.OpenFile(args[1], os.O_APPEND|os.O_WRONLY, 0)
		if err == nil {
			_, err = f.WriteString("+grown")
			f.Close()
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		fmt.Println(`{"event":"progress","phase":"comparing","done":1,"total":2}`)
		progress()
		fmt.Println(`{"event":"result","ok":true,"same_files":3,"new_files":1,"changed_files":0,"removed_files":0,` +
			`"added_bytes":6,"grown":1,"unused_bytes":0,"nothing":false,"par2":false}`)
		return 0
	case "sends", "sends-waits":
		if len(args) > 0 && args[0] == "send" {
			return send(mode, args)
		}
		fmt.Fprintln(os.Stderr, "error: not send:", args)
		return 2
	case "pauses":
		return pauses(args)
	case "ticks":
		for end := time.Now().Add(time.Minute); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
			fmt.Println("tick")
		}
		return 0
	case "waits":
		progress()
		waitCancel()
		fmt.Println(`{"event":"result","ok":false,"error":"cancelled"}`)
		return 130
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
	case "from-packed":
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
		from := len(args) > 1 && !strings.HasSuffix(args[1], "-dump")
		fmt.Printf("{\"event\":\"result\",\"ok\":true,\"original_size\":10,\"output_size\":8,\"from_packed\":%v,\"packed_level\":%d,\"kept_packs\":%v}\n",
			from, map[bool]int{true: 5}[from], from && hasArg(args, "--keep-packs"))
		return 0
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

// pauses is the mode "pauses" (see Env).
func pauses(args []string) int {
	if len(args) > 0 && args[0] == "verify" {
		fmt.Println(`{"event":"result","ok":true,"files_bad":0}`)
		return 0
	}
	out := output(args)
	if out != "" {
		if err := writePart(out, hasArg(args, "--p5z")); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
	}
	progress()
	lines := bufio.NewScanner(os.Stdin)
	for lines.Scan() {
		if path := os.Getenv(LinesEnv); path != "" {
			if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
				fmt.Fprintln(f, lines.Text())
				f.Close()
			}
		}
		switch lines.Text() {
		case "pause":
			fmt.Println(`{"event":"paused"}`)
		case "resume":
			fmt.Println(`{"event":"resumed"}`)
			fmt.Println(`{"event":"result","ok":true,"files_bad":0,"original_size":10,"output_size":8}`)
			return 0
		case "cancel":
			if out != "" {
				os.RemoveAll(out)
			}
			fmt.Println(`{"event":"result","ok":false,"error":"cancelled"}`)
			return 130
		}
	}
	if out != "" {
		os.RemoveAll(out)
	}
	fmt.Println(`{"event":"result","ok":false,"error":"cancelled"}`)
	return 130
}
