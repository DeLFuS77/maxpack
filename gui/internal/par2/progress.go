// SPDX-License-Identifier: GPL-3.0-or-later

package par2

// What par2 writes as it works: lines ended by "\n" and, for its progress, "<step>: 12.3%" ended by
// "\r" so a console writes the next one over it. The meter reads it and tells how far the whole run
// is, in thousandths, never going back: making a set reads the file once (or hashes it in a pass of
// its own first, when the set needs more memory than par2 has); repairing loads the set, reads the
// file, writes it again and reads what it wrote.

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
)

// What a meter reads.
const (
	Making    = iota // par2 create
	Repairing        // par2 repair
)

// Meter reads the output of par2 (it is an io.Writer): OnProgress is told the thousandths of the
// whole run each time they grow, and OnLine each line that is not progress.
type Meter struct {
	kind       int
	OnProgress func(permille uint64)
	OnLine     func(line string)
	pending    []byte
	told       uint64
	hashed     bool // making: a pass of hashes came first
	repairing  bool // repairing: the repair started, the next reading is of the repaired file
}

// NewMeter is a meter of what par2 writes while making a set (Making) or repairing (Repairing).
func NewMeter(kind int) *Meter { return &Meter{kind: kind} }

// a progress line: its step ("Processing: ", or nothing for the hashes) and its percentage
var progressLine = regexp.MustCompile(`^([A-Za-z ]*?:? ?)(\d{1,3})\.(\d)%$`)

// Write takes what par2 wrote, in any pieces.
func (m *Meter) Write(data []byte) (int, error) {
	m.pending = append(m.pending, data...)
	for {
		at := bytes.IndexAny(m.pending, "\r\n")
		if at < 0 {
			break
		}
		m.line(string(m.pending[:at]))
		m.pending = m.pending[at+1:]
	}
	return len(data), nil
}

// Close reads what is left without an end of line.
func (m *Meter) Close() error {
	if len(m.pending) > 0 {
		m.line(string(m.pending))
		m.pending = nil
	}
	return nil
}

func (m *Meter) line(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	found := progressLine.FindStringSubmatch(text)
	if found == nil {
		if m.OnLine != nil {
			m.OnLine(text)
		}
		return
	}
	whole, _ := strconv.Atoi(found[2])
	tenth, _ := strconv.Atoi(found[3])
	value := min(1000, uint64(whole*10+tenth))
	step := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(found[1]), ":"))
	var at uint64
	switch m.kind {
	case Making:
		switch step {
		case "":
			m.hashed = true
			at = value / 2
		case "Processing":
			at = value
			if m.hashed {
				at = 500 + value/2
			}
		default:
			return
		}
	case Repairing:
		switch step {
		case "Loading":
			at = value * 20 / 1000
		case "Scanning":
			at = 20 + value*330/1000
			if m.repairing {
				at = 700 + value*300/1000
			}
		case "Repairing", "Processing":
			m.repairing = true
			at = 350 + value*350/1000
		default:
			return
		}
	}
	if at > m.told {
		m.told = at
		if m.OnProgress != nil {
			m.OnProgress(at)
		}
	}
}

// Result is how a repair ended, by what par2 said: the file was repaired; it was whole already
// (Complete) or whole under another name (Renamed); or it could not be repaired, with the recovery
// blocks it lacked.
type Result struct {
	Repaired, Complete, Renamed bool
	NotPossible                 bool
	Missing                     int
}

var needMore = regexp.MustCompile(`^You need (\d+) more recovery blocks`)

// Outcome reads how a repair ended from the lines par2 wrote.
func Outcome(lines []string) Result {
	var r Result
	for _, line := range lines {
		switch {
		case line == "Repair complete.":
			r.Repaired = true
		case line == "All files are correct, repair is not required.":
			r.Complete = true
		case strings.HasSuffix(line, "file(s) have the wrong name."):
			r.Renamed = true
		case line == "Repair is not possible.":
			r.NotPossible = true
		default:
			if found := needMore.FindStringSubmatch(line); found != nil {
				r.Missing, _ = strconv.Atoi(found[1])
			}
		}
	}
	return r
}
