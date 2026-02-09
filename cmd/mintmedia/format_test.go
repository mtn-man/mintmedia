package main

import (
	"testing"
	"time"

	"github.com/Mtn-Man/mintmedia/internal/processor"
)

func TestProcessDropCompactLine_Applied(t *testing.T) {
	res := processor.Result{
		Applied: true,
		Plan: processor.Plan{
			DestMainPath: "/Volumes/media/Movies/All of Us Strangers (2023)/All of Us Strangers (2023).mp4",
		},
	}

	got := processDropCompactLine(res)
	want := "OK   All of Us Strangers (2023).mp4 -> /Volumes/media/Movies/All of Us Strangers (2023)/All of Us Strangers (2023).mp4"
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
	want := "SKIP Unknown.Release (no main media found in directory)"
	if got != want {
		t.Fatalf("processDropCompactLine(skipped) = %q, want %q", got, want)
	}
}

func TestProcessDropSummaryLine(t *testing.T) {
	sum := ProcessDropSummary{
		Candidates: 10,
		Results:    12,
		Applied:    10,
		Skipped:    2,
		Errors:     1,
		Elapsed:    3*time.Minute + 14*time.Second + 250*time.Millisecond,
	}

	got := processDropSummaryLine(sum)
	want := "SUMMARY candidates=10 results=12 applied=10 skipped=2 errors=1 elapsed=3m14s"
	if got != want {
		t.Fatalf("processDropSummaryLine() = %q, want %q", got, want)
	}
}
