// SPDX-License-Identifier: GPL-3.0-or-later

package par2

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"
)

// packetBytes is a packet as PAR 2.0 writes it: the header with its MD5, and the body.
func packetBytes(set ID, kind, body []byte) []byte {
	header := make([]byte, headerSize)
	copy(header, magic)
	binary.LittleEndian.PutUint64(header[8:16], uint64(headerSize+len(body)))
	copy(header[32:48], set[:])
	copy(header[48:64], kind)
	sum := md5.New()
	sum.Write(header[32:64])
	sum.Write(body)
	copy(header[16:32], sum.Sum(nil))
	return append(header, body...)
}

// fakeSet is the packets of a set that protects one file (data, named name) with blocks of
// blockSize: its index, and recovery packets with the exponents given (their data is noise, as the
// reader never looks at it). It gives the set's ID and the bytes of the index and of the recovery
// file.
func fakeSet(name string, data []byte, blockSize int, exponents ...uint32) (ID, []byte, []byte) {
	first := data
	if len(first) > 16<<10 {
		first = first[:16<<10]
	}
	hash16k, hash := md5.Sum(first), md5.Sum(data)
	nameBytes := []byte(name)
	for len(nameBytes)%4 != 0 {
		nameBytes = append(nameBytes, 0)
	}
	// the file ID is the MD5 of its hash16k, its size and its name
	idInput := append(append(append([]byte{}, hash16k[:]...), binary.LittleEndian.AppendUint64(nil, uint64(len(data)))...), []byte(name)...)
	fileID := ID(md5.Sum(idInput))
	main := binary.LittleEndian.AppendUint64(nil, uint64(blockSize))
	main = binary.LittleEndian.AppendUint32(main, 1)
	main = append(main, fileID[:]...)
	set := ID(md5.Sum(main))
	desc := append(append(append(append(append([]byte{}, fileID[:]...), hash[:]...), hash16k[:]...), binary.LittleEndian.AppendUint64(nil, uint64(len(data)))...), nameBytes...)
	ifsc := append([]byte{}, fileID[:]...)
	for at := 0; at < len(data); at += blockSize {
		block := make([]byte, blockSize)
		copy(block, data[at:min(len(data), at+blockSize)])
		sum := md5.Sum(block)
		ifsc = append(ifsc, sum[:]...)
		ifsc = binary.LittleEndian.AppendUint32(ifsc, crc32.ChecksumIEEE(block))
	}
	creator := append([]byte("test creator"), 0, 0, 0, 0)
	var index bytes.Buffer
	for _, p := range [][]byte{packetBytes(set, typeMain, main), packetBytes(set, typeFileDesc, desc), packetBytes(set, typeIFSC, ifsc),
		packetBytes(set, []byte("PAR 2.0\x00Creator\x00"), creator)} {
		index.Write(p)
	}
	var recovery bytes.Buffer
	for _, e := range exponents {
		body := binary.LittleEndian.AppendUint32(nil, e)
		body = append(body, bytes.Repeat([]byte{0xA5}, blockSize)...)
		recovery.Write(packetBytes(set, typeRecovery, body))
	}
	// a recovery file repeats the packets that matter
	recovery.Write(index.Bytes())
	return set, index.Bytes(), recovery.Bytes()
}

func writeFile(t *testing.T, path string, data []byte) string {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func noise(n int, seed byte) []byte {
	data := make([]byte, n)
	x := uint32(seed) + 1
	for i := range data {
		x = x*1664525 + 1013904223
		data[i] = byte(x >> 24)
	}
	return data
}

func TestReadSetFromPackets(t *testing.T) {
	dir := t.TempDir()
	data := noise(100_000, 1)
	id, index, recovery := fakeSet("Juego [PPSA10001].p5z", data, 4096, 0, 1, 2)
	indexPath := writeFile(t, filepath.Join(dir, "a.par2"), index)
	recoveryPath := writeFile(t, filepath.Join(dir, "a.vol0+3.par2"), recovery)

	s, err := ReadSet(indexPath, recoveryPath)
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != id || s.BlockSize != 4096 || len(s.Files) != 1 || s.Others != 0 || s.Missing != 0 {
		t.Fatalf("set: %+v", s)
	}
	f := s.Files[0]
	if f.Name != "Juego [PPSA10001].p5z" || f.Size != 100_000 || f.Hash16k != md5.Sum(data[:16<<10]) || f.Hash != md5.Sum(data) {
		t.Errorf("file: %q %d", f.Name, f.Size)
	}
	if s.Recovery != 3 || s.SourceBlocks() != 25 || len(f.Blocks) != 25 {
		t.Errorf("recovery %d, source blocks %d, checksums %d", s.Recovery, s.SourceBlocks(), len(f.Blocks))
	}
	// the index alone: no recovery blocks
	if s, err := ReadSet(indexPath); err != nil || s.Recovery != 0 || len(s.Files) != 1 {
		t.Errorf("index alone: %+v, %v", s, err)
	}
	// the recovery file alone has all a set needs
	if s, err := ReadSet(recoveryPath); err != nil || s.Recovery != 3 || len(s.Files) != 1 {
		t.Errorf("recovery file alone: %+v, %v", s, err)
	}
}

func TestReadSetLeavesOutWhatIsDamaged(t *testing.T) {
	dir := t.TempDir()
	data := noise(50_000, 2)
	_, index, recovery := fakeSet("x.p5z", data, 8192, 4, 5)
	// a byte changed in the FileDesc packet: that packet is not believed, but the recovery file has
	// its own copy
	damaged := append([]byte{}, index...)
	mainLength := binary.LittleEndian.Uint64(damaged[8:16])
	damaged[mainLength+headerSize+60] ^= 0xFF
	indexPath := writeFile(t, filepath.Join(dir, "x.p5z.par2"), damaged)
	if s, err := ReadSet(indexPath); err != nil || len(s.Files) != 0 || s.Missing != 1 {
		t.Errorf("damaged description alone: %+v, %v", s, err)
	}
	recoveryPath := writeFile(t, filepath.Join(dir, "x.p5z.vol0+2.par2"), recovery)
	if s, err := ReadSet(indexPath, recoveryPath); err != nil || len(s.Files) != 1 || s.Missing != 0 || s.Recovery != 2 {
		t.Errorf("with the recovery file: %+v, %v", s, err)
	}
	// a recovery file cut short keeps the packets before the cut: here its recovery blocks, the main
	// packet and the description, not the checksums of the blocks
	cut := writeFile(t, filepath.Join(dir, "cut.par2"), recovery[:len(recovery)-len(index)/2])
	if s, err := ReadSet(cut); err != nil || s.Recovery != 2 || len(s.Files) != 1 || s.Files[0].Blocks != nil {
		t.Errorf("a recovery file cut in its index: %+v, %v", s, err)
	}
	if s, err := ReadSet(writeFile(t, filepath.Join(dir, "head.par2"), recovery[:100])); err == nil {
		t.Errorf("a file with no main packet read as a set: %+v", s)
	}
	// noise is not a set
	if _, err := ReadSet(writeFile(t, filepath.Join(dir, "noise.par2"), noise(5000, 3))); err == nil {
		t.Error("noise read as a set")
	}
	if _, err := ReadSet(filepath.Join(dir, "missing.par2")); err == nil {
		t.Error("a file that is not there read as a set")
	}
}

func TestReadSetKeepsToItsOwnSet(t *testing.T) {
	dir := t.TempDir()
	_, index, _ := fakeSet("a.p5z", noise(30_000, 4), 4096, 0)
	_, _, other := fakeSet("b.p5z", noise(30_000, 5), 4096, 0, 1, 2, 3)
	// the recovery blocks of another set are not counted for this one
	s, err := ReadSet(writeFile(t, filepath.Join(dir, "a.par2"), index), writeFile(t, filepath.Join(dir, "b.vol.par2"), other))
	if err != nil {
		t.Fatal(err)
	}
	if s.Files[0].Name != "a.p5z" || s.Recovery != 0 {
		t.Errorf("%q, %d recovery blocks", s.Files[0].Name, s.Recovery)
	}
}
