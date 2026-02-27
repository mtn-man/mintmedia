package daemon

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Mtn-Man/mintmedia/internal/notify"
	"github.com/Mtn-Man/mintmedia/internal/processor"
	"github.com/Mtn-Man/mintmedia/internal/transmission"
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

		MaxConcurrent: 1,

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

		MaxConcurrent: 1,

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

		MaxConcurrent: 1,

		SoundInput: "",
		SoundDone:  "",

		ShutdownGraceDuration: 10 * time.Minute,
		ShutdownForceTimeout:  15 * time.Second,

		AutoCleanupCompletedTorrents: false,
		DeferDestinationChecks:       false,
	}

	stderr := captureStderr(t, func() {
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
	})

	if strings.Contains(stderr, "Shutdown requested; waiting up to") {
		t.Fatalf("unexpected waiting log with no in-flight jobs: %q", stderr)
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

		MaxConcurrent: 1,

		SoundInput: "",
		SoundDone:  "",

		ShutdownGraceDuration: 50 * time.Millisecond,
		ShutdownForceTimeout:  250 * time.Millisecond,

		AutoCleanupCompletedTorrents: false,
		DeferDestinationChecks:       false,
	}

	stderr := captureStderr(t, func() {
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
	})

	if !strings.Contains(stderr, "\nShutdown requested; waiting up to 50ms for in-flight jobs\n") {
		t.Fatalf("expected newline-prefixed waiting log, got: %q", stderr)
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

func TestDaemon_ProcessPathAsync_CleansCompletedWhenEnabled(t *testing.T) {
	root := t.TempDir()
	removedFile := filepath.Join(root, "removed.log")
	script := writeTxCleanupScript(t, root, removedFile)

	d := &Daemon{
		Proc: &stubProcessor{},
		Tx: &transmission.Client{
			RemotePath: script,
			Host:       "localhost:9091",
		},
		AutoCleanupCompletedTorrents: true,
		CleanupCooldown:              time.Millisecond,
		SoundDone:                    "",
	}

	sem := make(chan struct{}, 1)
	d.jobsWg.Add(1)
	d.processPathAsync(context.Background(), context.Background(), sem, "/tmp/input.mkv", "/tmp/input.mkv")

	b, err := os.ReadFile(removedFile)
	if err != nil {
		t.Fatalf("read removed file: %v", err)
	}
	ids := strings.Fields(string(b))
	if len(ids) != 1 || ids[0] != "7" {
		t.Fatalf("unexpected removed ids: %v", ids)
	}
}

func TestFormatDurationCompact(t *testing.T) {
	tests := []struct {
		name string
		in   time.Duration
		want string
	}{
		{name: "TenMinutes", in: 10 * time.Minute, want: "10m"},
		{name: "FifteenSeconds", in: 15 * time.Second, want: "15s"},
		{name: "OneHour", in: 1 * time.Hour, want: "1h"},
		{name: "HourAndMinutes", in: 1*time.Hour + 30*time.Minute, want: "1h30m"},
		{name: "HourMinuteSecond", in: 1*time.Hour + 30*time.Minute + 15*time.Second, want: "1h30m15s"},
		{name: "SubSecondFallback", in: 500 * time.Millisecond, want: "500ms"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatDurationCompact(tc.in)
			if got != tc.want {
				t.Fatalf("formatDurationCompact(%s) = %q, want %q", tc.in, got, tc.want)
			}
		})
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

			sem := make(chan struct{}, 1)
			d.jobsWg.Add(1)
			d.processPathAsync(context.Background(), context.Background(), sem, "/tmp/input.mkv", "/tmp/input.mkv")

			waitForSoundCount(t, soundCalls, tc.want, 2*time.Second)
			assertNoExtraSoundCalls(t, soundCalls, 150*time.Millisecond)
		})
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
	if s.blockUntilCtx {
		if s.blocked != nil {
			select {
			case s.blocked <- struct{}{}:
			default:
			}
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}

	if s.err != nil {
		return nil, s.err
	}
	if s.results != nil {
		out := make([]processor.Result, len(s.results))
		copy(out, s.results)
		return out, nil
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

func writeTxCleanupScript(t *testing.T, dir, removedFile string) string {
	t.Helper()

	scriptPath := filepath.Join(dir, "tx-cleanup.sh")
	script := `#!/usr/bin/env bash
set -euo pipefail

for ((i=1; i<=$#; i++)); do
  if [[ "${!i}" == "-l" ]]; then
cat <<'EOF'
ID     Done       Have  ETA           Up    Down  Ratio  Status       Name
   7   100%    1.00 GB  Done         0.0     0.0   0.0   Idle         done-one
Sum:            1.00 GB               0.0     0.0
EOF
    exit 0
  fi
done

for ((i=1; i<=$#; i++)); do
  if [[ "${!i}" == "-t" ]]; then
    j=$((i+1))
    printf "%s\n" "${!j}" >> "` + removedFile + `"
    exit 0
  fi
done

echo "unexpected invocation: $*" >&2
exit 9
`

	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write tx cleanup script: %v", err)
	}
	return scriptPath
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	fn()

	_ = w.Close()
	os.Stderr = old
	out := <-done
	_ = r.Close()
	return out
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
