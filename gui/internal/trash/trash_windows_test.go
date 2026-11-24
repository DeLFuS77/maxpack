// SPDX-License-Identifier: GPL-3.0-or-later

package trash

import "testing"

// The size Windows gives the Recycle Bin of a drive it has no settings for: 10 % of the first
// 40 GB and 5 % of the rest. A game of 100 GB does not fit in the one of a drive of 1 TB.
func TestDefaultCapacity(t *testing.T) {
	for _, c := range []struct{ drive, want int64 }{
		{0, 0},
		{10240, 1024},
		{40960, 4096},
		{1 << 20, 4096 + (1<<20-40960)/20},
	} {
		if got := capacityFor(c.drive); got != c.want {
			t.Errorf("%d MB: %d, expected %d", c.drive, got, c.want)
		}
	}
	if capacityFor(1<<20)<<20 > 100<<30 {
		t.Error("a game of 100 GB fits in the Recycle Bin of a drive of 1 TB")
	}
}

// A network share has no Recycle Bin: it is told before anything is asked of the shell.
func TestNetworkHasNoBin(t *testing.T) {
	for _, path := range []string{`\\server\share\game`, `\\?\UNC\server\share\game`} {
		if !isNetwork(path) {
			t.Errorf("%s is not a network path", path)
		}
		if why, ok := Why(check(path)); !ok || why.Reason != NoBin || why.Drive != `\\server\share` {
			t.Errorf("%s: %+v %v", path, why, ok)
		}
	}
	if isNetwork(`C:\games`) || isNetwork(`\\?\C:\games`) {
		t.Error("a local path is told as a network one")
	}
}
