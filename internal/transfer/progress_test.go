package transfer

import (
	"io"
	"os"
	"testing"
)

func TestTerminalReporter_Done_NoOutputOnPipe(t *testing.T) {
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
	reporter.Done()
	_ = w.Close()

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(out) != "" {
		t.Fatalf("expected no output on pipe, got %q", string(out))
	}
}
