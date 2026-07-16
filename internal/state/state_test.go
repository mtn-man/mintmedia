package state

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// deadPID spawns a trivial child process, waits for it to exit, and returns
// its PID -- guaranteed not to be alive, unlike a magic-number PID that
// could coincidentally collide with a real process in CI.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatalf("spawn helper process: %v", err)
	}
	return cmd.Process.Pid
}

func TestAcquireLock_BasicAndRelease(t *testing.T) {
	root := t.TempDir()
	lockPath := filepath.Join(root, "mintmedia.lock")

	release, err := AcquireLock(lockPath)
	if err != nil {
		t.Fatalf("AcquireLock error: %v", err)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("expected lock file to exist: %v", err)
	}

	_, err = AcquireLock(lockPath)
	if err == nil {
		_ = release()
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, ErrAlreadyRunning) {
		_ = release()
		t.Fatalf("expected ErrAlreadyRunning, got: %v", err)
	}

	if err := release(); err != nil {
		t.Fatalf("release error: %v", err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("expected lock file to be removed, stat err=%v", err)
	}

	release2, err := AcquireLock(lockPath)
	if err != nil {
		t.Fatalf("AcquireLock after release error: %v", err)
	}
	if err := release2(); err != nil {
		t.Fatalf("release2 error: %v", err)
	}
}

func TestAcquireLock_StaleLockRemovedAndRetried(t *testing.T) {
	root := t.TempDir()
	lockPath := filepath.Join(root, "mintmedia.lock")

	pid := deadPID(t)
	contents := fmt.Sprintf("pid=%d\nstarted=%s\n", pid, time.Now().Format(time.RFC3339))
	if err := os.WriteFile(lockPath, []byte(contents), 0o644); err != nil {
		t.Fatalf("write stale lock: %v", err)
	}

	release, err := AcquireLock(lockPath)
	if err != nil {
		t.Fatalf("AcquireLock over stale lock: %v", err)
	}
	defer func() { _ = release() }()

	b, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lock file: %v", err)
	}
	newInfo, err := parseLockContents(b)
	if err != nil {
		t.Fatalf("parse new lock contents: %v", err)
	}
	if newInfo.PID != os.Getpid() {
		t.Fatalf("lock PID = %d, want current process PID %d", newInfo.PID, os.Getpid())
	}
}

func TestAcquireLock_MalformedLockFailsClosed(t *testing.T) {
	root := t.TempDir()
	lockPath := filepath.Join(root, "mintmedia.lock")

	if err := os.WriteFile(lockPath, []byte("not a valid lock file"), 0o644); err != nil {
		t.Fatalf("write malformed lock: %v", err)
	}

	_, err := AcquireLock(lockPath)
	if err == nil {
		t.Fatalf("expected error for malformed lock, got nil")
	}
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("expected fail-closed ErrAlreadyRunning, got: %v", err)
	}
	if _, statErr := os.Stat(lockPath); statErr != nil {
		t.Fatalf("expected malformed lock file to be left in place: %v", statErr)
	}
}

func TestCheckLock_AbsentLockIsNotRunning(t *testing.T) {
	root := t.TempDir()
	lockPath := filepath.Join(root, "mintmedia.lock")

	info, running, err := CheckLock(lockPath)
	if err != nil {
		t.Fatalf("CheckLock error: %v", err)
	}
	if running {
		t.Fatalf("running = true, want false for absent lock")
	}
	if info != (LockInfo{}) {
		t.Fatalf("info = %+v, want zero value", info)
	}
}

func TestCheckLock_StaleLockIsNotRunning(t *testing.T) {
	root := t.TempDir()
	lockPath := filepath.Join(root, "mintmedia.lock")

	pid := deadPID(t)
	contents := fmt.Sprintf("pid=%d\n", pid)
	if err := os.WriteFile(lockPath, []byte(contents), 0o644); err != nil {
		t.Fatalf("write stale lock: %v", err)
	}

	info, running, err := CheckLock(lockPath)
	if err != nil {
		t.Fatalf("CheckLock error: %v", err)
	}
	if running {
		t.Fatalf("running = true, want false for stale lock")
	}
	if info.PID != pid {
		t.Fatalf("info.PID = %d, want %d", info.PID, pid)
	}
}

func TestCheckLock_MalformedLockReturnsError(t *testing.T) {
	root := t.TempDir()
	lockPath := filepath.Join(root, "mintmedia.lock")

	if err := os.WriteFile(lockPath, []byte("garbage"), 0o644); err != nil {
		t.Fatalf("write malformed lock: %v", err)
	}

	_, _, err := CheckLock(lockPath)
	if err == nil {
		t.Fatalf("expected error for malformed lock, got nil")
	}
}
