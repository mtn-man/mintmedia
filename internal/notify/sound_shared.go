//go:build darwin || linux

package notify

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func playSound(ctx context.Context, cmd, soundPath string) error {
	soundPath = strings.TrimSpace(soundPath)
	if soundPath == "" {
		return nil
	}
	if err := exec.CommandContext(ctx, cmd, soundPath).Run(); err != nil {
		return fmt.Errorf("play sound %q: %w", soundPath, err)
	}
	return nil
}
