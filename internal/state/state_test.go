package state

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFileHistoryWriter_AppendAndRecord(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "history.log")

	w, err := NewFileHistoryWriter(path, HistoryOptions{Fsync: false})
	if err != nil {
		t.Fatalf("NewFileHistoryWriter error: %v", err)
	}

	if err := w.Append(context.Background(), "first line\n"); err != nil {
		t.Fatalf("Append error: %v", err)
	}

	fixed := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := w.Record(context.Background(), Event{
		Time: fixed,
		Kind: "TEST",
		Fields: map[string]string{
			"beta":  "2",
			"alpha": "1",
		},
	}); err != nil {
		t.Fatalf("Record error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d (%q)", len(lines), string(data))
	}
	if lines[0] != "first line" {
		t.Fatalf("line 1 = %q, want %q", lines[0], "first line")
	}
	want := "2024-01-02T03:04:05Z\tEVENT=TEST\talpha=1\tbeta=2"
	if lines[1] != want {
		t.Fatalf("line 2 = %q, want %q", lines[1], want)
	}
}

func TestFileHistoryWriter_AppendEmptyDoesNotCreateFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "history.log")

	w, err := NewFileHistoryWriter(path, HistoryOptions{Fsync: false})
	if err != nil {
		t.Fatalf("NewFileHistoryWriter error: %v", err)
	}

	if err := w.Append(context.Background(), ""); err != nil {
		t.Fatalf("Append error: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected history file not to exist, stat err=%v", err)
	}
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
