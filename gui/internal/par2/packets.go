// SPDX-License-Identifier: GPL-3.0-or-later

package par2

// Reading a PAR2 set (the PAR 2.0 specification, Parchive): every file of a set is a run of packets,
// each a 64-byte header (the magic, its length, the MD5 of the rest, the ID of the set and its type)
// and a body. The Main packet says the block size and the files of the set, a FileDesc packet the
// name, size, MD5 and MD5 of the first 16 KiB of a file, an IFSC packet the MD5 and CRC32 of each of
// its blocks, and a RecvSlic packet is one recovery block. Only the headers of the recovery blocks
// are read: the reader seeks over their data, so a set of gigabytes is read in a moment.

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// The magic of a packet and the types this reader knows.
var (
	magic        = []byte("PAR2\x00PKT")
	typeMain     = []byte("PAR 2.0\x00Main\x00\x00\x00\x00")
	typeFileDesc = []byte("PAR 2.0\x00FileDesc")
	typeIFSC     = []byte("PAR 2.0\x00IFSC\x00\x00\x00\x00")
	typeRecovery = []byte("PAR 2.0\x00RecvSlic")
)

const (
	headerSize = 64
	// maxBody is the most a packet other than a recovery block may take: an IFSC packet of the 32768
	// blocks a set may have takes 640 KiB; anything bigger is not read (and not believed)
	maxBody = 4 << 20
)

// ID is the MD5 that names a set or a file.
type ID [16]byte

// Block is the checksum of a block of a file, as its IFSC packet says it.
type Block struct {
	MD5   [16]byte
	CRC32 uint32
}

// File is a file a set protects, as its FileDesc packet says it, with the checksums of its blocks
// when the set has its IFSC packet.
type File struct {
	ID      ID
	Name    string
	Size    uint64
	Hash    [16]byte // MD5 of the whole file
	Hash16k [16]byte // MD5 of its first 16 KiB (of all of it, when it is smaller)
	Blocks  []Block
}

// Set is what the files of a PAR2 set say of it: its ID, the block size, the files it can repair
// (in the order of its Main packet) and how many others it only checks, and the recovery blocks
// there are (each exponent once, however many files repeat it). Files without a FileDesc packet
// are not in Files: Missing counts them.
type Set struct {
	ID        ID
	BlockSize uint64
	Files     []File
	Others    int
	Missing   int
	Recovery  int
	// Paths are the files of the set that were read, in the order given
	Paths []string
}

// SourceBlocks are the blocks of the files the set can repair.
func (s Set) SourceBlocks() uint64 {
	var blocks uint64
	for _, f := range s.Files {
		if s.BlockSize > 0 {
			blocks += (f.Size + s.BlockSize - 1) / s.BlockSize
		}
	}
	return blocks
}

// ErrNoSet: none of the files given has the Main packet of a set.
var ErrNoSet = errors.New("no PAR2 set: no main packet")

// packet is a packet read: its set, its type and its body (none for a recovery block, whose
// exponent alone is read).
type packet struct {
	set      ID
	kind     []byte
	body     []byte
	exponent uint32
}

// readPackets gives the packets of a file, in order. A packet whose MD5 is not right is left out;
// the first header that is not one (a damaged file, or what follows the packets) ends the file.
// The data of a recovery block is never read, nor checked: par2 checks it when it uses it.
func readPackets(path string) ([]packet, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := info.Size()
	var packets []packet
	header := make([]byte, headerSize)
	for at := int64(0); at+headerSize <= size; {
		if _, err := f.ReadAt(header, at); err != nil {
			return packets, err
		}
		length := binary.LittleEndian.Uint64(header[8:16])
		if !bytes.Equal(header[:8], magic) || length < headerSize || length%4 != 0 || length > uint64(size-at) {
			break
		}
		p := packet{kind: append([]byte{}, header[48:64]...)}
		copy(p.set[:], header[32:48])
		bodyLength := int64(length) - headerSize
		switch {
		case bytes.Equal(p.kind, typeRecovery):
			if bodyLength >= 4 {
				exponent := make([]byte, 4)
				if _, err := f.ReadAt(exponent, at+headerSize); err != nil {
					return packets, err
				}
				p.exponent = binary.LittleEndian.Uint32(exponent)
				packets = append(packets, p)
			}
		case bodyLength <= maxBody:
			p.body = make([]byte, bodyLength)
			if _, err := f.ReadAt(p.body, at+headerSize); err != nil && !errors.Is(err, io.EOF) {
				return packets, err
			}
			// the MD5 of the packet covers its set, its type and its body
			sum := md5.New()
			sum.Write(header[32:64])
			sum.Write(p.body)
			if bytes.Equal(sum.Sum(nil), header[16:32]) {
				packets = append(packets, p)
			}
		}
		at += int64(length)
	}
	return packets, nil
}

// ReadSet reads the files of a set (its index and its recovery files, in any order) and gives what
// they say of the set whose Main packet comes first; packets of any other set are left out. A file
// that cannot be read is left out, unless none can.
func ReadSet(paths ...string) (Set, error) {
	var all [][]packet
	var read []string
	var firstErr error
	for _, path := range paths {
		packets, err := readPackets(path)
		if err != nil && len(packets) == 0 {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		all = append(all, packets)
		read = append(read, path)
	}
	if len(read) == 0 {
		if firstErr == nil {
			firstErr = ErrNoSet
		}
		return Set{}, firstErr
	}
	var main *packet
	for _, packets := range all {
		for i := range packets {
			if bytes.Equal(packets[i].kind, typeMain) && main == nil {
				main = &packets[i]
			}
		}
	}
	if main == nil || len(main.body) < 12 {
		return Set{}, ErrNoSet
	}
	s := Set{ID: main.set, BlockSize: binary.LittleEndian.Uint64(main.body[0:8]), Paths: read}
	recoverable := binary.LittleEndian.Uint32(main.body[8:12])
	ids := main.body[12:]
	if len(ids)%16 != 0 || uint64(recoverable)*16 > uint64(len(ids)) || s.BlockSize == 0 || s.BlockSize%4 != 0 {
		return Set{}, fmt.Errorf("the main packet of the set is not valid")
	}
	s.Others = len(ids)/16 - int(recoverable)
	descriptions := map[ID]File{}
	checksums := map[ID][]Block{}
	exponents := map[uint32]bool{}
	for _, packets := range all {
		for _, p := range packets {
			if p.set != s.ID {
				continue
			}
			switch {
			case bytes.Equal(p.kind, typeFileDesc) && len(p.body) >= 56:
				var f File
				copy(f.ID[:], p.body[0:16])
				copy(f.Hash[:], p.body[16:32])
				copy(f.Hash16k[:], p.body[32:48])
				f.Size = binary.LittleEndian.Uint64(p.body[48:56])
				f.Name = strings.TrimRight(string(p.body[56:]), "\x00")
				descriptions[f.ID] = f
			case bytes.Equal(p.kind, typeIFSC) && len(p.body) >= 16 && (len(p.body)-16)%20 == 0:
				var id ID
				copy(id[:], p.body[0:16])
				blocks := make([]Block, (len(p.body)-16)/20)
				for n := range blocks {
					entry := p.body[16+20*n:]
					copy(blocks[n].MD5[:], entry[0:16])
					blocks[n].CRC32 = binary.LittleEndian.Uint32(entry[16:20])
				}
				checksums[id] = blocks
			case bytes.Equal(p.kind, typeRecovery):
				exponents[p.exponent] = true
			}
		}
	}
	for n := 0; n < int(recoverable); n++ {
		var id ID
		copy(id[:], ids[16*n:16*n+16])
		f, ok := descriptions[id]
		if !ok {
			s.Missing++
			continue
		}
		if blocks := checksums[id]; uint64(len(blocks)) == (f.Size+s.BlockSize-1)/s.BlockSize {
			f.Blocks = blocks
		}
		s.Files = append(s.Files, f)
	}
	s.Recovery = len(exponents)
	return s, nil
}
