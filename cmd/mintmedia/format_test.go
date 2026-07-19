package main

import (
	"strings"
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

	got := processDropCompactLine(res, 0)
	want := "SORTED   All.of.Us.Strangers.2023.mkv\n    ->   /Volumes/media/Movies/All of Us Strangers (2023)/All of Us Strangers (2023).mp4"
	if got != want {
		t.Fatalf("processDropCompactLine(applied) = %q, want %q", got, want)
	}
}

func TestProcessDropCompactLine_AppliedWithDuration(t *testing.T) {
	res := processor.Result{
		Applied: true,
		Plan: processor.Plan{
			MainSourcePath: "/tmp/drop/Some.Large.Remux.mkv",
			DestMainPath:   "/Volumes/media/Movies/Some Large Remux/Some Large Remux.mkv",
		},
	}

	got := processDropCompactLine(res, 4*time.Second)
	want := "SORTED   Some.Large.Remux.mkv\n    ->   /Volumes/media/Movies/Some Large Remux/Some Large Remux.mkv  (4s)"
	if got != want {
		t.Fatalf("processDropCompactLine(applied, 4s) = %q, want %q", got, want)
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

	got := processDropCompactLine(res, 0)
	want := "SKIPPED  Unknown.Release -- no main media found in directory"
	if got != want {
		t.Fatalf("processDropCompactLine(skipped) = %q, want %q", got, want)
	}
}

func TestPrintPlan_DuplicateLine(t *testing.T) {
	pl := processor.Plan{
		Category:       processor.CategoryMovie,
		MainSourcePath: "/tmp/drop/Get.Smart.2008.1080p.BluRay.x264-GROUP.mkv",
		DestMainPath:   "/Volumes/media/Movies/Get Smart (2008)/Get Smart (2008).mkv",
		MovieTitle:     "Get Smart (2008)",
		Duplicate:      true,
	}

	out := captureStdout(t, func() { printPlan(pl) })
	if !strings.Contains(out, "Duplicate:    yes") {
		t.Fatalf("expected a Duplicate line in plan output, got:\n%s", out)
	}
}

func TestPrintPlan_NoDuplicateLineWhenNotDuplicate(t *testing.T) {
	pl := processor.Plan{
		Category:       processor.CategoryMovie,
		MainSourcePath: "/tmp/drop/Get.Smart.2008.1080p.BluRay.x264-GROUP.mkv",
		DestMainPath:   "/Volumes/media/Movies/Get Smart (2008)/Get Smart (2008).mkv",
		MovieTitle:     "Get Smart (2008)",
	}

	out := captureStdout(t, func() { printPlan(pl) })
	if strings.Contains(out, "Duplicate:") {
		t.Fatalf("expected no Duplicate line in plan output, got:\n%s", out)
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
				Applied: 7,
				Skipped: 2,
				Errors:  1,
				Elapsed: 3*time.Minute + 14*time.Second + 250*time.Millisecond,
			},
			want: "INFO     10 files -- 7 sorted, 2 skipped, 1 error (3m14s)",
		},
		{
			name: "clean run",
			sum: ProcessDropSummary{
				Applied: 3,
				Elapsed: 62 * time.Second,
			},
			want: "INFO     3 files -- 3 sorted (1m2s)",
		},
		{
			name: "single file",
			sum: ProcessDropSummary{
				Applied: 1,
				Elapsed: 5 * time.Second,
			},
			want: "INFO     1 file -- 1 sorted (5s)",
		},
		{
			name: "multiple errors",
			sum: ProcessDropSummary{
				Applied: 2,
				Errors:  2,
				Elapsed: 10 * time.Second,
			},
			want: "INFO     4 files -- 2 sorted, 2 errors (10s)",
		},
		{
			name: "season pack",
			sum: ProcessDropSummary{
				Applied: 8,
				Elapsed: 6*time.Minute + 2*time.Second,
			},
			want: "INFO     8 files -- 8 sorted (6m2s)",
		},
		{
			name: "sub-second duration still shown",
			sum: ProcessDropSummary{
				Applied: 1,
				Elapsed: 400 * time.Millisecond,
			},
			want: "INFO     1 file -- 1 sorted (0.4s)",
		},
		{
			name: "very fast duration shown with millisecond precision",
			sum: ProcessDropSummary{
				Applied: 76,
				Elapsed: 43 * time.Millisecond,
			},
			want: "INFO     76 files -- 76 sorted (0.043s)",
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
