package main

import (
	"testing"
	"time"

	"github.com/mtn-man/mintmedia/internal/processor"
)

func TestProcessDropCompactLine_Applied(t *testing.T) {
	res := processor.Result{
		Applied: true,
		Plan: processor.Plan{
			MainSourcePath: "/tmp/drop/All.of.Us.Strangers.2023.mkv",
			DestMainPath:   "/Volumes/media/Movies/All of Us Strangers (2023)/All of Us Strangers (2023).mp4",
		},
	}

	got := processDropCompactLine(res)
	want := "MOVED    All.of.Us.Strangers.2023.mkv\n    ->   /Volumes/media/Movies/All of Us Strangers (2023)/All of Us Strangers (2023).mp4"
	if got != want {
		t.Fatalf("processDropCompactLine(applied) = %q, want %q", got, want)
	}
}

func TestProcessDropCompactLine_Skipped(t *testing.T) {
	res := processor.Result{
		Applied: false,
		Reason:  "no main media found in directory",
		Plan: processor.Plan{
			InputPath: "/tmp/drop/Unknown.Release",
		},
	}

	got := processDropCompactLine(res)
	want := "SKIPPED  Unknown.Release \u2014 no main media found in directory"
	if got != want {
		t.Fatalf("processDropCompactLine(skipped) = %q, want %q", got, want)
	}
}

func TestProcessDropSummaryLine(t *testing.T) {
	tests := []struct {
		name string
		sum  ProcessDropSummary
		want string
	}{
		{
			name: "all fields",
			sum: ProcessDropSummary{
				Candidates: 10,
				Applied:    7,
				Skipped:    2,
				Errors:     1,
				Elapsed:    3*time.Minute + 14*time.Second + 250*time.Millisecond,
			},
			want: "INFO     10 files \u2014 7 moved, 2 skipped, 1 error (3m14s)",
		},
		{
			name: "clean run",
			sum: ProcessDropSummary{
				Candidates: 3,
				Applied:    3,
				Elapsed:    62 * time.Second,
			},
			want: "INFO     3 files \u2014 3 moved (1m2s)",
		},
		{
			name: "single file",
			sum: ProcessDropSummary{
				Candidates: 1,
				Applied:    1,
				Elapsed:    5 * time.Second,
			},
			want: "INFO     1 file \u2014 1 moved (5s)",
		},
		{
			name: "multiple errors",
			sum: ProcessDropSummary{
				Candidates: 4,
				Applied:    2,
				Errors:     2,
				Elapsed:    10 * time.Second,
			},
			want: "INFO     4 files \u2014 2 moved, 2 errors (10s)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := processDropSummaryLine(tt.sum)
			if got != tt.want {
				t.Fatalf("processDropSummaryLine() = %q, want %q", got, tt.want)
			}
		})
	}
}
