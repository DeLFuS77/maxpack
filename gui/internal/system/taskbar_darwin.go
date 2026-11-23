// SPDX-License-Identifier: GPL-3.0-or-later

//go:build darwin

package system

/*
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>
#include <stdlib.h>

// dockBadge puts the text on the icon of the Dock, on the main thread; "" clears it.
static void dockBadge(const char *text) {
	NSString *label = [NSString stringWithUTF8String:text];
	dispatch_async(dispatch_get_main_queue(), ^{
		[[NSApp dockTile] setBadgeLabel:(label.length > 0 ? label : nil)];
	});
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// SetTaskbarProgress shows the progress of the work on the icon of the Dock as a percentage
// ("42%"): state is "normal" while working; "none", "error" or any other clears it.
func SetTaskbarProgress(state string, done uint64, total uint64) {
	text := ""
	if state == "normal" && total > 0 {
		text = fmt.Sprintf("%d%%", min(done, total)*100/total)
	}
	c := C.CString(text)
	defer C.free(unsafe.Pointer(c))
	C.dockBadge(c)
}
