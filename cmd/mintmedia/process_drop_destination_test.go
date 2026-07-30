package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mtn-man/mintmedia/internal/processor"
)

// categoryForTestPath classifies a synthetic test path as a Show if its
// basename contains "show", otherwise as a Movie -- used by these tests'
// planFn stubs to mimic processor.CategoryForPath's real classification
// without depending on real filename parsing.
func categoryForTestPath(path string) processor.Category {
	if strings.Contains(strings.ToLower(filepath.Base(path)), "show") {
		return processor.CategoryShow
	}
	return processor.CategoryMovie
}

func TestProcessDropFolder_DestinationDegraded_SkipsRemainingCategoryItems(t *testing.T) {
	drop := t.TempDir()
	movieA := filepath.Join(drop, "movie-a.mkv")
	showA := filepath.Join(drop, "show-a.mkv")
	showB := filepath.Join(drop, "show-b.mkv")
	writeProcessDropFile(t, movieA)
	writeProcessDropFile(t, showA)
	writeProcessDropFile(t, showB)

	proc := &processDropStubProcessor{
		planFn: func(_ context.Context, req processor.Request) ([]processor.Plan, error) {
			return []processor.Plan{{InputPath: req.InputPath, Category: categoryForTestPath(req.InputPath)}}, nil
		},
		processFn: func(_ context.Context, req processor.Request) error {
			if strings.Contains(req.InputPath, "show-a") {
				return &processor.DestinationUnavailableError{Category: processor.CategoryShow, Err: errors.New("no space left on device")}
			}
			if req.OnResult != nil {
				req.OnResult(processor.Result{Applied: true})
			}
			return nil
		},
	}

	out := processDropFolder(
		context.Background(),
		proc,
		drop,
		t.TempDir(),
		t.TempDir(),
		"",
		"off",
		false,
		200*time.Millisecond,
		200*time.Millisecond,
	)

	if out.ErrorCount != 2 {
		t.Fatalf("ErrorCount = %d, want 2 (1 triggering + 1 fast-path skip)", out.ErrorCount)
	}

	calls := proc.Calls()
	wantAttempted := map[string]bool{movieA: true, showA: true}
	for _, c := range calls {
		if c == showB {
			t.Fatalf("show-b.mkv was attempted, want it proactively skipped once Shows was degraded: calls=%v", calls)
		}
		delete(wantAttempted, c)
	}
	if len(wantAttempted) != 0 {
		t.Fatalf("expected paths not attempted: %v (calls=%v)", wantAttempted, calls)
	}
}

func TestProcessDropFolder_DestinationDegraded_FastPathAvoidsPartialPlanError(t *testing.T) {
	drop := t.TempDir()
	showA := filepath.Join(drop, "show-a.mkv")
	showB := filepath.Join(drop, "show-b.mkv")
	writeProcessDropFile(t, showA)
	writeProcessDropFile(t, showB)

	proc := &processDropStubProcessor{
		planFn: func(_ context.Context, req processor.Request) ([]processor.Plan, error) {
			if strings.Contains(req.InputPath, "show-b") {
				// Plan still yields a usable plan for show-b alongside an
				// unrelated partial-plan error -- CategoryForPath must use
				// plans[0].Category rather than bailing out on the error.
				return []processor.Plan{{InputPath: req.InputPath, Category: processor.CategoryShow}},
					&processor.PartialPlanError{Issues: []processor.PlanIssue{{Path: "sibling.mkv", Err: errors.New("unparseable")}}}
			}
			return []processor.Plan{{InputPath: req.InputPath, Category: processor.CategoryShow}}, nil
		},
		processFn: func(_ context.Context, req processor.Request) error {
			if strings.Contains(req.InputPath, "show-a") {
				return &processor.DestinationUnavailableError{Category: processor.CategoryShow, Err: errors.New("no space left on device")}
			}
			if req.OnResult != nil {
				req.OnResult(processor.Result{Applied: true})
			}
			return nil
		},
	}

	out := processDropFolder(
		context.Background(),
		proc,
		drop,
		t.TempDir(),
		t.TempDir(),
		"",
		"off",
		false,
		200*time.Millisecond,
		200*time.Millisecond,
	)

	if out.ErrorCount != 2 {
		t.Fatalf("ErrorCount = %d, want 2 (1 triggering + 1 fast-path skip)", out.ErrorCount)
	}
	for _, c := range proc.Calls() {
		if c == showB {
			t.Fatalf("show-b.mkv reached Process despite the fast path having a usable plan (via PartialPlanError): calls=%v", proc.Calls())
		}
	}
}

func TestProcessDropFolder_DestinationDegraded_SummaryReflectsCount(t *testing.T) {
	drop := t.TempDir()
	movieA := filepath.Join(drop, "movie-a.mkv")
	showA := filepath.Join(drop, "show-a.mkv")
	showB := filepath.Join(drop, "show-b.mkv")
	writeProcessDropFile(t, movieA)
	writeProcessDropFile(t, showA)
	writeProcessDropFile(t, showB)

	proc := &processDropStubProcessor{
		planFn: func(_ context.Context, req processor.Request) ([]processor.Plan, error) {
			return []processor.Plan{{InputPath: req.InputPath, Category: categoryForTestPath(req.InputPath)}}, nil
		},
		processFn: func(_ context.Context, req processor.Request) error {
			if strings.Contains(req.InputPath, "show-a") {
				return &processor.DestinationUnavailableError{Category: processor.CategoryShow, Err: errors.New("no space left on device")}
			}
			if req.OnResult != nil {
				req.OnResult(processor.Result{Applied: true})
			}
			return nil
		},
	}

	var out ProcessDropOutcome
	stdout := captureStdout(t, func() {
		out = processDropFolder(
			context.Background(),
			proc,
			drop,
			t.TempDir(),
			t.TempDir(),
			"",
			"off",
			false,
			200*time.Millisecond,
			200*time.Millisecond,
		)
	})

	if out.ErrorCount != 2 {
		t.Fatalf("ErrorCount = %d, want 2", out.ErrorCount)
	}
	if !strings.Contains(stdout, "2 errors (2 destination unavailable)") {
		t.Fatalf("summary line missing expected destination-degraded annotation, got:\n%s", stdout)
	}
}

func TestProcessDropFolder_DestinationReadOnly_FailsUpfrontCheck(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permission bits")
	}

	drop := t.TempDir()
	writeProcessDropFile(t, filepath.Join(drop, "movie-a.mkv"))

	moviesDir := t.TempDir()
	showsDir := t.TempDir()
	if err := os.Chmod(showsDir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(showsDir, 0o755) })

	proc := &processDropStubProcessor{}

	stderr := captureOutput(t, &os.Stderr, func() {
		out := processDropFolder(
			context.Background(),
			proc,
			drop,
			moviesDir,
			showsDir,
			"",
			"off",
			false,
			200*time.Millisecond,
			200*time.Millisecond,
		)
		if out.ErrorCount != 1 {
			t.Fatalf("ErrorCount = %d, want 1", out.ErrorCount)
		}
	})

	if len(proc.Calls()) != 0 {
		t.Fatalf("expected no items processed before the destination check, got calls=%v", proc.Calls())
	}
	if !strings.Contains(stderr, "destination unavailable") {
		t.Fatalf("stderr missing destination-unavailable message, got:\n%s", stderr)
	}
}
