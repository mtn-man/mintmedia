//go:build !darwin || !cgo

package clipboard

import (
	"errors"
	"strings"
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
	if !strings.Contains(err.Error(), "requires darwin with cgo enabled") {
		t.Fatalf("expected actionable requirement in error, got %q", err.Error())
	}
}
