// Package state provides a history writer that is safe for concurrent goroutines and concurrent processes on macOS.
// It uses an in-process mutex plus an OS-level advisory file lock (syscall.Flock) instead of external flock.
// Locks are advisory and automatically released on file close.
package state

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// HistoryWriter is the minimal interface your processor already expects.
type HistoryWriter interface {
	Append(ctx context.Context, entry string) error
}

// History is the unified interface for Mintmedia history.
//
// - Processor code uses Append(...) for simple, preformatted lines.
// - Daemon and other components can use Record(...) for structured events.
//
// FileHistoryWriter implements both methods.
type History interface {
	Append(ctx context.Context, entry string) error
	Record(ctx context.Context, e Event) error
}

// FileHistoryWriter appends lines to a single history log.
// It is safe for concurrent goroutines within the same process.
type FileHistoryWriter struct {
	path string
	mu   sync.Mutex

	// If true, fsync after each write (more durable, slower).
	fsync bool
}

// HistoryOptions configures FileHistoryWriter.
type HistoryOptions struct {
	// If true, fsync after each write.
	Fsync bool
}

func NewFileHistoryWriter(path string, opts HistoryOptions) (*FileHistoryWriter, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("history path is empty")
	}
	return &FileHistoryWriter{
		path:  path,
		fsync: opts.Fsync,
	}, nil
}

// Append writes a preformatted line to the history file.
// The caller is responsible for including a timestamp if desired.
func (w *FileHistoryWriter) Append(ctx context.Context, entry string) (retErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}

	entry = strings.TrimRight(entry, "\n")
	if entry == "" {
		return nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(w.path), 0o755); err != nil {
		return fmt.Errorf("create history dir: %w", err)
	}

	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open history file: %w", err)
	}
	defer func() {
		if err := f.Close(); err != nil && retErr == nil {
			retErr = fmt.Errorf("close history file: %w", err)
		}
	}()

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock history file: %w", err)
	}
	defer func() {
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil && retErr == nil {
			retErr = fmt.Errorf("unlock history file: %w", err)
		}
	}()

	_, err = f.WriteString(entry + "\n")
	if err != nil {
		return fmt.Errorf("write history: %w", err)
	}

	if w.fsync {
		if err := f.Sync(); err != nil {
			return fmt.Errorf("fsync history: %w", err)
		}
	}

	return nil
}

// ----------------------------------------------------------------------------
// Structured events (recommended usage)
// ----------------------------------------------------------------------------

// Event is a structured record that serializes to a single line.
// Format (TSV-ish):
//
//	<RFC3339>\tEVENT=<KIND>\tkey=value\tkey=value...
type Event struct {
	Time   time.Time
	Kind   string            // e.g. "MAGNET_ADDED", "APPLIED", "WARN", "TX_CLEANUP"
	Fields map[string]string // optional key/value metadata
}

// Line renders the event as a single line suitable for Append().
func (e Event) Line() (string, error) {
	kind := strings.TrimSpace(e.Kind)
	if kind == "" {
		return "", errors.New("event kind is empty")
	}

	ts := e.Time
	if ts.IsZero() {
		ts = time.Now()
	}

	parts := []string{ts.Format(time.RFC3339), "EVENT=" + sanitizeToken(kind)}

	// Deterministic order for readability and testability.
	if len(e.Fields) > 0 {
		keys := make([]string, 0, len(e.Fields))
		for k := range e.Fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := e.Fields[k]
			if strings.TrimSpace(k) == "" {
				continue
			}
			parts = append(parts, sanitizeToken(k)+"="+sanitizeValue(v))
		}
	}

	return strings.Join(parts, "\t"), nil
}

// Record formats and appends a structured event in one call.
func (w *FileHistoryWriter) Record(ctx context.Context, e Event) error {
	line, err := e.Line()
	if err != nil {
		return err
	}
	return w.Append(ctx, line)
}

// Convenience helpers (optional; use as you like)

func MagnetAdded(btih, dn string) Event {
	return Event{
		Kind: "MAGNET_ADDED",
		Fields: map[string]string{
			"btih": btih,
			"dn":   dn,
		},
	}
}

func Applied(src, dst, category string, duration time.Duration) Event {
	return Event{
		Kind: "APPLIED",
		Fields: map[string]string{
			"src":      src,
			"dst":      dst,
			"category": category,
			"duration": duration.String(),
		},
	}
}

func TransmissionCleanup(removed int) Event {
	return Event{
		Kind: "TX_CLEANUP",
		Fields: map[string]string{
			"removed": fmt.Sprintf("%d", removed),
		},
	}
}

func Warn(msg string) Event {
	return Event{
		Kind: "WARN",
		Fields: map[string]string{
			"msg": msg,
		},
	}
}

func sanitizeToken(s string) string {
	// Tokens should be compact and safe inside key/value fields.
	// Replace whitespace and tabs to avoid breaking the TSV structure.
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.Join(strings.Fields(s), "_")
	return s
}

func sanitizeValue(s string) string {
	// Preserve readability but ensure one-line, tab-safe output.
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return s
}
