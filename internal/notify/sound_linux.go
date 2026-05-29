//go:build linux

package notify

import "context"

// PlaySound plays a sound file via paplay (PulseAudio/PipeWire). Best-effort; callers often ignore the error.
// Non-blocking usage should call this in a goroutine.
func PlaySound(ctx context.Context, soundPath string) error {
	return playSound(ctx, "paplay", soundPath)
}
