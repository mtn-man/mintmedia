//go:build linux

package clipboard

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
)

var linuxClipboard struct {
	mu      sync.Mutex
	content string
	count   int64
}

func pasteboardChangeCount(ctx context.Context) int64 {
	content := wlPasteRead(ctx)
	linuxClipboard.mu.Lock()
	defer linuxClipboard.mu.Unlock()
	if content != linuxClipboard.content {
		linuxClipboard.content = content
		linuxClipboard.count++
	}
	return linuxClipboard.count
}

// pasteboardReadString returns the content already fetched by the most
// recent pasteboardChangeCount call instead of re-invoking wl-paste: unlike
// darwin's NSPasteboard.changeCount (a cheap in-process property read),
// every wl-paste call forks a subprocess, and the poller always calls
// pasteboardChangeCount immediately before pasteboardReadString within the
// same tick, so the cached content is already current.
func pasteboardReadString(_ context.Context) string {
	linuxClipboard.mu.Lock()
	defer linuxClipboard.mu.Unlock()
	return linuxClipboard.content
}

func wlPasteRead(ctx context.Context) string {
	out, err := exec.CommandContext(ctx, "wl-paste", "--no-newline").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func clipboardBackendSupported() bool {
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		return false
	}
	_, err := exec.LookPath("wl-paste")
	return err == nil
}

func clipboardBackendRequirement() string {
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		return "clipboard magnet polling requires a Wayland session (WAYLAND_DISPLAY is not set)"
	}
	return "clipboard magnet polling requires wl-paste (install wl-clipboard)"
}
