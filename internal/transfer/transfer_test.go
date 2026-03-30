package transfer

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCopyThenReplace_WritesFileAndEmitsDone(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src.mkv")
	dst := filepath.Join(root, "dst.mkv")
	writeFile(t, src, strings.Repeat("a", 128))

	reporter := &stubReporter{}

	xfer := NewRenameOrCopy(Options{
		Reporter: reporter,
	})

	if err := xfer.copyThenReplace(context.Background(), src, dst); err != nil {
		t.Fatalf("copyThenReplace error: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != strings.Repeat("a", 128) {
		t.Fatalf("dest contents mismatch")
	}

	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("expected source to be removed, stat err=%v", err)
	}

	if !reporter.DoneCalled() {
		t.Fatalf("expected reporter.Done to be called")
	}
	if reporter.DoneCount() != 1 {
		t.Fatalf("done count = %d, want 1", reporter.DoneCount())
	}
}

func TestCopyThenReplace_EmitsPeriodicUpdate(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src.mkv")
	dst := filepath.Join(root, "dst.mkv")
	writeFile(t, src, strings.Repeat("p", 64*1024))

	reporter := &stubReporter{updateNotify: make(chan struct{}, 1)}
	firstChunkWritten := make(chan struct{}, 1)
	allowFirstChunk := make(chan struct{}, 1)
	allowRemainder := make(chan struct{}, 1)
	fakeTick := newFakeTicker()

	xfer := NewRenameOrCopy(Options{
		Reporter:    reporter,
		UpdateEvery: 50 * time.Millisecond,
	})
	xfer.newTicker = func(time.Duration) ticker { return fakeTick }
	xfer.copyFn = func(w io.Writer, r io.Reader) (int64, error) {
		<-allowFirstChunk

		buf := make([]byte, 1024)
		n, err := r.Read(buf)
		if n > 0 {
			written, writeErr := w.Write(buf[:n])
			if writeErr != nil {
				return int64(written), writeErr
			}
			if written != n {
				return int64(written), io.ErrShortWrite
			}
		}
		firstChunkWritten <- struct{}{}

		if err != nil && err != io.EOF {
			return int64(n), err
		}
		if err == io.EOF {
			return int64(n), nil
		}

		<-allowRemainder
		rest, restErr := io.Copy(w, r)
		return int64(n) + rest, restErr
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- xfer.copyThenReplace(context.Background(), src, dst)
	}()

	allowFirstChunk <- struct{}{}
	waitForSignal(t, firstChunkWritten, "first chunk to be written")

	fakeTick.tick(time.Now())
	waitForSignal(t, reporter.updateNotify, "reporter update callback")

	allowRemainder <- struct{}{}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("copyThenReplace error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for copyThenReplace to return")
	}

	if reporter.UpdateCount() == 0 {
		t.Fatalf("expected at least one reporter.Update callback")
	}
	if !reporter.DoneCalled() {
		t.Fatalf("expected reporter.Done callback")
	}
	if reporter.LateUpdateCount() != 0 {
		t.Fatalf("unexpected reporter.Update calls after Done: %d", reporter.LateUpdateCount())
	}
}

func TestCopyThenReplace_ContextCanceled(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src.mkv")
	dst := filepath.Join(root, "dst.mkv")
	writeFile(t, src, "data")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	xfer := NewRenameOrCopy(Options{})
	if err := xfer.copyThenReplace(ctx, src, dst); err == nil {
		t.Fatalf("expected error, got nil")
	}

	if _, err := os.Stat(src); err != nil {
		t.Fatalf("expected source to remain, stat err=%v", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("expected dest to not exist, stat err=%v", err)
	}
}

func TestMove_RenameFastPath(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src.mkv")
	dst := filepath.Join(root, "dst.mkv")
	writeFile(t, src, strings.Repeat("c", 32))

	xfer := NewRenameOrCopy(Options{})
	if err := xfer.Move(context.Background(), src, dst); err != nil {
		t.Fatalf("Move error: %v", err)
	}

	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("expected source to be removed, stat err=%v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != strings.Repeat("c", 32) {
		t.Fatalf("dest contents mismatch")
	}
}

func TestCopyThenReplace_NilReporter_NoCallbacks(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src.mkv")
	dst := filepath.Join(root, "dst.mkv")
	writeFile(t, src, strings.Repeat("z", 128))

	xfer := NewRenameOrCopy(Options{})
	if err := xfer.copyThenReplace(context.Background(), src, dst); err != nil {
		t.Fatalf("copyThenReplace error: %v", err)
	}

	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("expected source to be removed, stat err=%v", err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("expected destination to exist, stat err=%v", err)
	}
}

func TestMove_DestinationExists(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src.mkv")
	dst := filepath.Join(root, "dst.mkv")
	writeFile(t, src, "data")
	writeFile(t, dst, "existing")

	xfer := NewRenameOrCopy(Options{})
	err := xfer.Move(context.Background(), src, dst)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "destination already exists") {
		t.Fatalf("expected destination exists error, got: %v", err)
	}

	if _, err := os.Stat(src); err != nil {
		t.Fatalf("expected source to remain, stat err=%v", err)
	}
}

func TestSameDevice_TempDir(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src.mkv")
	writeFile(t, src, "data")

	same, err := sameDevice(src, root)
	if err != nil {
		t.Fatalf("sameDevice error: %v", err)
	}
	if !same {
		t.Fatalf("expected same device for temp dir paths")
	}
}

func TestSameDevice_StatError(t *testing.T) {
	root := t.TempDir()
	_, err := sameDevice(filepath.Join(root, "missing.mkv"), root)
	if err == nil {
		t.Fatalf("expected error for missing source path")
	}
}

func TestMove_SameDeviceRenameFailure_NoFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod semantics differ on windows")
	}

	root := t.TempDir()
	src := filepath.Join(root, "src.mkv")
	writeFile(t, src, strings.Repeat("d", 16))

	dstDir := filepath.Join(root, "dstdir")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		t.Fatalf("mkdir dest dir: %v", err)
	}
	if err := os.Chmod(dstDir, 0o555); err != nil {
		t.Fatalf("chmod dest dir: %v", err)
	}
	defer func() {
		_ = os.Chmod(dstDir, 0o755)
	}()

	dst := filepath.Join(dstDir, "dst.mkv")
	xfer := NewRenameOrCopy(Options{})
	if err := xfer.Move(context.Background(), src, dst); err == nil {
		t.Fatalf("expected rename error, got nil")
	}

	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("expected destination to not exist, stat err=%v", err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("expected source to remain, stat err=%v", err)
	}
}

type stubReporter struct {
	mu              sync.Mutex
	doneCount       int
	updateCount     int
	lateUpdateCount int
	updateNotify    chan struct{}
}

func (s *stubReporter) Update(Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.doneCount > 0 {
		s.lateUpdateCount++
	}
	s.updateCount++
	if s.updateNotify != nil {
		select {
		case s.updateNotify <- struct{}{}:
		default:
		}
	}
}

func (s *stubReporter) Done() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.doneCount++
}

func (s *stubReporter) DoneCalled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.doneCount > 0
}

func (s *stubReporter) DoneCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.doneCount
}

func (s *stubReporter) UpdateCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.updateCount
}

func (s *stubReporter) LateUpdateCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lateUpdateCount
}

func writeFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

type fakeTicker struct {
	ch chan time.Time
}

func newFakeTicker() *fakeTicker {
	return &fakeTicker{ch: make(chan time.Time, 16)}
}

func (ft *fakeTicker) C() <-chan time.Time {
	return ft.ch
}

func (ft *fakeTicker) Stop() {}

func (ft *fakeTicker) tick(now time.Time) {
	ft.ch <- now
}

func waitForSignal(t *testing.T, ch <-chan struct{}, reason string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", reason)
	}
}
