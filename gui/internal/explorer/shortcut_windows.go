// SPDX-License-Identifier: GPL-3.0-or-later

//go:build windows

package explorer

// The shortcut of "Send to": written with the shell of Windows, and read back from its file to
// tell which program it points to. Adapted from NXPress (see explorer.go).

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// shortcutTarget reads the local path a shortcut (.lnk) points to, from its LinkInfo (the Shell
// Link format of Windows): in UTF-16 when the shortcut holds it so, else in the code page of the
// system. A shortcut to a network folder has none.
func shortcutTarget(shortcut string) (string, error) {
	data, err := os.ReadFile(shortcut)
	if err != nil {
		return "", err
	}
	const (
		headerSize  = 0x4C
		hasIDList   = 1 << 0
		hasLinkInfo = 1 << 1
		// of the LinkInfo: it has a volume and a local path
		localPath = 1 << 0
	)
	notRead := errors.New("the shortcut holds no local path")
	if len(data) < headerSize || binary.LittleEndian.Uint32(data) != headerSize {
		return "", notRead
	}
	flags := binary.LittleEndian.Uint32(data[0x14:])
	at := headerSize
	if flags&hasIDList != 0 {
		if len(data) < at+2 {
			return "", notRead
		}
		at += 2 + int(binary.LittleEndian.Uint16(data[at:]))
	}
	if flags&hasLinkInfo == 0 || len(data) < at+0x1C {
		return "", notRead
	}
	info := data[at:]
	size := int(binary.LittleEndian.Uint32(info))
	if size < 0x1C || size > len(info) || binary.LittleEndian.Uint32(info[8:])&localPath == 0 {
		return "", notRead
	}
	info = info[:size]
	// the path is its base and its suffix, each ended by a zero
	if infoHeader := binary.LittleEndian.Uint32(info[4:]); infoHeader >= 0x24 && size >= 0x24 {
		return wideString(info, binary.LittleEndian.Uint32(info[0x1C:])) + wideString(info, binary.LittleEndian.Uint32(info[0x20:])), nil
	}
	return systemString(append(ansiString(info, binary.LittleEndian.Uint32(info[0x10:])), ansiString(info, binary.LittleEndian.Uint32(info[0x18:]))...))
}

// ansiString is the text at offset of data, up to a zero, as it is stored.
func ansiString(data []byte, offset uint32) []byte {
	if offset == 0 || int(offset) >= len(data) {
		return nil
	}
	text := data[offset:]
	if end := bytes.IndexByte(text, 0); end >= 0 {
		text = text[:end]
	}
	return text
}

// wideString is the text in UTF-16 at offset of data, up to a zero.
func wideString(data []byte, offset uint32) string {
	if offset == 0 || int(offset) >= len(data) {
		return ""
	}
	units := []uint16{}
	for i := int(offset); i+1 < len(data); i += 2 {
		unit := binary.LittleEndian.Uint16(data[i:])
		if unit == 0 {
			break
		}
		units = append(units, unit)
	}
	return string(utf16.Decode(units))
}

var kernel32 = windows.NewLazySystemDLL("kernel32.dll")

// systemString turns a text in the code page of the system into a string.
func systemString(text []byte) (string, error) {
	if len(text) == 0 {
		return "", nil
	}
	convert := kernel32.NewProc("MultiByteToWideChar")
	const codePageOfTheSystem = 0
	size, _, err := convert.Call(codePageOfTheSystem, 0, uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)), 0, 0)
	if size == 0 {
		return "", err
	}
	wide := make([]uint16, size)
	convert.Call(codePageOfTheSystem, 0, uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)), uintptr(unsafe.Pointer(&wide[0])), size)
	return string(utf16.Decode(wide)), nil
}

// the COM classes and interfaces of a shortcut
var (
	ole32           = windows.NewLazySystemDLL("ole32.dll")
	clsidShellLink  = windows.GUID{Data1: 0x00021401, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidShellLinkW   = windows.GUID{Data1: 0x000214F9, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIPersistFile = windows.GUID{Data1: 0x0000010B, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
)

// the methods used, by their place in the tables of IShellLinkW and IPersistFile
const (
	comQueryInterface   = 0
	comRelease          = 2
	linkSetDescription  = 7
	linkSetIconLocation = 17
	linkSetPath         = 20
	persistSave         = 6
)

// comCall calls the method at index of the table of a COM object, and gives its HRESULT.
func comCall(object unsafe.Pointer, index int, args ...uintptr) uintptr {
	table := *(*unsafe.Pointer)(object)
	method := *(*uintptr)(unsafe.Add(table, uintptr(index)*unsafe.Sizeof(uintptr(0))))
	result, _, _ := syscall.SyscallN(method, append([]uintptr{uintptr(object)}, args...)...)
	return result
}

// makeShortcut writes a shortcut at path to target, with the shell of Windows (IShellLinkW). Its
// paths go to the shell as they are, in UTF-16: no script is written with them, so a folder named
// O’Brien works, and a name made to run code that way runs nothing.
func makeShortcut(path, target string) error {
	failed := func(hresult uintptr) error {
		return fmt.Errorf("the shortcut cannot be made: %w", windows.Errno(hresult))
	}
	pathText, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	targetText, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	description, _ := windows.UTF16PtrFromString("MaxPack")
	// COM works on one thread; one already set for COM by another part of the window is used as it is
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	const apartmentThreaded = 0x2
	if result, _, _ := ole32.NewProc("CoInitializeEx").Call(0, apartmentThreaded); int32(result) >= 0 {
		defer ole32.NewProc("CoUninitialize").Call()
	}
	var link unsafe.Pointer
	const inProcess = 0x1
	if result, _, _ := ole32.NewProc("CoCreateInstance").Call(uintptr(unsafe.Pointer(&clsidShellLink)), 0, inProcess,
		uintptr(unsafe.Pointer(&iidShellLinkW)), uintptr(unsafe.Pointer(&link))); int32(result) < 0 {
		return failed(result)
	}
	defer comCall(link, comRelease)
	for _, call := range []struct {
		method int
		args   []uintptr
	}{
		{linkSetPath, []uintptr{uintptr(unsafe.Pointer(targetText))}},
		{linkSetIconLocation, []uintptr{uintptr(unsafe.Pointer(targetText)), 0}},
		{linkSetDescription, []uintptr{uintptr(unsafe.Pointer(description))}},
	} {
		if result := comCall(link, call.method, call.args...); int32(result) < 0 {
			return failed(result)
		}
	}
	var file unsafe.Pointer
	if result := comCall(link, comQueryInterface, uintptr(unsafe.Pointer(&iidIPersistFile)), uintptr(unsafe.Pointer(&file))); int32(result) < 0 {
		return failed(result)
	}
	defer comCall(file, comRelease)
	if result := comCall(file, persistSave, uintptr(unsafe.Pointer(pathText)), 1); int32(result) < 0 {
		return failed(result)
	}
	runtime.KeepAlive(pathText)
	runtime.KeepAlive(targetText)
	runtime.KeepAlive(description)
	return nil
}
