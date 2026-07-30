// internal/processor/category_test.go
package processor

import (
	"context"
	"errors"
	"testing"
)

// categoryTestProcessor is a minimal Processor fake for exercising
// CategoryForPath in isolation, without going through a real processorImpl.
type categoryTestProcessor struct {
	planFn func(ctx context.Context, req Request) ([]Plan, error)
}

func (f *categoryTestProcessor) Plan(ctx context.Context, req Request) ([]Plan, error) {
	return f.planFn(ctx, req)
}

func (f *categoryTestProcessor) Apply(context.Context, []Plan) ([]Result, error) { return nil, nil }

func (f *categoryTestProcessor) Process(context.Context, Request) error { return nil }

func (f *categoryTestProcessor) SortCandidates(_ context.Context, paths []string) ([]string, []SortError, error) {
	return paths, nil, nil
}

func (f *categoryTestProcessor) CountMainMedia(context.Context, string) (int, error) { return 0, nil }

func TestCategoryForPath_PlanSuccess(t *testing.T) {
	proc := &categoryTestProcessor{
		planFn: func(context.Context, Request) ([]Plan, error) {
			return []Plan{{Category: CategoryShow}}, nil
		},
	}

	cat, ok := CategoryForPath(context.Background(), proc, "/drop/Show.S01E01.mkv")
	if !ok {
		t.Fatalf("ok = false, want true")
	}
	if cat != CategoryShow {
		t.Fatalf("cat = %q, want %q", cat, CategoryShow)
	}
}

func TestCategoryForPath_PartialPlanErrorStillUsesFirstPlan(t *testing.T) {
	proc := &categoryTestProcessor{
		planFn: func(context.Context, Request) ([]Plan, error) {
			return []Plan{{Category: CategoryMovie}}, &PartialPlanError{
				Issues: []PlanIssue{{Path: "/drop/sibling.mkv", Err: errors.New("unparseable")}},
			}
		},
	}

	cat, ok := CategoryForPath(context.Background(), proc, "/drop/Movie.2020.mkv")
	if !ok {
		t.Fatalf("ok = false, want true")
	}
	if cat != CategoryMovie {
		t.Fatalf("cat = %q, want %q", cat, CategoryMovie)
	}
}

func TestCategoryForPath_PlanFailsWithDestinationUnavailable(t *testing.T) {
	proc := &categoryTestProcessor{
		planFn: func(context.Context, Request) ([]Plan, error) {
			return nil, &DestinationUnavailableError{Category: CategoryShow, Err: errors.New("no space left on device")}
		},
	}

	cat, ok := CategoryForPath(context.Background(), proc, "/drop/Show.S02E03.mkv")
	if !ok {
		t.Fatalf("ok = false, want true")
	}
	if cat != CategoryShow {
		t.Fatalf("cat = %q, want %q", cat, CategoryShow)
	}
}

func TestCategoryForPath_PlanFailsForUnrelatedReason(t *testing.T) {
	proc := &categoryTestProcessor{
		planFn: func(context.Context, Request) ([]Plan, error) {
			return nil, errors.New("boom")
		},
	}

	cat, ok := CategoryForPath(context.Background(), proc, "/drop/unknown.mkv")
	if ok {
		t.Fatalf("ok = true, want false")
	}
	if cat != "" {
		t.Fatalf("cat = %q, want empty", cat)
	}
}
