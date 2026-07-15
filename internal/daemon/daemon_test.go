package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mtn-man/mintmedia/internal/logging"
	"github.com/mtn-man/mintmedia/internal/notify"
	"github.com/mtn-man/mintmedia/internal/processor"
	"github.com/mtn-man/mintmedia/internal/shutdown"
	"github.com/mtn-man/mintmedia/internal/transmission"
	"github.com/mtn-man/mintmedia/internal/watch"
)

func TestDaemon_RunProcessesDropEvents(t *testing.T) {
	root := t.TempDir()
	drop := filepath.Join(root, "drop")
	movies := filepath.Join(root, "Movies")
	shows := filepath.Join(root, "Shows")

	mkdirAll(t, drop)
	mkdirAll(t, movies)
	mkdirAll(t, shows)

	w, err := watch.NewDropFolderWatcher(drop, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("NewDropFolderWatcher error: %v", err)
	}

	proc := &stubProcessor{started: make(chan string, 1)}

	d := &Daemon{
		Watcher: w,
		Proc:    proc,

		MoviesDir: movies,
		ShowsDir:  shows,

		SoundInput: "",
		SoundDone:  "",

		AutoCleanupCompletedTorrents: false,
		DeferDestinationChecks:       false,
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := w.Start(ctx); err != nil {
		cancel()
		t.Fatalf("Start watcher error: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	target := filepath.Join(drop, "test.mkv")
	writeFile(t, target, "data")

	got := waitForPath(t, proc.started, 3*time.Second)
	if got != target {
		cancel()
		t.Fatalf("Process called with %q, want %q", got, target)
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("Run did not exit after cancel")
	}
}

func TestDaemon_RunWaitsForInFlightJobsOnShutdown(t *testing.T) {
	root := t.TempDir()
	drop := filepath.Join(root, "drop")
	movies := filepath.Join(root, "Movies")
	shows := filepath.Join(root, "Shows")

	mkdirAll(t, drop)
	mkdirAll(t, movies)
	mkdirAll(t, shows)

	w, err := watch.NewDropFolderWatcher(drop, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("NewDropFolderWatcher error: %v", err)
	}

	block := make(chan struct{})
	proc := &stubProcessor{
		started: make(chan string, 1),
		block:   block,
		blocked: make(chan struct{}, 1),
	}

	d := &Daemon{
		Watcher: w,
		Proc:    proc,

		MoviesDir: movies,
		ShowsDir:  shows,

		SoundInput: "",
		SoundDone:  "",

		AutoCleanupCompletedTorrents: false,
		DeferDestinationChecks:       false,
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := w.Start(ctx); err != nil {
		cancel()
		t.Fatalf("Start watcher error: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	target := filepath.Join(drop, "block.mkv")
	writeFile(t, target, "data")

	_ = waitForPath(t, proc.started, 3*time.Second)
	waitForSignal(t, proc.blocked, 3*time.Second)

	cancel()

	select {
	case err := <-done:
		close(block)
		t.Fatalf("Run exited early with err=%v", err)
	case <-time.After(1 * time.Second):
	}

	close(block)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("Run did not exit after unblocking")
	}
}

func TestDaemon_RunCaffeinateStaysActiveDuringShutdownDrain(t *testing.T) {
	root := t.TempDir()
	drop := filepath.Join(root, "drop")
	movies := filepath.Join(root, "Movies")
	shows := filepath.Join(root, "Shows")

	mkdirAll(t, drop)
	mkdirAll(t, movies)
	mkdirAll(t, shows)

	w, err := watch.NewDropFolderWatcher(drop, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("NewDropFolderWatcher error: %v", err)
	}

	block := make(chan struct{})
	proc := &stubProcessor{
		started: make(chan string, 1),
		block:   block,
		blocked: make(chan struct{}, 1),
	}

	fakeCaff := &fakeDaemonCaffeinate{
		startCalled: make(chan struct{}),
	}
	oldNewDaemonCaffeinate := newDaemonCaffeinate
	newDaemonCaffeinate = func() notify.CaffeinateController {
		return fakeCaff
	}
	defer func() { newDaemonCaffeinate = oldNewDaemonCaffeinate }()

	d := &Daemon{
		Watcher: w,
		Proc:    proc,

		MoviesDir: movies,
		ShowsDir:  shows,

		SoundInput: "",
		SoundDone:  "",

		AutoCleanupCompletedTorrents: false,
		DeferDestinationChecks:       false,
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := w.Start(ctx); err != nil {
		cancel()
		t.Fatalf("Start watcher error: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	select {
	case <-fakeCaff.startCalled:
	case <-time.After(500 * time.Millisecond):
		cancel()
		close(block)
		t.Fatalf("timeout waiting for caffeinate start")
	}

	target := filepath.Join(drop, "block.mkv")
	writeFile(t, target, "data")

	_ = waitForPath(t, proc.started, 3*time.Second)
	waitForSignal(t, proc.blocked, 3*time.Second)

	cancel()
	time.Sleep(30 * time.Millisecond)

	if fakeCaff.startContextCanceled() {
		close(block)
		t.Fatalf("caffeinate context canceled during shutdown drain")
	}

	close(block)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("Run did not exit after unblocking")
	}

	if got := fakeCaff.stopCallsCount(); got != 1 {
		t.Fatalf("Stop calls = %d, want 1", got)
	}
}

func TestDaemon_RunForceCancelsInFlightAfterGrace(t *testing.T) {
	root := t.TempDir()
	drop := filepath.Join(root, "drop")
	movies := filepath.Join(root, "Movies")
	shows := filepath.Join(root, "Shows")

	mkdirAll(t, drop)
	mkdirAll(t, movies)
	mkdirAll(t, shows)

	w, err := watch.NewDropFolderWatcher(drop, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("NewDropFolderWatcher error: %v", err)
	}

	proc := &stubProcessor{
		started:       make(chan string, 1),
		blocked:       make(chan struct{}, 1),
		blockUntilCtx: true,
	}

	d := &Daemon{
		Watcher: w,
		Proc:    proc,

		MoviesDir: movies,
		ShowsDir:  shows,

		SoundInput: "",
		SoundDone:  "",

		ShutdownGraceDuration: 60 * time.Millisecond,
		ShutdownForceTimeout:  250 * time.Millisecond,

		AutoCleanupCompletedTorrents: false,
		DeferDestinationChecks:       false,
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := w.Start(ctx); err != nil {
		cancel()
		t.Fatalf("Start watcher error: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	target := filepath.Join(drop, "block-on-proc-ctx.mkv")
	writeFile(t, target, "data")

	_ = waitForPath(t, proc.started, 3*time.Second)
	waitForSignal(t, proc.blocked, 3*time.Second)

	cancelAt := time.Now()
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
		if waited := time.Since(cancelAt); waited < 50*time.Millisecond {
			t.Fatalf("shutdown completed too quickly; waited=%s grace=%s", waited, d.ShutdownGraceDuration)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("Run did not exit after force-cancel path")
	}
}

func TestDaemon_RunReturnsShutdownTimeoutWhenJobsIgnoreCancel(t *testing.T) {
	root := t.TempDir()
	drop := filepath.Join(root, "drop")
	movies := filepath.Join(root, "Movies")
	shows := filepath.Join(root, "Shows")

	mkdirAll(t, drop)
	mkdirAll(t, movies)
	mkdirAll(t, shows)

	w, err := watch.NewDropFolderWatcher(drop, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("NewDropFolderWatcher error: %v", err)
	}

	block := make(chan struct{})
	proc := &stubProcessor{
		started: make(chan string, 1),
		blocked: make(chan struct{}, 1),
		block:   block, // ignores context cancellation
	}

	d := &Daemon{
		Watcher: w,
		Proc:    proc,

		MoviesDir: movies,
		ShowsDir:  shows,

		SoundInput: "",
		SoundDone:  "",

		ShutdownGraceDuration: 50 * time.Millisecond,
		ShutdownForceTimeout:  60 * time.Millisecond,

		AutoCleanupCompletedTorrents: false,
		DeferDestinationChecks:       false,
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := w.Start(ctx); err != nil {
		cancel()
		t.Fatalf("Start watcher error: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	target := filepath.Join(drop, "ignore-cancel.mkv")
	writeFile(t, target, "data")

	_ = waitForPath(t, proc.started, 3*time.Second)
	waitForSignal(t, proc.blocked, 3*time.Second)

	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, ErrShutdownTimedOut) {
			t.Fatalf("Run error = %v, want ErrShutdownTimedOut", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("Run did not return timeout error")
	}

	// Allow blocked processing goroutine to unwind after timeout return.
	close(block)
}

func TestDaemon_RunSkipsWaitingLogWhenNoInFlightJobs(t *testing.T) {
	root := t.TempDir()
	drop := filepath.Join(root, "drop")
	movies := filepath.Join(root, "Movies")
	shows := filepath.Join(root, "Shows")

	mkdirAll(t, drop)
	mkdirAll(t, movies)
	mkdirAll(t, shows)

	w, err := watch.NewDropFolderWatcher(drop, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("NewDropFolderWatcher error: %v", err)
	}

	d := &Daemon{
		Watcher: w,
		Proc:    &stubProcessor{},

		MoviesDir: movies,
		ShowsDir:  shows,

		SoundInput: "",
		SoundDone:  "",

		ShutdownGraceDuration: 10 * time.Minute,
		ShutdownForceTimeout:  15 * time.Second,

		AutoCleanupCompletedTorrents: false,
		DeferDestinationChecks:       false,
	}

	var stderr strings.Builder
	d.Logger = newRuntimeLoggerForTest(t, io.Discard, &stderr)

	ctx, cancel := context.WithCancel(context.Background())
	if err := w.Start(ctx); err != nil {
		cancel()
		t.Fatalf("Start watcher error: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	time.Sleep(150 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("Run did not exit after cancel")
	}

	if strings.Contains(stderr.String(), "Shutdown requested; waiting up to") {
		t.Fatalf("unexpected waiting log with no in-flight jobs: %q", stderr.String())
	}
}

func TestDaemon_RunWaitingLogStartsOnNewLineForInFlightJobs(t *testing.T) {
	root := t.TempDir()
	drop := filepath.Join(root, "drop")
	movies := filepath.Join(root, "Movies")
	shows := filepath.Join(root, "Shows")

	mkdirAll(t, drop)
	mkdirAll(t, movies)
	mkdirAll(t, shows)

	w, err := watch.NewDropFolderWatcher(drop, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("NewDropFolderWatcher error: %v", err)
	}

	proc := &stubProcessor{
		started:       make(chan string, 1),
		blocked:       make(chan struct{}, 1),
		blockUntilCtx: true,
	}

	d := &Daemon{
		Watcher: w,
		Proc:    proc,

		MoviesDir: movies,
		ShowsDir:  shows,

		SoundInput: "",
		SoundDone:  "",

		ShutdownGraceDuration: 50 * time.Millisecond,
		ShutdownForceTimeout:  250 * time.Millisecond,

		AutoCleanupCompletedTorrents: false,
		DeferDestinationChecks:       false,
	}

	var stderr strings.Builder
	d.Logger = newRuntimeLoggerForTest(t, io.Discard, &stderr)

	ctx, cancel := context.WithCancel(context.Background())
	if err := w.Start(ctx); err != nil {
		cancel()
		t.Fatalf("Start watcher error: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	target := filepath.Join(drop, "inflight.mkv")
	writeFile(t, target, "data")

	_ = waitForPath(t, proc.started, 3*time.Second)
	waitForSignal(t, proc.blocked, 3*time.Second)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("Run did not exit after cancel")
	}

	if !strings.Contains(stderr.String(), "\nWARNING  shutdown requested. Waiting up to 50ms for in-flight jobs.\n") {
		t.Fatalf("expected newline-prefixed waiting log, got: %q", stderr.String())
	}
}

func TestDaemon_DeferDestinationChecks(t *testing.T) {
	root := t.TempDir()
	drop := filepath.Join(root, "drop")
	movies := filepath.Join(root, "Movies")
	shows := filepath.Join(root, "Shows")

	mkdirAll(t, drop)

	w, err := watch.NewDropFolderWatcher(drop, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("NewDropFolderWatcher error: %v", err)
	}

	proc := &stubProcessor{started: make(chan string, 1)}

	d := &Daemon{
		Watcher: w,
		Proc:    proc,

		MoviesDir: movies,
		ShowsDir:  shows,

		SoundInput: "",
		SoundDone:  "",

		AutoCleanupCompletedTorrents: false,
		DeferDestinationChecks:       true,
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := w.Start(ctx); err != nil {
		cancel()
		t.Fatalf("Start watcher error: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	target := filepath.Join(drop, "pending.mkv")
	writeFile(t, target, "data")

	expectNoPath(t, proc.started, 700*time.Millisecond)

	mkdirAll(t, movies)
	mkdirAll(t, shows)

	got := waitForPath(t, proc.started, 7*time.Second)
	if got != target {
		cancel()
		t.Fatalf("Process called with %q, want %q", got, target)
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("Run did not exit after cancel")
	}
}

func TestDaemon_ProcessPathAsync_CleansCompletedWhenEnabled(t *testing.T) {
	var mu sync.Mutex
	var removedIDs []int

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Transmission-Session-Id") == "" {
			w.Header().Set("X-Transmission-Session-Id", "test-session-id")
			w.WriteHeader(http.StatusConflict)
			return
		}
		var req struct {
			Method    string          `json:"method"`
			Arguments json.RawMessage `json:"arguments"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "torrent-get":
			_, _ = fmt.Fprintf(w, `{"result":"success","arguments":{"torrents":[{"id":7,"percentDone":1.0}]}}`)
		case "torrent-remove":
			var a struct {
				IDs []int `json:"ids"`
			}
			_ = json.Unmarshal(req.Arguments, &a)
			mu.Lock()
			removedIDs = append(removedIDs, a.IDs...)
			mu.Unlock()
			_, _ = fmt.Fprintf(w, `{"result":"success","arguments":{}}`)
		default:
			http.Error(w, "unexpected method", http.StatusBadRequest)
		}
	}))
	defer ts.Close()

	host := strings.TrimPrefix(ts.URL, "http://")
	d := &Daemon{
		Proc:                         &stubProcessor{},
		Tx:                           &transmission.Client{Host: host},
		AutoCleanupCompletedTorrents: true,
		CleanupCooldown:              time.Millisecond,
		SoundDone:                    "",
	}

	d.processPath(context.Background(), shutdown.ResolvePolicy(0, 0), shutdown.Hooks{}, "/tmp/input.mkv", "/tmp/input.mkv")

	mu.Lock()
	ids := removedIDs
	mu.Unlock()

	if len(ids) != 1 || ids[0] != 7 {
		t.Fatalf("removedIDs = %v, want [7]", ids)
	}
}

func TestDaemon_ProcessPathAsync_DoneNotificationModes(t *testing.T) {
	tests := []struct {
		name    string
		mode    string
		results []processor.Result
		want    int
	}{
		{
			name: "PerFile_PlaysPerAppliedMain",
			mode: notify.DoneNotificationPerFile,
			results: []processor.Result{
				{Applied: true},
				{Applied: true},
				{Applied: true},
				{Applied: false, Reason: processor.ErrNotMedia.Error()},
			},
			want: 3,
		},
		{
			name: "PerJob_PlaysOnceWhenAnyApplied",
			mode: notify.DoneNotificationPerJob,
			results: []processor.Result{
				{Applied: true},
				{Applied: true},
				{Applied: true},
			},
			want: 1,
		},
		{
			name: "Off_PlaysNone",
			mode: notify.DoneNotificationOff,
			results: []processor.Result{
				{Applied: true},
				{Applied: true},
			},
			want: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			soundCalls := make(chan struct{}, 16)

			d := &Daemon{
				Proc:                         &stubProcessor{results: tc.results},
				SoundDone:                    "/tmp/done.aiff",
				DoneNotificationMode:         tc.mode,
				playSoundFn:                  func(context.Context, string) error { soundCalls <- struct{}{}; return nil },
				CleanupCooldown:              time.Millisecond,
				AutoCleanupCompletedTorrents: false,
			}

			d.processPath(context.Background(), shutdown.ResolvePolicy(0, 0), shutdown.Hooks{}, "/tmp/input.mkv", "/tmp/input.mkv")

			waitForSoundCount(t, soundCalls, tc.want, 2*time.Second)
			assertNoExtraSoundCalls(t, soundCalls, 150*time.Millisecond)
		})
	}
}

func TestDaemon_ProcessPathAsync_DoneNotificationModes_StreamedResults(t *testing.T) {
	tests := []struct {
		name    string
		mode    string
		results []processor.Result
		want    int
	}{
		{
			name: "PerFile_PlaysPerAppliedMain",
			mode: notify.DoneNotificationPerFile,
			results: []processor.Result{
				{Applied: true},
				{Applied: true},
				{Applied: true},
				{Applied: false, Reason: processor.ErrNotMedia.Error()},
			},
			want: 3,
		},
		{
			name: "PerJob_PlaysOnceWhenAnyApplied",
			mode: notify.DoneNotificationPerJob,
			results: []processor.Result{
				{Applied: true},
				{Applied: true},
				{Applied: true},
			},
			want: 1,
		},
		{
			name: "Off_PlaysNone",
			mode: notify.DoneNotificationOff,
			results: []processor.Result{
				{Applied: true},
				{Applied: true},
			},
			want: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			soundCalls := make(chan struct{}, 16)

			d := &Daemon{
				Proc: &stubProcessor{
					results: tc.results,
				},
				SoundDone:                    "/tmp/done.aiff",
				DoneNotificationMode:         tc.mode,
				playSoundFn:                  func(context.Context, string) error { soundCalls <- struct{}{}; return nil },
				CleanupCooldown:              time.Millisecond,
				AutoCleanupCompletedTorrents: false,
			}

			d.processPath(context.Background(), shutdown.ResolvePolicy(0, 0), shutdown.Hooks{}, "/tmp/input.mkv", "/tmp/input.mkv")

			waitForSoundCount(t, soundCalls, tc.want, 2*time.Second)
			assertNoExtraSoundCalls(t, soundCalls, 150*time.Millisecond)
		})
	}
}

// TestDaemon_RunWorkerCancelDuringQueue_DrainsOnlyCurrentItem guards against a
// regression where runWorker's unbiased select between <-runCtx.Done() and
// <-queue could, after cancellation, keep dequeuing additional already-queued
// items (each independently re-entering jobrunner.Run's own grace/force drain
// and re-firing shutdown hooks) instead of stopping after the item that was
// in flight when shutdown began.
func TestDaemon_RunWorkerCancelDuringQueue_DrainsOnlyCurrentItem(t *testing.T) {
	block := make(chan struct{})
	proc := &stubProcessor{
		started: make(chan string, 3),
		block:   block,
	}

	d := &Daemon{Proc: proc}
	d.inFlightMu.Lock()
	d.inFlight = make(map[string]struct{})
	d.inFlightMu.Unlock()

	queue := make(chan workItem, 3)
	queue <- workItem{path: "/tmp/a.mkv", inFlightKey: "/tmp/a.mkv"}
	queue <- workItem{path: "/tmp/b.mkv", inFlightKey: "/tmp/b.mkv"}
	queue <- workItem{path: "/tmp/c.mkv", inFlightKey: "/tmp/c.mkv"}

	ctx, cancel := context.WithCancel(context.Background())

	var waitStarts int32
	hooks := shutdown.Hooks{
		OnWaitStart: func(time.Duration) { atomic.AddInt32(&waitStarts, 1) },
	}
	policy := shutdown.Policy{Grace: 2 * time.Second, Force: 2 * time.Second}

	outcome := make(chan workerOutcome, 1)
	go d.runWorker(ctx, policy, hooks, queue, outcome)

	first := waitForPath(t, proc.started, 3*time.Second)
	if first != "/tmp/a.mkv" {
		t.Fatalf("first item processed = %q, want /tmp/a.mkv", first)
	}

	// Shutdown fires while items b and c are still sitting in queue.
	cancel()
	close(block)

	select {
	case <-outcome:
	case <-time.After(3 * time.Second):
		t.Fatalf("runWorker did not stop after cancellation")
	}

	proc.mu.Lock()
	calls := append([]string(nil), proc.calls...)
	proc.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("Process called %d times (%v), want 1 -- items b/c must not be dequeued after shutdown", len(calls), calls)
	}
	if got := atomic.LoadInt32(&waitStarts); got != 1 {
		t.Fatalf("OnWaitStart called %d times, want 1 -- shutdown must bound to one grace+force window regardless of queue depth", got)
	}
}

type stubProcessor struct {
	mu      sync.Mutex
	calls   []string
	started chan string
	block   <-chan struct{}
	blocked chan struct{}
	// When true, Process blocks until ctx is canceled.
	blockUntilCtx bool
	results       []processor.Result
	err           error
}

type fakeDaemonCaffeinate struct {
	mu          sync.Mutex
	startCtx    context.Context
	stopCalls   int
	startCalled chan struct{}
}

func (f *fakeDaemonCaffeinate) Start(ctx context.Context) error {
	f.mu.Lock()
	f.startCtx = ctx
	startCalled := f.startCalled
	f.mu.Unlock()
	close(startCalled)
	return nil
}

func (f *fakeDaemonCaffeinate) Stop() error {
	f.mu.Lock()
	f.stopCalls++
	f.mu.Unlock()
	return nil
}

func (f *fakeDaemonCaffeinate) startContextCanceled() bool {
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

func (f *fakeDaemonCaffeinate) stopCallsCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stopCalls
}

func (s *stubProcessor) Plan(context.Context, processor.Request) ([]processor.Plan, error) {
	return nil, nil
}

func (s *stubProcessor) Apply(context.Context, []processor.Plan) ([]processor.Result, error) {
	return nil, nil
}

func (s *stubProcessor) Process(ctx context.Context, req processor.Request) error {
	s.mu.Lock()
	s.calls = append(s.calls, req.InputPath)
	s.mu.Unlock()

	if s.started != nil {
		select {
		case s.started <- req.InputPath:
		default:
		}
	}

	if s.block != nil {
		if s.blocked != nil {
			select {
			case s.blocked <- struct{}{}:
			default:
			}
		}
		<-s.block
	}
	if s.blockUntilCtx {
		if s.blocked != nil {
			select {
			case s.blocked <- struct{}{}:
			default:
			}
		}
		<-ctx.Done()
		return ctx.Err()
	}

	if s.err != nil {
		return s.err
	}
	results := s.results
	if results == nil {
		results = []processor.Result{{Applied: true}}
	}
	if req.OnResult != nil {
		for _, r := range results {
			req.OnResult(r)
		}
	}
	return nil
}

func (s *stubProcessor) SortCandidates(_ context.Context, paths []string) ([]string, []processor.SortError, error) {
	return paths, nil, nil
}

func waitForPath(t *testing.T, ch <-chan string, timeout time.Duration) string {
	t.Helper()

	select {
	case p := <-ch:
		return p
	case <-time.After(timeout):
		t.Fatalf("timeout waiting for process call")
		return ""
	}
}

func waitForSoundCount(t *testing.T, ch <-chan struct{}, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	got := 0
	for got < want {
		select {
		case <-ch:
			got++
		case <-deadline:
			t.Fatalf("timeout waiting for sound calls: got=%d want=%d", got, want)
		}
	}
}

func assertNoExtraSoundCalls(t *testing.T, ch <-chan struct{}, wait time.Duration) {
	t.Helper()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ch:
		t.Fatalf("unexpected extra sound call")
	case <-timer.C:
	}
}

func newRuntimeLoggerForTest(t *testing.T, stdout, stderr io.Writer) logging.Logger {
	t.Helper()
	l, err := logging.New(logging.Options{
		Stdout:               stdout,
		Stderr:               stderr,
		ConsoleLevel:         "INFO",
		HistoryLevel:         "WARN",
		HistoryFile:          filepath.Join(t.TempDir(), "history.jsonl"),
		HistoryInfoAllowlist: logging.DefaultHistoryInfoAllowlist(),
	})
	if err != nil {
		t.Fatalf("logging.New() error: %v", err)
	}
	return l
}

func expectNoPath(t *testing.T, ch <-chan string, timeout time.Duration) {
	t.Helper()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case p := <-ch:
		t.Fatalf("unexpected process call: %q", p)
	case <-timer.C:
	}
}

func waitForSignal(t *testing.T, ch <-chan struct{}, timeout time.Duration) {
	t.Helper()

	select {
	case <-ch:
		return
	case <-time.After(timeout):
		t.Fatalf("timeout waiting for signal")
	}
}

func mkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
}

func writeFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func TestDaemon_InFlightDedupe(t *testing.T) {
	d := &Daemon{}
	path := "/tmp/example"

	if !d.tryMarkInFlight(path) {
		t.Fatalf("expected first mark to succeed")
	}
	if !d.isInFlight(path) {
		t.Fatalf("expected path to be in-flight")
	}
	if d.tryMarkInFlight(path) {
		t.Fatalf("expected duplicate mark to fail")
	}

	d.clearInFlight(path)

	if d.isInFlight(path) {
		t.Fatalf("expected path to be cleared from in-flight")
	}
	if !d.tryMarkInFlight(path) {
		t.Fatalf("expected mark after clear to succeed")
	}
}

func TestDaemon_InFlightKeyCanonicalization(t *testing.T) {
	d := &Daemon{}
	base := t.TempDir()

	realDir := filepath.Join(base, "RealCaps")
	subDir := filepath.Join(realDir, "SubCaps")
	mkdirAll(t, subDir)

	linkDir := filepath.Join(base, "LinkCaps")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	targetPath := filepath.Join(linkDir, "SubCaps")
	key := d.inFlightKey(targetPath)

	eval, err := filepath.EvalSymlinks(targetPath)
	if err != nil {
		t.Fatalf("EvalSymlinks failed: %v", err)
	}

	expected := filepath.Clean(eval)
	if isCaseInsensitiveFS() {
		expected = strings.ToLower(expected)
	}

	if key != expected {
		t.Fatalf("unexpected in-flight key: got %q want %q", key, expected)
	}
}
