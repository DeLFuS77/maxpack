// SPDX-License-Identifier: GPL-3.0-or-later

package par2

// How MaxPack makes a set, measured on a Ryzen 9 7950X3D (32 threads) with a 990 PRO NVMe and a
// generated .p5z of 5.63 GB (see the CHANGELOG): par2 has to hash the whole file with MD5, one
// stream, so making the set goes at the speed of that hash (about 600-700 MB/s there) whatever its
// blocks; more blocks only take more CPU. About 4000 blocks keep that work a small share of the CPU
// and a bad byte costs one block of at most 32 MiB (about 25 MB for a game of 100 GB). Each block is
// a whole number of 4 KiB, so a bad sector of a disk is in one block only. par2 is given 960 MiB of
// memory: a set that needs more is made in several passes over the file.

import (
	"math/bits"
	"path/filepath"
	"strconv"
)

const (
	// TargetBlocks are the blocks a file is cut into, about
	TargetBlocks = 4000
	// MinBlockSize and MaxBlockSize bound a block; BlockAlign is what each one is a multiple of
	MinBlockSize = 4096
	MaxBlockSize = 32 << 20
	BlockAlign   = 4096
	// MaxSourceBlocks are the most blocks par2 cuts a file into
	MaxSourceBlocks = 32768
	// MemoryMB is the memory par2 may take, in MiB (its -m)
	MemoryMB = 960
	// Percents are the amounts of repair data the window offers, and DefaultPercent the one it proposes
	DefaultPercent = 5
)

// Percents are the amounts of repair data the window offers, as a percentage of the .p5z.
var Percents = []int{1, 5, 10}

// ValidPercent tells whether p is one of Percents.
func ValidPercent(p int) bool {
	for _, x := range Percents {
		if x == p {
			return true
		}
	}
	return false
}

func ceilDiv(a, b uint64) uint64 { return (a + b - 1) / b }

func roundUp(a, to uint64) uint64 { return ceilDiv(a, to) * to }

// Blocks is how a file of size bytes is protected at percent: the size of its blocks, how many it
// is cut into and the recovery blocks of the set (one at least).
func Blocks(size uint64, percent int) (blockSize, source, recovery uint64) {
	if size == 0 {
		return MinBlockSize, 0, 0
	}
	blockSize = roundUp(ceilDiv(size, TargetBlocks), BlockAlign)
	blockSize = min(max(blockSize, MinBlockSize), MaxBlockSize)
	blockSize = max(blockSize, roundUp(ceilDiv(size, MaxSourceBlocks), BlockAlign))
	source = ceilDiv(size, blockSize)
	recovery = max(1, (source*uint64(percent)+50)/100)
	return blockSize, source, recovery
}

// The sizes of the packets of a set, as par2 writes them: a recovery block is a header, its
// exponent and the block; the main, description and checksums packets are written in the index and
// again in the recovery file, as many times as the recovery blocks have bits.
const (
	recoveryOverhead = headerSize + 4
	mainPacket       = headerSize + 12 + 16
	descPacket       = headerSize + 56 + 200 // a name of 200 bytes at most, as MaxPack makes them
	creatorPacket    = headerSize + 64
)

// SetSize is about what the set of a file of size bytes takes at percent (never less): what the
// space of a queue counts for it.
func SetSize(size uint64, percent int) uint64 {
	if size == 0 || percent <= 0 {
		return 0
	}
	bs, source, recovery := Blocks(size, percent)
	critical := uint64(mainPacket+descPacket) + headerSize + 16 + 20*source
	copies := uint64(bits.Len64(recovery))
	index := critical + creatorPacket
	volume := recovery*(bs+recoveryOverhead) + copies*critical + creatorPacket
	return index + volume
}

// Create is a set to make: of File, written as Index (and its recovery file, named after it) with
// blocks of BlockSize and Recovery recovery blocks; Threads 0 is all of the CPU's.
type Create struct {
	File, Index         string
	BlockSize, Recovery uint64
	Threads             int
}

func threads(n int) []string {
	if n > 0 {
		return []string{"-t" + strconv.Itoa(n)}
	}
	return nil
}

func memory() string { return "-m" + strconv.Itoa(MemoryMB) }

// CreateArgs are the arguments of par2 that make a set: one recovery file (-n1), with the file named
// by its name alone (the base path is its folder), whatever folder the set is made in.
func CreateArgs(c Create) []string {
	args := []string{"create", "-s" + strconv.FormatUint(c.BlockSize, 10), "-c" + strconv.FormatUint(c.Recovery, 10), "-n1", memory()}
	args = append(args, threads(c.Threads)...)
	return append(args, "-B"+filepath.Dir(c.File), "--", c.Index, c.File)
}

// Repair is a repair to make with the set of Index: the file it protects is looked for, and
// written again, in Folder (par2 keeps the damaged one there as <name>.1).
type Repair struct {
	Index, Folder string
	Threads       int
}

// RepairArgs are the arguments of par2 that repair a file in a folder of its own (never -p: the
// damaged one is kept until the repaired one is checked).
func RepairArgs(r Repair) []string {
	args := append([]string{"repair", memory()}, threads(r.Threads)...)
	return append(args, "-B"+r.Folder, "--", r.Index)
}

// VerifyArgs are the arguments of par2 that check a file with a set: the file is given by its own
// path, so a set renamed with its .p5z still finds it.
func VerifyArgs(index, file string, n int) []string {
	args := append([]string{"verify", memory()}, threads(n)...)
	return append(args, "-B"+filepath.Dir(file), "--", index, file)
}
