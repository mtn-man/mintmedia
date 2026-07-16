package state

import (
	"context"
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

// LockInfo holds the parsed contents of a lock file.
type LockInfo struct {
	PID     int
	Started time.Time // zero value if not present in the lock file
}

// CheckLock reports whether the lock at lockPath is held by a live process.
// Returns the lock contents and true when the daemon is running.
// Returns (LockInfo{}, false, nil) for a stale or absent lock.
// Returns an error if the lock file exists but cannot be parsed.
func CheckLock(lockPath string) (LockInfo, bool, error) {
	info, exists, err := readLockFile(lockPath)
	if err != nil {
		return LockInfo{}, false, err
	}
	if !exists {
		return LockInfo{}, false, nil
	}
	return info, isProcessAlive(info.PID), nil
}

// readLockFile reads and parses the lock file at lockPath. It is the single
// mechanism shared by CheckLock (fail-open: an absent/unreadable lock means
// "not running") and AcquireLock (fail-closed: an absent/unreadable lock
// mid-race is treated as "can't be confident, refuse to start") -- each
// caller applies its own policy on top of exists/err rather than sharing one.
//
// exists reports whether the lock file exists. When exists is false, err is
// always nil (a missing lock file is not an error). When exists is true, err
// is non-nil if the file could not be read or parsed.
func readLockFile(lockPath string) (info LockInfo, exists bool, err error) {
	b, err := os.ReadFile(lockPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return LockInfo{}, false, nil
		}
		return LockInfo{}, true, fmt.Errorf("read lock file: %w", err)
	}
	info, err = parseLockContents(b)
	return info, true, err
}

// parseLockContents parses the key=value contents of a lock file.
// A missing or malformed pid= line is a hard error; started= is optional.
func parseLockContents(b []byte) (LockInfo, error) {
	var info LockInfo
	for ln := range strings.SplitSeq(string(b), "\n") {
		ln = strings.TrimSpace(ln)
		if v, ok := strings.CutPrefix(ln, "pid="); ok {
			pid, err := strconv.Atoi(v)
			if err != nil {
				return LockInfo{}, fmt.Errorf("parse PID from lock file: %w", err)
			}
			if pid <= 0 {
				return LockInfo{}, fmt.Errorf("parse PID from lock file: non-positive value %d", pid)
			}
			info.PID = pid
		} else if v, ok := strings.CutPrefix(ln, "started="); ok {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				info.Started = t
			}
		}
	}
	if info.PID == 0 {
		return LockInfo{}, fmt.Errorf("pid not found in lock file")
	}
	return info, nil
}

// WaitUntilReleased polls lockPath until it disappears or ctx is cancelled.
// It checks immediately before starting the ticker so a fast exit is detected
// without waiting a full poll cycle. If the lock file persists but the process
// in info is no longer alive (e.g. SIGKILL or crash before cleanup), it returns
// nil immediately rather than waiting out the full timeout.
func WaitUntilReleased(ctx context.Context, lockPath string, info LockInfo, pollInterval time.Duration) error {
	released := func() bool {
		if _, err := os.Stat(lockPath); errors.Is(err, os.ErrNotExist) {
			return true
		}
		return !isProcessAlive(info.PID)
	}
	if released() {
		return nil
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if released() {
				return nil
			}
		}
	}
}

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
	pid, alive, staleErr := checkStaleness(lockPath)
	if staleErr != nil {
		// If we can't confidently read the lock, fail closed.
		return nil, fmt.Errorf("%w: lock=%s", ErrAlreadyRunning, lockPath)
	}
	if alive {
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
		pid, alive, staleErr := checkStaleness(lockPath)
		if staleErr == nil && alive {
			return nil, fmt.Errorf("%w: pid=%d lock=%s", ErrAlreadyRunning, pid, lockPath)
		}
		return nil, fmt.Errorf("%w: lock=%s", ErrAlreadyRunning, lockPath)
	}
	return nil, err
}

// checkStaleness reads the lock at lockPath and reports whether its PID is
// alive. Fail-closed policy on top of readLockFile: a vanished or unreadable
// lock file returns a non-nil err (see readLockFile's doc comment for why
// this differs from CheckLock's fail-open behavior).
func checkStaleness(lockPath string) (pid int, alive bool, err error) {
	info, exists, err := readLockFile(lockPath)
	if err != nil {
		return 0, false, err
	}
	if !exists {
		return 0, false, fmt.Errorf("lock file vanished mid-check")
	}
	return info.PID, isProcessAlive(info.PID), nil
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
