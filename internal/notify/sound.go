package notify

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// PlaySound plays a system sound file via afplay (macOS).
// Best-effort: returns error if afplay fails, but callers often ignore it.
// Non-blocking usage should call this in a goroutine.
func PlaySound(ctx context.Context, soundPath string) error {
	soundPath = strings.TrimSpace(soundPath)
	if soundPath == "" {
		return nil
	}
	cmd := exec.CommandContext(ctx, "afplay", soundPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("play sound %q: %w", soundPath, err)
	}
	return nil
}