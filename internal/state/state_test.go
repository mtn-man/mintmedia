package state

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

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
