package logging

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

// ConsoleSink writes human-facing operational messages without prefixes.
type ConsoleSink struct {
	stdout io.Writer
	stderr io.Writer
}

func NewConsoleSink(stdout, stderr io.Writer) *ConsoleSink {
	return &ConsoleSink{stdout: stdout, stderr: stderr}
}

func (s *ConsoleSink) Write(entry Entry) error {
	msg := strings.TrimSpace(entry.Message)
	if msg == "" {
		return nil
	}
	target := s.stdout
	if entry.Level == LevelWarn || entry.Level == LevelError {
		target = s.stderr
	}
	if target == nil {
		return nil
	}
	_, err := fmt.Fprintln(target, msg)
	return err
}

// HistorySink appends JSONL records to a single file.
type HistorySink struct {
	path string
	mu   sync.Mutex
}

func NewHistorySink(path string) *HistorySink {
	return &HistorySink{path: strings.TrimSpace(path)}
}

func (s *HistorySink) Write(entry Entry) (retErr error) {
	if strings.TrimSpace(s.path) == "" {
		return fmt.Errorf("history path is empty")
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal history entry: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create history dir: %w", err)
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open history file: %w", err)
	}
	defer func() {
		if err := f.Close(); err != nil && retErr == nil {
			retErr = fmt.Errorf("close history file: %w", err)
		}
	}()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock history file: %w", err)
	}
	defer func() {
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil && retErr == nil {
			retErr = fmt.Errorf("unlock history file: %w", err)
		}
	}()

	if _, err := f.Write(append(payload, '\n')); err != nil {
		return fmt.Errorf("write history entry: %w", err)
	}

	return nil
}
