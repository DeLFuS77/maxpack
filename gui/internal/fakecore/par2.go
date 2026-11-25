// SPDX-License-Identifier: GPL-3.0-or-later

package fakecore

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/DeLFuS77/maxpack/gui/internal/par2/par2test"
)

// Par2Env says that the binary is to act as maxpack-par2 when it is run as the window runs par2
// (create, verify or repair with -m first), and how:
//   - "makes": create writes a set of the file (par2test: it reads as a set, and repairs nothing)
//     with the block size and recovery blocks it is given, telling some progress, and ends well;
//   - "fails": says why and ends with code 3, writing nothing;
//   - "slow": writes part of a set next to its index (create), or keeps the file of a repair as
//     <name>.1 and begins the repaired one (repair), tells some progress and waits a minute, to be
//     killed;
//   - "not-possible": a repair finds too much damage, as par2 says it, and ends with code 2.
const Par2Env = "MP_FAKE_PAR2"

// par2Call tells whether the arguments are the ones the window gives par2 (its version, -V, or a
// create, verify or repair): the engine is never given -m (the memory of par2), nor -V.
func par2Call(args []string) bool {
	if len(args) == 1 && args[0] == "-V" {
		return true
	}
	if len(args) < 2 {
		return false
	}
	switch args[0] {
	case "create", "verify", "repair":
		for _, arg := range args[1:] {
			if strings.HasPrefix(arg, "-m") {
				return true
			}
		}
	}
	return false
}

// par2Base is the base path of a call (-B).
func par2Base(args []string) string {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-B") {
			return arg[2:]
		}
	}
	return ""
}

// par2Files are the index and the file protected of a call: the two arguments after "--" of a
// create, the one of a repair.
func par2Files(args []string) (index, file string) {
	for i, arg := range args {
		if arg == "--" {
			rest := args[i+1:]
			if len(rest) > 0 {
				index = rest[0]
			}
			if len(rest) > 1 {
				file = rest[1]
			}
		}
	}
	return index, file
}

func par2Number(args []string, flag string) int {
	for _, arg := range args {
		if n, err := strconv.Atoi(strings.TrimPrefix(arg, flag)); strings.HasPrefix(arg, flag) && err == nil {
			return n
		}
	}
	return 0
}

func par2(mode string, args []string) int {
	if args[0] == "-V" {
		fmt.Println("par2cmdline-turbo version 1.5.0")
		return 0
	}
	index, file := par2Files(args)
	switch mode {
	case "makes":
		if args[0] != "create" {
			fmt.Print("All files are correct, repair is not required.\r\n")
			return 0
		}
		fmt.Print("Opening: x\r\nProcessing: 50.0%\rProcessing: 100.0%\rDone\r\n")
		blockSize, recovery := par2Number(args, "-s"), par2Number(args, "-c")
		head, volume := par2test.Set(filepath.Base(file), mustRead(file), blockSize, recovery)
		if err := os.WriteFile(index, head, 0o644); err != nil {
			fmt.Println(err)
			return 6
		}
		vol := strings.TrimSuffix(index, ".par2") + ".vol0+" + strconv.Itoa(recovery) + ".par2"
		if err := os.WriteFile(vol, volume, 0o644); err != nil {
			fmt.Println(err)
			return 6
		}
		return 0
	case "fails":
		fmt.Print("Invalid option specified\r\n")
		return 3
	case "slow":
		switch args[0] {
		case "create":
			os.WriteFile(strings.TrimSuffix(index, ".par2")+".vol0+1.par2", []byte("PAR2\x00PKT a medias"), 0o644)
		case "repair":
			// as par2 does once it repairs: the damaged one is kept as <name>.1, and the repaired one
			// is begun under its name
			base := par2Base(args)
			entries, _ := os.ReadDir(base)
			for _, entry := range entries {
				if name := entry.Name(); strings.HasSuffix(strings.ToLower(name), ".p5z") {
					os.Rename(filepath.Join(base, name), filepath.Join(base, name+".1"))
					os.WriteFile(filepath.Join(base, name), []byte("a medias"), 0o644)
				}
			}
		}
		fmt.Print("Opening: x\r\nProcessing: 10.0%\rRepairing: 10.0%\r")
		time.Sleep(time.Minute)
		return 0
	case "not-possible":
		fmt.Print("Scanning: 100.0%\rRepair is required.\r\nRepair is not possible.\r\nYou need 7 more recovery blocks to be able to repair.\r\n")
		return 2
	}
	fmt.Fprintln(os.Stderr, "MP_FAKE_PAR2 unknown:", mode)
	return 3
}

func mustRead(path string) []byte {
	data, _ := os.ReadFile(path)
	return data
}
