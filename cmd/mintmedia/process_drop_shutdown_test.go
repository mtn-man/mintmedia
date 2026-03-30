package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Mtn-Man/mintmedia/internal/notify"
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

func TestProcessDropFolder_ForceTimeout_DropsLateOnResultCallbacks(t *testing.T) {
	drop := t.TempDir()
	first := filepath.Join(drop, "first.mkv")
	writeProcessDropFile(t, first)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	soundMu := sync.Mutex{}
	soundCount := 0
	oldPlayDoneSound := playDoneSound
	playDoneSound = func(context.Context, string) error {
		soundMu.Lock()
		soundCount++
		soundMu.Unlock()
		return nil
	}
	defer func() { playDoneSound = oldPlayDoneSound }()

	release := make(chan struct{})
	started := make(chan struct{}, 1)
	workerDone := make(chan struct{}, 1)
	proc := &processDropStubProcessor{
		processFn: func(ctx context.Context, req processor.Request) ([]processor.Result, error) {
			_ = ctx // ignore cancellation on purpose
			select {
			case started <- struct{}{}:
			default:
			}
			<-release
			if req.OnResult != nil {
				req.OnResult(processor.Result{
					Applied: true,
					Plan:    processor.Plan{DestMainPath: "/tmp/late-callback.mkv"},
				})
			}
			workerDone <- struct{}{}
			return []processor.Result{{
				Applied: true,
				Plan:    processor.Plan{DestMainPath: "/tmp/late-callback.mkv"},
			}}, nil
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
		"/tmp/done.aiff",
		notify.DoneNotificationPerFile,
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

	close(release)
	select {
	case <-workerDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for late callback worker completion")
	}

	soundMu.Lock()
	gotSounds := soundCount
	soundMu.Unlock()
	if gotSounds != 0 {
		t.Fatalf("sound count = %d, want 0", gotSounds)
	}
}

func TestProcessDropFolder_CaffeinateStaysActiveDuringShutdownDrain(t *testing.T) {
	drop := t.TempDir()
	first := filepath.Join(drop, "first.mkv")
	writeProcessDropFile(t, first)

	fakeCaff := &fakeProcessDropCaffeinate{
		startCalled: make(chan struct{}),
	}
	oldNewCaffeinate := newProcessDropCaffeinate
	newProcessDropCaffeinate = func() processDropCaffeinateController {
		return fakeCaff
	}
	defer func() { newProcessDropCaffeinate = oldNewCaffeinate }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	block := make(chan struct{})
	started := make(chan struct{}, 1)
	proc := &processDropStubProcessor{
		processFn: func(ctx context.Context, req processor.Request) ([]processor.Result, error) {
			_ = ctx // block independent of cancellation so drain path is exercised.
			_ = req
			select {
			case started <- struct{}{}:
			default:
			}
			<-block
			return []processor.Result{{Applied: true}}, nil
		},
	}

	done := make(chan ProcessDropOutcome, 1)
	go func() {
		done <- processDropFolder(
			ctx,
			proc,
			drop,
			"",
			"off",
			false,
			200*time.Millisecond,
			200*time.Millisecond,
		)
	}()

	select {
	case <-fakeCaff.startCalled:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for caffeinate start")
	}
	select {
	case <-started:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for processing start")
	}

	cancel()
	time.Sleep(30 * time.Millisecond)

	if fakeCaff.startContextCanceled() {
		close(block)
		t.Fatalf("caffeinate context canceled during shutdown drain")
	}

	close(block)

	select {
	case out := <-done:
		if !out.Interrupted {
			t.Fatalf("Interrupted = false, want true")
		}
		if out.TimedOut {
			t.Fatalf("TimedOut = true, want false")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for processDropFolder completion")
	}

	if got := fakeCaff.stopCallsCount(); got != 1 {
		t.Fatalf("Stop calls = %d, want 1", got)
	}
}

func TestProcessDropFolder_NoCandidates_PrintsNoFilesWithoutConfigPath(t *testing.T) {
	drop := t.TempDir()

	out := captureStdout(t, func() {
		result := processDropFolder(
			context.Background(),
			&processDropStubProcessor{},
			drop,
			"",
			"off",
			false,
			10*time.Second,
			10*time.Second,
		)
		if result.ErrorCount != 0 {
			t.Fatalf("ErrorCount = %d, want 0", result.ErrorCount)
		}
		if result.Interrupted {
			t.Fatalf("Interrupted = true, want false")
		}
		if result.TimedOut {
			t.Fatalf("TimedOut = true, want false")
		}
	})

	if !strings.Contains(out, "INFO     No files detected.") {
		t.Fatalf("expected no-files message, got: %q", out)
	}
	if strings.Contains(out, "Config file:") {
		t.Fatalf("unexpected config path output, got: %q", out)
	}
}

func TestProcessDropFolder_StreamedPerFile_NoDuplicateLinesOrSounds(t *testing.T) {
	drop := t.TempDir()
	input := filepath.Join(drop, "pack")
	if err := os.MkdirAll(input, 0o755); err != nil {
		t.Fatalf("mkdir pack: %v", err)
	}

	soundMu := sync.Mutex{}
	soundCount := 0
	oldPlayDoneSound := playDoneSound
	playDoneSound = func(context.Context, string) error {
		soundMu.Lock()
		soundCount++
		soundMu.Unlock()
		return nil
	}
	defer func() { playDoneSound = oldPlayDoneSound }()

	proc := &processDropStubProcessor{
		processFn: func(ctx context.Context, req processor.Request) ([]processor.Result, error) {
			_ = ctx
			out := []processor.Result{
				{Applied: true, Plan: processor.Plan{DestMainPath: "/tmp/A.mkv"}},
				{Applied: true, Plan: processor.Plan{DestMainPath: "/tmp/B.mkv"}},
			}
			if req.OnResult != nil {
				for _, r := range out {
					req.OnResult(r)
				}
			}
			return out, nil
		},
	}

	out := captureStdout(t, func() {
		result := processDropFolder(
			context.Background(),
			proc,
			drop,
			"/tmp/done.aiff",
			notify.DoneNotificationPerFile,
			false,
			10*time.Second,
			10*time.Second,
		)
		if result.ErrorCount != 0 {
			t.Fatalf("ErrorCount = %d, want 0", result.ErrorCount)
		}
	})

	if got := strings.Count(out, "MOVED    "); got != 2 {
		t.Fatalf("expected 2 compact moved lines, got %d (output=%q)", got, out)
	}

	soundMu.Lock()
	gotSounds := soundCount
	soundMu.Unlock()
	if gotSounds != 2 {
		t.Fatalf("sound count = %d, want 2", gotSounds)
	}
}

func TestProcessDropFolder_StreamedPerJob_PlaysOneSoundForWholeJob(t *testing.T) {
	drop := t.TempDir()
	input := filepath.Join(drop, "pack")
	if err := os.MkdirAll(input, 0o755); err != nil {
		t.Fatalf("mkdir pack: %v", err)
	}

	soundMu := sync.Mutex{}
	soundCount := 0
	oldPlayDoneSound := playDoneSound
	playDoneSound = func(context.Context, string) error {
		soundMu.Lock()
		soundCount++
		soundMu.Unlock()
		return nil
	}
	defer func() { playDoneSound = oldPlayDoneSound }()

	proc := &processDropStubProcessor{
		processFn: func(ctx context.Context, req processor.Request) ([]processor.Result, error) {
			_ = ctx
			out := []processor.Result{
				{Applied: true, Plan: processor.Plan{DestMainPath: "/tmp/A.mkv"}},
				{Applied: true, Plan: processor.Plan{DestMainPath: "/tmp/B.mkv"}},
			}
			if req.OnResult != nil {
				for _, r := range out {
					req.OnResult(r)
				}
			}
			return out, nil
		},
	}

	result := processDropFolder(
		context.Background(),
		proc,
		drop,
		"/tmp/done.aiff",
		notify.DoneNotificationPerJob,
		false,
		10*time.Second,
		10*time.Second,
	)
	if result.ErrorCount != 0 {
		t.Fatalf("ErrorCount = %d, want 0", result.ErrorCount)
	}

	soundMu.Lock()
	gotSounds := soundCount
	soundMu.Unlock()
	if gotSounds != 1 {
		t.Fatalf("sound count = %d, want 1", gotSounds)
	}
}

func TestProcessDropFolder_NonStreamedPerFile_PlaysPerAppliedMain(t *testing.T) {
	drop := t.TempDir()
	input := filepath.Join(drop, "pack")
	if err := os.MkdirAll(input, 0o755); err != nil {
		t.Fatalf("mkdir pack: %v", err)
	}

	soundMu := sync.Mutex{}
	soundCount := 0
	oldPlayDoneSound := playDoneSound
	playDoneSound = func(context.Context, string) error {
		soundMu.Lock()
		soundCount++
		soundMu.Unlock()
		return nil
	}
	defer func() { playDoneSound = oldPlayDoneSound }()

	proc := &processDropStubProcessor{
		processFn: func(ctx context.Context, req processor.Request) ([]processor.Result, error) {
			_ = ctx
			_ = req
			return []processor.Result{
				{Applied: true, Plan: processor.Plan{DestMainPath: "/tmp/A.mkv"}},
				{Applied: true, Plan: processor.Plan{DestMainPath: "/tmp/B.mkv"}},
			}, nil
		},
	}

	result := processDropFolder(
		context.Background(),
		proc,
		drop,
		"/tmp/done.aiff",
		notify.DoneNotificationPerFile,
		false,
		10*time.Second,
		10*time.Second,
	)
	if result.ErrorCount != 0 {
		t.Fatalf("ErrorCount = %d, want 0", result.ErrorCount)
	}

	soundMu.Lock()
	gotSounds := soundCount
	soundMu.Unlock()
	if gotSounds != 2 {
		t.Fatalf("sound count = %d, want 2", gotSounds)
	}
}

type processDropStubProcessor struct {
	mu        sync.Mutex
	calls     []string
	processFn func(context.Context, processor.Request) ([]processor.Result, error)
}

type fakeProcessDropCaffeinate struct {
	mu          sync.Mutex
	startCtx    context.Context
	stopCalls   int
	startCalled chan struct{}
}

func (f *fakeProcessDropCaffeinate) Start(ctx context.Context) error {
	f.mu.Lock()
	f.startCtx = ctx
	startCalled := f.startCalled
	f.mu.Unlock()
	close(startCalled)
	return nil
}

func (f *fakeProcessDropCaffeinate) Stop() error {
	f.mu.Lock()
	f.stopCalls++
	f.mu.Unlock()
	return nil
}

func (f *fakeProcessDropCaffeinate) startContextCanceled() bool {
	f.mu.Lock()
	ctx := f.startCtx
	f.mu.Unlock()
	if ctx == nil {
		return false
	}
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

func (f *fakeProcessDropCaffeinate) stopCallsCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stopCalls
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

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	fn()

	_ = w.Close()
	os.Stdout = old
	out := <-done
	_ = r.Close()
	return out
}
