//go:build (!darwin || !cgo) && !linux

package clipboard

import "context"

func pasteboardChangeCount(_ context.Context) int64 {
	return 0
}

func pasteboardReadString(_ context.Context) string {
	return ""
}

func clipboardBackendSupported() bool {
	return false
}

func clipboardBackendRequirement() string {
	return "clipboard magnet polling requires darwin with cgo enabled (AppKit pasteboard backend)"
}
