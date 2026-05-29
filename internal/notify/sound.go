//go:build !darwin && !linux

package notify

import "context"

// PlaySound is a no-op on unsupported platforms.
func PlaySound(_ context.Context, _ string) error {
	return nil
}
