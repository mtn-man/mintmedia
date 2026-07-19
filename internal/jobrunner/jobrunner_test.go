package jobrunner_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mtn-man/mintmedia/internal/jobrunner"
	"github.com/mtn-man/mintmedia/internal/processor"
	"github.com/mtn-man/mintmedia/internal/shutdown"
)

type fakeProcessor struct {
	processFn func(ctx context.Context, req processor.Request) error
}

func (f *fakeProcessor) Plan(context.Context, processor.Request) ([]processor.Plan, error) {
	return nil, nil
}

func (f *fakeProcessor) Apply(context.Context, []processor.Plan) ([]processor.Result, error) {
	return nil, nil
}

func (f *fakeProcessor) Process(ctx context.Context, req processor.Request) error {
	return f.processFn(ctx, req)
}

func (f *fakeProcessor) SortCandidates(_ context.Context, paths []string) ([]string, []processor.SortError, error) {
	return paths, nil, nil
}

func (f *fakeProcessor) CountMainMedia(context.Context, string) (int, error) {
	return 0, nil
}

type resultCollector struct {
	mu      sync.Mutex
	results []processor.Result
}

func (c *resultCollector) onResult(r processor.Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.results = append(c.results, r)
}

func (c *resultCollector) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.results)
}

func (c *resultCollector) snapshot() []processor.Result {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]processor.Result, len(c.results))
	copy(out, c.results)
	return out
}

func TestRun_CompletesNormally_NoShutdown(t *testing.T) {
	proc := &fakeProcessor{
		processFn: func(ctx context.Context, req processor.Request) error {
			req.OnResult(processor.Result{Applied: true, Reason: "one"})
			return nil
		},
	}

	var collector resultCollector
	drain, err := jobrunner.Run(context.Background(), shutdown.Policy{}, shutdown.Hooks{}, proc, "/tmp/x.mkv", collector.onResult)

	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if drain != (shutdown.Result{}) {
		t.Fatalf("drain = %+v, want zero value", drain)
	}
	results := collector.snapshot()
	if len(results) != 1 || results[0].Reason != "one" {
		t.Fatalf("results = %+v, want one result with Reason=one", results)
	}
}

func TestRun_StreamsMultipleResultsInOrder(t *testing.T) {
	proc := &fakeProcessor{
		processFn: func(ctx context.Context, req processor.Request) error {
			for i := range 3 {
				req.OnResult(processor.Result{Reason: string(rune('a' + i))})
			}
			return nil
		},
	}

	var collector resultCollector
	drain, err := jobrunner.Run(context.Background(), shutdown.Policy{}, shutdown.Hooks{}, proc, "/tmp/x.mkv", collector.onResult)

	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if drain.TimedOut || drain.GraceElapsed {
		t.Fatalf("drain = %+v, want zero value", drain)
	}
	results := collector.snapshot()
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	want := "abc"
	for i, r := range results {
		if r.Reason != string(want[i]) {
			t.Fatalf("results[%d].Reason = %q, want %q (order broken)", i, r.Reason, string(want[i]))
		}
	}
}

func TestRun_ForwardsUnderlyingProcessError(t *testing.T) {
	sentinel := errors.New("boom")
	proc := &fakeProcessor{
		processFn: func(ctx context.Context, req processor.Request) error {
			return sentinel
		},
	}

	var collector resultCollector
	drain, err := jobrunner.Run(context.Background(), shutdown.Policy{}, shutdown.Hooks{}, proc, "/tmp/x.mkv", collector.onResult)

	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want %v", err, sentinel)
	}
	if drain != (shutdown.Result{}) {
		t.Fatalf("drain = %+v, want zero value", drain)
	}
	if collector.count() != 0 {
		t.Fatalf("got %d results, want 0", collector.count())
	}
}

func TestRun_GraceWindow_JobFinishesDuringGrace(t *testing.T) {
	release := make(chan struct{})
	proc := &fakeProcessor{
		processFn: func(ctx context.Context, req processor.Request) error {
			<-release // blocks on something unrelated to ctx; released within grace
			req.OnResult(processor.Result{Applied: true})
			return nil
		},
	}

	shutdownCtx, cancel := context.WithCancel(context.Background())

	var onWaitStart, onGraceElapsed int32
	hooks := shutdown.Hooks{
		OnWaitStart:    func(time.Duration) { onWaitStart++ },
		OnGraceElapsed: func(time.Duration) { onGraceElapsed++ },
	}

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
		time.Sleep(20 * time.Millisecond)
		close(release)
	}()

	var collector resultCollector
	policy := shutdown.Policy{Grace: 2 * time.Second, Force: 2 * time.Second}
	drain, err := jobrunner.Run(shutdownCtx, policy, hooks, proc, "/tmp/x.mkv", collector.onResult)

	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if drain.GraceElapsed {
		t.Fatalf("drain.GraceElapsed = true, want false")
	}
	if drain.TimedOut {
		t.Fatalf("drain.TimedOut = true, want false")
	}
	if onWaitStart != 1 {
		t.Fatalf("OnWaitStart calls = %d, want 1", onWaitStart)
	}
	if onGraceElapsed != 0 {
		t.Fatalf("OnGraceElapsed calls = %d, want 0", onGraceElapsed)
	}
	if collector.count() != 1 {
		t.Fatalf("got %d results, want 1", collector.count())
	}
}

func TestRun_ForceCancel_JobRespectsItemCtxCancellation(t *testing.T) {
	proc := &fakeProcessor{
		processFn: func(ctx context.Context, req processor.Request) error {
			<-ctx.Done() // only stops once its own (force-canceled) context is done
			req.OnResult(processor.Result{Applied: true})
			return ctx.Err()
		},
	}

	shutdownCtx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	var onWaitStart, onGraceElapsed int32
	hooks := shutdown.Hooks{
		OnWaitStart:    func(time.Duration) { onWaitStart++ },
		OnGraceElapsed: func(time.Duration) { onGraceElapsed++ },
	}

	var collector resultCollector
	policy := shutdown.Policy{Grace: 40 * time.Millisecond, Force: 2 * time.Second}
	drain, runErr := jobrunner.Run(shutdownCtx, policy, hooks, proc, "/tmp/x.mkv", collector.onResult)

	if errors.Is(runErr, jobrunner.ErrAbandoned) {
		t.Fatalf("err = %v, want a job-returned error, not ErrAbandoned (job completed within the force window)", runErr)
	}
	if onWaitStart != 1 {
		t.Fatalf("OnWaitStart calls = %d, want 1", onWaitStart)
	}
	if onGraceElapsed != 1 {
		t.Fatalf("OnGraceElapsed calls = %d, want 1", onGraceElapsed)
	}
	if !drain.GraceElapsed {
		t.Fatalf("drain.GraceElapsed = false, want true")
	}
	if drain.TimedOut {
		t.Fatalf("drain.TimedOut = true, want false")
	}
	if collector.count() != 1 {
		t.Fatalf("got %d results, want 1", collector.count())
	}
}

func TestRun_ForceTimeout_JobIgnoresCancellationEntirely(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	proc := &fakeProcessor{
		processFn: func(ctx context.Context, req processor.Request) error {
			select {
			case started <- struct{}{}:
			default:
			}
			<-release // ignores ctx entirely
			return nil
		},
	}

	shutdownCtx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()

	var collector resultCollector
	policy := shutdown.Policy{Grace: 20 * time.Millisecond, Force: 20 * time.Millisecond}

	runDone := make(chan struct{})
	var runErr error
	var drain shutdown.Result
	go func() {
		drain, runErr = jobrunner.Run(shutdownCtx, policy, shutdown.Hooks{}, proc, "/tmp/x.mkv", collector.onResult)
		close(runDone)
	}()

	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatalf("Run did not return within bounded grace+force window")
	}

	if !errors.Is(runErr, jobrunner.ErrAbandoned) {
		t.Fatalf("err = %v, want errors.Is(err, jobrunner.ErrAbandoned)", runErr)
	}
	if !drain.TimedOut {
		t.Fatalf("drain.TimedOut = false, want true")
	}

	close(release)
}

func TestRun_ForceTimeout_DropsLateOnResultCallback(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	workerDone := make(chan struct{})
	proc := &fakeProcessor{
		processFn: func(ctx context.Context, req processor.Request) error {
			select {
			case started <- struct{}{}:
			default:
			}
			<-release // ignores ctx entirely
			req.OnResult(processor.Result{Applied: true})
			close(workerDone)
			return nil
		},
	}

	shutdownCtx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()

	var collector resultCollector
	policy := shutdown.Policy{Grace: 20 * time.Millisecond, Force: 20 * time.Millisecond}

	drain, runErr := jobrunner.Run(shutdownCtx, policy, shutdown.Hooks{}, proc, "/tmp/x.mkv", collector.onResult)
	if !errors.Is(runErr, jobrunner.ErrAbandoned) {
		t.Fatalf("err = %v, want errors.Is(err, jobrunner.ErrAbandoned)", runErr)
	}
	if !drain.TimedOut {
		t.Fatalf("drain.TimedOut = false, want true")
	}

	close(release)
	select {
	case <-workerDone:
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for abandoned goroutine to finish")
	}

	// Give the abandoned goroutine's late OnResult call a moment to (not) arrive.
	time.Sleep(50 * time.Millisecond)
	if got := collector.count(); got != 0 {
		t.Fatalf("collector.count() = %d, want 0 (late callback should be dropped)", got)
	}
}

func TestRun_HooksInvokedWithCorrectDurations(t *testing.T) {
	release := make(chan struct{})
	proc := &fakeProcessor{
		processFn: func(ctx context.Context, req processor.Request) error {
			<-ctx.Done()
			return nil
		},
	}

	shutdownCtx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	grace := 30 * time.Millisecond
	force := 40 * time.Millisecond
	var gotGrace, gotForce time.Duration
	hooks := shutdown.Hooks{
		OnWaitStart:    func(g time.Duration) { gotGrace = g },
		OnGraceElapsed: func(f time.Duration) { gotForce = f },
	}

	var collector resultCollector
	policy := shutdown.Policy{Grace: grace, Force: force}
	_, _ = jobrunner.Run(shutdownCtx, policy, hooks, proc, "/tmp/x.mkv", collector.onResult)
	close(release)

	if gotGrace != grace {
		t.Fatalf("OnWaitStart grace = %v, want %v", gotGrace, grace)
	}
	if gotForce != force {
		t.Fatalf("OnGraceElapsed force = %v, want %v", gotForce, force)
	}
}

func TestRun_NilHooksDoNotPanic(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	proc := &fakeProcessor{
		processFn: func(ctx context.Context, req processor.Request) error {
			select {
			case started <- struct{}{}:
			default:
			}
			<-release
			return nil
		},
	}

	shutdownCtx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()

	policy := shutdown.Policy{Grace: 15 * time.Millisecond, Force: 15 * time.Millisecond}

	defer close(release)

	var collector resultCollector
	drain, runErr := jobrunner.Run(shutdownCtx, policy, shutdown.Hooks{}, proc, "/tmp/x.mkv", collector.onResult)
	if !errors.Is(runErr, jobrunner.ErrAbandoned) {
		t.Fatalf("err = %v, want errors.Is(err, jobrunner.ErrAbandoned)", runErr)
	}
	if !drain.TimedOut {
		t.Fatalf("drain.TimedOut = false, want true")
	}
}
