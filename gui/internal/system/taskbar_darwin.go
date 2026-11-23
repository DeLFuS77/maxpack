// SPDX-License-Identifier: GPL-3.0-or-later

//go:build darwin

package system

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>
#include <stdlib.h>
#include <string.h>

// dockBadge puts the text on the icon of the Dock, on the main thread; "" clears it. The text is
// copied, as the caller frees its own before the block runs, and the string is made inside a
// pool of its own: this runs on a thread of Go, which has none.
static void dockBadge(const char *text) {
	char *copy = strdup(text);
	dispatch_async(dispatch_get_main_queue(), ^{
		@autoreleasepool {
			NSString *label = [NSString stringWithUTF8String:copy];
			[[NSApp dockTile] setBadgeLabel:(label.length > 0 ? label : nil)];
		}
		free(copy);
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
