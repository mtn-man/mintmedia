package transfer

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mtn-man/mintmedia/internal/console"
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

func TestTruncateWithEllipsis(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		maxLen int
		want   string
	}{
		{"zero maxLen", "hello", 0, ""},
		{"negative maxLen", "hello", -1, ""},
		{"maxLen one", "hello", 1, "…"},
		{"shorter than limit unchanged", "hi", 10, "hi"},
		{"exactly at limit unchanged", "hello", 5, "hello"},
		{"longer truncated with ellipsis", "hello world", 8, "hello w…"},
		{"multi-byte runes counted correctly", "日本語のタイトル", 4, "日本語…"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateWithEllipsis(tt.s, tt.maxLen)
			if got != tt.want {
				t.Fatalf("truncateWithEllipsis(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestShouldShowBar(t *testing.T) {
	baseOpts := ReportOptions{
		EnableBar:     true,
		BarMinBytes:   100,
		BarMinElapsed: 500 * time.Millisecond,
	}

	tests := []struct {
		name    string
		inPlace bool
		opts    ReportOptions
		snap    Snapshot
		want    bool
	}{
		{
			name:    "all conditions met",
			inPlace: true,
			opts:    baseOpts,
			snap:    Snapshot{Total: 1000, Elapsed: time.Second},
			want:    true,
		},
		{
			name:    "not in-place",
			inPlace: false,
			opts:    baseOpts,
			snap:    Snapshot{Total: 1000, Elapsed: time.Second},
			want:    false,
		},
		{
			name:    "bar disabled",
			inPlace: true,
			opts:    ReportOptions{EnableBar: false, BarMinBytes: 100, BarMinElapsed: 500 * time.Millisecond},
			snap:    Snapshot{Total: 1000, Elapsed: time.Second},
			want:    false,
		},
		{
			name:    "total unknown",
			inPlace: true,
			opts:    baseOpts,
			snap:    Snapshot{Total: 0, Elapsed: time.Second},
			want:    false,
		},
		{
			name:    "under BarMinBytes",
			inPlace: true,
			opts:    baseOpts,
			snap:    Snapshot{Total: 50, Elapsed: time.Second},
			want:    false,
		},
		{
			name:    "under BarMinElapsed",
			inPlace: true,
			opts:    baseOpts,
			snap:    Snapshot{Total: 1000, Elapsed: 100 * time.Millisecond},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &terminalReporter{inPlace: tt.inPlace, opts: tt.opts}
			got := r.shouldShowBar(tt.snap)
			if got != tt.want {
				t.Fatalf("shouldShowBar(%+v) with inPlace=%v opts=%+v = %v, want %v", tt.snap, tt.inPlace, tt.opts, got, tt.want)
			}
		})
	}
}

func TestEtaToken(t *testing.T) {
	baseOpts := ReportOptions{EnableETA: true, EtaMinElapsed: 500 * time.Millisecond}

	tests := []struct {
		name   string
		opts   ReportOptions
		snap   Snapshot
		wantOK bool
	}{
		{
			name:   "eta disabled",
			opts:   ReportOptions{EnableETA: false},
			snap:   Snapshot{Total: 1000, Copied: 500, Elapsed: time.Second},
			wantOK: false,
		},
		{
			name:   "total unknown",
			opts:   baseOpts,
			snap:   Snapshot{Total: 0, Copied: 500, Elapsed: time.Second},
			wantOK: false,
		},
		{
			name:   "nothing copied yet",
			opts:   baseOpts,
			snap:   Snapshot{Total: 1000, Copied: 0, Elapsed: time.Second},
			wantOK: false,
		},
		{
			name:   "zero elapsed",
			opts:   baseOpts,
			snap:   Snapshot{Total: 1000, Copied: 500, Elapsed: 0},
			wantOK: false,
		},
		{
			name:   "under EtaMinElapsed",
			opts:   baseOpts,
			snap:   Snapshot{Total: 1000, Copied: 500, Elapsed: 100 * time.Millisecond},
			wantOK: false,
		},
		{
			name:   "already complete",
			opts:   baseOpts,
			snap:   Snapshot{Total: 1000, Copied: 1000, Elapsed: time.Second},
			wantOK: false,
		},
		{
			name:   "normal case",
			opts:   baseOpts,
			snap:   Snapshot{Total: 1000, Copied: 500, Elapsed: time.Second},
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &terminalReporter{opts: tt.opts}
			got, ok := r.etaToken(tt.snap)
			if ok != tt.wantOK {
				t.Fatalf("etaToken(%+v) ok = %v, want %v (token %q)", tt.snap, ok, tt.wantOK, got)
			}
			if ok && !strings.HasPrefix(got, "ETA ") {
				t.Fatalf("etaToken(%+v) = %q, want prefix %q", tt.snap, got, "ETA ")
			}
		})
	}
}

func TestRenderClassicCopyingLine(t *testing.T) {
	t.Run("known total, in-place colorizes", func(t *testing.T) {
		r := &terminalReporter{inPlace: true}
		line := r.renderClassicCopyingLine("movie.mkv", Snapshot{Copied: 500, Total: 1000, RateMBps: 1.5})
		if !strings.Contains(line, "movie.mkv") || !strings.Contains(line, "50.0%") || !strings.Contains(line, "500 B / 1000 B") {
			t.Fatalf("renderClassicCopyingLine = %q, missing expected tokens", line)
		}
		if !strings.Contains(line, console.Yellow) || !strings.Contains(line, console.Cyan) {
			t.Fatalf("renderClassicCopyingLine (in-place) = %q, want ANSI color codes", line)
		}
	})

	t.Run("known total, not in-place is uncolored", func(t *testing.T) {
		r := &terminalReporter{inPlace: false}
		line := r.renderClassicCopyingLine("movie.mkv", Snapshot{Copied: 500, Total: 1000, RateMBps: 1.5})
		if strings.Contains(line, console.Reset) {
			t.Fatalf("renderClassicCopyingLine (not in-place) = %q, want no ANSI codes", line)
		}
	})

	t.Run("unknown total", func(t *testing.T) {
		r := &terminalReporter{inPlace: false}
		line := r.renderClassicCopyingLine("movie.mkv", Snapshot{Copied: 500, Total: 0, RateMBps: 1.5})
		if !strings.Contains(line, "movie.mkv") || !strings.Contains(line, "500 B copied") {
			t.Fatalf("renderClassicCopyingLine(unknown total) = %q, missing expected tokens", line)
		}
	})
}

func TestRenderCopyingLine(t *testing.T) {
	t.Run("dispatches to bar line when conditions met", func(t *testing.T) {
		r := &terminalReporter{
			inPlace: true,
			opts:    ReportOptions{EnableBar: true, BarMinBytes: 100, BarMinElapsed: 0, BarWidth: 10},
		}
		line := r.renderCopyingLine("movie.mkv", Snapshot{Copied: 500, Total: 1000, RateMBps: 1.5})
		if !strings.Contains(line, "[") || !strings.Contains(line, "]") {
			t.Fatalf("renderCopyingLine (bar conditions met) = %q, want a bracketed bar", line)
		}
	})

	t.Run("dispatches to classic line otherwise", func(t *testing.T) {
		r := &terminalReporter{inPlace: false, opts: ReportOptions{EnableBar: false}}
		line := r.renderCopyingLine("movie.mkv", Snapshot{Copied: 500, Total: 1000, RateMBps: 1.5})
		if strings.Contains(line, "[") {
			t.Fatalf("renderCopyingLine (bar disabled) = %q, want classic line without a bar", line)
		}
		if !strings.Contains(line, "50.0%") {
			t.Fatalf("renderCopyingLine (bar disabled) = %q, want classic percentage format", line)
		}
	})
}

func TestRenderBarLine(t *testing.T) {
	// Snapshot.out is left nil so terminalWidth(nil) fails and the name is
	// never truncated -- this test only concerns itself with the bar/pct/
	// byte-size/rate tokens, not terminal-width-aware truncation.
	r := &terminalReporter{
		inPlace: true,
		opts:    ReportOptions{EnableBar: true, BarWidth: 10},
	}
	line := r.renderBarLine("movie.mkv", Snapshot{Copied: 500, Total: 1000, RateMBps: 1.5})

	for _, want := range []string{"movie.mkv", "50%", "500 B/1000 B", "1.5 MB/s"} {
		if !strings.Contains(line, want) {
			t.Fatalf("renderBarLine = %q, missing expected token %q", line, want)
		}
	}
	if !strings.Contains(line, console.Cyan) {
		t.Fatalf("renderBarLine = %q, want a colored bar/percentage", line)
	}
}

func TestRenderBarLine_ZeroWidthFallsBackToClassic(t *testing.T) {
	r := &terminalReporter{
		inPlace: true,
		opts:    ReportOptions{EnableBar: true, BarWidth: 0},
	}
	line := r.renderBarLine("movie.mkv", Snapshot{Copied: 500, Total: 1000, RateMBps: 1.5})
	if !strings.Contains(line, "50.0%") {
		t.Fatalf("renderBarLine(BarWidth=0) = %q, want classic-line percentage format", line)
	}
}

func TestColorizeCopyingLine(t *testing.T) {
	line := "SORTING  movie.mkv 50.0% (500 B / 1000 B) 1.5 MB/s"
	got := colorizeCopyingLine(line)

	if !strings.Contains(got, console.Yellow+"SORTING  "+console.Reset) {
		t.Fatalf("colorizeCopyingLine label = %q, want colored %q prefix", got, "SORTING  ")
	}
	if !strings.Contains(got, console.Cyan+"50.0%"+console.Reset) {
		t.Fatalf("colorizeCopyingLine = %q, want colored percentage token", got)
	}
}

func TestColorizeCopyingLine_NoPercentUnchanged(t *testing.T) {
	line := "SORTING  movie.mkv (500 B copied) 1.5 MB/s"
	got := colorizeCopyingLine(line)
	if !strings.Contains(got, console.Yellow+"SORTING  "+console.Reset) {
		t.Fatalf("colorizeCopyingLine = %q, want colored label even with no percentage", got)
	}
}

func TestTerminalWidth_Pipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = r.Close()
		_ = w.Close()
	})

	if _, ok := terminalWidth(w); ok {
		t.Fatal("expected terminalWidth(pipe) to report ok=false")
	}
}

func TestTerminalWidth_Nil(t *testing.T) {
	if _, ok := terminalWidth(nil); ok {
		t.Fatal("expected terminalWidth(nil) to report ok=false")
	}
}
