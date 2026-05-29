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

func pasteboardReadString(ctx context.Context) string {
	return wlPasteRead(ctx)
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
