// SPDX-License-Identifier: GPL-3.0-or-later

package fakecore

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// SendFailEnv makes "send" of the modes "sends" end with that code, as the real one ends with each refusal
// of the console (and "verify_failed" with what the console found wrong).
const SendFailEnv = "MP_FAKE_SEND_FAIL"

// sendExit is the exit code of the real one for each code (maxpack send's usage).
var sendExit = map[string]int{"unreachable": 3, "receive_off": 4, "bad_code": 5, "locked": 6, "exists": 7, "busy": 8,
	"no_space": 9, "fat32": 10, "bad_name": 11, "no_drive": 12, "verify_failed": 13, "console_cancelled": 14}

// send is "send" of the modes "sends" and "sends-waits": --drives lists two drives; a .p5z is "sent" (the
// engine writes <p5z>.sent with the address, drive and code it was given), with the progress of the bytes
// and of the console's verify, as the real one tells them. The code comes in MAXPACK_SEND_CODE; one on the
// command line is refused, as is none.
func send(mode string, args []string) int {
	code := os.Getenv("MAXPACK_SEND_CODE")
	for _, a := range args {
		if a == "--code" {
			fmt.Println(`{"event":"result","ok":false,"error":"code_on_the_command_line"}`)
			return 2
		}
	}
	fail := os.Getenv(SendFailEnv)
	if code == "" {
		fail = "bad_code"
	}
	if hasArg(args, "--drives") {
		if fail != "" {
			fmt.Printf("{\"ok\":false,\"error\":%q,\"message\":\"the console says %s\"}\n", fail, fail)
			return sendExit[fail]
		}
		fmt.Println(`{"drives":[{"id":"internal","free":600000000000,"total":825000000000,"fs":"ufs"},` +
			`{"id":"usb0","free":1500000000000,"total":2000000000000,"fs":"exfat"}]}`)
		return 0
	}
	if len(args) < 3 {
		return 2
	}
	p5z, address, drive := args[1], args[2], ""
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--drive" {
			drive = args[i+1]
		}
	}
	info, err := os.Stat(p5z)
	if err != nil {
		fmt.Println(`{"event":"result","ok":false,"error":"bad_input"}`)
		return 1
	}
	size := uint64(info.Size())
	fmt.Printf("{\"event\":\"started\",\"name\":%q,\"drive\":%q,\"have\":0,\"size\":%d}\n", info.Name(), drive, size)
	fmt.Printf("{\"event\":\"progress\",\"phase\":\"sending\",\"done\":%d,\"total\":%d,\"rate\":1000000,\"eta\":1}\n", size/2, size)
	if mode == "sends-waits" {
		waitCancel()
		fmt.Println(`{"event":"result","ok":false,"error":"cancelled"}`)
		return 130
	}
	switch fail {
	case "":
	case "verify_failed":
		bad, _ := json.Marshal([]map[string]any{{"path": "ampr_zstd-001.pak", "volume": 1, "why": "sha256"}})
		fmt.Printf("{\"event\":\"progress\",\"phase\":\"verifying\",\"done\":%d,\"total\":%d}\n", size, size)
		fmt.Printf("{\"event\":\"result\",\"ok\":false,\"error\":\"verify_failed\",\"bad\":%s}\n", bad)
		return 13
	default:
		fmt.Printf("{\"event\":\"result\",\"ok\":false,\"error\":%q,\"message\":\"no\"}\n", fail)
		if n, ok := sendExit[fail]; ok {
			return n
		}
		return 1
	}
	fmt.Printf("{\"event\":\"progress\",\"phase\":\"sending\",\"done\":%d,\"total\":%d,\"rate\":1000000,\"eta\":0}\n", size, size)
	fmt.Printf("{\"event\":\"progress\",\"phase\":\"verifying\",\"done\":%d,\"total\":%d}\n", size/2, size)
	fmt.Printf("{\"event\":\"progress\",\"phase\":\"verifying\",\"done\":%d,\"total\":%d}\n", size, size)
	record := "to " + address + " drive " + drive + " code " + code
	if err := os.WriteFile(p5z+".sent", []byte(strings.TrimSpace(record)), 0o644); err != nil {
		fmt.Println(`{"event":"result","ok":false,"error":"io"}`)
		return 1
	}
	fmt.Printf("{\"event\":\"result\",\"ok\":true,\"name\":%q,\"drive\":%q,\"size\":%d,\"sent\":%d,\"resumed_from\":0,\"retries\":0}\n",
		info.Name(), drive, size, size)
	return 0
}
