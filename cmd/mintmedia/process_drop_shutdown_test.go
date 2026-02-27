package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Mtn-Man/mintmedia/internal/processor"
)

func TestProcessDropFolder_InterruptStopsAfterCurrentItem(t *testing.T) {
	drop := t.TempDir()
	first := filepath.Join(drop, "first.mkv")
	writeProcessDropFile(t, first)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	block := make(chan struct{})
	started := make(chan struct{}, 1)
	proc := &processDropStubProcessor{
		processFn: func(ctx context.Context, req processor.Request) ([]processor.Result, error) {
			if filepath.Base(req.InputPath) == "first.mkv" {
				select {
				case started <- struct{}{}:
				default:
				}
				<-block
			}
			return []processor.Result{{Applied: true}}, nil
		},
	}

	go func() {
		<-started
		cancel()
		time.Sleep(20 * time.Millisecond)
		close(block)
	}()

	out := processDropFolder(
		ctx,
		proc,
		drop,
		"",
		"off",
		false,
		200*time.Millisecond,
		200*time.Millisecond,
	)

	if !out.Interrupted {
		t.Fatalf("Interrupted = false, want true")
	}
	if out.TimedOut {
		t.Fatalf("TimedOut = true, want false")
	}
	if out.ErrorCount != 0 {
		t.Fatalf("ErrorCount = %d, want 0", out.ErrorCount)
	}

	calls := proc.Calls()
	if len(calls) != 1 {
		t.Fatalf("processed %d item(s), want 1", len(calls))
	}
	if calls[0] != first {
		t.Fatalf("processed path = %q, want %q", calls[0], first)
	}
}

func TestProcessDropFolder_ForceTimeoutWhenItemIgnoresCancel(t *testing.T) {
	drop := t.TempDir()
	first := filepath.Join(drop, "first.mkv")
	writeProcessDropFile(t, first)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	block := make(chan struct{})
	started := make(chan struct{}, 1)
	proc := &processDropStubProcessor{
		processFn: func(ctx context.Context, req processor.Request) ([]processor.Result, error) {
			select {
			case started <- struct{}{}:
			default:
			}
			<-block // ignores ctx cancellation on purpose
			return []processor.Result{{Applied: true}}, nil
		},
	}

	go func() {
		<-started
		cancel()
	}()

	out := processDropFolder(
		ctx,
		proc,
		drop,
		"",
		"off",
		false,
		40*time.Millisecond,
		50*time.Millisecond,
	)

	if !out.Interrupted {
		t.Fatalf("Interrupted = false, want true")
	}
	if !out.TimedOut {
		t.Fatalf("TimedOut = false, want true")
	}
	if out.ErrorCount != 1 {
		t.Fatalf("ErrorCount = %d, want 1", out.ErrorCount)
	}

	close(block)
}

type processDropStubProcessor struct {
	mu        sync.Mutex
	calls     []string
	processFn func(context.Context, processor.Request) ([]processor.Result, error)
}

func (s *processDropStubProcessor) Plan(context.Context, processor.Request) ([]processor.Plan, error) {
	return nil, nil
}

func (s *processDropStubProcessor) Apply(context.Context, []processor.Plan) ([]processor.Result, error) {
	return nil, nil
}

func (s *processDropStubProcessor) Process(ctx context.Context, req processor.Request) ([]processor.Result, error) {
	s.mu.Lock()
	s.calls = append(s.calls, req.InputPath)
	s.mu.Unlock()

	if s.processFn != nil {
		return s.processFn(ctx, req)
	}
	return []processor.Result{{Applied: true}}, nil
}

func (s *processDropStubProcessor) Calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.calls))
	copy(out, s.calls)
	return out
}

func writeProcessDropFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}
