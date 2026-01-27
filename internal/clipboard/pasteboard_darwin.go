//go:build darwin && cgo

package clipboard

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit -framework Foundation
// Note: some Go toolchains on macOS emit ld LC_DYSYMTAB warnings with cgo.
#import <AppKit/AppKit.h>
#import <Foundation/Foundation.h>
#include <stdlib.h>
#include <string.h>

static long jm_pasteboard_change_count(void) {
    @autoreleasepool {
        return (long)[[NSPasteboard generalPasteboard] changeCount];
    }
}

static char* jm_pasteboard_read_string(void) {
    @autoreleasepool {
        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        NSString *s = [pb stringForType:NSPasteboardTypeString];
        if (!s) return NULL;
        const char *utf8 = [s UTF8String];
        if (!utf8) return NULL;
        return strdup(utf8);
    }
}
*/
import "C"

import (
	"context"
	"strings"
	"unsafe"
)

// ctx is reserved for future cancellation/hooks; pasteboard APIs are synchronous today.
func pasteboardChangeCount(_ context.Context) int64 {
	return int64(C.jm_pasteboard_change_count())
}

// ctx is reserved for future cancellation/hooks; pasteboard APIs are synchronous today.
func pasteboardReadString(_ context.Context) string {
	ptr := C.jm_pasteboard_read_string()
	if ptr == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(ptr))
	return strings.TrimSpace(C.GoString(ptr))
}
