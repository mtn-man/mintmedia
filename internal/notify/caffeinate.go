package notify

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
)

// Caffeinate prevents macOS from idle sleeping while the daemon is running.
// It is best-effort: failures should be logged by callers but should not be fatal.
type Caffeinate struct {
	mu   sync.Mutex
	cmd  *exec.Cmd
	done chan struct{}
}

// NewCaffeinate returns a new controller.
// On non-darwin platforms, Start() will return nil (no-op).
func NewCaffeinate() *Caffeinate {
	return &Caffeinate{}
}

// Start launches "caffeinate -i" in the background if not already running.
// On non-macOS platforms, this is a no-op.
func (c *Caffeinate) Start(ctx context.Context) error {
	if runtime.GOOS != "darwin" {
		return nil
	}

	c.mu.Lock()
	// Already running?
	if c.cmd != nil {
		if c.done != nil {
			select {
			case <-c.done:
				c.cmd = nil
				c.done = nil
			default:
				c.mu.Unlock()
				return nil
			}
		} else {
			c.mu.Unlock()
			return nil
		}
	}

	cmd := exec.CommandContext(ctx, "caffeinate", "-i")

	// Start the process
	if err := cmd.Start(); err != nil {
		c.mu.Unlock()
		return fmt.Errorf("start caffeinate: %w", err)
	}

	done := make(chan struct{})
	c.cmd = cmd
	c.done = done
	c.mu.Unlock()

	// Reap in background so we don't leak zombies.
	go func(localCmd *exec.Cmd, done chan struct{}) {
		_ = localCmd.Wait()
		close(done)
		c.mu.Lock()
		if c.cmd == localCmd {
			c.cmd = nil
			c.done = nil
		}
		c.mu.Unlock()
	}(cmd, done)

	return nil
}

// Stop terminates the caffeinate process if running.
// On non-macOS platforms, this is a no-op.
func (c *Caffeinate) Stop() error {
	if runtime.GOOS != "darwin" {
		return nil
	}

	c.mu.Lock()
	cmd := c.cmd
	done := c.done
	if cmd == nil || cmd.Process == nil {
		c.mu.Unlock()
		return nil
	}

	if done != nil {
		select {
		case <-done:
			c.cmd = nil
			c.done = nil
			c.mu.Unlock()
			return nil
		default:
		}
	}

	// Try graceful termination first.
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		// If the process is already gone, treat as success.
		if errors.Is(err, os.ErrProcessDone) {
			c.cmd = nil
			c.done = nil
			c.mu.Unlock()
			return nil
		}
		// Some systems return ESRCH for missing proc
		c.mu.Unlock()
		return fmt.Errorf("signal caffeinate: %w", err)
	}

	// Clear state; process will be reaped by Wait goroutine.
	c.cmd = nil
	c.done = nil
	c.mu.Unlock()
	return nil
}
