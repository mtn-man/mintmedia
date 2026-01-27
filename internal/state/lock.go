package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ReleaseFunc releases a lock previously acquired by AcquireLock.
type ReleaseFunc func() error

var (
	// ErrAlreadyRunning indicates another live instance holds the lock.
	ErrAlreadyRunning = errors.New("another instance is already running")
)

// AcquireLock attempts to acquire an exclusive lock by creating lockPath atomically.
// lockPath should be a file path (e.g. <state_dir>/mintmedia.lock).
//
// Behavior:
// - Creates lock file with O_CREATE|O_EXCL (atomic).
// - Writes PID + timestamp into the file.
// - If the lock exists, attempts to determine whether it is stale:
//   - If the PID in the lock is alive, returns ErrAlreadyRunning (wrapped).
//   - If PID is missing/not alive, deletes the stale lock and retries once.
func AcquireLock(lockPath string) (ReleaseFunc, error) {
	lockPath = strings.TrimSpace(lockPath)
	if lockPath == "" {
		return nil, fmt.Errorf("lock path is empty")
	}

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, fmt.Errorf("create lock dir: %w", err)
	}

	// First attempt
	release, err := tryCreateLock(lockPath)
	if err == nil {
		return release, nil
	}
	if !errors.Is(err, os.ErrExist) {
		return nil, err
	}

	// Lock file exists: check staleness.
	pid, readErr := readPIDFromLock(lockPath)
	if readErr != nil || pid <= 0 {
		// If we can't confidently read the lock, fail closed.
		return nil, fmt.Errorf("%w: lock=%s", ErrAlreadyRunning, lockPath)
	}
	if isProcessAlive(pid) {
		return nil, fmt.Errorf("%w: pid=%d lock=%s", ErrAlreadyRunning, pid, lockPath)
	}

	// Stale lock: remove and retry once.
	_ = os.Remove(lockPath)

	release, err = tryCreateLock(lockPath)
	if err == nil {
		return release, nil
	}
	if errors.Is(err, os.ErrExist) {
		// Race: someone else acquired it after we removed/while retrying.
		pid, readErr := readPIDFromLock(lockPath)
		if readErr == nil && pid > 0 && isProcessAlive(pid) {
			return nil, fmt.Errorf("%w: pid=%d lock=%s", ErrAlreadyRunning, pid, lockPath)
		}
		return nil, fmt.Errorf("%w: lock=%s", ErrAlreadyRunning, lockPath)
	}
	return nil, err
}

func tryCreateLock(lockPath string) (ReleaseFunc, error) {
	f, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return nil, fmt.Errorf("create lock file: %w", err)
	}

	pid := os.Getpid()
	now := time.Now().Format(time.RFC3339)

	// Best-effort write; if it fails, still keep the lock (file exists).
	_, _ = fmt.Fprintf(f, "pid=%d\nstarted=%s\n", pid, now)
	_ = f.Close()

	// Release deletes the lockfile. Best-effort; safe to call multiple times.
	released := false
	return func() error {
		if released {
			return nil
		}
		released = true
		if err := os.Remove(lockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}, nil
}

func readPIDFromLock(lockPath string) (int, error) {
	b, err := os.ReadFile(lockPath)
	if err != nil {
		return 0, fmt.Errorf("read lock file: %w", err)
	}
	lines := strings.Split(string(b), "\n")
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		if strings.HasPrefix(ln, "pid=") {
			s := strings.TrimSpace(strings.TrimPrefix(ln, "pid="))
			pid, err := strconv.Atoi(s)
			if err != nil {
				return 0, fmt.Errorf("parse PID from lock file: %w", err)
			}
			return pid, nil
		}
	}

	// Fallback: allow lockfile that contains only a PID on the first line.
	first := strings.TrimSpace(lines[0])
	if first != "" {
		if pid, err := strconv.Atoi(first); err == nil {
			return pid, nil
		}
	}

	return 0, fmt.Errorf("pid not found in lock file")
}

// isProcessAlive returns true if a process with pid appears to be running.
// On Unix, sending signal 0 checks existence/permission without killing.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	// If we lack permission to signal, the process still exists.
	if errors.Is(err, syscall.EPERM) {
		return true
	}
	return false
}
