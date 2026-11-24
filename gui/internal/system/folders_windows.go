// SPDX-License-Identifier: GPL-3.0-or-later

//go:build windows

package system

import (
	"errors"
	"runtime"
	"syscall"
	"unsafe"

	ole "github.com/go-ole/go-ole"
	"golang.org/x/sys/windows"
)

// The folder dialog of Windows that takes several folders at once (IFileOpenDialog with
// FOS_PICKFOLDERS and FOS_ALLOWMULTISELECT): the one of Wails takes one. It runs on a thread of
// its own with COM, owned by the window of MaxPack, which it keeps from being used meanwhile.

var (
	clsidFileOpenDialog = ole.NewGUID("{DC1C5A9C-E88A-4dde-A5A1-60F82A20AEF7}")
	iidFileOpenDialog   = ole.NewGUID("{d57c7288-d4ad-4768-be02-9d969532d960}")
	procCoTaskMemFree   = windows.NewLazySystemDLL("ole32.dll").NewProc("CoTaskMemFree")
)

const (
	fosPickFolders     = 0x00000020
	fosForceFileSystem = 0x00000040
	fosAllowMultiple   = 0x00000200
	fosPathMustExist   = 0x00000800
	sigdnFileSysPath   = 0x80058000
	// HRESULT_FROM_WIN32(ERROR_CANCELLED): the dialog was closed without a choice
	hrCancelled = 0x800704C7
)

// call calls method n of the table of a COM object.
func call(object unsafe.Pointer, n int, args ...uintptr) uintptr {
	table := (*[32]uintptr)(*(*unsafe.Pointer)(object))
	result, _, _ := syscall.SyscallN(table[n], append([]uintptr{uintptr(object)}, args...)...)
	return result
}

func release(object unsafe.Pointer) { call(object, 2) }

// ChooseFolders asks for folders, several at once; none when the dialog is closed without them.
func ChooseFolders(title string) ([]string, error) {
	type answer struct {
		paths []string
		err   error
	}
	done := make(chan answer, 1)
	go func() {
		paths, err := chooseFolders(title)
		done <- answer{paths, err}
	}()
	a := <-done
	return a.paths, a.err
}

func chooseFolders(title string) ([]string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err == nil {
		defer ole.CoUninitialize()
	}
	created, err := ole.CreateInstance(clsidFileOpenDialog, iidFileOpenDialog)
	if err != nil {
		return nil, err
	}
	dialog := unsafe.Pointer(created)
	defer release(dialog)
	// IFileDialog: GetOptions 10, SetOptions 9, SetTitle 17; IModalWindow: Show 3;
	// IFileOpenDialog: GetResults 27
	var options uint32
	call(dialog, 10, uintptr(unsafe.Pointer(&options)))
	if hr := call(dialog, 9, uintptr(options|fosPickFolders|fosForceFileSystem|fosAllowMultiple|fosPathMustExist)); hr != 0 {
		return nil, ole.NewError(hr)
	}
	if title != "" {
		if text, err := windows.UTF16PtrFromString(title); err == nil {
			call(dialog, 17, uintptr(unsafe.Pointer(text)))
		}
	}
	switch hr := call(dialog, 3, uintptr(FindOwnWindow(false))); hr {
	case 0:
	case hrCancelled:
		return []string{}, nil
	default:
		return nil, ole.NewError(hr)
	}
	var items unsafe.Pointer
	if hr := call(dialog, 27, uintptr(unsafe.Pointer(&items))); hr != 0 || items == nil {
		return nil, ole.NewError(hr)
	}
	defer release(items)
	// IShellItemArray: GetCount 7, GetItemAt 8; IShellItem: GetDisplayName 5
	var count uint32
	if hr := call(items, 7, uintptr(unsafe.Pointer(&count))); hr != 0 {
		return nil, ole.NewError(hr)
	}
	paths := []string{}
	for i := uint32(0); i < count; i++ {
		var item unsafe.Pointer
		if call(items, 8, uintptr(i), uintptr(unsafe.Pointer(&item))) != 0 || item == nil {
			continue
		}
		var name *uint16
		if call(item, 5, sigdnFileSysPath, uintptr(unsafe.Pointer(&name))) == 0 && name != nil {
			paths = append(paths, windows.UTF16PtrToString(name))
			procCoTaskMemFree.Call(uintptr(unsafe.Pointer(name)))
		}
		release(item)
	}
	if len(paths) == 0 && count > 0 {
		return nil, errors.New("the folders chosen have no path on the disk")
	}
	return paths, nil
}
