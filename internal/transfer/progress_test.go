package transfer

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestTerminalReporter_Done_PrintsByDefault(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Errorf("close reader pipe: %v", err)
		}
	})

	reporter := NewTerminalReporter(w, ReportOptions{})
	reporter.Done(Snapshot{
		Name:    "Example.mkv",
		Total:   1024,
		Elapsed: 2 * time.Second,
	})
	_ = w.Close()

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(out), "COPY DONE:") {
		t.Fatalf("expected output to include COPY DONE, got %q", string(out))
	}
}

func TestTerminalReporter_Done_CanBeSuppressed(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Errorf("close reader pipe: %v", err)
		}
	})

	reporter := NewTerminalReporter(w, ReportOptions{
		SuppressDoneLine: true,
	})
	reporter.Done(Snapshot{
		Name:    "Example.mkv",
		Total:   1024,
		Elapsed: 2 * time.Second,
	})
	_ = w.Close()

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(out) != "" {
		t.Fatalf("expected no output when done line suppressed, got %q", string(out))
	}
}
