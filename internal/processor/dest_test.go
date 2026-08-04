// internal/processor/dest_test.go
package processor

import (
	"context"
	"errors"
	"testing"
)

func TestDestDegradedTracker_ClassifyDegraded_NoneDegradedSkipsPlan(t *testing.T) {
	var tracker DestDegradedTracker
	proc := &categoryTestProcessor{
		planFn: func(context.Context, Request) ([]Plan, error) {
			t.Fatal("Plan should not be called when no category is degraded")
			return nil, nil
		},
	}

	cat, degraded := tracker.ClassifyDegraded(context.Background(), proc, "/drop/Movie.2020.mkv")
	if degraded {
		t.Fatalf("degraded = true, want false")
	}
	if cat != "" {
		t.Fatalf("cat = %q, want empty", cat)
	}
}

func TestDestDegradedTracker_ClassifyDegraded_MatchingCategoryIsDegraded(t *testing.T) {
	var tracker DestDegradedTracker
	tracker.Mark(CategoryMovie)
	proc := &categoryTestProcessor{
		planFn: func(context.Context, Request) ([]Plan, error) {
			return []Plan{{Category: CategoryMovie}}, nil
		},
	}

	cat, degraded := tracker.ClassifyDegraded(context.Background(), proc, "/drop/Movie.2020.mkv")
	if !degraded {
		t.Fatalf("degraded = false, want true")
	}
	if cat != CategoryMovie {
		t.Fatalf("cat = %q, want %q", cat, CategoryMovie)
	}
}

func TestDestDegradedTracker_ClassifyDegraded_OtherCategoryStaysHealthy(t *testing.T) {
	var tracker DestDegradedTracker
	tracker.Mark(CategoryShow)
	proc := &categoryTestProcessor{
		planFn: func(context.Context, Request) ([]Plan, error) {
			return []Plan{{Category: CategoryMovie}}, nil
		},
	}

	cat, degraded := tracker.ClassifyDegraded(context.Background(), proc, "/drop/Movie.2020.mkv")
	if degraded {
		t.Fatalf("degraded = true, want false")
	}
	if cat != CategoryMovie {
		t.Fatalf("cat = %q, want %q -- ClassifyDegraded only leaves cat meaningless when degraded is true, not when it's false", cat, CategoryMovie)
	}
}

func TestDestDegradedTracker_ClassifyDegraded_UnknownCategoryStaysHealthy(t *testing.T) {
	var tracker DestDegradedTracker
	tracker.Mark(CategoryMovie)
	proc := &categoryTestProcessor{
		planFn: func(context.Context, Request) ([]Plan, error) {
			return nil, errors.New("boom")
		},
	}

	cat, degraded := tracker.ClassifyDegraded(context.Background(), proc, "/drop/unknown.mkv")
	if degraded {
		t.Fatalf("degraded = true, want false")
	}
	if cat != "" {
		t.Fatalf("cat = %q, want empty", cat)
	}
}
