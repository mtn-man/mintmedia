//go:build (!darwin || !cgo) && !linux

package clipboard

import (
	"errors"
	"testing"
	"time"
)

func TestNewPollerUnsupportedPlatform(t *testing.T) {
	_, err := NewPoller(time.Second)
	if err == nil {
		t.Fatalf("expected unsupported platform error, got nil")
	}
	if !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("expected ErrUnsupportedPlatform, got %v", err)
	}
}
