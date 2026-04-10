package processor

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// stubRunProcessor is a minimal Processor for testing ProcessEach.
// If stream is true, it invokes req.OnResult for each result before returning.
// It always returns results and optionally an error.
type stubRunProcessor struct {
	results []Result
	err     error
	stream  bool
}

func (s *stubRunProcessor) Plan(_ context.Context, _ Request) ([]Plan, error) { return nil, nil }
func (s *stubRunProcessor) Apply(_ context.Context, _ []Plan) ([]Result, error) {
	return nil, nil
}
func (s *stubRunProcessor) Process(_ context.Context, req Request) ([]Result, error) {
	if s.stream && req.OnResult != nil {
		for _, r := range s.results {
			req.OnResult(r)
		}
	}
	return s.results, s.err
}

func TestProcessEach_Streaming(t *testing.T) {
	want := []Result{
		{Applied: true, Reason: ""},
		{Applied: false, Reason: "some reason"},
	}
	proc := &stubRunProcessor{results: want, stream: true}

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

func TestProcessEach_StreamingNoBatchDuplicate(t *testing.T) {
	// When Process streams results, the batch slice must not be re-emitted.
	proc := &stubRunProcessor{
		results: []Result{{Applied: true}},
		stream:  true,
	}

	var count int
	_ = ProcessEach(context.Background(), proc, Request{InputPath: "/tmp/x"}, func(r Result) {
		count++
	})
	if count != 1 {
		t.Fatalf("onResult called %d times, want 1", count)
	}
}

func TestProcessEach_BatchFallback(t *testing.T) {
	// When Process does not call OnResult, batch results must flow through onResult.
	want := []Result{
		{Applied: true},
		{Applied: false, Reason: "parse error"},
	}
	proc := &stubRunProcessor{results: want, stream: false}

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
		stream:  false,
	}

	var count int
	err := ProcessEach(context.Background(), proc, Request{InputPath: "/tmp/x"}, func(r Result) {
		count++
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want %v", err, sentinel)
	}
	// Batch fallback still runs even when there's an error.
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
