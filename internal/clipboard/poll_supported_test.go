//go:build (darwin && cgo) || linux

package clipboard

import (
	"testing"
	"time"
)

func TestNewPollerSupportedPlatform(t *testing.T) {
	if !clipboardBackendSupported() {
		t.Skip("clipboard backend not available (wl-paste not installed?)")
	}
	p, err := NewPoller(time.Second)
	if err != nil {
		t.Fatalf("expected poller creation to succeed, got error: %v", err)
	}
	if p == nil {
		t.Fatalf("expected poller instance, got nil")
	}
}

func TestNewPollerValidatesInterval(t *testing.T) {
	_, err := NewPoller(0)
	if err == nil {
		t.Fatalf("expected interval validation error, got nil")
	}
}
