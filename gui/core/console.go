// SPDX-License-Identifier: GPL-3.0-or-later

package core

import (
	"bytes"
	"encoding/json"
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
// words. The code goes to the engine in its environment, never on its command line.
func ConsoleDrives(core, address, code string) ([]ConsoleDrive, string, error) {
	var stderr bytes.Buffer
	command := hide(exec.Command(core, "send", "--drives", address, "--json"))
	command.Env = append(os.Environ(), SendCodeEnv+"="+code)
	command.Stderr = &stderr
	out, err := command.Output()
	var answer struct {
		Drives  []ConsoleDrive `json:"drives"`
		OK      *bool          `json:"ok"`
		Error   string         `json:"error"`
		Message string         `json:"message"`
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
