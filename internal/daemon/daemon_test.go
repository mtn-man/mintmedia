package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Mtn-Man/mintmedia/internal/processor"
	"github.com/Mtn-Man/mintmedia/internal/watch"
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

		MaxConcurrent: 1,

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

		MaxConcurrent: 1,

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

		MaxConcurrent: 1,

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

type stubProcessor struct {
	mu      sync.Mutex
	calls   []string
	started chan string
	block   <-chan struct{}
	blocked chan struct{}
}

func (s *stubProcessor) Plan(context.Context, processor.Request) ([]processor.Plan, error) {
	return nil, nil
}

func (s *stubProcessor) Apply(context.Context, []processor.Plan) ([]processor.Result, error) {
	return nil, nil
}

func (s *stubProcessor) Process(ctx context.Context, req processor.Request) ([]processor.Result, error) {
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

	return []processor.Result{{
		Applied: true,
	}}, nil
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
