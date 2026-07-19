package processor

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// stubRunProcessor is a minimal Processor for testing ProcessEach.
type stubRunProcessor struct {
	results []Result
	err     error
}

func (s *stubRunProcessor) Plan(_ context.Context, _ Request) ([]Plan, error) { return nil, nil }
func (s *stubRunProcessor) Apply(_ context.Context, _ []Plan) ([]Result, error) {
	return nil, nil
}
func (s *stubRunProcessor) Process(_ context.Context, req Request) error {
	if req.OnResult != nil {
		for _, r := range s.results {
			req.OnResult(r)
		}
	}
	return s.err
}
func (s *stubRunProcessor) SortCandidates(_ context.Context, paths []string) ([]string, []SortError, error) {
	return paths, nil, nil
}

func (s *stubRunProcessor) CountMainMedia(context.Context, string) (int, error) {
	return 0, nil
}

func TestProcessEach_Streaming(t *testing.T) {
	want := []Result{
		{Applied: true, Reason: ""},
		{Applied: false, Reason: "some reason"},
	}
	proc := &stubRunProcessor{results: want}

	var got []Result
	err := ProcessEach(context.Background(), proc, Request{InputPath: "/tmp/x"}, func(r Result) {
		got = append(got, r)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d results, want %d", len(got), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("got[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestProcessEach_ErrorPropagated(t *testing.T) {
	sentinel := errors.New("process failed")
	proc := &stubRunProcessor{
		results: []Result{{Applied: false, Reason: "x"}},
		err:     sentinel,
	}

	var count int
	err := ProcessEach(context.Background(), proc, Request{InputPath: "/tmp/x"}, func(r Result) {
		count++
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want %v", err, sentinel)
	}
	if count != 1 {
		t.Fatalf("onResult called %d times, want 1", count)
	}
}

func TestIsSuppressedResult(t *testing.T) {
	suppressed := []Result{
		{Reason: ErrNotMedia.Error()},
		{Reason: ErrNoMainMediaFound.Error()},
		{Reason: ErrInputMissing.Error()},
	}
	for _, r := range suppressed {
		if !IsSuppressedResult(r) {
			t.Errorf("IsSuppressedResult(%q) = false, want true", r.Reason)
		}
	}

	notSuppressed := []Result{
		{Applied: true},
		{Reason: "parse error: could not determine show name"},
		{Reason: ErrUncategorized.Error()},
		{Reason: ErrAmbiguousShow.Error()},
	}
	for _, r := range notSuppressed {
		if IsSuppressedResult(r) {
			t.Errorf("IsSuppressedResult(%q) = true, want false", r.Reason)
		}
	}
}
