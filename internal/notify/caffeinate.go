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

// Caffeinate prevents idle sleep while the daemon is running.
// Uses caffeinate on macOS and systemd-inhibit on Linux.
// It is best-effort: failures should be logged by callers but should not be fatal.
type Caffeinate struct {
	mu   sync.Mutex
	cmd  *exec.Cmd
	done chan struct{}
}

// NewCaffeinate returns a new controller.
// On unsupported platforms, Start() returns nil immediately.
func NewCaffeinate() *Caffeinate {
	return &Caffeinate{}
}

// inhibitCmd returns the platform-specific command that prevents idle sleep,
// or nil if unsupported. Uses exec.Command (not CommandContext) so Stop() owns
// the process lifecycle exclusively — context cancellation must not race with SIGTERM.
func inhibitCmd() *exec.Cmd {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("caffeinate", "-i")
	case "linux":
		return exec.Command("systemd-inhibit",
			"--what=idle:sleep",
			"--who=mintmedia",
			"--why=processing media",
			"--mode=block",
			"sleep", "infinity",
		)
	default:
		return nil
	}
}

// Start launches the platform sleep-inhibit command in the background if not already running.
// On unsupported platforms, this is a no-op.
func (c *Caffeinate) Start(_ context.Context) error {
	cmd := inhibitCmd()
	if cmd == nil {
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

	// Start the process
	if err := cmd.Start(); err != nil {
		c.mu.Unlock()
		return fmt.Errorf("start sleep inhibit: %w", err)
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

// Stop terminates the sleep-inhibit process if running.
func (c *Caffeinate) Stop() error {
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
		return fmt.Errorf("signal sleep inhibit: %w", err)
	}

	// Clear state; process will be reaped by Wait goroutine.
	c.cmd = nil
	c.done = nil
	c.mu.Unlock()
	return nil
}
