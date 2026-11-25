// SPDX-License-Identifier: GPL-3.0-or-later

package par2

import (
	"reflect"
	"strings"
	"testing"
)

// feed gives the output to a meter in pieces of n bytes, as a pipe may cut it, and gives the
// permilles it told and the lines that were not progress.
func feed(m *Meter, output string, n int) (told []uint64, lines []string) {
	m.OnProgress = func(permille uint64) { told = append(told, permille) }
	m.OnLine = func(line string) { lines = append(lines, line) }
	for at := 0; at < len(output); at += n {
		m.Write([]byte(output[at:min(len(output), at+n)]))
	}
	m.Close()
	return told, lines
}

// what par2 writes while it makes a set in one pass (the hashes go with the processing)
const createOnePass = "Block size: 1048576\r\nSource file count: 1\r\nSource block count: 64\r\nRecovery block count: 3\r\n" +
	"Recovery file count: 1\r\n\r\n\r\nOpening: Game A.p5z\r\nProcessing: 1.6%\rProcessing: 59.4%\rProcessing: 100.0%\r" +
	"Writing recovery packets\rWrote 3145728 bytes to disk\r\nWriting verification packets\r\nDone\r\n"

// and in several passes: the hashes first, with no name before the percentage
const createPasses = "Block size: 1048576\r\nOpening: g.p5z\r\n0.3%\r13.3%\r51.7%\r100.0%\rProcessing: 0.1%\r" +
	"Writing recovery packets\rWrote 455840 bytes to disk\r\nProcessing: 19.9%\rProcessing: 100.0%\rDone\r\n"

func TestMeterCreate(t *testing.T) {
	for _, n := range []int{1, 7, 4096} {
		told, lines := feed(NewMeter(Making), createOnePass, n)
		if want := []uint64{16, 594, 1000}; !reflect.DeepEqual(told, want) {
			t.Errorf("one pass, pieces of %d: %v", n, told)
		}
		if len(lines) == 0 || lines[len(lines)-1] != "Done" || strings.Contains(strings.Join(lines, "|"), "%") {
			t.Errorf("lines: %q", lines)
		}
		told, _ = feed(NewMeter(Making), createPasses, n)
		// the hashes are the first half, the processing the second
		if want := []uint64{1, 66, 258, 500, 599, 1000}; !reflect.DeepEqual(told, want) {
			t.Errorf("passes, pieces of %d: %v", n, told)
		}
	}
}

// what par2 writes while it repairs a file
const repairOutput = "Loading \"a.p5z.par2\".\r\nLoading: 5.4%\rLoading: 100.0%\rLoaded 4 new packets\r\n" +
	"Loading \"a.p5z.vol0+3.par2\".\r\nLoading: 33.3%\rLoading: 100.0%\rLoaded 3 new packets including 3 recovery blocks\r\n" +
	"Verifying source files:\r\n\r\nOpening: \"a.p5z\"\r\nScanning: 1.6%\rScanning: 100.0%\rTarget: \"a.p5z\" - damaged. Found 62 of 64 data blocks.\r\n" +
	"Repair is required.\r\nRepair is possible.\r\nComputing Reed Solomon matrix.\r\nConstructing: 0.0%\rConstructing: done.\r\n" +
	"Solving: 20.0%\rSolving: done.\r\nRepairing: 1.6%\rRepairing: 100.0%\rWriting recovered data\r\n\r\nVerifying repaired files:\r\n\r\n" +
	"Opening: \"a.p5z\"\r\nScanning: 60.9%\rScanning: 100.0%\rTarget: \"a.p5z\" - found.\r\n\r\nRepair complete.\r\n"

func TestMeterRepair(t *testing.T) {
	told, lines := feed(NewMeter(Repairing), repairOutput, 5)
	// loading the set, reading the file, repairing it and reading it again
	if want := []uint64{1, 20, 25, 350, 355, 700, 882, 1000}; !reflect.DeepEqual(told, want) {
		t.Errorf("repair: %v", told)
	}
	if !reflect.DeepEqual(Outcome(lines), Result{Repaired: true}) {
		t.Errorf("outcome: %+v", Outcome(lines))
	}
}

func TestOutcome(t *testing.T) {
	cases := []struct {
		output string
		want   Result
	}{
		{"Repair is required.\nYou have 60 out of 64 data blocks available.\nRepair is not possible.\nYou need 1 more recovery blocks to be able to repair.\n",
			Result{NotPossible: true, Missing: 1}},
		{"Verifying source files:\nTarget: \"a.p5z\" - found.\nAll files are correct, repair is not required.\n", Result{Complete: true}},
		{"Target: \"b.p5z\" - found.\nRepair is required.\n1 file(s) have the wrong name.\nRepair is possible.\n", Result{Renamed: true}},
		{"Repair complete.\n", Result{Repaired: true}},
		{"Repair Failed.\n", Result{}},
	}
	for _, c := range cases {
		if got := Outcome(strings.Split(strings.TrimSpace(c.output), "\n")); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: %+v", c.output, got)
		}
	}
}

func TestMeterNeverGoesBack(t *testing.T) {
	told, _ := feed(NewMeter(Making), "Processing: 50.0%\rProcessing: 40.0%\rProcessing: 60.0%\r", 3)
	if want := []uint64{500, 600}; !reflect.DeepEqual(told, want) {
		t.Errorf("%v", told)
	}
}
