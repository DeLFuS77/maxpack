// SPDX-License-Identifier: GPL-3.0-or-later

// Package par2test writes PAR2 sets for the tests, without par2: the packets a set of one file has
// (Main, FileDesc, IFSC, Creator) and recovery packets whose data is noise. A set it writes is read
// as par2 reads it, but cannot repair anything. Only the tests use it.
package par2test

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"hash/crc32"
	"os"
	"strconv"
)

func packet(set [16]byte, kind string, body []byte) []byte {
	header := make([]byte, 64)
	copy(header, "PAR2\x00PKT")
	binary.LittleEndian.PutUint64(header[8:16], uint64(64+len(body)))
	copy(header[32:48], set[:])
	copy(header[48:64], kind)
	sum := md5.New()
	sum.Write(header[32:64])
	sum.Write(body)
	copy(header[16:32], sum.Sum(nil))
	return append(header, body...)
}

// Set gives the bytes of the index and of the recovery file of a set that protects data under the
// name name, with blocks of blockSize and recovery recovery blocks.
func Set(name string, data []byte, blockSize, recovery int) (index, volume []byte) {
	first := data
	if len(first) > 16<<10 {
		first = first[:16<<10]
	}
	hash16k, hash := md5.Sum(first), md5.Sum(data)
	size := binary.LittleEndian.AppendUint64(nil, uint64(len(data)))
	fileID := md5.Sum(append(append(append([]byte{}, hash16k[:]...), size...), name...))
	main := binary.LittleEndian.AppendUint64(nil, uint64(blockSize))
	main = binary.LittleEndian.AppendUint32(main, 1)
	main = append(main, fileID[:]...)
	set := md5.Sum(main)
	padded := []byte(name)
	for len(padded)%4 != 0 {
		padded = append(padded, 0)
	}
	desc := append(append(append(append(append([]byte{}, fileID[:]...), hash[:]...), hash16k[:]...), size...), padded...)
	ifsc := append([]byte{}, fileID[:]...)
	for at := 0; at < len(data); at += blockSize {
		block := make([]byte, blockSize)
		copy(block, data[at:min(len(data), at+blockSize)])
		sum := md5.Sum(block)
		ifsc = binary.LittleEndian.AppendUint32(append(ifsc, sum[:]...), crc32.ChecksumIEEE(block))
	}
	var head bytes.Buffer
	head.Write(packet(set, "PAR 2.0\x00Main\x00\x00\x00\x00", main))
	head.Write(packet(set, "PAR 2.0\x00FileDesc", desc))
	head.Write(packet(set, "PAR 2.0\x00IFSC\x00\x00\x00\x00", ifsc))
	head.Write(packet(set, "PAR 2.0\x00Creator\x00", []byte("par2test")))
	var vol bytes.Buffer
	for e := 0; e < recovery; e++ {
		vol.Write(packet(set, "PAR 2.0\x00RecvSlic", append(binary.LittleEndian.AppendUint32(nil, uint32(e)), bytes.Repeat([]byte{0x5A}, blockSize)...)))
	}
	vol.Write(head.Bytes())
	return head.Bytes(), vol.Bytes()
}

// Write writes the set of the file at path next to it, as MaxPack names it (<path>.par2 and
// <path>.vol0+<recovery>.par2), protecting it under the name name, and gives the two files.
func Write(path, name string, blockSize, recovery int) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	index, volume := Set(name, data, blockSize, recovery)
	files := []string{path + ".par2", path + ".vol0+" + strconv.Itoa(recovery) + ".par2"}
	if err := os.WriteFile(files[0], index, 0o644); err != nil {
		return nil, err
	}
	return files, os.WriteFile(files[1], volume, 0o644)
}
