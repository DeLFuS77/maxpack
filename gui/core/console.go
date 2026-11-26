// SPDX-License-Identifier: GPL-3.0-or-later

package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ConsoleDrive is a drive of MaxPack for the console a .p5z can be sent to: its id ("internal", "usb0"...),
// its free space and size, and its file system.
type ConsoleDrive struct {
	ID    string `json:"id"`
	Free  uint64 `json:"free"`
	Total uint64 `json:"total"`
	FS    string `json:"fs"`
}

// SendCodeEnv is the variable the engine takes the pairing code from (maxpack send).
const SendCodeEnv = "MAXPACK_SEND_CODE"

// ConsoleDrives asks the console at address (send --drives) which drives it has. A refusal of the console,
// or no answer, is an error with its code ("bad_code", "receive_off", "unreachable"...) and the engine's
// words; ctx ending stops the engine ("cancelled", or "unreachable" when its deadline passed). The code goes to
// the engine in its environment, never on its command line.
func ConsoleDrives(ctx context.Context, core, address, code string) ([]ConsoleDrive, string, error) {
	var stderr bytes.Buffer
	command := hide(exec.CommandContext(ctx, core, "send", "--drives", address, "--json"))
	command.Env = append(os.Environ(), SendCodeEnv+"="+code)
	command.Stderr = &stderr
	out, err := command.Output()
	var answer struct {
		Drives  []ConsoleDrive `json:"drives"`
		OK      *bool          `json:"ok"`
		Error   string         `json:"error"`
		Message string         `json:"message"`
	}
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, "unreachable", fmt.Errorf("no answer from the console at %s in time", address)
		}
		return nil, "cancelled", ctx.Err()
	}
	if json.Unmarshal(bytes.TrimSpace(out), &answer) != nil {
		return nil, "io", fmt.Errorf("the drives of the console: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	if answer.Error != "" {
		return nil, answer.Error, fmt.Errorf("%s", answer.Message)
	}
	if err != nil {
		return nil, "io", fmt.Errorf("the drives of the console: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if answer.Drives == nil {
		answer.Drives = []ConsoleDrive{}
	}
	return answer.Drives, "", nil
}

// DiscardOnConsole deletes what the console at address received of the .p5z at path on that drive (send
// --discard): it gives what there was, or an error with its code ("exists": the console has it whole, and keeps
// it). The code goes to the engine in its environment.
func DiscardOnConsole(ctx context.Context, core, address, code, path, drive string) (uint64, string, error) {
	var stderr bytes.Buffer
	args := []string{"send", "--discard", path, address, "--json"}
	if drive != "" {
		args = append(args, "--drive", drive)
	}
	command := hide(exec.CommandContext(ctx, core, args...))
	command.Env = append(os.Environ(), SendCodeEnv+"="+code)
	command.Stderr = &stderr
	out, err := command.Output()
	var answer struct {
		OK      bool   `json:"ok"`
		Had     uint64 `json:"had"`
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(bytes.TrimSpace(out), &answer) != nil {
		return 0, "io", fmt.Errorf("deleting on the console: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	if !answer.OK {
		if answer.Error == "" {
			answer.Error = "io"
		}
		return 0, answer.Error, fmt.Errorf("%s", answer.Message)
	}
	return answer.Had, "", nil
}
